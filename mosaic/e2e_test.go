package main

import (
	"encoding/json"
	"fmt"
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

	postPipeline(t, baseURL, "/stop")
	health := getHealth(t, baseURL)

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
	if health.Subsystems.Mosaic.State != "idle" {
		t.Fatalf("mosaic state = %q, want %q while the pipeline is stopped", health.Subsystems.Mosaic.State, "idle")
	}
	if health.Subsystems.Mosaic.Participants != 0 {
		t.Fatalf("mosaic participant count = %d, want 0 without participants", health.Subsystems.Mosaic.Participants)
	}
	if health.Subsystems.Mosaic.Grid != nil {
		t.Fatalf("mosaic grid = %+v, want no grid while the pipeline is stopped", health.Subsystems.Mosaic.Grid)
	}
	if health.Subsystems.Mosaic.RotationTimeoutSeconds != 0 {
		t.Fatalf("mosaic rotation timeout = %d, want 0 while the pipeline is stopped", health.Subsystems.Mosaic.RotationTimeoutSeconds)
	}

	postPipelineWithGridAndRotation(t, baseURL, "/start", 5, 2, 7)
	stopped := false
	defer func() {
		if !stopped {
			postPipeline(t, baseURL, "/stop")
		}
	}()

	health = getHealth(t, baseURL)
	if health.Subsystems.Mosaic.State != "running" {
		t.Fatalf("mosaic state = %q, want %q while the pipeline is started", health.Subsystems.Mosaic.State, "running")
	}
	if health.Subsystems.Mosaic.Grid == nil {
		t.Fatal("mosaic grid is missing while the pipeline is running")
	}
	if grid := health.Subsystems.Mosaic.Grid; grid.Columns != 5 || grid.Rows != 2 {
		t.Fatalf("mosaic grid = %dx%d, want 5x2", grid.Columns, grid.Rows)
	}
	if got := health.Subsystems.Mosaic.RotationTimeoutSeconds; got != 7 {
		t.Fatalf("mosaic rotation timeout = %d, want 7", got)
	}

	postPipeline(t, baseURL, "/stop")
	stopped = true
}

func postPipelineWithGrid(t *testing.T, baseURL, path string, columns, rows int) {
	t.Helper()

	body := fmt.Sprintf(`{"columns":%d,"rows":%d}`, columns, rows)
	if err := postPipelineRequest(baseURL, path, body); err != nil {
		t.Fatal(err)
	}
}

func postPipelineWithGridAndRotation(t *testing.T, baseURL, path string, columns, rows, rotationTimeoutSeconds int) {
	t.Helper()

	body := fmt.Sprintf(
		`{"columns":%d,"rows":%d,"rotation_timeout_seconds":%d}`,
		columns,
		rows,
		rotationTimeoutSeconds,
	)
	if err := postPipelineRequest(baseURL, path, body); err != nil {
		t.Fatal(err)
	}
}

func getHealth(t *testing.T, baseURL string) HealthResponse {
	t.Helper()

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
	return health
}
