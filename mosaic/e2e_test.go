package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHealthEndpointEndToEnd(t *testing.T) {
	baseURL := os.Getenv("MOSAIC_URL")
	if baseURL == "" {
		t.Skip("MOSAIC_URL is not set; run ./ci/run_tests.sh against the dev stand")
	}
	url := strings.TrimRight(baseURL, "/") + "/health"

	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d", url, response.StatusCode, http.StatusOK)
	}
	if got := response.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}

	var health HealthResponse
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatalf("decode health response: %v", err)
	}

	if health.Status != "ok" {
		t.Fatalf("overall status = %q, want %q", health.Status, "ok")
	}
	livekit := health.Subsystems.LiveKit
	if livekit.Status != "ok" {
		t.Fatalf("livekit status = %q, want %q", livekit.Status, "ok")
	}
	if !livekit.Connected || livekit.ConnectionState != "connected" {
		t.Fatalf("livekit connection = %+v, want a connected subsystem", livekit)
	}
	if livekit.URL == "" || livekit.Room == "" || livekit.Identity == "" {
		t.Fatalf("livekit connection info is incomplete: %+v", livekit)
	}
}
