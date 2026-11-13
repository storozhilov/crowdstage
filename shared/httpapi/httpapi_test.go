package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testSubsystems struct {
	Service testHealth `json:"service"`
}

type testHealth struct {
	State string `json:"state"`
}

func TestHealthHandlerReportsHealthyResponse(t *testing.T) {
	want := HealthResponse[testSubsystems]{
		Status: StatusOK,
		Subsystems: testSubsystems{
			Service: testHealth{State: "running"},
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	HealthHandler(func() HealthResponse[testSubsystems] { return want }).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}

	var got HealthResponse[testSubsystems]
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != want.Status {
		t.Fatalf("status = %q, want %q", got.Status, want.Status)
	}
	if got.Subsystems.Service != want.Subsystems.Service {
		t.Fatalf("service health = %+v, want %+v", got.Subsystems.Service, want.Subsystems.Service)
	}
}

func TestHealthHandlerReportsUnavailableSubsystem(t *testing.T) {
	health := HealthResponse[testSubsystems]{
		Status: StatusDegraded,
		Subsystems: testSubsystems{
			Service: testHealth{State: "failed"},
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	HealthHandler(func() HealthResponse[testSubsystems] { return health }).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestPipelineHandlersStartAndStop(t *testing.T) {
	started := 0
	stopped := 0

	mux := http.NewServeMux()
	mux.Handle("POST /start", StartHandler(func() error {
		started++
		return nil
	}))
	mux.Handle("POST /stop", StopHandler(func() error {
		stopped++
		return nil
	}))

	requests := []struct {
		path       string
		wantStatus string
	}{
		{path: "/start", wantStatus: "started"},
		{path: "/stop", wantStatus: "stopped"},
	}

	for _, request := range requests {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, request.path, nil))

		if response.Code != http.StatusOK {
			t.Fatalf("POST %s status = %d, want %d", request.path, response.Code, http.StatusOK)
		}

		var body PipelineResponse
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("decode POST %s response: %v", request.path, err)
		}
		if body.Status != request.wantStatus {
			t.Fatalf("POST %s status = %q, want %q", request.path, body.Status, request.wantStatus)
		}
	}

	if started != 1 {
		t.Fatalf("start calls = %d, want 1", started)
	}
	if stopped != 1 {
		t.Fatalf("stop calls = %d, want 1", stopped)
	}
}

func TestPipelineHandlersReportErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("POST /start", StartHandler(func() error {
		return errors.New("start failed")
	}))
	mux.Handle("POST /stop", StopHandler(func() error {
		return errors.New("stop failed")
	}))

	for _, path := range []string{"/start", "/stop"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("POST %s status = %d, want %d", path, response.Code, http.StatusInternalServerError)
		}

		var body PipelineErrorResponse
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("decode POST %s response: %v", path, err)
		}
		if body.Status != "error" {
			t.Fatalf("POST %s status = %q, want %q", path, body.Status, "error")
		}
	}
}

func TestWriteErrorUsesProvidedStatus(t *testing.T) {
	response := httptest.NewRecorder()

	WriteError(response, http.StatusBadRequest, errors.New("invalid request"))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusBadRequest)
	}

	var body PipelineErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "error" || body.Error != "invalid request" {
		t.Fatalf("body = %+v, want an error response", body)
	}
}
