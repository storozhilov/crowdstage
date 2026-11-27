package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"
)

type mediaMixerPipelineAPI interface {
	setSource(source *studioVideo, sourceName string) error
	clearSource()
	syncAudioSources(sources []audioSource) error
	stop() error
	failure() error
}

type mediaMixer struct {
	config        config
	room          *lksdk.Room
	guestClient   *lksdk.RoomServiceClient
	guestLookup   func(participantIdentity string) (*studioVideo, error)
	mosaicLookup  func(participantIdentity string) (*studioVideo, error)
	startPipeline func(config config) (mediaMixerPipelineAPI, error)

	mu                     sync.Mutex
	running                bool
	pipeline               mediaMixerPipelineAPI
	selected               *studioVideo // desired studio source, preserved across stops
	broadcasted            *studioVideo // source attached to the running pipeline
	selectedSourceIdentity string
	guestBroadcast         *studioVideo // temporary guest source overriding the studio source
	guestBroadcastSeconds  int
	guestBroadcastTimer    *time.Timer
	guestBroadcastTimerID  uint64

	opsMu            sync.Mutex
	videos           studioVideoTracker
	audios           audioSourceTracker
	nextSource       int
	sourceIdentities map[string]struct{}
}

func connectMediaMixer(config config) (*mediaMixer, error) {
	mixer := &mediaMixer{
		config:                config,
		nextSource:            1,
		sourceIdentities:      make(map[string]struct{}),
		guestBroadcastSeconds: defaultGuestBroadcastSeconds,
	}
	mixer.guestClient = lksdk.NewRoomServiceClient(roomServiceURL(config.LiveKitURL), config.APIKey, config.APISecret)

	callback := lksdk.NewRoomCallback()
	callback.OnParticipantConnected = func(participant *lksdk.RemoteParticipant) {
		if mixer.isSourceIdentity(participant.Identity()) {
			return
		}
		log.Printf("studio participant connected: participant=%q identity=%q", participant.SID(), participant.Identity())
	}
	callback.OnTrackPublished = func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
		if mixer.isSourceIdentity(participant.Identity()) {
			return
		}
		if publication.Kind() == lksdk.TrackKindVideo {
			log.Printf(
				"studio video published: participant=%q identity=%q track=%q",
				participant.SID(),
				participant.Identity(),
				publication.SID(),
			)
			mixer.addStudioVideo(participant, publication)
		}
		mixer.refreshStudioAudio(participant)
	}
	callback.OnTrackUnpublished = func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
		if mixer.isSourceIdentity(participant.Identity()) {
			return
		}
		if publication.Kind() == lksdk.TrackKindVideo {
			log.Printf(
				"studio video unpublished: participant=%q identity=%q track=%q",
				participant.SID(),
				participant.Identity(),
				publication.SID(),
			)
			mixer.videos.removePublication(publication.SID())
			mixer.syncForwarding()
		}
		mixer.refreshStudioAudio(participant)
	}
	callback.OnParticipantDisconnected = func(participant *lksdk.RemoteParticipant) {
		if mixer.isSourceIdentity(participant.Identity()) {
			mixer.forgetSourceIdentity(participant.Identity())
			return
		}
		log.Printf("studio participant disconnected: participant=%q identity=%q", participant.SID(), participant.Identity())
		mixer.videos.removeParticipant(participant.Identity())
		mixer.audios.removeParticipant(participant.Identity())
		mixer.syncForwarding()
	}

	room, err := lksdk.ConnectToRoom(config.LiveKitURL, lksdk.ConnectInfo{
		APIKey:              config.APIKey,
		APISecret:           config.APISecret,
		RoomName:            config.InputRoom,
		ParticipantIdentity: config.Identity,
	}, callback, lksdk.WithAutoSubscribe(false), lksdk.WithConnectTimeout(30*time.Second))
	if err != nil {
		return nil, fmt.Errorf("connect to studio room %q: %w", config.InputRoom, err)
	}
	mixer.room = room

	for _, participant := range room.GetRemoteParticipants() {
		if mixer.isSourceIdentity(participant.Identity()) {
			continue
		}
		for _, publication := range participant.TrackPublications() {
			if publication.Kind() != lksdk.TrackKindVideo {
				continue
			}
			remotePublication, ok := publication.(*lksdk.RemoteTrackPublication)
			if !ok {
				continue
			}
			mixer.addStudioVideo(participant, remotePublication)
		}
		mixer.refreshStudioAudio(participant)
	}

	return mixer, nil
}

func (m *mediaMixer) addStudioVideo(participant *lksdk.RemoteParticipant, publication *lksdk.RemoteTrackPublication) {
	m.videos.add(studioVideo{
		participantIdentity: participant.Identity(),
		participantName:     participant.Name(),
		participantSID:      participant.SID(),
		publicationSID:      publication.SID(),
		publishedAt:         time.Now(),
		room:                m.config.InputRoom,
	})
	m.syncForwarding()
}

func (m *mediaMixer) refreshStudioAudio(participant *lksdk.RemoteParticipant) {
	identity := participant.Identity()
	publishesVideo := false
	audioPublications := make([]*lksdk.RemoteTrackPublication, 0, 1)
	for _, publication := range participant.TrackPublications() {
		switch publication.Kind() {
		case lksdk.TrackKindVideo:
			publishesVideo = true
		case lksdk.TrackKindAudio:
			remotePublication, ok := publication.(*lksdk.RemoteTrackPublication)
			if ok {
				audioPublications = append(audioPublications, remotePublication)
			}
		}
	}

	current := m.audios.findByIdentity(identity)
	if publishesVideo || len(audioPublications) == 0 {
		if current != nil {
			log.Printf("studio audio removed: identity=%q", identity)
			m.audios.removeParticipant(identity)
			m.syncForwarding()
		}
		return
	}

	if current != nil {
		for _, publication := range audioPublications {
			if current.publicationSID == publication.SID() {
				return
			}
		}
	}

	publication := audioPublications[0]
	source := audioSource{
		participantIdentity: identity,
		participantName:     participant.Name(),
		participantSID:      participant.SID(),
		publicationSID:      publication.SID(),
		publishedAt:         time.Now(),
		room:                m.config.InputRoom,
	}
	log.Printf(
		"studio audio published: participant=%q identity=%q track=%q",
		participant.SID(),
		identity,
		publication.SID(),
	)
	if !m.audios.replace(identity, source) {
		m.audios.add(source)
	}
	m.syncForwarding()
}

func (m *mediaMixer) syncForwarding() {
	if err := m.reconcile(); err != nil {
		log.Printf("update media mixer forwarding: %v", err)
	}
}

func (m *mediaMixer) reconcile() error {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()
	return m.reconcileLocked()
}

func (m *mediaMixer) reconcileLocked() error {
	m.mu.Lock()
	running := m.running
	selected := m.selected
	guestBroadcast := m.guestBroadcast
	broadcasted := m.broadcasted
	pipeline := m.pipeline
	m.mu.Unlock()

	if !running {
		return nil
	}
	if pipeline == nil {
		var err error
		if pipeline, err = m.ensurePipelineLocked(); err != nil {
			return err
		}
	}

	if err := pipeline.syncAudioSources(m.audios.list()); err != nil {
		log.Printf("update media mixer audio sources: %v", err)
	}

	next := m.nextVideo(selected)
	updateSelection := true
	if guestBroadcast != nil {
		next = guestBroadcast
		updateSelection = false
	}
	if sameBroadcastSource(broadcasted, next) {
		if updateSelection {
			m.mu.Lock()
			m.selected = next
			m.mu.Unlock()
		}
		return nil
	}
	return m.applySourceLocked(next, updateSelection)
}

func sameBroadcastSource(left, right *studioVideo) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return sameStudioVideo(left, right)
}

func (m *mediaMixer) nextVideo(selected *studioVideo) *studioVideo {
	if selected != nil {
		if current := m.videos.findByIdentity(selected.participantIdentity); current != nil {
			return current
		}
	}
	return m.videos.oldest()
}

func (m *mediaMixer) ensurePipelineLocked() (mediaMixerPipelineAPI, error) {
	m.mu.Lock()
	pipeline := m.pipeline
	m.mu.Unlock()
	if pipeline != nil {
		return pipeline, nil
	}

	var err error
	if m.startPipeline != nil {
		pipeline, err = m.startPipeline(m.config)
	} else {
		pipeline, err = startMediaMixerPipeline(m.config)
	}
	if err != nil {
		return nil, err
	}

	if levelPipeline, ok := pipeline.(*mediaMixerPipeline); ok {
		levelPipeline.setAudioLevelListener(m.setAudioSignalLevel)
	}

	m.mu.Lock()
	m.pipeline = pipeline
	m.mu.Unlock()
	return pipeline, nil
}

func (m *mediaMixer) applySourceLocked(next *studioVideo, updateSelection bool) error {
	m.mu.Lock()
	pipeline := m.pipeline
	previousIdentity := m.selectedSourceIdentity
	if updateSelection {
		m.selected = next
	}
	m.mu.Unlock()

	if pipeline == nil {
		return fmt.Errorf("media mixer pipeline is not running")
	}

	if next == nil {
		pipeline.clearSource()
		m.forgetSourceIdentity(previousIdentity)
		m.mu.Lock()
		m.broadcasted = nil
		m.selectedSourceIdentity = ""
		m.mu.Unlock()
		return nil
	}

	sourceName, sourceIdentity := m.newStudioSource()
	if err := pipeline.setSource(next, sourceName); err != nil {
		m.forgetSourceIdentity(sourceIdentity)
		return err
	}

	m.forgetSourceIdentity(previousIdentity)
	m.mu.Lock()
	m.broadcasted = next
	m.selectedSourceIdentity = sourceIdentity
	m.mu.Unlock()
	return nil
}

func (m *mediaMixer) stopPipelineLocked() error {
	m.mu.Lock()
	pipeline := m.pipeline
	sourceIdentity := m.selectedSourceIdentity
	m.pipeline = nil
	m.broadcasted = nil
	m.selectedSourceIdentity = ""
	m.clearGuestBroadcastLocked()
	m.mu.Unlock()

	if pipeline == nil {
		return nil
	}
	err := pipeline.stop()
	m.forgetSourceIdentity(sourceIdentity)
	return err
}

func (m *mediaMixer) newStudioSource() (name, identity string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sourceIdentities == nil {
		m.sourceIdentities = make(map[string]struct{})
	}
	name = fmt.Sprintf("studio-src-%d", m.nextSource)
	m.nextSource++
	identity = m.config.Identity + "-" + name
	m.sourceIdentities[identity] = struct{}{}
	return name, identity
}

func (m *mediaMixer) forgetSourceIdentity(identity string) {
	m.mu.Lock()
	delete(m.sourceIdentities, identity)
	m.mu.Unlock()
}

func (m *mediaMixer) isSourceIdentity(identity string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.sourceIdentities[identity]
	return ok
}

func (m *mediaMixer) start() error {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	m.mu.Lock()
	alreadyRunning := m.running
	hasPipeline := m.pipeline != nil
	m.running = true
	m.mu.Unlock()

	if alreadyRunning && hasPipeline {
		log.Printf("media mixer is already running")
		return nil
	}

	if err := m.reconcileLocked(); err != nil {
		if !alreadyRunning {
			_ = m.stopPipelineLocked()
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
		}
		return err
	}
	return nil
}

func (m *mediaMixer) stop() error {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	m.mu.Lock()
	running := m.running
	m.running = false
	m.mu.Unlock()

	if !running {
		log.Printf("media mixer is not running")
		return nil
	}
	return m.stopPipelineLocked()
}

func (m *mediaMixer) close() {
	if err := m.stop(); err != nil {
		log.Printf("stop media mixer: %v", err)
	}
	if m.room != nil {
		m.room.Disconnect()
	}
}

func (m *mediaMixer) health() HealthResponse {
	m.mu.Lock()
	pipeline := m.pipeline
	running := m.running
	broadcasted := m.broadcasted
	guestBroadcastSeconds := m.guestBroadcastSeconds
	m.mu.Unlock()

	var pipelineErr error
	if pipeline != nil {
		pipelineErr = pipeline.failure()
	}

	broadcastedParticipantIdentity := ""
	if running && pipeline != nil && pipelineErr == nil && broadcasted != nil {
		broadcastedParticipantIdentity = broadcasted.participantIdentity
	}

	mixer := MixerHealth{
		State:                           mixerStateIdle,
		Inputs:                          m.videos.count(),
		Outputs:                         mixerOutputCount,
		BroadcastedParticipantIdentity:  broadcastedParticipantIdentity,
		PublishingParticipantIdentities: m.videos.identities(),
		GuestBroadcastSeconds:           guestBroadcastSeconds,
		AudioSources:                    audioSourcesHealth(m.audios.list()),
	}
	status := sharedhttp.StatusOK
	if running {
		mixer.State = mixerStateRunning
		if pipelineErr != nil {
			status = sharedhttp.StatusDegraded
			mixer.State = mixerStateFailed
		}
	}

	livekit := m.livekitHealth()
	if livekit.Status != sharedhttp.StatusOK {
		status = sharedhttp.StatusDegraded
	}

	return HealthResponse{
		Status:     status,
		Subsystems: Subsystems{LiveKit: livekit, Mixer: mixer},
	}
}

func (m *mediaMixer) livekitHealth() LiveKitHealth {
	connectionState := string(lksdk.ConnectionStateDisconnected)
	if m.room != nil {
		connectionState = string(m.room.ConnectionState())
	}

	connected := connectionState == string(lksdk.ConnectionStateConnected)
	status := sharedhttp.StatusOK
	if !connected {
		status = sharedhttp.StatusDegraded
	}

	return LiveKitHealth{
		Status:          status,
		Connected:       connected,
		URL:             m.config.LiveKitURL,
		Room:            m.config.InputRoom,
		TargetRoom:      m.config.OutputRoom,
		Identity:        m.config.Identity,
		ConnectionState: connectionState,
	}
}

func main() {
	config, err := configFromEnv()
	if err != nil {
		log.Printf("media mixer configuration error: %v", err)
		os.Exit(1)
	}

	mixer, err := connectMediaMixer(config)
	if err != nil {
		log.Printf("media mixer LiveKit connection error: %v", err)
		os.Exit(1)
	}
	defer mixer.close()

	mux := http.NewServeMux()
	mux.Handle("GET /health", sharedhttp.HealthHandler(mixer.health))
	mux.Handle("POST /start", startHandler(mixer))
	mux.Handle("POST /stop", sharedhttp.StopHandler(mixer.stop))
	mux.Handle("POST /switch", switchSourceHandler(mixer))
	mux.Handle("POST /audio/{identity}/mute", audioMuteHandler(mixer))
	mux.Handle("POST /audio/{identity}/solo", audioSoloHandler(mixer))
	mux.Handle("POST /audio/{identity}/gain", audioGainHandler(mixer))

	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("connected to LiveKit room %q at %s", config.InputRoom, config.LiveKitURL)
		log.Printf("media mixer listening on %s", config.ListenAddr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("media mixer server error: %v", err)
		}
	case <-ctx.Done():
		log.Printf("media mixer shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
