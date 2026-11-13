package livekitgst

import (
	"strings"
	"testing"
)

func TestSourceDescriptionSubscribesToProducer(t *testing.T) {
	description := SourceDescription("studio-src-1", Endpoint{
		URL:       "ws://livekit:7880",
		APIKey:    "key",
		APISecret: "secret",
		Room:      "studio",
		Identity:  "media-mixer",
	}, "publisher-a")

	expected := []string{
		`livekitwebrtcsrc name="studio-src-1"`,
		`signaller::ws-url="ws://livekit:7880"`,
		`signaller::room-name="studio"`,
		`signaller::identity="media-mixer-studio-src-1"`,
		`signaller::producer-peer-id="publisher-a"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("source description does not contain %q:\n%s", want, description)
		}
	}
}

func TestSinkDescriptionPublishesToOutputRoom(t *testing.T) {
	description := SinkDescription(Endpoint{
		URL:       "ws://livekit:7880",
		APIKey:    "key",
		APISecret: "secret",
		Room:      "broadcast",
		Identity:  "media-mixer",
	}, "video/x-vp8")

	expected := []string{
		"livekitwebrtcsink",
		`signaller::room-name="broadcast"`,
		`signaller::identity="media-mixer"`,
		`video-caps="video/x-vp8"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("sink description does not contain %q:\n%s", want, description)
		}
	}
}
