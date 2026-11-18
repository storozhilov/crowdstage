package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStartHandlerAcceptsAGuestBroadcastDuration(t *testing.T) {
	pipeline := &fakeMixerPipeline{}
	mixer := &mediaMixer{
		startPipeline: func(config config) (mediaMixerPipelineAPI, error) {
			return pipeline, nil
		},
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/start",
		strings.NewReader(`{"guest_broadcast_seconds":3}`),
	)
	response := httptest.NewRecorder()

	startHandler(mixer).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if mixer.guestBroadcastSeconds != 3 {
		t.Fatalf("guest broadcast seconds = %d, want 3", mixer.guestBroadcastSeconds)
	}
}

func TestStartHandlerUsesTheDefaultGuestBroadcastDuration(t *testing.T) {
	pipeline := &fakeMixerPipeline{}
	mixer := &mediaMixer{
		guestBroadcastSeconds: defaultGuestBroadcastSeconds,
		startPipeline: func(config config) (mediaMixerPipelineAPI, error) {
			return pipeline, nil
		},
	}

	response := httptest.NewRecorder()
	startHandler(mixer).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/start", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if mixer.guestBroadcastSeconds != defaultGuestBroadcastSeconds {
		t.Fatalf(
			"guest broadcast seconds = %d, want %d",
			mixer.guestBroadcastSeconds,
			defaultGuestBroadcastSeconds,
		)
	}
}

func TestStartHandlerRejectsInvalidGuestBroadcastDurations(t *testing.T) {
	for _, body := range []string{
		`{"guest_broadcast_seconds":0}`,
		`{"guest_broadcast_seconds":-1}`,
		`{"guest_broadcast_seconds":"slow"}`,
		`not-json`,
	} {
		mixer := &mediaMixer{}

		response := httptest.NewRecorder()
		startHandler(mixer).ServeHTTP(
			response,
			httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(body)),
		)

		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want %d", body, response.Code, http.StatusBadRequest)
		}
		if mixer.running {
			t.Fatalf("body %q started the mixer despite an invalid duration", body)
		}
	}
}
