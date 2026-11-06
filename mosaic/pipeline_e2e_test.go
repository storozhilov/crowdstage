package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMosaicPipelineRecordsMediaToHostDirectory(t *testing.T) {
	baseURL := os.Getenv("MOSAIC_URL")
	if baseURL == "" {
		t.Skip("MOSAIC_URL is not set; run ./ci/run_tests.sh against the dev stand")
	}
	url := os.Getenv("LIVEKIT_URL")
	if url == "" {
		t.Skip("LIVEKIT_URL is not set; run ./ci/run_tests.sh against the dev stand")
	}
	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		t.Fatal("LIVEKIT_API_KEY and LIVEKIT_API_SECRET are required")
	}

	participantRoom := envOrDefault("MOSAIC_ROOM", "mosaic-composer")

	recordingsDir := os.Getenv("MOSAIC_RECORDINGS_HOST_DIR")
	if recordingsDir == "" {
		recordingsDir = filepath.Join("..", "ci", "compose", "recordings")
	}
	if err := os.MkdirAll(recordingsDir, 0o755); err != nil {
		t.Fatalf("create host recordings directory %s: %v", recordingsDir, err)
	}
	existing := recordingFiles(t, recordingsDir)

	postPipeline(t, baseURL, "/start")
	stopped := false
	defer func() {
		if !stopped {
			postPipeline(t, baseURL, "/stop")
		}
	}()

	frames := readIVFFrames(t, forwardingFixture)
	suffix := time.Now().UnixNano()
	publisherIdentity := fmt.Sprintf("pipeline-publisher-%d", suffix)
	trackName := fmt.Sprintf("pipeline-camera-%d", suffix)

	publisher, sourceTrack := joinForwardingPublisher(t, url, apiKey, apiSecret, participantRoom, publisherIdentity, trackName)
	defer publisher.Disconnect()

	stopPublishing := make(chan struct{})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() { close(stopPublishing) })
	}
	defer stop()
	go publishIVFFrames(sourceTrack, frames, stopPublishing)

	path := waitForNewRecording(t, recordingsDir, existing)

	postPipeline(t, baseURL, "/stop")
	stopped = true

	stop()
	publisher.Disconnect()

	codec, frameCount := probeRecording(t, path)
	if codec != "vp8" {
		t.Fatalf("recorded video codec = %q, want %q", codec, "vp8")
	}
	if frameCount == 0 {
		t.Fatal("recorded video contains no frames")
	}
}

func postPipeline(t *testing.T, baseURL, path string) {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + path
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status = %d, want %d", url, response.StatusCode, http.StatusOK)
	}

	var body PipelineResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode POST %s response: %v", url, err)
	}
	want := map[string]string{"/start": "started", "/stop": "stopped"}[path]
	if body.Status != want {
		t.Fatalf("POST %s status = %q, want %q", url, body.Status, want)
	}
}

func recordingFiles(t *testing.T, dir string) map[string]struct{} {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(dir, "*.mkv"))
	if err != nil {
		t.Fatalf("find recordings in %s: %v", dir, err)
	}

	files := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		files[path] = struct{}{}
	}
	return files
}

func waitForNewRecording(t *testing.T, dir string, existing map[string]struct{}) string {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		paths, err := filepath.Glob(filepath.Join(dir, "*.mkv"))
		if err != nil {
			t.Fatalf("find recordings in %s: %v", dir, err)
		}
		for _, path := range paths {
			if _, ok := existing[path]; ok {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if info.Size() > 1024 {
				return path
			}
		}

		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for participant media to be recorded to the host directory")
		}

		<-ticker.C
	}
}
