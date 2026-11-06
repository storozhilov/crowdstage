package main

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMediaRecorderStoresVP8FramesInFile(t *testing.T) {
	frames := readIVFFrames(t, forwardingFixture)
	path := filepath.Join(t.TempDir(), "recording.mkv")

	recorder, err := newMediaRecorder(path, forwardingWidth, forwardingHeight)
	if err != nil {
		t.Fatalf("create media recorder: %v", err)
	}

	for _, frame := range frames {
		if err := recorder.writeSample(frame, 33*time.Millisecond); err != nil {
			t.Fatalf("write media sample: %v", err)
		}
	}

	if err := recorder.close(); err != nil {
		t.Fatalf("close media recorder: %v", err)
	}

	codec, frameCount := probeRecording(t, path)
	if codec != "vp8" {
		t.Fatalf("recorded video codec = %q, want %q", codec, "vp8")
	}
	if frameCount == 0 {
		t.Fatal("recorded video contains no frames")
	}
}

func probeRecording(t *testing.T, path string) (string, int) {
	t.Helper()

	output, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-count_frames",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name,nb_read_frames",
		"-of", "default=noprint_wrappers=1",
		path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe %s: %v\n%s", path, err, output)
	}

	var codec string
	var frameCount int
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "codec_name":
			codec = value
		case "nb_read_frames":
			frameCount, err = strconv.Atoi(value)
			if err != nil {
				t.Fatalf("parse ffprobe frame count %q: %v", value, err)
			}
		}
	}

	return codec, frameCount
}
