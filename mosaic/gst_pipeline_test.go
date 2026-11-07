package main

import (
	"strings"
	"testing"
)

func TestPipelineDescriptionConnectsParticipantAndStudioRooms(t *testing.T) {
	config := config{
		LiveKitURL:     "ws://livekit:7880",
		APIKey:         "key",
		APISecret:      "secret",
		RoomName:       "participants",
		StudioRoomName: "studio",
		Identity:       "composer",
	}

	description := pipelineDescription(config)

	expected := []string{
		"livekitwebrtcsrc",
		"livekitwebrtcsink",
		`signaller::ws-url="ws://livekit:7880"`,
		`signaller::room-name="participants"`,
		`signaller::room-name="studio"`,
		`signaller::identity="composer-participant"`,
		`signaller::identity="composer"`,
		"video/x-raw,width=1280,height=720,framerate=30/1",
		`video-caps="video/x-vp8"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("pipeline description does not contain %q:\n%s", want, description)
		}
	}
}

func TestGstStringEscapesValues(t *testing.T) {
	if got, want := gstString(`a"b\c`), `"a\"b\\c"`; got != want {
		t.Errorf("gstString() = %s, want %s", got, want)
	}
}
