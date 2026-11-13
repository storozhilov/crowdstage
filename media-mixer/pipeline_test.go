package main

import (
	"strings"
	"testing"
)

func TestMediaMixerPipelineDescriptionStreamsPictureToOutputRoom(t *testing.T) {
	config := config{
		LiveKitURL: "ws://livekit:7880",
		APIKey:     "key",
		APISecret:  "secret",
		InputRoom:  "studio",
		OutputRoom: "broadcast",
		Identity:   "media-mixer",
	}

	description := mediaMixerPipelineDescription(config)

	expected := []string{
		"videotestsrc",
		"livekitwebrtcsink",
		`signaller::ws-url="ws://livekit:7880"`,
		`signaller::room-name="broadcast"`,
		`signaller::identity="media-mixer"`,
		"video/x-raw,width=1280,height=720,framerate=30/1",
		`video-caps="video/x-vp8"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("pipeline description does not contain %q:\n%s", want, description)
		}
	}
	if strings.Contains(description, "livekitwebrtcsrc") {
		t.Errorf("pipeline description must not subscribe to input media:\n%s", description)
	}
}
