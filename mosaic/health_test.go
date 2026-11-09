package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestHealthHandlerReportsLiveKitConnection(t *testing.T) {
	want := HealthResponse{
		Status: "ok",
		Subsystems: Subsystems{
			LiveKit: LiveKitHealth{
				Status:          "ok",
				Connected:       true,
				URL:             "ws://livekit:7880",
				Room:            "mosaic-composer",
				Identity:        "mosaic-composer",
				ConnectionState: "connected",
			},
			Mosaic: MosaicHealth{
				State:        "running",
				Participants: 3,
				Grid:         &GridHealth{Columns: 5, Rows: 2},
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
	if got.Subsystems.LiveKit != want.Subsystems.LiveKit {
		t.Fatalf("livekit health = %+v, want %+v", got.Subsystems.LiveKit, want.Subsystems.LiveKit)
	}
	if !reflect.DeepEqual(got.Subsystems.Mosaic, want.Subsystems.Mosaic) {
		t.Fatalf("mosaic health = %+v, want %+v", got.Subsystems.Mosaic, want.Subsystems.Mosaic)
	}
}

func TestHealthHandlerReportsUnavailableSubsystem(t *testing.T) {
	health := HealthResponse{
		Status: "degraded",
		Subsystems: Subsystems{
			LiveKit: LiveKitHealth{
				Status:          "degraded",
				Connected:       false,
				URL:             "ws://livekit:7880",
				Room:            "mosaic-composer",
				Identity:        "mosaic-composer",
				ConnectionState: "disconnected",
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

func TestHealthHandlerOmitsGridWhenMosaicIsIdle(t *testing.T) {
	health := HealthResponse{
		Status: "ok",
		Subsystems: Subsystems{
			Mosaic: MosaicHealth{
				State:        "idle",
				Participants: 0,
			},
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	healthHandler(func() HealthResponse { return health }).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Body.String(); strings.Contains(got, `"grid"`) {
		t.Fatalf("idle health response contains grid dimensions: %s", got)
	}
}

func TestComposerMosaicHealthReportsGridWhileRunning(t *testing.T) {
	composer := &composer{
		pipeline: &mosaicPipeline{columns: 5, rows: 2},
	}

	health := composer.mosaicHealth()

	if health.State != mosaicStateRunning {
		t.Fatalf("mosaic state = %q, want %q", health.State, mosaicStateRunning)
	}
	if health.Grid == nil {
		t.Fatal("mosaic health does not report grid dimensions while running")
	}
	if health.Grid.Columns != 5 || health.Grid.Rows != 2 {
		t.Fatalf("mosaic grid = %dx%d, want 5x2", health.Grid.Columns, health.Grid.Rows)
	}
}

func TestComposerMosaicHealthOmitsGridWhileIdle(t *testing.T) {
	composer := &composer{}

	health := composer.mosaicHealth()

	if health.State != mosaicStateIdle {
		t.Fatalf("mosaic state = %q, want %q", health.State, mosaicStateIdle)
	}
	if health.Grid != nil {
		t.Fatalf("idle mosaic health reports grid dimensions: %+v", health.Grid)
	}
}

func TestHealthRouteOnlyAcceptsGet(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /health", healthHandler(func() HealthResponse {
		return HealthResponse{Status: "ok"}
	}))

	request := httptest.NewRequest(http.MethodPost, "/health", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
