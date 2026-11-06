package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
