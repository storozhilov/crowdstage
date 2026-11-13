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

type mediaMixer struct {
	config config
	room   *lksdk.Room

	mu                     sync.Mutex
	running                bool
	pipeline               *mediaMixerPipeline
	selected               *studioVideo
	selectedSourceIdentity string

	opsMu            sync.Mutex
	videos           studioVideoTracker
	nextSource       int
	sourceIdentities map[string]struct{}
}

func connectMediaMixer(config config) (*mediaMixer, error) {
	mixer := &mediaMixer{
		config:           config,
		nextSource:       1,
		sourceIdentities: make(map[string]struct{}),
	}

	callback := lksdk.NewRoomCallback()
	callback.OnParticipantConnected = func(participant *lksdk.RemoteParticipant) {
		if mixer.isSourceIdentity(participant.Identity()) {
			return
		}
		log.Printf("studio participant connected: participant=%q identity=%q", participant.SID(), participant.Identity())
	}
	callback.OnTrackPublished = func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
		if mixer.isSourceIdentity(participant.Identity()) || publication.Kind() != lksdk.TrackKindVideo {
			return
		}
		log.Printf(
			"studio video published: participant=%q identity=%q track=%q",
			participant.SID(),
			participant.Identity(),
			publication.SID(),
		)
		mixer.addStudioVideo(participant, publication)
	}
	callback.OnTrackUnpublished = func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
		if mixer.isSourceIdentity(participant.Identity()) || publication.Kind() != lksdk.TrackKindVideo {
			return
		}
		log.Printf(
			"studio video unpublished: participant=%q identity=%q track=%q",
			participant.SID(),
			participant.Identity(),
			publication.SID(),
		)
		mixer.videos.removePublication(publication.SID())
		mixer.syncForwarding()
	}
	callback.OnParticipantDisconnected = func(participant *lksdk.RemoteParticipant) {
		if mixer.isSourceIdentity(participant.Identity()) {
			mixer.forgetSourceIdentity(participant.Identity())
			return
		}
		log.Printf("studio participant disconnected: participant=%q identity=%q", participant.SID(), participant.Identity())
		mixer.videos.removeParticipant(participant.Identity())
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
	})
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
	hasPipeline := m.pipeline != nil
	m.mu.Unlock()

	if !running {
		return nil
	}

	next := m.videos.oldest()
	if selected != nil && next != nil && selected.participantIdentity == next.participantIdentity {
		m.mu.Lock()
		m.selected = next
		m.mu.Unlock()
		return nil
	}
	if selected == nil && next == nil && hasPipeline {
		return nil
	}
	return m.replacePipelineLocked(next)
}

func (m *mediaMixer) replacePipelineLocked(next *studioVideo) error {
	if err := m.stopPipelineLocked(); err != nil {
		return err
	}

	sourceName := ""
	sourceIdentity := ""
	if next != nil {
		sourceName, sourceIdentity = m.newStudioSource()
	}

	pipeline, err := startMediaMixerPipeline(m.config, next, sourceName)
	if err != nil {
		if sourceIdentity != "" {
			m.forgetSourceIdentity(sourceIdentity)
		}
		return err
	}

	m.mu.Lock()
	m.pipeline = pipeline
	m.selected = next
	m.selectedSourceIdentity = sourceIdentity
	m.mu.Unlock()
	return nil
}

func (m *mediaMixer) stopPipelineLocked() error {
	m.mu.Lock()
	pipeline := m.pipeline
	sourceIdentity := m.selectedSourceIdentity
	m.pipeline = nil
	m.selected = nil
	m.selectedSourceIdentity = ""
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
	if m.running {
		m.mu.Unlock()
		log.Printf("media mixer is already running")
		return nil
	}
	m.running = true
	m.mu.Unlock()

	if err := m.reconcileLocked(); err != nil {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
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
	m.mu.Unlock()

	mixer := MixerHealth{
		State:   mixerStateIdle,
		Inputs:  m.videos.count(),
		Outputs: mixerOutputCount,
	}
	status := sharedhttp.StatusOK
	if running {
		mixer.State = mixerStateRunning
		if pipeline != nil && pipeline.failure() != nil {
			status = sharedhttp.StatusDegraded
			mixer.State = mixerStateFailed
		}
	}

	return HealthResponse{
		Status:     status,
		Subsystems: Subsystems{Mixer: mixer},
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
	mux.Handle("POST /start", sharedhttp.StartHandler(mixer.start))
	mux.Handle("POST /stop", sharedhttp.StopHandler(mixer.stop))

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
