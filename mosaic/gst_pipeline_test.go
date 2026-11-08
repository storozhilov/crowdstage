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
		"livekitwebrtcsrc name=participant-src",
		"livekitwebrtcsink",
		"compositor name=mixer",
		"sink_0::xpos=0",
		"sink_0::ypos=0",
		"sink_0::width=1280",
		"sink_0::height=720",
		"sink_0::zorder=0",
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

func TestParticipantSourceDescriptionUsesRequestedName(t *testing.T) {
	config := config{
		LiveKitURL: "ws://livekit:7880",
		APIKey:     "key",
		APISecret:  "secret",
		RoomName:   "participants",
		Identity:   "composer",
	}

	description := participantSourceDescription(config, "participant-src-2")

	expected := []string{
		`livekitwebrtcsrc name="participant-src-2"`,
		`signaller::ws-url="ws://livekit:7880"`,
		`signaller::room-name="participants"`,
		`signaller::identity="composer-participant"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("participant source description does not contain %q:\n%s", want, description)
		}
	}
	if strings.Contains(description, "producer-peer-id") {
		t.Errorf("participant source description must not target a specific producer:\n%s", description)
	}
}

func TestParticipantSIDFromMSID(t *testing.T) {
	tests := []struct {
		name string
		msid string
		want string
	}{
		{name: "participant and track", msid: "PA_abc|TR_def TR_def", want: "PA_abc"},
		{name: "participant only", msid: "PA_abc", want: "PA_abc"},
		{name: "empty", msid: "", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := participantSIDFromMSID(test.msid); got != test.want {
				t.Errorf("participantSIDFromMSID(%q) = %q, want %q", test.msid, got, test.want)
			}
		})
	}
}

func TestParticipantOverlayIsPlacedInTheTopRightCorner(t *testing.T) {
	xpos, ypos, width, height := participantOverlayPosition()

	if width != participantWidth || height != participantHeight {
		t.Fatalf("overlay size = %dx%d, want %dx%d", width, height, participantWidth, participantHeight)
	}
	if xpos+width > backgroundWidth {
		t.Fatalf("overlay right edge = %d, exceeds canvas width %d", xpos+width, backgroundWidth)
	}
	if ypos < 0 || ypos+height > backgroundHeight {
		t.Fatalf("overlay top edge %d or bottom edge %d is outside the canvas", ypos, ypos+height)
	}
	if xpos < backgroundWidth/2 || ypos >= backgroundHeight/2 {
		t.Fatalf("overlay position (%d,%d) is not in the top-right corner", xpos, ypos)
	}
}

func TestGstStringEscapesValues(t *testing.T) {
	if got, want := gstString(`a"b\c`), `"a\"b\\c"`; got != want {
		t.Errorf("gstString() = %s, want %s", got, want)
	}
}
