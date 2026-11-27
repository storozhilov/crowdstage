package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
	sharedconfig "github.com/storozhilov/crowdstage/shared/config"
)

const (
	mixerAudioToneA           = 440.0
	mixerAudioToneB           = 880.0
	mixerAudioSampleRate      = 48000
	mixerAudioMinTone         = 0.005
	mixerAudioCollectDuration = 2 * time.Second
	mixerAudioMinPackets      = 10
)

type mixerAudioPacket struct {
	packet     *rtp.Packet
	receivedAt time.Time
}

type mixerAudioStream struct {
	mu      sync.Mutex
	seen    map[string]struct{}
	packets chan mixerAudioPacket

	first     chan struct{}
	firstOnce sync.Once
}

func TestMediaMixerForwardsStudioAudioMix(t *testing.T) {
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
	script := mixerAudioScriptPath(t)

	targetRoom := sharedconfig.EnvOrDefault("MEDIA_MIXER_TARGET_ROOM", "broadcast")
	mixerIdentity := sharedconfig.EnvOrDefault("MEDIA_MIXER_IDENTITY", "media-mixer")
	viewerIdentity := fmt.Sprintf("media-mixer-audio-viewer-%d", time.Now().UnixNano())

	postMixer(t, baseURL, "/stop")
	postMixer(t, baseURL, "/start")
	defer postMixer(t, baseURL, "/stop")

	viewer, stream := joinMixerAudioViewer(t, url, apiKey, apiSecret, targetRoom, viewerIdentity, mixerIdentity)
	defer viewer.Disconnect()
	stream.waitForFirstTrack(t, mixerTrackTimeout)

	first := startMixerAudioProcess(t, script, "first", "sine", "440")
	waitForMixerAudioSource(t, baseURL, first.identity, func(source AudioSourceHealth) bool {
		return !source.Muted && !source.Solo && source.Gain == defaultAudioGain && source.SignalLevel > 0
	}, mixerTrackTimeout)
	waitForMixerAudioTones(t, stream, "the first studio audio source", mixerTrackTimeout, func(tone map[float64]float64) bool {
		return tone[mixerAudioToneA] > mixerAudioMinTone &&
			tone[mixerAudioToneB] < tone[mixerAudioToneA]*0.25
	})

	second := startMixerAudioProcess(t, script, "second", "sine", "880")
	waitForMixerAudioSource(t, baseURL, second.identity, func(source AudioSourceHealth) bool {
		return !source.Muted && source.Gain == defaultAudioGain
	}, mixerTrackTimeout)
	waitForMixerAudioSources(t, baseURL, 2, mixerTrackTimeout)
	waitForMixerAudioTones(t, stream, "both studio audio sources", mixerTrackTimeout, func(tone map[float64]float64) bool {
		return tone[mixerAudioToneA] > mixerAudioMinTone &&
			tone[mixerAudioToneB] > mixerAudioMinTone
	})

	postMixerAudio(t, baseURL, first.identity, "mute", map[string]any{"muted": true})
	waitForMixerAudioSource(t, baseURL, first.identity, func(source AudioSourceHealth) bool {
		return source.Muted
	}, mixerTrackTimeout)
	waitForMixerAudioTones(t, stream, "the second audio source after muting the first", mixerTrackTimeout, func(tone map[float64]float64) bool {
		return tone[mixerAudioToneB] > mixerAudioMinTone &&
			tone[mixerAudioToneA] < tone[mixerAudioToneB]*0.25
	})

	postMixerAudio(t, baseURL, first.identity, "mute", map[string]any{"muted": false})
	waitForMixerAudioSource(t, baseURL, first.identity, func(source AudioSourceHealth) bool {
		return !source.Muted
	}, mixerTrackTimeout)
	waitForMixerAudioTones(t, stream, "both audio sources after unmuting the first", mixerTrackTimeout, func(tone map[float64]float64) bool {
		return tone[mixerAudioToneA] > mixerAudioMinTone &&
			tone[mixerAudioToneB] > mixerAudioMinTone
	})

	postMixerAudio(t, baseURL, second.identity, "solo", map[string]any{"solo": true})
	waitForMixerAudioSource(t, baseURL, second.identity, func(source AudioSourceHealth) bool {
		return source.Solo
	}, mixerTrackTimeout)
	waitForMixerAudioTones(t, stream, "the soloed second audio source", mixerTrackTimeout, func(tone map[float64]float64) bool {
		return tone[mixerAudioToneB] > mixerAudioMinTone &&
			tone[mixerAudioToneA] < tone[mixerAudioToneB]*0.25
	})

	postMixerAudio(t, baseURL, second.identity, "solo", map[string]any{"solo": false})
	waitForMixerAudioSource(t, baseURL, second.identity, func(source AudioSourceHealth) bool {
		return !source.Solo
	}, mixerTrackTimeout)
	waitForMixerAudioTones(t, stream, "both audio sources after unsoloing the second", mixerTrackTimeout, func(tone map[float64]float64) bool {
		return tone[mixerAudioToneA] > mixerAudioMinTone &&
			tone[mixerAudioToneB] > mixerAudioMinTone
	})

	postMixerAudio(t, baseURL, second.identity, "gain", map[string]any{"gain": 0.1})
	waitForMixerAudioSource(t, baseURL, second.identity, func(source AudioSourceHealth) bool {
		return source.Gain == 0.1
	}, mixerTrackTimeout)
	waitForMixerAudioTones(t, stream, "the second audio source at reduced gain", mixerTrackTimeout, func(tone map[float64]float64) bool {
		return tone[mixerAudioToneA] > mixerAudioMinTone &&
			tone[mixerAudioToneB] < tone[mixerAudioToneA]*0.25
	})

	stopMixerStudioProcess(t, second)
	stopMixerStudioProcess(t, first)
	waitForMixerAudioSources(t, baseURL, 0, mixerTrackTimeout)
}

func mixerAudioScriptPath(t *testing.T) string {
	t.Helper()

	script := os.Getenv("MEDIA_MIXER_AUDIO_SCRIPT")
	if script == "" {
		if _, err := os.Stat("/var/run/docker.sock"); err != nil {
			t.Skip("Docker socket is not available; run ./ci/run_tests.sh with Docker access")
		}
		script = filepath.Join("..", "ci", "media-mixer", "run_audio.sh")
	}

	absolute, err := filepath.Abs(script)
	if err != nil {
		t.Fatalf("resolve audio publisher script path %q: %v", script, err)
	}
	if _, err := os.Stat(absolute); err != nil {
		t.Fatalf("audio publisher script is not available at %s: %v", absolute, err)
	}
	return absolute
}

func startMixerAudioProcess(t *testing.T, script, name, wave, frequency string) *mixerStudioProcess {
	t.Helper()

	identity := fmt.Sprintf("audio-e2e-%s-%d", name, time.Now().UnixNano())
	command := exec.Command(script)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Env = append(
		os.Environ(),
		"MEDIA_MIXER_AUDIO_IDENTITY="+identity,
		"MEDIA_MIXER_AUDIO_WAVE="+wave,
		"MEDIA_MIXER_AUDIO_FREQUENCY="+frequency,
	)

	output := &bytes.Buffer{}
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		t.Fatalf("start audio publisher script %s: %v", script, err)
	}

	process := newMixerStudioProcess(command, output, identity)
	t.Cleanup(func() {
		if process.finished() {
			return
		}

		_ = command.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() {
			done <- process.wait()
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = command.Process.Kill()
			<-done
		}

		if t.Failed() {
			t.Logf("audio publisher output:\n%s", output.String())
		}
	})
	return process
}

func joinMixerAudioViewer(t *testing.T, url, apiKey, apiSecret, roomName, identity, mixerIdentity string) (*lksdk.Room, *mixerAudioStream) {
	t.Helper()

	stream := newMixerAudioStream()
	callback := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackPublished: func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
				if participant.Identity() != mixerIdentity || publication.Kind() != lksdk.TrackKindAudio {
					return
				}
				if err := publication.SetSubscribed(true); err != nil {
					t.Logf("subscribe to mixer audio track: %v", err)
				}
			},
			OnTrackSubscribed: func(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
				if participant.Identity() != mixerIdentity || track.Kind() != webrtc.RTPCodecTypeAudio {
					return
				}
				stream.addTrack(track, publication.SID())
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
		t.Fatalf("connect audio viewer to target room %q: %v", roomName, err)
	}

	for _, participant := range room.GetRemoteParticipants() {
		if participant.Identity() != mixerIdentity {
			continue
		}
		for _, publication := range participant.TrackPublications() {
			if publication.Kind() != lksdk.TrackKindAudio {
				continue
			}
			remotePublication, ok := publication.(*lksdk.RemoteTrackPublication)
			if !ok {
				continue
			}
			if track := remotePublication.TrackRemote(); remotePublication.IsSubscribed() && track != nil {
				stream.addTrack(track, remotePublication.SID())
				continue
			}
			if err := remotePublication.SetSubscribed(true); err != nil {
				room.Disconnect()
				t.Fatalf("subscribe to existing mixer audio track: %v", err)
			}
		}
	}

	return room, stream
}

func newMixerAudioStream() *mixerAudioStream {
	return &mixerAudioStream{
		seen:    make(map[string]struct{}),
		packets: make(chan mixerAudioPacket, 2048),
		first:   make(chan struct{}),
	}
}

func (s *mixerAudioStream) addTrack(track *webrtc.TrackRemote, publicationSID string) {
	s.mu.Lock()
	if _, seen := s.seen[publicationSID]; seen {
		s.mu.Unlock()
		return
	}
	s.seen[publicationSID] = struct{}{}
	s.mu.Unlock()

	s.firstOnce.Do(func() { close(s.first) })

	go func() {
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			select {
			case s.packets <- mixerAudioPacket{packet: packet.Clone(), receivedAt: time.Now()}:
			default:
			}
		}
	}()
}

func (s *mixerAudioStream) waitForFirstTrack(t *testing.T, timeout time.Duration) {
	t.Helper()

	select {
	case <-s.first:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for the mixer audio track in the target room")
	}
}

func (s *mixerAudioStream) drain() {
	for {
		select {
		case <-s.packets:
		default:
			return
		}
	}
}

func (s *mixerAudioStream) collectFor(t *testing.T, duration time.Duration) []mixerAudioPacket {
	t.Helper()

	deadline := time.NewTimer(duration)
	defer deadline.Stop()

	collected := make([]mixerAudioPacket, 0, 128)
	for {
		select {
		case packet := <-s.packets:
			collected = append(collected, packet)
		case <-deadline.C:
			return collected
		}
	}
}

func waitForMixerAudioTones(
	t *testing.T,
	stream *mixerAudioStream,
	description string,
	timeout time.Duration,
	accept func(map[float64]float64) bool,
) []float32 {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastTone := map[float64]float64{}
	for {
		stream.drain()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf(
				"timed out waiting for %s: tone strengths 440Hz=%.4f 880Hz=%.4f",
				description,
				lastTone[mixerAudioToneA],
				lastTone[mixerAudioToneB],
			)
		}
		if remaining > mixerAudioCollectDuration {
			remaining = mixerAudioCollectDuration
		}
		packets := stream.collectFor(t, remaining)
		if len(packets) < mixerAudioMinPackets {
			continue
		}
		samples := decodeMixerAudio(t, writeMixerAudioOgg(t, packets))
		lastTone = map[float64]float64{
			mixerAudioToneA: mixerToneMagnitude(samples, mixerAudioToneA),
			mixerAudioToneB: mixerToneMagnitude(samples, mixerAudioToneB),
		}
		if accept(lastTone) {
			return samples
		}
	}
}

func writeMixerAudioOgg(t *testing.T, packets []mixerAudioPacket) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "mixer.ogg")
	writer, err := oggwriter.New(path, mixerAudioSampleRate, 2)
	if err != nil {
		t.Fatalf("create OGG writer: %v", err)
	}
	for _, packet := range packets {
		if err := writer.WriteRTP(packet.packet); err != nil {
			_ = writer.Close()
			t.Fatalf("write mixer audio RTP packet: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close OGG writer: %v", err)
	}
	return path
}

func decodeMixerAudio(t *testing.T, path string) []float32 {
	t.Helper()

	var stderr bytes.Buffer
	command := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-i", path,
		"-map", "0:a:0",
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", mixerAudioSampleRate),
		"-f", "f32le",
		"-",
	)
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("decode %s with ffmpeg: %v\n%s", path, err, stderr.String())
	}

	samples := make([]float32, len(output)/4)
	for index := range samples {
		bits := binary.LittleEndian.Uint32(output[index*4 : (index+1)*4])
		samples[index] = math.Float32frombits(bits)
	}
	return samples
}

func mixerToneMagnitude(samples []float32, frequency float64) float64 {
	if len(samples) == 0 {
		return 0
	}

	coefficient := 2 * math.Cos(2*math.Pi*frequency/mixerAudioSampleRate)
	var first, second float64
	for _, sample := range samples {
		value := float64(sample) + coefficient*first - second
		second = first
		first = value
	}
	power := first*first + second*second - coefficient*first*second
	if power < 0 {
		power = 0
	}
	return math.Sqrt(power) / float64(len(samples))
}

func waitForMixerAudioSource(
	t *testing.T,
	baseURL, identity string,
	accept func(AudioSourceHealth) bool,
	timeout time.Duration,
) AudioSourceHealth {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var last AudioSourceHealth
	for {
		health := getMixerHealthForAudioWait(t, baseURL)
		for _, source := range health.Subsystems.Mixer.AudioSources {
			if source.ParticipantIdentity != identity {
				continue
			}
			last = source
			if accept(source) {
				return source
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for audio source %q, last %+v", identity, last)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func waitForMixerAudioSources(t *testing.T, baseURL string, expected int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		health := getMixerHealthForAudioWait(t, baseURL)
		if got := len(health.Subsystems.Mixer.AudioSources); got == expected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"timed out waiting for %d audio source(s), last %+v",
				expected,
				health.Subsystems.Mixer.AudioSources,
			)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func postMixerAudio(t *testing.T, baseURL, identity, action string, body map[string]any) {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode audio %s request: %v", action, err)
	}
	url := strings.TrimRight(baseURL, "/") + "/audio/" + identity + "/" + action
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read POST %s response: %v", url, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status = %d, want %d: %s", url, response.StatusCode, http.StatusOK, responseBody)
	}
}

func getMixerHealthForAudioWait(t *testing.T, baseURL string) HealthResponse {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + "/health"
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET %s status = %d, want 200 or 503", url, response.StatusCode)
	}

	var health HealthResponse
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	return health
}
