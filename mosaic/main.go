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
	sharedconfig "github.com/storozhilov/crowdstage/shared/config"
	sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"
)

const healthcheckURL = "http://127.0.0.1:7980/health"

type composer struct {
	config          config
	participantRoom *lksdk.Room

	pipelineMu sync.Mutex
	pipeline   *mosaicPipeline

	sourceMu         sync.Mutex
	nextSource       int
	sourceIdentities map[string]struct{}
}

func connectRoom(config config, roomName string, callback *lksdk.RoomCallback, options ...lksdk.ConnectOption) (*lksdk.Room, error) {
	options = append(options, lksdk.WithConnectTimeout(30*time.Second))
	return lksdk.ConnectToRoom(config.LiveKitURL, lksdk.ConnectInfo{
		APIKey:              config.APIKey,
		APISecret:           config.APISecret,
		RoomName:            roomName,
		ParticipantIdentity: config.Identity,
	}, callback, options...)
}

func connectComposer(config config) (*composer, error) {
	c := &composer{
		config:           config,
		nextSource:       1,
		sourceIdentities: make(map[string]struct{}),
	}
	callback := lksdk.NewRoomCallback()
	callback.OnParticipantConnected = func(participant *lksdk.RemoteParticipant) {
		if c.isParticipantSourceIdentity(participant.Identity()) {
			return
		}
		log.Printf("participant connected: participant=%q identity=%q", participant.SID(), participant.Identity())
	}
	callback.OnTrackPublished = func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
		if c.isParticipantSourceIdentity(participant.Identity()) || publication.Kind() != lksdk.TrackKindVideo {
			return
		}
		log.Printf("participant video published: participant=%q identity=%q track=%q", participant.SID(), participant.Identity(), publication.SID())
		c.addParticipantVideo(participant.Identity(), participant.Name(), participant.SID())
	}
	callback.OnTrackUnpublished = func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
		if c.isParticipantSourceIdentity(participant.Identity()) || publication.Kind() != lksdk.TrackKindVideo {
			return
		}
		log.Printf("participant video unpublished: participant=%q identity=%q track=%q", participant.SID(), participant.Identity(), publication.SID())
		c.removeParticipantVideo(participant.Identity())
	}
	callback.OnParticipantDisconnected = func(participant *lksdk.RemoteParticipant) {
		if c.isParticipantSourceIdentity(participant.Identity()) {
			c.forgetParticipantSource(participant.Identity())
			return
		}
		log.Printf("participant disconnected: participant=%q identity=%q", participant.SID(), participant.Identity())
		c.removeParticipantVideo(participant.Identity())
	}

	participantRoom, err := connectRoom(config, config.RoomName, callback, lksdk.WithAutoSubscribe(false))
	if err != nil {
		return nil, fmt.Errorf("connect to participant room %q: %w", config.RoomName, err)
	}
	c.participantRoom = participantRoom

	return c, nil
}

func (c *composer) addParticipantVideo(identity, displayName, participantSID string) {
	c.pipelineMu.Lock()
	pipeline := c.pipeline
	c.pipelineMu.Unlock()

	if pipeline == nil {
		return
	}
	c.addParticipantVideoToPipeline(pipeline, identity, displayName, participantSID)
}

func (c *composer) addParticipantVideoToPipeline(pipeline *mosaicPipeline, identity, displayName, participantSID string) {
	sourceName, sourceIdentity := c.newParticipantSource()
	if !pipeline.addParticipant(identity, displayName, participantSID, sourceName) {
		c.forgetParticipantSource(sourceIdentity)
	}
}

func (c *composer) newParticipantSource() (name, identity string) {
	c.sourceMu.Lock()
	defer c.sourceMu.Unlock()

	if c.sourceIdentities == nil {
		c.sourceIdentities = make(map[string]struct{})
	}
	sourceName := fmt.Sprintf("participant-src-%d", c.nextSource)
	c.nextSource++
	sourceIdentity := participantSourceIdentity(c.config, sourceName)
	c.sourceIdentities[sourceIdentity] = struct{}{}

	return sourceName, sourceIdentity
}

func (c *composer) forgetParticipantSource(identity string) {
	c.sourceMu.Lock()
	delete(c.sourceIdentities, identity)
	c.sourceMu.Unlock()
}

func (c *composer) isParticipantSourceIdentity(identity string) bool {
	c.sourceMu.Lock()
	defer c.sourceMu.Unlock()

	_, ok := c.sourceIdentities[identity]
	return ok
}

func (c *composer) removeParticipantVideo(identity string) {
	c.pipelineMu.Lock()
	pipeline := c.pipeline
	c.pipelineMu.Unlock()

	if pipeline == nil {
		return
	}
	pipeline.removeParticipant(identity)
}

func (c *composer) close() {
	if err := c.stopPipeline(); err != nil {
		log.Printf("stop mosaic pipeline: %v", err)
	}
	c.participantRoom.Disconnect()
}

func (c *composer) startPipeline(columns, rows int, rotationTimeout time.Duration) error {
	c.pipelineMu.Lock()
	defer c.pipelineMu.Unlock()

	if c.pipeline != nil {
		return nil
	}

	pipeline, err := startMosaicPipeline(c.config, columns, rows, rotationTimeout)
	if err != nil {
		return err
	}
	c.pipeline = pipeline
	for _, participant := range c.participantRoom.GetRemoteParticipants() {
		if c.isParticipantSourceIdentity(participant.Identity()) {
			continue
		}
		for _, publication := range participant.TrackPublications() {
			if publication.Kind() != lksdk.TrackKindVideo {
				continue
			}
			c.addParticipantVideoToPipeline(pipeline, participant.Identity(), participant.Name(), participant.SID())
			break
		}
	}
	log.Printf("publishing static background to target room %q", c.config.StudioRoomName)
	return nil
}

func (c *composer) stopPipeline() error {
	c.pipelineMu.Lock()
	defer c.pipelineMu.Unlock()

	if c.pipeline == nil {
		return nil
	}

	pipeline := c.pipeline
	c.pipeline = nil
	if err := pipeline.stop(); err != nil {
		return err
	}
	log.Printf("stopped publishing to target room %q", c.config.StudioRoomName)
	return nil
}

func (c *composer) health() HealthResponse {
	connectionState := string(c.participantRoom.ConnectionState())
	connected := connectionState == string(lksdk.ConnectionStateConnected)
	status := sharedhttp.StatusOK
	if !connected {
		status = sharedhttp.StatusDegraded
	}

	mosaic := c.mosaicHealth()
	mosaic.Participants = c.participantCount()

	return HealthResponse{
		Status: status,
		Subsystems: Subsystems{
			LiveKit: LiveKitHealth{
				Status:          status,
				Connected:       connected,
				URL:             c.config.LiveKitURL,
				Room:            c.config.RoomName,
				Identity:        c.config.Identity,
				ConnectionState: connectionState,
			},
			Mosaic: mosaic,
		},
	}
}

func (c *composer) mosaicHealth() MosaicHealth {
	c.pipelineMu.Lock()
	defer c.pipelineMu.Unlock()

	if c.pipeline == nil {
		return MosaicHealth{State: mosaicStateIdle}
	}
	return MosaicHealth{
		State:                  mosaicStateRunning,
		RotationTimeoutSeconds: int(c.pipeline.rotationTimeout / time.Second),
		Grid: &GridHealth{
			Columns: c.pipeline.gridColumns(),
			Rows:    c.pipeline.gridRows(),
		},
	}
}

func (c *composer) participantCount() int {
	count := 0
	for _, participant := range c.participantRoom.GetRemoteParticipants() {
		if c.isParticipantSourceIdentity(participant.Identity()) {
			continue
		}
		count++
	}
	return count
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}

	logger := log.New(os.Stdout, "mosaic-composer: ", log.LstdFlags)

	config, err := configFromEnv()
	if err != nil {
		logger.Fatalf("invalid configuration: %v", err)
	}

	composer, err := connectComposer(config)
	if err != nil {
		logger.Fatalf("connect to LiveKit at %s: %v", config.LiveKitURL, err)
	}
	defer composer.close()

	mux := http.NewServeMux()
	mux.Handle("GET /health", sharedhttp.HealthHandler(composer.health))
	mux.Handle("POST /start", startPipelineHandler(composer.startPipeline))
	mux.Handle("POST /stop", sharedhttp.StopHandler(composer.stopPipeline))

	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Printf("connected to LiveKit room %q at %s", config.RoomName, config.LiveKitURL)
		logger.Printf("listening on %s", config.ListenAddr)
		serverErrors <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("HTTP server failed: %v", err)
		}
	case received := <-stop:
		logger.Printf("received %s; shutting down", received)
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Printf("HTTP server shutdown failed: %v", err)
		}
	}
}

func runHealthcheck() int {
	url := sharedconfig.EnvOrDefault("MOSAIC_HEALTHCHECK_URL", healthcheckURL)
	client := &http.Client{Timeout: 5 * time.Second}

	response, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck GET %s: %v\n", url, err)
		return 1
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck GET %s status = %d, want %d\n", url, response.StatusCode, http.StatusOK)
		return 1
	}

	return 0
}
