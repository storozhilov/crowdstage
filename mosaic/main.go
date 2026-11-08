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
)

const healthcheckURL = "http://127.0.0.1:7980/health"

type composer struct {
	config          config
	participantRoom *lksdk.Room

	pipelineMu sync.Mutex
	pipeline   *mosaicPipeline
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
	c := &composer{config: config}
	callback := &lksdk.RoomCallback{
		OnParticipantDisconnected: func(participant *lksdk.RemoteParticipant) {
			log.Printf("participant disconnected: participant=%q identity=%q", participant.SID(), participant.Identity())
			c.removeParticipantVideo(participant.SID())
		},
	}

	participantRoom, err := connectRoom(config, config.RoomName, callback, lksdk.WithAutoSubscribe(false))
	if err != nil {
		return nil, fmt.Errorf("connect to participant room %q: %w", config.RoomName, err)
	}
	c.participantRoom = participantRoom

	return c, nil
}

func (c *composer) removeParticipantVideo(participantSID string) {
	c.pipelineMu.Lock()
	pipeline := c.pipeline
	c.pipelineMu.Unlock()

	if pipeline == nil {
		return
	}
	pipeline.removeParticipant(participantSID)
}

func (c *composer) close() {
	if err := c.stopPipeline(); err != nil {
		log.Printf("stop mosaic pipeline: %v", err)
	}
	c.participantRoom.Disconnect()
}

func (c *composer) startPipeline() error {
	c.pipelineMu.Lock()
	defer c.pipelineMu.Unlock()

	if c.pipeline != nil {
		return nil
	}

	pipeline, err := startMosaicPipeline(c.config)
	if err != nil {
		return err
	}
	c.pipeline = pipeline
	log.Printf("publishing static background to studio room %q", c.config.StudioRoomName)
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
	log.Printf("stopped publishing to studio room %q", c.config.StudioRoomName)
	return nil
}

func (c *composer) health() HealthResponse {
	connectionState := string(c.participantRoom.ConnectionState())
	connected := connectionState == string(lksdk.ConnectionStateConnected)
	status := statusOK
	if !connected {
		status = statusDegraded
	}

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
		},
	}
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
	mux.Handle("GET /health", healthHandler(composer.health))
	mux.Handle("POST /start", startPipelineHandler(composer.startPipeline))
	mux.Handle("POST /stop", stopPipelineHandler(composer.stopPipeline))

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
	url := envOrDefault("MOSAIC_HEALTHCHECK_URL", healthcheckURL)
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
