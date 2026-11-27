package livekitgst

import (
	"fmt"

	gstutil "github.com/storozhilov/crowdstage/shared/gstutil"
)

type Endpoint struct {
	URL       string
	APIKey    string
	APISecret string
	Room      string
	Identity  string
}

func SourceDescription(name string, endpoint Endpoint, producerPeerID string) string {
	identity := endpoint.Identity + "-" + name

	return fmt.Sprintf(`livekitwebrtcsrc name=%s
  signaller::ws-url=%s
  signaller::api-key=%s
  signaller::secret-key=%s
  signaller::room-name=%s
  signaller::identity=%s
  signaller::participant-name=%s
  signaller::producer-peer-id=%s
  stun-server=""`,
		gstutil.String(name),
		gstutil.String(endpoint.URL),
		gstutil.String(endpoint.APIKey),
		gstutil.String(endpoint.APISecret),
		gstutil.String(endpoint.Room),
		gstutil.String(identity),
		gstutil.String(identity),
		gstutil.String(producerPeerID),
	)
}

func SinkDescription(endpoint Endpoint, videoCaps string) string {
	return AudioVideoSinkDescription("", endpoint, videoCaps, "")
}

// AudioVideoSinkDescription describes a single LiveKit participant that
// publishes both a video and an audio track through one sink element.
func AudioVideoSinkDescription(name string, endpoint Endpoint, videoCaps, audioCaps string) string {
	element := "livekitwebrtcsink"
	if name != "" {
		element += " name=" + gstutil.String(name)
	}

	caps := ""
	if videoCaps != "" {
		caps += fmt.Sprintf("\n  video-caps=%s", gstutil.String(videoCaps))
	}
	if audioCaps != "" {
		caps += fmt.Sprintf("\n  audio-caps=%s", gstutil.String(audioCaps))
	}

	return fmt.Sprintf(`%s
  signaller::ws-url=%s
  signaller::api-key=%s
  signaller::secret-key=%s
  signaller::room-name=%s
  signaller::identity=%s
  signaller::participant-name=%s%s
  stun-server=""`,
		element,
		gstutil.String(endpoint.URL),
		gstutil.String(endpoint.APIKey),
		gstutil.String(endpoint.APISecret),
		gstutil.String(endpoint.Room),
		gstutil.String(endpoint.Identity),
		gstutil.String(endpoint.Identity),
		caps,
	)
}
