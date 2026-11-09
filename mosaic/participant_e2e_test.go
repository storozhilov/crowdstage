package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/livekit/server-sdk-go/v2/pkg/samplebuilder"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

const (
	participantJoinTimeout            = 30 * time.Second
	participantDisconnectTimeout      = 35 * time.Second
	participantTimeoutDisconnectLimit = 10 * time.Second
	participantTimeoutSeconds         = 3
	participantCycles                 = 3
	participantOverlayThreshold       = 10.0
	participantRestoredThreshold      = 3.0
)

type participantTestProcess struct {
	command *exec.Cmd
	output  *bytes.Buffer
}

type studioFrameStream struct {
	frames chan []byte
}

type frameRegion struct {
	xpos   int
	ypos   int
	width  int
	height int
}

func TestMosaicComposerShowsParticipantOverlayAcrossJoinDisconnectCycles(t *testing.T) {
	baseURL := os.Getenv("MOSAIC_URL")
	if baseURL == "" {
		t.Skip("MOSAIC_URL is not set; run ./ci/run_tests.sh against the dev stand")
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
	script := participantScriptPath(t)

	studioRoom := envOrDefault("MOSAIC_STUDIO_ROOM", "studio")
	composerIdentity := envOrDefault("MOSAIC_IDENTITY", "mosaic-composer")
	viewerIdentity := fmt.Sprintf("participant-viewer-%d", time.Now().UnixNano())

	postPipeline(t, baseURL, "/stop")
	postPipeline(t, baseURL, "/start")
	stopped := false
	defer func() {
		if !stopped {
			postPipeline(t, baseURL, "/stop")
		}
	}()

	viewer, track := joinBackgroundViewer(t, url, apiKey, apiSecret, studioRoom, viewerIdentity, composerIdentity)
	defer viewer.Disconnect()

	stream := newStudioFrameStream(track)
	baselineFrames := stream.collect(t, 45, 30*time.Second)
	baselineFrames = framesFromLastKeyframe(baselineFrames)
	baseline := lastFrame(t, decodeGrayFrames(t, writeIVF(t, baselineFrames)))
	region := participantFrameRegion()

	for cycle := 1; cycle <= participantCycles; cycle++ {
		timeoutSeconds := 0
		if cycle == 2 {
			timeoutSeconds = participantTimeoutSeconds
		}

		t.Logf("cycle %d: joining participant (timeout %ds)", cycle, timeoutSeconds)
		process := startParticipantProcess(t, script, cycle, timeoutSeconds)
		waitForParticipantOverlay(t, stream, baseline, region, true, participantJoinTimeout)

		t.Logf("cycle %d: disconnecting participant", cycle)
		if timeoutSeconds > 0 {
			waitForParticipantOverlay(t, stream, baseline, region, false, participantTimeoutDisconnectLimit)
			stopParticipantProcess(t, process)
		} else {
			stopParticipantProcess(t, process)
			waitForParticipantOverlay(t, stream, baseline, region, false, participantDisconnectTimeout)
		}
	}

	postPipeline(t, baseURL, "/stop")
	stopped = true
	waitForNoComposerVideo(t, viewer, composerIdentity)
}

func TestMosaicComposerShowsParticipantGridAcrossRandomJoinLeave(t *testing.T) {
	baseURL := os.Getenv("MOSAIC_URL")
	if baseURL == "" {
		t.Skip("MOSAIC_URL is not set; run ./ci/run_tests.sh against the dev stand")
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
	script := participantScriptPath(t)

	studioRoom := envOrDefault("MOSAIC_STUDIO_ROOM", "studio")
	composerIdentity := envOrDefault("MOSAIC_IDENTITY", "mosaic-composer")
	viewerIdentity := fmt.Sprintf("participant-grid-viewer-%d", time.Now().UnixNano())

	postPipeline(t, baseURL, "/stop")
	postPipeline(t, baseURL, "/start")
	stopped := false
	defer func() {
		if !stopped {
			postPipeline(t, baseURL, "/stop")
		}
	}()

	viewer, track := joinBackgroundViewer(t, url, apiKey, apiSecret, studioRoom, viewerIdentity, composerIdentity)
	defer viewer.Disconnect()

	stream := newStudioFrameStream(track)
	baselineFrames := stream.collect(t, 45, 30*time.Second)
	baselineFrames = framesFromLastKeyframe(baselineFrames)
	baseline := lastFrame(t, decodeGrayFrames(t, writeIVF(t, baselineFrames)))
	regions := participantGridRegions()

	rng := rand.New(rand.NewSource(45))
	nextCycle := 1
	active := make([]*participantTestProcess, 0, maxParticipantTiles)

	joinParticipant := func() {
		process := startParticipantProcess(t, script, nextCycle, 0)
		nextCycle++
		active = append(active, process)
		t.Logf("join: %d active participant(s)", len(active))
		waitForParticipantTileCount(t, stream, baseline, regions, len(active), participantJoinTimeout)
	}
	leaveParticipant := func() {
		index := rng.Intn(len(active))
		process := active[index]
		active = append(active[:index], active[index+1:]...)
		t.Logf("leave: %d active participant(s)", len(active))
		stopParticipantProcess(t, process)
		if len(active) == 0 {
			waitForParticipantTileCount(t, stream, baseline, regions, 0, participantDisconnectTimeout)
			return
		}
		waitForParticipantTileCountStable(t, stream, baseline, regions, len(active), participantDisconnectTimeout)
	}

	for len(active) < maxParticipantTiles {
		joinParticipant()
	}

	for step := 0; step < 6; step++ {
		switch {
		case len(active) == 0:
			joinParticipant()
		case len(active) == maxParticipantTiles:
			leaveParticipant()
		case rng.Intn(2) == 0:
			joinParticipant()
		default:
			leaveParticipant()
		}
	}
	for len(active) > 0 {
		leaveParticipant()
	}

	postPipeline(t, baseURL, "/stop")
	stopped = true
	waitForNoComposerVideo(t, viewer, composerIdentity)
}

func participantScriptPath(t *testing.T) string {
	t.Helper()

	script := os.Getenv("MOSAIC_PARTICIPANT_SCRIPT")
	if script == "" {
		if _, err := os.Stat("/var/run/docker.sock"); err != nil {
			t.Skip("Docker socket is not available; run ./ci/run_tests.sh with Docker access")
		}
		script = filepath.Join("..", "ci", "mosaic", "run_participant.sh")
	}

	absolute, err := filepath.Abs(script)
	if err != nil {
		t.Fatalf("resolve participant script path %q: %v", script, err)
	}
	if _, err := os.Stat(absolute); err != nil {
		t.Fatalf("participant script is not available at %s: %v", absolute, err)
	}
	return absolute
}

func startParticipantProcess(t *testing.T, script string, cycle int, timeoutSeconds int) *participantTestProcess {
	t.Helper()

	command := exec.Command(script)
	if timeoutSeconds > 0 {
		command.Args = append(command.Args, fmt.Sprintf("%d", timeoutSeconds))
	}
	command.Env = append(
		os.Environ(),
		fmt.Sprintf("MOSAIC_PARTICIPANT_IDENTITY=participant-e2e-%d-%d", time.Now().UnixNano(), cycle),
		"MOSAIC_PARTICIPANT_PATTERN=smpte",
	)

	output := &bytes.Buffer{}
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		t.Fatalf("start participant script %s: %v", script, err)
	}

	process := &participantTestProcess{command: command, output: output}
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
			t.Logf("participant output:\n%s", output.String())
		}
	})
	return process
}

func stopParticipantProcess(t *testing.T, process *participantTestProcess) {
	t.Helper()

	if process == nil || process.command.Process == nil {
		return
	}

	if err := process.command.Process.Signal(os.Interrupt); err != nil {
		t.Logf("send interrupt to participant script: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- process.command.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("participant script exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		_ = process.command.Process.Kill()
		<-done
		t.Fatalf("timed out stopping participant script\n%s", process.output.String())
	}

	if t.Failed() {
		t.Logf("participant output:\n%s", process.output.String())
	}
}

func newStudioFrameStream(track *webrtc.TrackRemote) *studioFrameStream {
	stream := &studioFrameStream{frames: make(chan []byte, 128)}

	go func() {
		defer close(stream.frames)

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
				case stream.frames <- append([]byte(nil), sample.Data...):
				default:
				}
			}
		}
	}()

	return stream
}

func (stream *studioFrameStream) collect(t *testing.T, count int, timeout time.Duration) [][]byte {
	t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	collected := make([][]byte, 0, count)
	for len(collected) < count {
		select {
		case frame, ok := <-stream.frames:
			if !ok {
				t.Fatal("studio video track ended while waiting for frames")
			}
			collected = append(collected, frame)
		case <-deadline.C:
			t.Fatalf("timed out waiting for participant test frames: received %d of %d", len(collected), count)
		}
	}
	return collected
}

func lastFrame(t *testing.T, frames [][]byte) []byte {
	t.Helper()

	if len(frames) == 0 {
		t.Fatal("no frames were collected")
	}
	return frames[len(frames)-1]
}

func participantFrameRegion() frameRegion {
	xpos, ypos, width, height := participantOverlayPosition()
	return frameRegion{xpos: xpos, ypos: ypos, width: width, height: height}
}

func participantGridRegions() []frameRegion {
	regions := make([]frameRegion, 0, maxParticipantTiles)
	for slot := 0; slot < maxParticipantTiles; slot++ {
		xpos, ypos, width, height := participantGridPosition(slot)
		regions = append(regions, frameRegion{xpos: xpos, ypos: ypos, width: width, height: height})
	}
	return regions
}

func waitForParticipantOverlay(t *testing.T, stream *studioFrameStream, baseline []byte, region frameRegion, present bool, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastDifference := -1.0

	for {
		if time.Now().After(deadline) {
			state := "appear"
			if !present {
				state = "disappear"
			}
			t.Fatalf(
				"timed out waiting for the participant overlay to %s (last mean luma difference %.3f)",
				state,
				lastDifference,
			)
		}

		frames := stream.collect(t, 45, 15*time.Second)
		frames = framesFromLastKeyframe(frames)
		decoded := decodeGrayFrames(t, writeIVF(t, frames))
		if len(decoded) == 0 {
			continue
		}

		lastDifference = meanRegionDifference(decoded[len(decoded)-1], baseline, region)
		if present && lastDifference > participantOverlayThreshold {
			return
		}
		if !present && lastDifference < participantRestoredThreshold {
			return
		}
	}
}

func waitForParticipantTileCount(t *testing.T, stream *studioFrameStream, baseline []byte, regions []frameRegion, expected int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastCount := -1

	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d participant tile(s), last count %d", expected, lastCount)
		}

		frames := stream.collect(t, 45, 15*time.Second)
		frames = framesFromLastKeyframe(frames)
		decoded := decodeGrayFrames(t, writeIVF(t, frames))
		if len(decoded) == 0 {
			continue
		}

		lastCount = countParticipantTiles(decoded[len(decoded)-1], baseline, regions)
		if lastCount == expected {
			return
		}
	}
}

func waitForParticipantTileCountStable(t *testing.T, stream *studioFrameStream, baseline []byte, regions []frameRegion, expected int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastCount := -1

	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d stable participant tile(s), last count %d", expected, lastCount)
		}

		frames := stream.collect(t, 45, 15*time.Second)
		frames = framesFromLastKeyframe(frames)
		decoded := decodeGrayFrames(t, writeIVF(t, frames))
		for _, frame := range decoded {
			count := countParticipantTiles(frame, baseline, regions)
			if count < expected {
				t.Fatalf("participant tiles dropped to %d, want at least %d while a participant leaves", count, expected)
			}
			lastCount = count
		}
		if lastCount == expected {
			return
		}
	}
}

func countParticipantTiles(frame, baseline []byte, regions []frameRegion) int {
	count := 0
	for _, region := range regions {
		if meanRegionDifference(frame, baseline, region) > participantOverlayThreshold {
			count++
		}
	}
	return count
}

func framesFromLastKeyframe(frames [][]byte) [][]byte {
	start := 0
	for index, frame := range frames {
		if len(frame) > 0 && frame[0]&0x01 == 0 {
			start = index
		}
	}
	return frames[start:]
}

func decodeGrayFrames(t *testing.T, path string) [][]byte {
	t.Helper()

	var stderr bytes.Buffer
	command := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-err_detect", "ignore_err",
		"-max_error_rate", "1",
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

	frameSize := backgroundWidth * backgroundHeight
	if len(output)%frameSize != 0 {
		t.Fatalf("decoded output size %d is not a multiple of frame size %d", len(output), frameSize)
	}

	frames := make([][]byte, 0, len(output)/frameSize)
	for offset := 0; offset+frameSize <= len(output); offset += frameSize {
		frames = append(frames, output[offset:offset+frameSize])
	}
	return frames
}

func meanRegionDifference(frame, reference []byte, region frameRegion) float64 {
	var totalDifference uint64
	var count uint64

	for y := region.ypos; y < region.ypos+region.height; y++ {
		row := y * backgroundWidth
		for x := region.xpos; x < region.xpos+region.width; x++ {
			totalDifference += uint64(absoluteDifference(frame[row+x], reference[row+x]))
			count++
		}
	}

	return float64(totalDifference) / float64(count)
}
