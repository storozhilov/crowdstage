package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/livekit/server-sdk-go/v2/pkg/samplebuilder"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	forwardingFixture = "testdata/forwarding.ivf"
	forwardingWidth   = 96
	forwardingHeight  = 64
)

func TestForwardsParticipantMediaToStudio(t *testing.T) {
	url := os.Getenv("LIVEKIT_URL")
	if url == "" {
		t.Skip("LIVEKIT_URL is not set; run ./ci/run_tests.sh against the dev stand")
	}
	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		t.Fatal("LIVEKIT_API_KEY and LIVEKIT_API_SECRET are required")
	}
	baseURL := os.Getenv("MOSAIC_URL")
	if baseURL == "" {
		t.Skip("MOSAIC_URL is not set; run ./ci/run_tests.sh against the dev stand")
	}

	participantRoom := envOrDefault("MOSAIC_ROOM", "mosaic-composer")
	studioRoom := envOrDefault("MOSAIC_STUDIO_ROOM", "studio")
	composerIdentity := envOrDefault("MOSAIC_IDENTITY", "mosaic-composer")

	postPipeline(t, baseURL, "/start")
	defer postPipeline(t, baseURL, "/stop")

	frames := readIVFFrames(t, forwardingFixture)
	sourceFrames := make(map[string]struct{}, len(frames))
	for _, frame := range frames {
		sourceFrames[string(frame)] = struct{}{}
	}

	suffix := time.Now().UnixNano()
	publisherIdentity := fmt.Sprintf("forwarding-publisher-%d", suffix)
	viewerIdentity := fmt.Sprintf("forwarding-viewer-%d", suffix)
	trackName := fmt.Sprintf("forwarding-camera-%d", suffix)

	publisher, sourceTrack := joinForwardingPublisher(t, url, apiKey, apiSecret, participantRoom, publisherIdentity, trackName)
	defer publisher.Disconnect()

	stopPublishing := make(chan struct{})
	defer close(stopPublishing)
	go publishIVFFrames(sourceTrack, frames, stopPublishing)

	received := make(chan []byte, 256)
	viewer := joinForwardingViewer(t, url, apiKey, apiSecret, studioRoom, viewerIdentity, composerIdentity, trackName, received)
	defer viewer.Disconnect()

	deadline := time.After(30 * time.Second)
	for {
		select {
		case frame := <-received:
			if _, ok := sourceFrames[string(frame)]; !ok {
				t.Fatalf("received forwarded frame does not match any published frame")
			}
			t.Logf("received forwarded frame unchanged from the participant")
			return
		case <-deadline:
			t.Fatal("timed out waiting for forwarded participant media in the studio room")
		}
	}
}

func joinForwardingPublisher(t *testing.T, url, apiKey, apiSecret, roomName, identity, trackName string) (*lksdk.Room, *lksdk.LocalTrack) {
	t.Helper()

	room, err := lksdk.ConnectToRoom(url, lksdk.ConnectInfo{
		APIKey:              apiKey,
		APISecret:           apiSecret,
		RoomName:            roomName,
		ParticipantIdentity: identity,
	}, &lksdk.RoomCallback{}, lksdk.WithAutoSubscribe(false))
	if err != nil {
		t.Fatalf("connect publisher to participant room %q: %v", roomName, err)
	}

	track, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeVP8,
		ClockRate: 90000,
	})
	if err != nil {
		room.Disconnect()
		t.Fatalf("create publisher track: %v", err)
	}

	if _, err := room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{
		Name:        trackName,
		Source:      livekit.TrackSource_CAMERA,
		VideoWidth:  forwardingWidth,
		VideoHeight: forwardingHeight,
	}); err != nil {
		room.Disconnect()
		t.Fatalf("publish participant track: %v", err)
	}

	return room, track
}

func publishIVFFrames(track *lksdk.LocalTrack, frames [][]byte, stop <-chan struct{}) {
	ticker := time.NewTicker(33 * time.Millisecond)
	defer ticker.Stop()

	for index := 0; ; index++ {
		select {
		case <-stop:
			return
		default:
		}

		if err := track.WriteSample(media.Sample{
			Data:     frames[index%len(frames)],
			Duration: 33 * time.Millisecond,
		}, nil); err != nil {
			return
		}

		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

func joinForwardingViewer(t *testing.T, url, apiKey, apiSecret, roomName, identity, composerIdentity, trackName string, frames chan<- []byte) *lksdk.Room {
	t.Helper()

	var start sync.Once
	startReading := func(track *webrtc.TrackRemote) {
		start.Do(func() {
			go readVP8Frames(track, frames)
		})
	}

	callback := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackPublished: func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
				if participant.Identity() != composerIdentity || publication.Name() != trackName {
					return
				}
				if err := publication.SetSubscribed(true); err != nil {
					t.Logf("subscribe to forwarded track: %v", err)
				}
			},
			OnTrackSubscribed: func(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
				if participant.Identity() != composerIdentity || publication.Name() != trackName {
					return
				}
				startReading(track)
			},
		},
	}

	room, err := lksdk.ConnectToRoom(url, lksdk.ConnectInfo{
		APIKey:              apiKey,
		APISecret:           apiSecret,
		RoomName:            roomName,
		ParticipantIdentity: identity,
	}, callback, lksdk.WithAutoSubscribe(false))
	if err != nil {
		t.Fatalf("connect viewer to studio room %q: %v", roomName, err)
	}

	for _, participant := range room.GetRemoteParticipants() {
		if participant.Identity() != composerIdentity {
			continue
		}
		for _, publication := range participant.TrackPublications() {
			if publication.Name() != trackName {
				continue
			}
			remotePublication, ok := publication.(*lksdk.RemoteTrackPublication)
			if !ok {
				continue
			}
			if track := remotePublication.TrackRemote(); remotePublication.IsSubscribed() && track != nil {
				startReading(track)
				continue
			}
			if err := remotePublication.SetSubscribed(true); err != nil {
				room.Disconnect()
				t.Fatalf("subscribe to existing forwarded track: %v", err)
			}
		}
	}

	return room
}

func readVP8Frames(track *webrtc.TrackRemote, frames chan<- []byte) {
	builder := samplebuilder.New(1000, &codecs.VP8Packet{}, track.Codec().ClockRate)
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		builder.Push(packet)
		for {
			sample := builder.Pop()
			if sample == nil {
				break
			}
			select {
			case frames <- append([]byte(nil), sample.Data...):
			default:
			}
		}
	}
}

func readIVFFrames(t *testing.T, path string) [][]byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read IVF fixture %s: %v", path, err)
	}
	if len(data) < 32 || !bytes.Equal(data[:4], []byte("DKIF")) {
		t.Fatalf("%s is not an IVF file", path)
	}

	var frames [][]byte
	for offset := 32; offset+12 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 12
		if size <= 0 || offset+size > len(data) {
			t.Fatalf("invalid frame size %d at offset %d in %s", size, offset, path)
		}
		frames = append(frames, append([]byte(nil), data[offset:offset+size]...))
		offset += size
	}
	if len(frames) == 0 {
		t.Fatalf("%s contains no frames", path)
	}
	return frames
}
