package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"
)

func TestHealthHandlerReportsMixerState(t *testing.T) {
	want := HealthResponse{
		Status: sharedhttp.StatusOK,
		Subsystems: Subsystems{
			Mixer: MixerHealth{
				State:   mixerStateRunning,
				Inputs:  0,
				Outputs: 1,
			},
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	sharedhttp.HealthHandler(func() HealthResponse { return want }).ServeHTTP(response, request)

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
		Status: sharedhttp.StatusDegraded,
		Subsystems: Subsystems{
			Mixer: MixerHealth{
				State:   mixerStateIdle,
				Inputs:  0,
				Outputs: 1,
			},
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	sharedhttp.HealthHandler(func() HealthResponse { return health }).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthRouteOnlyAcceptsGet(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /health", sharedhttp.HealthHandler(func() HealthResponse {
		return HealthResponse{Status: sharedhttp.StatusOK}
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
		config:   config{InputRoom: "studio", OutputRoom: "broadcast"},
		pipeline: pipeline,
	}

	health := mixer.health()

	if health.Status != sharedhttp.StatusDegraded {
		t.Fatalf("status = %q, want %q", health.Status, sharedhttp.StatusDegraded)
	}
	if health.Subsystems.Mixer.State != mixerStateFailed {
		t.Fatalf("mixer state = %q, want %q", health.Subsystems.Mixer.State, mixerStateFailed)
	}
}

func TestMediaMixerHealthReportsInputAndOutputCounts(t *testing.T) {
	mixer := &mediaMixer{config: config{InputRoom: "studio", OutputRoom: "broadcast"}}

	health := mixer.health()

	if health.Subsystems.Mixer.Inputs != 0 {
		t.Fatalf("inputs = %d, want 0", health.Subsystems.Mixer.Inputs)
	}
	if health.Subsystems.Mixer.Outputs != 1 {
		t.Fatalf("outputs = %d, want 1", health.Subsystems.Mixer.Outputs)
	}
}
