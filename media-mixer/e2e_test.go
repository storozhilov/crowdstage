package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/livekit/server-sdk-go/v2/pkg/samplebuilder"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	sharedconfig "github.com/storozhilov/crowdstage/shared/config"
	sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"
)

const (
	mixerTestWidth        = 320
	mixerTestHeight       = 240
	mixerTestFrameRate    = 30
	mixerStaticTestWidth  = 1280
	mixerStaticTestHeight = 720
	mixerStaticDifference = 3.0
	mixerTrackTimeout     = 45 * time.Second
)

type mixerStudioProcess struct {
	command *exec.Cmd
	output  *bytes.Buffer
}

type mixerFrame struct {
	generation int
	data       []byte
}

type mixerVideoStream struct {
	mu         sync.Mutex
	generation int
	seen       map[string]struct{}
	frames     chan mixerFrame

	first     chan struct{}
	firstOnce sync.Once
}

func TestMediaMixerForwardsOldestStudioVideo(t *testing.T) {
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
	script := mixerStudioScriptPath(t)

	targetRoom := sharedconfig.EnvOrDefault("MEDIA_MIXER_TARGET_ROOM", "broadcast")
	mixerIdentity := sharedconfig.EnvOrDefault("MEDIA_MIXER_IDENTITY", "media-mixer")
	viewerIdentity := fmt.Sprintf("media-mixer-viewer-%d", time.Now().UnixNano())

	postMixer(t, baseURL, "/stop")
	health := getMixerHealth(t, baseURL)
	if health.Subsystems.Mixer.State != mixerStateIdle {
		t.Fatalf("mixer state = %q, want %q before start", health.Subsystems.Mixer.State, mixerStateIdle)
	}
	if health.Subsystems.Mixer.Inputs != 0 {
		t.Fatalf("mixer inputs = %d, want 0 before start", health.Subsystems.Mixer.Inputs)
	}
	if health.Subsystems.Mixer.Outputs != 1 {
		t.Fatalf("mixer outputs = %d, want 1 before start", health.Subsystems.Mixer.Outputs)
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
	if health.Subsystems.Mixer.Inputs != 0 {
		t.Fatalf("mixer inputs = %d, want 0 without studio video", health.Subsystems.Mixer.Inputs)
	}

	viewer, stream := joinMixerViewer(t, url, apiKey, apiSecret, targetRoom, viewerIdentity, mixerIdentity)
	defer viewer.Disconnect()
	generation := stream.waitForFirstTrack(t, mixerTrackTimeout)

	frames := stream.collect(t, 45, mixerTrackTimeout, generation)
	ivfPath := writeMixerIVF(t, frames, mixerStaticTestWidth, mixerStaticTestHeight)
	codec, width, height := probeMixerVideo(t, ivfPath)
	if codec != "vp8" {
		t.Fatalf("static mixer video codec = %q, want %q", codec, "vp8")
	}
	if width != mixerStaticTestWidth || height != mixerStaticTestHeight {
		t.Fatalf("static mixer video size = %dx%d, want %dx%d", width, height, mixerStaticTestWidth, mixerStaticTestHeight)
	}
	assertMixerStaticPicture(t, ivfPath, width, height)

	first := startMixerStudioProcess(t, script, "first", "gradient")
	waitForMixerInputs(t, baseURL, 1, mixerTrackTimeout)

	firstGeneration := generation + 1
	frames = stream.collect(t, 45, mixerTrackTimeout, firstGeneration)
	ivfPath = writeMixerIVF(t, frames, mixerTestWidth, mixerTestHeight)
	codec, width, height = probeMixerVideo(t, ivfPath)
	if codec != "vp8" {
		t.Fatalf("mixer video codec = %q, want %q", codec, "vp8")
	}
	if width != mixerTestWidth || height != mixerTestHeight {
		t.Fatalf("mixer video size = %dx%d, want %dx%d", width, height, mixerTestWidth, mixerTestHeight)
	}
	assertMixerStaticPicture(t, ivfPath, width, height)

	second := startMixerStudioProcess(t, script, "second", "ball")
	waitForMixerInputs(t, baseURL, 2, mixerTrackTimeout)
	stream.drain()

	frames = stream.collect(t, 45, mixerTrackTimeout, firstGeneration)
	ivfPath = writeMixerIVF(t, frames, mixerTestWidth, mixerTestHeight)
	assertMixerStaticPicture(t, ivfPath, width, height)

	stopMixerStudioProcess(t, first)
	waitForMixerInputs(t, baseURL, 1, mixerTrackTimeout)

	switchedGeneration := firstGeneration + 1
	frames = stream.collect(t, 45, mixerTrackTimeout, switchedGeneration)
	ivfPath = writeMixerIVF(t, frames, mixerTestWidth, mixerTestHeight)
	codec, width, height = probeMixerVideo(t, ivfPath)
	if codec != "vp8" {
		t.Fatalf("mixer video codec after switch = %q, want %q", codec, "vp8")
	}
	if width != mixerTestWidth || height != mixerTestHeight {
		t.Fatalf("mixer video size after switch = %dx%d, want %dx%d", width, height, mixerTestWidth, mixerTestHeight)
	}
	assertMixerMovingPicture(t, ivfPath, width, height)

	stopMixerStudioProcess(t, second)
	waitForMixerInputs(t, baseURL, 0, mixerTrackTimeout)

	fallbackGeneration := switchedGeneration + 1
	frames = stream.collect(t, 45, mixerTrackTimeout, fallbackGeneration)
	ivfPath = writeMixerIVF(t, frames, mixerStaticTestWidth, mixerStaticTestHeight)
	codec, width, height = probeMixerVideo(t, ivfPath)
	if codec != "vp8" {
		t.Fatalf("fallback mixer video codec = %q, want %q", codec, "vp8")
	}
	if width != mixerStaticTestWidth || height != mixerStaticTestHeight {
		t.Fatalf("fallback mixer video size = %dx%d, want %dx%d", width, height, mixerStaticTestWidth, mixerStaticTestHeight)
	}
	assertMixerStaticPicture(t, ivfPath, width, height)

	postMixer(t, baseURL, "/stop")
	stopped = true
	waitForNoMixerVideo(t, viewer, mixerIdentity)
}

func mixerStudioScriptPath(t *testing.T) string {
	t.Helper()

	script := os.Getenv("MEDIA_MIXER_STUDIO_SCRIPT")
	if script == "" {
		if _, err := os.Stat("/var/run/docker.sock"); err != nil {
			t.Skip("Docker socket is not available; run ./ci/run_tests.sh with Docker access")
		}
		script = filepath.Join("..", "ci", "media-mixer", "run_studio.sh")
	}

	absolute, err := filepath.Abs(script)
	if err != nil {
		t.Fatalf("resolve studio publisher script path %q: %v", script, err)
	}
	if _, err := os.Stat(absolute); err != nil {
		t.Fatalf("studio publisher script is not available at %s: %v", absolute, err)
	}
	return absolute
}

func startMixerStudioProcess(t *testing.T, script, name, pattern string) *mixerStudioProcess {
	t.Helper()

	command := exec.Command(script)
	command.Env = append(
		os.Environ(),
		fmt.Sprintf("MEDIA_MIXER_STUDIO_IDENTITY=studio-e2e-%s-%d", name, time.Now().UnixNano()),
		"MEDIA_MIXER_STUDIO_PATTERN="+pattern,
	)

	output := &bytes.Buffer{}
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		t.Fatalf("start studio publisher script %s: %v", script, err)
	}

	process := &mixerStudioProcess{command: command, output: output}
	t.Cleanup(func() {
		if command.ProcessState != nil {
			return
		}

		_ = command.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() {
			_, _ = command.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = command.Process.Kill()
			<-done
		}

		if t.Failed() {
			t.Logf("studio publisher output:\n%s", output.String())
		}
	})
	return process
}

func stopMixerStudioProcess(t *testing.T, process *mixerStudioProcess) {
	t.Helper()

	if process == nil || process.command.Process == nil {
		return
	}

	if err := process.command.Process.Signal(os.Interrupt); err != nil {
		t.Logf("send interrupt to studio publisher script: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- process.command.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("studio publisher script exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		_ = process.command.Process.Kill()
		<-done
		t.Fatalf("timed out stopping studio publisher script\n%s", process.output.String())
	}

	if t.Failed() {
		t.Logf("studio publisher output:\n%s", process.output.String())
	}
}

func joinMixerViewer(t *testing.T, url, apiKey, apiSecret, roomName, identity, mixerIdentity string) (*lksdk.Room, *mixerVideoStream) {
	t.Helper()

	stream := newMixerVideoStream()
	callback := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackPublished: func(publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
				if participant.Identity() != mixerIdentity || publication.Kind() != lksdk.TrackKindVideo {
					return
				}
				if err := publication.SetSubscribed(true); err != nil {
					t.Logf("subscribe to mixer video track: %v", err)
				}
			},
			OnTrackSubscribed: func(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
				if participant.Identity() != mixerIdentity || track.Kind() != webrtc.RTPCodecTypeVideo {
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
		t.Fatalf("connect viewer to target room %q: %v", roomName, err)
	}

	for _, participant := range room.GetRemoteParticipants() {
		if participant.Identity() != mixerIdentity {
			continue
		}
		for _, publication := range participant.TrackPublications() {
			if publication.Kind() != lksdk.TrackKindVideo {
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
				t.Fatalf("subscribe to existing mixer video track: %v", err)
			}
		}
	}

	return room, stream
}

func newMixerVideoStream() *mixerVideoStream {
	return &mixerVideoStream{
		seen:   make(map[string]struct{}),
		frames: make(chan mixerFrame, 256),
		first:  make(chan struct{}),
	}
}

func (s *mixerVideoStream) addTrack(track *webrtc.TrackRemote, publicationSID string) {
	s.mu.Lock()
	if _, seen := s.seen[publicationSID]; seen {
		s.mu.Unlock()
		return
	}
	s.seen[publicationSID] = struct{}{}
	s.generation++
	generation := s.generation
	s.mu.Unlock()

	s.firstOnce.Do(func() { close(s.first) })

	go func() {
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
				case s.frames <- mixerFrame{generation: generation, data: append([]byte(nil), sample.Data...)}:
				default:
				}
			}
		}
	}()
}

func (s *mixerVideoStream) currentGeneration() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation
}

func (s *mixerVideoStream) waitForFirstTrack(t *testing.T, timeout time.Duration) int {
	t.Helper()

	select {
	case <-s.first:
		return s.currentGeneration()
	case <-time.After(timeout):
		t.Fatal("timed out waiting for the mixer video track in the target room")
		return 0
	}
}

func (s *mixerVideoStream) drain() {
	for {
		select {
		case <-s.frames:
		default:
			return
		}
	}
}

func (s *mixerVideoStream) collect(t *testing.T, count int, timeout time.Duration, minimumGeneration int) [][]byte {
	t.Helper()

	deadline := time.After(timeout)
	collected := make([][]byte, 0, count)
	for len(collected) < count {
		select {
		case frame := <-s.frames:
			if frame.generation < minimumGeneration {
				continue
			}
			collected = append(collected, frame.data)
		case <-deadline:
			t.Fatalf(
				"timed out waiting for mixer video frames from generation %d: received %d of %d",
				minimumGeneration,
				len(collected),
				count,
			)
		}
	}
	return collected
}

func writeMixerIVF(t *testing.T, frames [][]byte, width, height int) string {
	t.Helper()

	path := t.TempDir() + "/mixer.ivf"
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create IVF file: %v", err)
	}
	defer file.Close()

	header := make([]byte, 32)
	copy(header[0:4], "DKIF")
	binary.LittleEndian.PutUint16(header[4:6], 0)
	binary.LittleEndian.PutUint16(header[6:8], 32)
	copy(header[8:12], "VP80")
	binary.LittleEndian.PutUint16(header[12:14], uint16(width))
	binary.LittleEndian.PutUint16(header[14:16], uint16(height))
	binary.LittleEndian.PutUint32(header[16:20], mixerTestFrameRate)
	binary.LittleEndian.PutUint32(header[20:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(len(frames)))
	if _, err := file.Write(header); err != nil {
		t.Fatalf("write IVF header: %v", err)
	}

	for index, frame := range frames {
		frameHeader := make([]byte, 12)
		binary.LittleEndian.PutUint32(frameHeader[0:4], uint32(len(frame)))
		binary.LittleEndian.PutUint64(frameHeader[4:12], uint64(index))
		if _, err := file.Write(frameHeader); err != nil {
			t.Fatalf("write IVF frame %d header: %v", index, err)
		}
		if _, err := file.Write(frame); err != nil {
			t.Fatalf("write IVF frame %d: %v", index, err)
		}
	}

	return path
}

func probeMixerVideo(t *testing.T, path string) (string, int, int) {
	t.Helper()

	output, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height",
		"-of", "default=noprint_wrappers=1",
		path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe %s: %v\n%s", path, err, output)
	}

	values := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			values[key] = value
		}
	}

	width, err := strconv.Atoi(values["width"])
	if err != nil {
		t.Fatalf("parse video width %q: %v", values["width"], err)
	}
	height, err := strconv.Atoi(values["height"])
	if err != nil {
		t.Fatalf("parse video height %q: %v", values["height"], err)
	}

	return values["codec_name"], width, height
}

func assertMixerStaticPicture(t *testing.T, path string, width, height int) {
	t.Helper()

	output := decodeMixerGrayFrames(t, path)
	frameSize := width * height
	frameCount := len(output) / frameSize
	if frameCount < 8 {
		t.Fatalf("decoded frame count = %d, want at least 8", frameCount)
	}

	startIndex := 3
	endIndex := frameCount - 2
	referenceIndex := (startIndex + endIndex) / 2
	reference := output[referenceIndex*frameSize : (referenceIndex+1)*frameSize]
	minimum, maximum := reference[0], reference[0]
	for _, value := range reference {
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	if maximum-minimum < 16 {
		t.Fatalf("the reference frame is nearly uniform (luma range %d); no picture was broadcast", maximum-minimum)
	}

	for index := startIndex; index < endIndex; index++ {
		frame := output[index*frameSize : (index+1)*frameSize]
		var totalDifference uint64
		for offset := range frame {
			totalDifference += uint64(mixerAbsoluteDifference(frame[offset], reference[offset]))
		}
		meanDifference := float64(totalDifference) / float64(frameSize)
		if meanDifference > mixerStaticDifference {
			t.Fatalf("decoded frame %d differs from the reference frame (mean luma difference %.3f); the picture is not static", index, meanDifference)
		}
	}
}

func assertMixerMovingPicture(t *testing.T, path string, width, height int) {
	t.Helper()

	output := decodeMixerGrayFrames(t, path)
	frameSize := width * height
	frameCount := len(output) / frameSize
	if frameCount < 8 {
		t.Fatalf("decoded frame count = %d, want at least 8", frameCount)
	}

	largestDifference := 0.0
	for index := 3; index < frameCount-2; index++ {
		previous := output[(index-1)*frameSize : index*frameSize]
		frame := output[index*frameSize : (index+1)*frameSize]
		var totalDifference uint64
		for offset := range frame {
			totalDifference += uint64(mixerAbsoluteDifference(frame[offset], previous[offset]))
		}
		meanDifference := float64(totalDifference) / float64(frameSize)
		if meanDifference > largestDifference {
			largestDifference = meanDifference
		}
	}
	if largestDifference <= 1 {
		t.Fatalf("consecutive decoded frames are nearly identical (largest mean luma difference %.3f); the picture is not moving", largestDifference)
	}
}

func decodeMixerGrayFrames(t *testing.T, path string) []byte {
	t.Helper()

	var stderr bytes.Buffer
	command := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-i", path,
		"-map", "0:v:0",
		"-vf", "format=gray",
		"-f", "rawvideo",
		"-",
	)
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("decode %s with ffmpeg: %v\n%s", path, err, stderr.String())
	}
	return output
}

func mixerAbsoluteDifference(a, b byte) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func waitForMixerInputs(t *testing.T, baseURL string, expected int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		health := getMixerHealth(t, baseURL)
		if health.Subsystems.Mixer.Inputs == expected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"timed out waiting for %d mixer input(s), last count %d",
				expected,
				health.Subsystems.Mixer.Inputs,
			)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func waitForNoMixerVideo(t *testing.T, room *lksdk.Room, mixerIdentity string) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if !hasMixerVideo(room, mixerIdentity) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the mixer video track to be unpublished")
		}
		<-ticker.C
	}
}

func hasMixerVideo(room *lksdk.Room, mixerIdentity string) bool {
	for _, participant := range room.GetRemoteParticipants() {
		if participant.Identity() != mixerIdentity {
			continue
		}
		for _, publication := range participant.TrackPublications() {
			if publication.Kind() == lksdk.TrackKindVideo {
				return true
			}
		}
	}
	return false
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

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read POST %s response: %v", url, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status = %d, want %d: %s", url, response.StatusCode, http.StatusOK, body)
	}

	var pipelineResponse sharedhttp.PipelineResponse
	if err := json.Unmarshal(body, &pipelineResponse); err != nil {
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
