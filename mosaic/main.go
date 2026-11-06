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
	studioRoom      *lksdk.Room

	forwardsMu sync.Mutex
	forwards   map[string]*forward
}

func connectRoom(config config, roomName string, callback *lksdk.RoomCallback) (*lksdk.Room, error) {
	return lksdk.ConnectToRoom(config.LiveKitURL, lksdk.ConnectInfo{
		APIKey:              config.APIKey,
		APISecret:           config.APISecret,
		RoomName:            roomName,
		ParticipantIdentity: config.Identity,
	}, callback, lksdk.WithConnectTimeout(30*time.Second))
}

func connectComposer(config config) (*composer, error) {
	composer := &composer{
		config:   config,
		forwards: make(map[string]*forward),
	}

	studioRoom, err := connectRoom(config, config.StudioRoomName, &lksdk.RoomCallback{})
	if err != nil {
		return nil, fmt.Errorf("connect to studio room %q: %w", config.StudioRoomName, err)
	}
	composer.studioRoom = studioRoom

	callback := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed:   composer.forwardTrack,
			OnTrackUnsubscribed: composer.stopForward,
		},
	}
	participantRoom, err := connectRoom(config, config.RoomName, callback)
	if err != nil {
		studioRoom.Disconnect()
		return nil, fmt.Errorf("connect to participant room %q: %w", config.RoomName, err)
	}
	composer.participantRoom = participantRoom

	return composer, nil
}

func (c *composer) close() {
	c.closeForwards()
	c.participantRoom.Disconnect()
	c.studioRoom.Disconnect()
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
