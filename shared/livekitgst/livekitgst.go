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
	return fmt.Sprintf(`livekitwebrtcsink
  signaller::ws-url=%s
  signaller::api-key=%s
  signaller::secret-key=%s
  signaller::room-name=%s
  signaller::identity=%s
  signaller::participant-name=%s
  video-caps=%s
  stun-server=""`,
		gstutil.String(endpoint.URL),
		gstutil.String(endpoint.APIKey),
		gstutil.String(endpoint.APISecret),
		gstutil.String(endpoint.Room),
		gstutil.String(endpoint.Identity),
		gstutil.String(endpoint.Identity),
		gstutil.String(videoCaps),
	)
}
