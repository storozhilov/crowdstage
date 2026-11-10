package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/livekit/server-sdk-go/v2/pkg/samplebuilder"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

const mixerTrackTimeout = 45 * time.Second

type mixerTestViewer struct {
	room  *lksdk.Room
	video chan *webrtc.TrackRemote
	audio chan *webrtc.TrackRemote
}

type mixerTestPublisher struct {
	command *exec.Cmd
	output  *bytes.Buffer
}

func TestMediaMixerForwardsAudioAndVideoToTargetRoom(t *testing.T) {
	baseURL := os.Getenv("MEDIA_MIXER_URL")
	if baseURL == "" {
		t.Skip("MEDIA_MIXER_URL is not set; run ./ci/run_tests.sh against the dev stand")
	}
	url := os.Getenv("LIVEKIT_URL")
	if url == "" {
		t.Skip("LIVEKIT_URL is not set; run ./ci/run_tests.sh against the dev stand")
	}
	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		t.Fatal("LIVEKIT_API_KEY and LIVEKIT_API_SECRET are required")
	}

	sourceRoom := envOrDefault("MEDIA_MIXER_SOURCE_ROOM", "studio")
	targetRoom := envOrDefault("MEDIA_MIXER_TARGET_ROOM", "broadcast")
	mixerIdentity := envOrDefault("MEDIA_MIXER_IDENTITY", "media-mixer")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	postMixer(t, baseURL, "/stop")
	health := getMixerHealth(t, baseURL)
	if health.Subsystems.Mixer.State != mixerStateIdle {
		t.Fatalf("mixer state = %q, want %q before start", health.Subsystems.Mixer.State, mixerStateIdle)
	}
	if health.Subsystems.Mixer.ForwardedTracks != 0 {
		t.Fatalf("forwarded tracks = %d, want 0 before start", health.Subsystems.Mixer.ForwardedTracks)
	}

	postMixer(t, baseURL, "/start")
	stopped := false
	defer func() {
		if !stopped {
			postMixer(t, baseURL, "/stop")
		}
	}()

	health = getMixerHealth(t, baseURL)
	if health.Subsystems.Mixer.State != mixerStateRunning {
		t.Fatalf("mixer state = %q, want %q after start", health.Subsystems.Mixer.State, mixerStateRunning)
	}
	if health.Subsystems.Mixer.SourceRoom != sourceRoom || health.Subsystems.Mixer.TargetRoom != targetRoom {
		t.Fatalf(
			"mixer rooms = %q -> %q, want %q -> %q",
			health.Subsystems.Mixer.SourceRoom,
			health.Subsystems.Mixer.TargetRoom,
			sourceRoom,
			targetRoom,
		)
	}

	viewer := joinMixerViewer(t, url, apiKey, apiSecret, targetRoom, "media-mixer-viewer-"+suffix, mixerIdentity)
	defer viewer.room.Disconnect()

	videoPublisher := startMixerTestPublisher(t, "video", url, apiKey, apiSecret, sourceRoom, "media-mixer-video-"+suffix)
	defer stopMixerTestPublisher(t, videoPublisher)
	audioPublisher := startMixerTestPublisher(t, "audio", url, apiKey, apiSecret, sourceRoom, "media-mixer-audio-"+suffix)
	defer stopMixerTestPublisher(t, audioPublisher)

	videoTrack := waitForMixerTrack(t, viewer.video, "video", videoPublisher, audioPublisher)
	audioTrack := waitForMixerTrack(t, viewer.audio, "audio", videoPublisher, audioPublisher)

	if frame := readVP8Frame(t, videoTrack, mixerTrackTimeout); len(frame) == 0 {
		t.Fatal("forwarded video track produced an empty VP8 frame")
	}
	if packet := readRTPPacket(t, audioTrack, mixerTrackTimeout); len(packet) == 0 {
		t.Fatal("forwarded audio track produced an empty RTP packet")
	}

	waitForForwardedTracks(t, baseURL, 2, mixerTrackTimeout)

	postMixer(t, baseURL, "/stop")
	stopped = true

	health = getMixerHealth(t, baseURL)
	if health.Subsystems.Mixer.State != mixerStateIdle {
		t.Fatalf("mixer state = %q, want %q after stop", health.Subsystems.Mixer.State, mixerStateIdle)
	}
	if health.Subsystems.Mixer.ForwardedTracks != 0 {
		t.Fatalf("forwarded tracks = %d, want 0 after stop", health.Subsystems.Mixer.ForwardedTracks)
	}
	waitForNoMixerMedia(t, viewer.room, mixerIdentity, 20*time.Second)
}

func joinMixerViewer(t *testing.T, url, apiKey, apiSecret, roomName, identity, mixerIdentity string) *mixerTestViewer {
	t.Helper()

	viewer := &mixerTestViewer{
		video: make(chan *webrtc.TrackRemote, 1),
		audio: make(chan *webrtc.TrackRemote, 1),
	}
	var videoOnce, audioOnce sync.Once

	capture := func(track *webrtc.TrackRemote, _ *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
		if !isMixerParticipant(participant.Identity(), mixerIdentity) {
			return
		}
		switch track.Kind() {
		case webrtc.RTPCodecTypeVideo:
			videoOnce.Do(func() {
				viewer.video <- track
			})
		case webrtc.RTPCodecTypeAudio:
			audioOnce.Do(func() {
				viewer.audio <- track
			})
		}
	}

	subscribe := func(participant *lksdk.RemoteParticipant) {
		if !isMixerParticipant(participant.Identity(), mixerIdentity) {
			return
		}
		for _, publication := range participant.TrackPublications() {
			if publication.Kind() != lksdk.TrackKindVideo && publication.Kind() != lksdk.TrackKindAudio {
				continue
			}
			remotePublication, ok := publication.(*lksdk.RemoteTrackPublication)
			if !ok {
				continue
			}
			if track := remotePublication.TrackRemote(); remotePublication.IsSubscribed() && track != nil {
				capture(track, remotePublication, participant)
				continue
			}
			if err := remotePublication.SetSubscribed(true); err != nil {
				t.Logf("subscribe to mixer track: %v", err)
			}
		}
	}

	callback := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackPublished: func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
				if !isMixerParticipant(participant.Identity(), mixerIdentity) {
					return
				}
				if err := publication.SetSubscribed(true); err != nil {
					t.Logf("subscribe to published mixer track: %v", err)
				}
			},
			OnTrackSubscribed: capture,
		},
	}

	room, err := lksdk.ConnectToRoom(url, lksdk.ConnectInfo{
		APIKey:              apiKey,
		APISecret:           apiSecret,
		RoomName:            roomName,
		ParticipantIdentity: identity,
	}, callback, lksdk.WithAutoSubscribe(false))
	if err != nil {
		t.Fatalf("connect viewer to target room %q: %v", roomName, err)
	}
	viewer.room = room

	for _, participant := range room.GetRemoteParticipants() {
		subscribe(participant)
	}

	return viewer
}

func waitForMixerTrack(t *testing.T, tracks <-chan *webrtc.TrackRemote, kind string, publishers ...*mixerTestPublisher) *webrtc.TrackRemote {
	t.Helper()

	select {
	case track := <-tracks:
		return track
	case <-time.After(mixerTrackTimeout):
		for _, publisher := range publishers {
			t.Logf("%s publisher output:\n%s", kind, publisher.output.String())
		}
		t.Fatalf("timed out waiting for the forwarded %s track in the target room", kind)
		return nil
	}
}

func readVP8Frame(t *testing.T, track *webrtc.TrackRemote, timeout time.Duration) []byte {
	t.Helper()

	result := make(chan []byte, 1)
	go func() {
		builder := samplebuilder.New(1000, &codecs.VP8Packet{}, track.Codec().ClockRate)
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			builder.Push(packet)
			if sample := builder.Pop(); sample != nil {
				result <- append([]byte(nil), sample.Data...)
				return
			}
		}
	}()

	select {
	case frame := <-result:
		return frame
	case <-time.After(timeout):
		t.Fatal("timed out waiting for a forwarded VP8 frame")
		return nil
	}
}

func readRTPPacket(t *testing.T, track *webrtc.TrackRemote, timeout time.Duration) []byte {
	t.Helper()

	result := make(chan []byte, 1)
	go func() {
		packet, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		result <- packet.Payload
	}()

	select {
	case payload := <-result:
		return payload
	case <-time.After(timeout):
		t.Fatal("timed out waiting for a forwarded audio RTP packet")
		return nil
	}
}

func startMixerTestPublisher(t *testing.T, kind, url, apiKey, apiSecret, roomName, identity string) *mixerTestPublisher {
	t.Helper()

	var media string
	sinkArgs := []string{
		"livekitwebrtcsink",
		"signaller::ws-url=" + url,
		"signaller::api-key=" + apiKey,
		"signaller::secret-key=" + apiSecret,
		"signaller::room-name=" + roomName,
		"signaller::identity=" + identity,
		"signaller::participant-name=" + identity,
		`stun-server=""`,
	}
	switch kind {
	case "video":
		media = "videotestsrc is-live=true pattern=smpte ! " +
			"video/x-raw,width=320,height=240,framerate=30/1 ! " +
			"videoconvert ! " +
			"vp8enc deadline=1 keyframe-max-dist=30 target-bitrate=500000 ! " +
			"queue ! "
		sinkArgs = append(sinkArgs, "video-caps=video/x-vp8")
	case "audio":
		media = "audiotestsrc is-live=true wave=sine ! " +
			"audio/x-raw,rate=48000,channels=1 ! " +
			"audioconvert ! " +
			"audioresample ! " +
			"opusenc ! " +
			"queue ! "
		sinkArgs = append(sinkArgs, "audio-caps=audio/x-opus")
	default:
		t.Fatalf("unknown publisher kind %q", kind)
	}

	args := append([]string{"-e"}, strings.Fields(media)...)
	args = append(args, sinkArgs...)
	command := exec.Command("gst-launch-1.0", args...)
	output := &bytes.Buffer{}
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		t.Fatalf("start %s publisher: %v", kind, err)
	}

	publisher := &mixerTestPublisher{command: command, output: output}
	t.Cleanup(func() {
		if command.ProcessState != nil {
			return
		}
		stopMixerTestPublisher(t, publisher)
	})
	return publisher
}

func stopMixerTestPublisher(t *testing.T, publisher *mixerTestPublisher) {
	t.Helper()

	if publisher == nil || publisher.command.Process == nil || publisher.command.ProcessState != nil {
		return
	}
	if err := publisher.command.Process.Signal(os.Interrupt); err != nil {
		t.Logf("interrupt publisher: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- publisher.command.Wait()
	}()

	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "signal: interrupt") {
			t.Logf("publisher exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		_ = publisher.command.Process.Kill()
		<-done
		t.Logf("timed out stopping publisher\n%s", publisher.output.String())
	}
}

func waitForForwardedTracks(t *testing.T, baseURL string, expected int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	last := -1
	for {
		health := getMixerHealth(t, baseURL)
		last = health.Subsystems.Mixer.ForwardedTracks
		if last == expected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d forwarded track(s), last count %d", expected, last)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func waitForNoMixerMedia(t *testing.T, room *lksdk.Room, mixerIdentity string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if !hasMixerMedia(room, mixerIdentity) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the mixer tracks to be unpublished")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func hasMixerMedia(room *lksdk.Room, mixerIdentity string) bool {
	for _, participant := range room.GetRemoteParticipants() {
		if !isMixerParticipant(participant.Identity(), mixerIdentity) {
			continue
		}
		for _, publication := range participant.TrackPublications() {
			if publication.Kind() == lksdk.TrackKindVideo || publication.Kind() == lksdk.TrackKindAudio {
				return true
			}
		}
	}
	return false
}

func isMixerParticipant(identity, mixerIdentity string) bool {
	return identity == mixerIdentity || strings.HasPrefix(identity, mixerIdentity+"-")
}

func postMixer(t *testing.T, baseURL, path string) {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + path
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer response.Body.Close()

	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status = %d, want %d: %s", url, response.StatusCode, http.StatusOK, body.String())
	}

	var pipelineResponse PipelineResponse
	if err := json.Unmarshal(body.Bytes(), &pipelineResponse); err != nil {
		t.Fatalf("decode POST %s response: %v", url, err)
	}
	want := map[string]string{"/start": "started", "/stop": "stopped"}[path]
	if pipelineResponse.Status != want {
		t.Fatalf("POST %s status = %q, want %q", url, pipelineResponse.Status, want)
	}
}

func getMixerHealth(t *testing.T, baseURL string) HealthResponse {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + "/health"
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d", url, response.StatusCode, http.StatusOK)
	}
	if got := response.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}

	var health HealthResponse
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	return health
}
