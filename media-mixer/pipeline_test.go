package main

import (
	"strings"
	"testing"
)

func TestMediaMixerPipelineDescriptionPublishesAStaticBackground(t *testing.T) {
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
		"videotestsrc is-live=true pattern=gradient",
		"video/x-raw,width=1280,height=720,framerate=30/1",
		"compositor name=mixer",
		"sink_0::xpos=0",
		"sink_0::ypos=0",
		"sink_0::width=1280",
		"sink_0::height=720",
		"sink_0::zorder=0",
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
		t.Errorf("the persistent pipeline must not create a studio source:\n%s", description)
	}
}

func TestMediaMixerSourceDescriptionUsesTheSourceRoom(t *testing.T) {
	config := config{
		LiveKitURL: "ws://livekit:7880",
		APIKey:     "key",
		APISecret:  "secret",
		InputRoom:  "studio",
		GuestRoom:  "guests",
		OutputRoom: "broadcast",
		Identity:   "media-mixer",
	}
	source := &studioVideo{
		participantIdentity: "guest-a",
		room:                "guests",
	}

	description := mediaMixerSourceDescription(config, "studio-src-1", source)

	if !strings.Contains(description, `signaller::room-name="guests"`) {
		t.Errorf("source description does not use the guest room:\n%s", description)
	}
}

func TestMediaMixerSourceDescriptionSubscribesToTheStudioParticipant(t *testing.T) {
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

	description := mediaMixerSourceDescription(config, "studio-src-1", source)

	expected := []string{
		`livekitwebrtcsrc name="studio-src-1"`,
		`signaller::room-name="studio"`,
		`signaller::identity="media-mixer-studio-src-1"`,
		`signaller::producer-peer-id="studio-publisher-video"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("source description does not contain %q:\n%s", want, description)
		}
	}
}

func TestMediaMixerPipelineDescriptionMixesStudioAudioIntoOneStereoTrack(t *testing.T) {
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
		"audiotestsrc is-live=true wave=silence",
		"audio/x-raw,format=F32LE,rate=48000,channels=2,layout=interleaved",
		"audiomixer name=audio-mixer",
		"opusenc",
		`audio-caps="audio/x-opus"`,
		"sink.",
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("pipeline description does not contain %q:\n%s", want, description)
		}
	}
}

func TestMediaMixerAudioSourceDescriptionSubscribesToTheStudioParticipant(t *testing.T) {
	config := config{
		LiveKitURL: "ws://livekit:7880",
		APIKey:     "key",
		APISecret:  "secret",
		InputRoom:  "studio",
		OutputRoom: "broadcast",
		Identity:   "media-mixer",
	}
	source := &audioSource{
		participantIdentity: "audio-publisher",
		participantName:     "Audio Publisher",
		participantSID:      "PA_123",
		publicationSID:      "TR_audio",
	}

	description := mediaMixerAudioSourceDescription(config, "audio-src-1", source)

	expected := []string{
		`livekitwebrtcsrc name="audio-src-1"`,
		`signaller::room-name="studio"`,
		`signaller::identity="media-mixer-audio-src-1"`,
		`signaller::producer-peer-id="audio-publisher"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("audio source description does not contain %q:\n%s", want, description)
		}
	}
}
