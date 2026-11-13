package main

import (
	"strings"
	"testing"
)

func TestMediaMixerPipelineDescriptionStreamsStaticPictureWithoutStudioVideo(t *testing.T) {
	config := config{
		LiveKitURL: "ws://livekit:7880",
		APIKey:     "key",
		APISecret:  "secret",
		InputRoom:  "studio",
		OutputRoom: "broadcast",
		Identity:   "media-mixer",
	}

	description := mediaMixerPipelineDescription(config, nil, "")

	expected := []string{
		"videotestsrc is-live=true pattern=gradient",
		"video/x-raw,width=1280,height=720,framerate=30/1",
		"vp8enc",
		"livekitwebrtcsink",
		`signaller::room-name="broadcast"`,
		`signaller::identity="media-mixer"`,
		`video-caps="video/x-vp8"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("pipeline description does not contain %q:\n%s", want, description)
		}
	}
	if strings.Contains(description, "livekitwebrtcsrc") {
		t.Errorf("static picture pipeline must not subscribe to studio media:\n%s", description)
	}
}

func TestMediaMixerPipelineDescriptionForwardsOldestStudioVideo(t *testing.T) {
	config := config{
		LiveKitURL: "ws://livekit:7880",
		APIKey:     "key",
		APISecret:  "secret",
		InputRoom:  "studio",
		OutputRoom: "broadcast",
		Identity:   "media-mixer",
	}
	source := &studioVideo{
		participantIdentity: "studio-publisher-video",
		participantName:     "Studio Publisher",
		participantSID:      "PA_123",
		publicationSID:      "TR_123",
	}

	description := mediaMixerPipelineDescription(config, source, "studio-src-1")

	expected := []string{
		`livekitwebrtcsrc name="studio-src-1"`,
		`signaller::room-name="studio"`,
		`signaller::identity="media-mixer-studio-src-1"`,
		`signaller::producer-peer-id="studio-publisher-video"`,
		"decodebin",
		"vp8enc",
		"livekitwebrtcsink",
		`signaller::room-name="broadcast"`,
		`signaller::identity="media-mixer"`,
		`video-caps="video/x-vp8"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("pipeline description does not contain %q:\n%s", want, description)
		}
	}
	if strings.Contains(description, "videotestsrc") {
		t.Errorf("pipeline description must not generate a stand-in picture:\n%s", description)
	}
}
