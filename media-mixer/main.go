package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type mediaMixer struct {
	config config

	mu       sync.Mutex
	pipeline *mediaMixerPipeline
}

func (m *mediaMixer) start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pipeline != nil {
		log.Printf("media mixer is already running")
		return nil
	}

	pipeline, err := startMediaMixerPipeline(m.config)
	if err != nil {
		return err
	}
	m.pipeline = pipeline
	return nil
}

func (m *mediaMixer) stop() error {
	m.mu.Lock()
	pipeline := m.pipeline
	m.pipeline = nil
	m.mu.Unlock()

	if pipeline == nil {
		log.Printf("media mixer is not running")
		return nil
	}
	return pipeline.stop()
}

func (m *mediaMixer) health() HealthResponse {
	m.mu.Lock()
	pipeline := m.pipeline
	m.mu.Unlock()

	mixer := MixerHealth{
		State:      mixerStateIdle,
		SourceRoom: m.config.SourceRoom,
		TargetRoom: m.config.TargetRoom,
	}
	if pipeline != nil {
		mixer.State = mixerStateRunning
		mixer.ForwardedTracks = pipeline.trackCount()
	}

	return HealthResponse{
		Status:     statusOK,
		Subsystems: Subsystems{Mixer: mixer},
	}
}

func main() {
	config, err := configFromEnv()
	if err != nil {
		log.Printf("media mixer configuration error: %v", err)
		os.Exit(1)
	}

	mixer := &mediaMixer{config: config}
	mux := http.NewServeMux()
	mux.Handle("GET /health", healthHandler(mixer.health))
	mux.Handle("POST /start", startMixerHandler(mixer.start))
	mux.Handle("POST /stop", stopMixerHandler(mixer.stop))

	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
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

	if err := mixer.stop(); err != nil {
		log.Printf("media mixer shutdown error: %v", err)
	}
}
