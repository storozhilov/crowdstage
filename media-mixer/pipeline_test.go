package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPipelineHandlersStartAndStopThePipeline(t *testing.T) {
	started := 0
	stopped := 0

	mux := http.NewServeMux()
	mux.Handle("POST /start", startMixerHandler(func() error {
		started++
		return nil
	}))
	mux.Handle("POST /stop", stopMixerHandler(func() error {
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
		if got := response.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("POST %s Content-Type = %q, want %q", request.path, got, "application/json")
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
	mux.Handle("POST /start", startMixerHandler(func() error {
		return errors.New("start failed")
	}))
	mux.Handle("POST /stop", stopMixerHandler(func() error {
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

func TestPipelineRoutesOnlyAcceptPost(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("POST /start", startMixerHandler(func() error { return nil }))
	mux.Handle("POST /stop", stopMixerHandler(func() error { return nil }))

	for _, path := range []string{"/start", "/stop"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))

		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusMethodNotAllowed)
		}
	}
}

func TestPadMediaType(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "video_unique_0", want: mediaTypeVideo},
		{name: "audio_unique_0", want: mediaTypeAudio},
		{name: "video_ABC_1", want: mediaTypeVideo},
		{name: "audio_ABC_1", want: mediaTypeAudio},
		{name: "data_unique_0", want: ""},
		{name: "", want: ""},
	}

	for _, test := range tests {
		if got := padMediaType(test.name); got != test.want {
			t.Fatalf("padMediaType(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestPipelineTrackCountCountsOnlyForwardedTracks(t *testing.T) {
	pipeline := &mediaMixerPipeline{
		tracks: map[string]*forwardedTrack{
			"forwarded":     {},
			"not-forwarded": {},
		},
	}
	pipeline.tracks["forwarded"].forwarded.Store(true)

	if got := pipeline.trackCount(); got != 1 {
		t.Fatalf("track count = %d, want 1", got)
	}
}
