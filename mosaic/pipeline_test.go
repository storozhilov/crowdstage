package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"
)

func TestPipelineHandlersStartAndStopThePipeline(t *testing.T) {
	started := 0
	stopped := 0

	mux := http.NewServeMux()
	mux.Handle("POST /start", startPipelineHandler(func(columns, rows int, rotationTimeout time.Duration) error {
		if columns != mosaicColumns || rows != mosaicRows {
			t.Fatalf("start grid = %dx%d, want %dx%d", columns, rows, mosaicColumns, mosaicRows)
		}
		if rotationTimeout != defaultRotationTimeout {
			t.Fatalf("rotation timeout = %v, want %v", rotationTimeout, defaultRotationTimeout)
		}
		started++
		return nil
	}))
	mux.Handle("POST /stop", sharedhttp.StopHandler(func() error {
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
	mux.Handle("POST /start", startPipelineHandler(func(columns, rows int, rotationTimeout time.Duration) error {
		return errors.New("start failed")
	}))
	mux.Handle("POST /stop", sharedhttp.StopHandler(func() error {
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
	mux.Handle("POST /start", startPipelineHandler(func(columns, rows int, rotationTimeout time.Duration) error { return nil }))
	mux.Handle("POST /stop", sharedhttp.StopHandler(func() error { return nil }))

	for _, path := range []string{"/start", "/stop"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))

		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusMethodNotAllowed)
		}
	}
}

func TestStartPipelineHandlerParsesGridFromJSONBody(t *testing.T) {
	tests := []struct {
		name                string
		body                string
		wantColumns         int
		wantRows            int
		wantRotationTimeout time.Duration
	}{
		{name: "both values", body: `{"columns":2,"rows":5}`, wantColumns: 2, wantRows: 5, wantRotationTimeout: defaultRotationTimeout},
		{name: "columns only", body: `{"columns":2}`, wantColumns: 2, wantRows: mosaicRows, wantRotationTimeout: defaultRotationTimeout},
		{name: "rows only", body: `{"rows":5}`, wantColumns: mosaicColumns, wantRows: 5, wantRotationTimeout: defaultRotationTimeout},
		{name: "maximum capacity", body: `{"columns":10,"rows":10}`, wantColumns: 10, wantRows: 10, wantRotationTimeout: defaultRotationTimeout},
		{name: "empty object", body: `{}`, wantColumns: mosaicColumns, wantRows: mosaicRows, wantRotationTimeout: defaultRotationTimeout},
		{name: "null body", body: `null`, wantColumns: mosaicColumns, wantRows: mosaicRows, wantRotationTimeout: defaultRotationTimeout},
		{name: "rotation timeout only", body: `{"rotation_timeout_seconds":7}`, wantColumns: mosaicColumns, wantRows: mosaicRows, wantRotationTimeout: 7 * time.Second},
		{name: "grid and rotation timeout", body: `{"columns":2,"rows":2,"rotation_timeout_seconds":3}`, wantColumns: 2, wantRows: 2, wantRotationTimeout: 3 * time.Second},
		{name: "maximum rotation timeout", body: `{"rotation_timeout_seconds":9223372036}`, wantColumns: mosaicColumns, wantRows: mosaicRows, wantRotationTimeout: 9223372036 * time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			started := false
			mux := http.NewServeMux()
			mux.Handle("POST /start", startPipelineHandler(func(columns, rows int, rotationTimeout time.Duration) error {
				started = true
				if columns != test.wantColumns || rows != test.wantRows {
					t.Fatalf("start grid = %dx%d, want %dx%d", columns, rows, test.wantColumns, test.wantRows)
				}
				if rotationTimeout != test.wantRotationTimeout {
					t.Fatalf("rotation timeout = %v, want %v", rotationTimeout, test.wantRotationTimeout)
				}
				return nil
			}))

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(test.body))
			mux.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("POST /start status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}
			if !started {
				t.Fatal("POST /start did not start the pipeline")
			}
		})
	}
}

func TestStartPipelineHandlerRejectsInvalidRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{`},
		{name: "zero columns", body: `{"columns":0}`},
		{name: "negative columns", body: `{"columns":-1}`},
		{name: "zero rows", body: `{"rows":0}`},
		{name: "negative rows", body: `{"rows":-1}`},
		{name: "capacity exceeded", body: `{"columns":11,"rows":10}`},
		{name: "oversized dimensions", body: `{"columns":100000,"rows":100000}`},
		{name: "grid does not fit canvas", body: `{"columns":1,"rows":100}`},
		{name: "zero rotation timeout", body: `{"rotation_timeout_seconds":0}`},
		{name: "negative rotation timeout", body: `{"rotation_timeout_seconds":-1}`},
		{name: "fractional rotation timeout", body: `{"rotation_timeout_seconds":1.5}`},
		{name: "string rotation timeout", body: `{"rotation_timeout_seconds":"10"}`},
		{name: "rotation timeout overflows duration", body: `{"rotation_timeout_seconds":9223372037}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			started := false
			mux := http.NewServeMux()
			mux.Handle("POST /start", startPipelineHandler(func(columns, rows int, rotationTimeout time.Duration) error {
				started = true
				return nil
			}))

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(test.body))
			mux.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("POST /start status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			if started {
				t.Fatal("POST /start started the pipeline with an invalid grid")
			}

			var body PipelineErrorResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode POST /start response: %v", err)
			}
			if body.Status != "error" {
				t.Fatalf("POST /start status = %q, want %q", body.Status, "error")
			}
		})
	}
}
