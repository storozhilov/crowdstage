package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestComposerRecordsParticipantMediaToFile(t *testing.T) {
	url := os.Getenv("LIVEKIT_URL")
	if url == "" {
		t.Skip("LIVEKIT_URL is not set; run ./ci/run_tests.sh against the dev stand")
	}
	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		t.Fatal("LIVEKIT_API_KEY and LIVEKIT_API_SECRET are required")
	}

	recordingsDir := t.TempDir()
	suffix := time.Now().UnixNano()
	participantRoom := fmt.Sprintf("recording-participants-%d", suffix)
	studioRoom := fmt.Sprintf("recording-studio-%d", suffix)
	composerIdentity := fmt.Sprintf("recording-composer-%d", suffix)
	publisherIdentity := fmt.Sprintf("recording-publisher-%d", suffix)
	trackName := fmt.Sprintf("recording-camera-%d", suffix)

	composer, err := connectComposer(config{
		LiveKitURL:     url,
		APIKey:         apiKey,
		APISecret:      apiSecret,
		RoomName:       participantRoom,
		StudioRoomName: studioRoom,
		Identity:       composerIdentity,
		RecordingsDir:  recordingsDir,
	})
	if err != nil {
		t.Fatalf("connect composer: %v", err)
	}
	composerClosed := false
	defer func() {
		if !composerClosed {
			composer.close()
		}
	}()

	frames := readIVFFrames(t, forwardingFixture)
	publisher, sourceTrack := joinForwardingPublisher(t, url, apiKey, apiSecret, participantRoom, publisherIdentity, trackName)
	defer publisher.Disconnect()

	stopPublishing := make(chan struct{})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() { close(stopPublishing) })
	}
	defer stop()
	go publishIVFFrames(sourceTrack, frames, stopPublishing)

	path := waitForRecording(t, recordingsDir)

	stop()
	publisher.Disconnect()
	composer.close()
	composerClosed = true

	codec, frameCount := probeRecording(t, path)
	if codec != "vp8" {
		t.Fatalf("recorded video codec = %q, want %q", codec, "vp8")
	}
	if frameCount == 0 {
		t.Fatal("recorded video contains no frames")
	}
}

func waitForRecording(t *testing.T, dir string) string {
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
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if info.Size() > 1024 {
				return path
			}
		}

		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for participant media to be recorded")
		}

		<-ticker.C
	}
}
