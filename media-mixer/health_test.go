package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandlerReportsMixerState(t *testing.T) {
	want := HealthResponse{
		Status: statusOK,
		Subsystems: Subsystems{
			Mixer: MixerHealth{
				State:           mixerStateRunning,
				SourceRoom:      "studio",
				TargetRoom:      "broadcast",
				ForwardedTracks: 2,
			},
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	healthHandler(func() HealthResponse { return want }).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}

	var got HealthResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != want.Status {
		t.Fatalf("status = %q, want %q", got.Status, want.Status)
	}
	if got.Subsystems.Mixer != want.Subsystems.Mixer {
		t.Fatalf("mixer health = %+v, want %+v", got.Subsystems.Mixer, want.Subsystems.Mixer)
	}
}

func TestHealthHandlerReportsUnavailableSubsystem(t *testing.T) {
	health := HealthResponse{
		Status: statusDegraded,
		Subsystems: Subsystems{
			Mixer: MixerHealth{
				State:      mixerStateIdle,
				SourceRoom: "studio",
				TargetRoom: "broadcast",
			},
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	healthHandler(func() HealthResponse { return health }).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthRouteOnlyAcceptsGet(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /health", healthHandler(func() HealthResponse {
		return HealthResponse{Status: statusOK}
	}))

	request := httptest.NewRequest(http.MethodPost, "/health", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestMediaMixerHealthReportsPipelineFailure(t *testing.T) {
	pipeline := &mediaMixerPipeline{}
	pipeline.markFailed(errors.New("pipeline failed"))
	mixer := &mediaMixer{
		config: config{
			SourceRoom: "studio",
			TargetRoom: "broadcast",
		},
		pipeline: pipeline,
	}

	health := mixer.health()

	if health.Status != statusDegraded {
		t.Fatalf("status = %q, want %q", health.Status, statusDegraded)
	}
	if health.Subsystems.Mixer.State != mixerStateFailed {
		t.Fatalf("mixer state = %q, want %q", health.Subsystems.Mixer.State, mixerStateFailed)
	}
}
