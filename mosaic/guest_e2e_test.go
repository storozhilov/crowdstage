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
	sharedconfig "github.com/storozhilov/crowdstage/shared/config"
)

const (
	guestJoinTimeout            = 30 * time.Second
	guestDisconnectTimeout      = 35 * time.Second
	guestTimeoutDisconnectLimit = 10 * time.Second
	guestTimeoutSeconds         = 3
	guestCycles                 = 3
	guestGridE2ELimit           = 6
	guestOverlayThreshold       = 10.0
	guestRestoredThreshold      = 3.0
)

type guestTestProcess struct {
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

func TestMosaicComposerShowsGuestOverlayAcrossJoinDisconnectCycles(t *testing.T) {
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
	script := guestScriptPath(t)

	studioRoom := sharedconfig.EnvOrDefault("MOSAIC_TARGET_ROOM", "mosaic")
	composerIdentity := sharedconfig.EnvOrDefault("MOSAIC_IDENTITY", "mosaic-composer")
	viewerIdentity := fmt.Sprintf("guest-viewer-%d", time.Now().UnixNano())

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
	xpos, ypos, width, height := guestGridPosition(0)
	region := frameRegion{xpos: xpos, ypos: ypos, width: width, height: height + mosaicLabelHeight}

	for cycle := 1; cycle <= guestCycles; cycle++ {
		timeoutSeconds := 0
		if cycle == 2 {
			timeoutSeconds = guestTimeoutSeconds
		}

		t.Logf("cycle %d: joining guest (timeout %ds)", cycle, timeoutSeconds)
		process := startGuestProcess(t, script, cycle, timeoutSeconds)
		waitForGuestOverlay(t, stream, baseline, region, true, guestJoinTimeout)

		t.Logf("cycle %d: disconnecting guest", cycle)
		if timeoutSeconds > 0 {
			waitForGuestOverlay(t, stream, baseline, region, false, guestTimeoutDisconnectLimit)
			stopGuestProcess(t, process)
		} else {
			stopGuestProcess(t, process)
			waitForGuestOverlay(t, stream, baseline, region, false, guestDisconnectTimeout)
		}
	}

	postPipeline(t, baseURL, "/stop")
	stopped = true
	waitForNoComposerVideo(t, viewer, composerIdentity)
}

func TestMosaicComposerShowsGuestGridAcrossRandomJoinLeave(t *testing.T) {
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
	script := guestScriptPath(t)

	studioRoom := sharedconfig.EnvOrDefault("MOSAIC_TARGET_ROOM", "mosaic")
	composerIdentity := sharedconfig.EnvOrDefault("MOSAIC_IDENTITY", "mosaic-composer")
	viewerIdentity := fmt.Sprintf("guest-grid-viewer-%d", time.Now().UnixNano())

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
	regions := guestGridRegions()

	rng := rand.New(rand.NewSource(45))
	nextCycle := 1
	active := make([]*guestTestProcess, 0, maxGuestTiles)

	joinGuest := func() {
		process := startGuestProcess(t, script, nextCycle, 0)
		nextCycle++
		active = append(active, process)
		t.Logf("join: %d active guest(s)", len(active))
		waitForGuestTileCount(t, stream, baseline, regions, len(active), guestJoinTimeout)
	}
	leaveGuest := func() {
		index := rng.Intn(len(active))
		process := active[index]
		active = append(active[:index], active[index+1:]...)
		t.Logf("leave: %d active guest(s)", len(active))
		stopGuestProcess(t, process)
		if len(active) == 0 {
			waitForGuestTileCount(t, stream, baseline, regions, 0, guestDisconnectTimeout)
			return
		}
		waitForGuestTileCountStable(t, stream, baseline, regions, len(active), guestDisconnectTimeout)
	}

	for len(active) < guestGridE2ELimit {
		joinGuest()
	}

	health := getHealth(t, baseURL)
	if health.Subsystems.Mosaic.Guests != len(active) {
		t.Fatalf("mosaic guest count = %d, want %d", health.Subsystems.Mosaic.Guests, len(active))
	}

	for step := 0; step < 6; step++ {
		switch {
		case len(active) == 0:
			joinGuest()
		case len(active) == guestGridE2ELimit:
			leaveGuest()
		case rng.Intn(2) == 0:
			joinGuest()
		default:
			leaveGuest()
		}
	}
	for len(active) > 0 {
		leaveGuest()
	}

	postPipeline(t, baseURL, "/stop")
	stopped = true
	waitForNoComposerVideo(t, viewer, composerIdentity)
}

func TestMosaicComposerRotatesGuestsThroughSmallGrid(t *testing.T) {
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
	script := guestScriptPath(t)

	studioRoom := sharedconfig.EnvOrDefault("MOSAIC_TARGET_ROOM", "mosaic")
	composerIdentity := sharedconfig.EnvOrDefault("MOSAIC_IDENTITY", "mosaic-composer")
	viewerIdentity := fmt.Sprintf("guest-rotation-viewer-%d", time.Now().UnixNano())

	postPipeline(t, baseURL, "/stop")
	postPipelineWithGridAndRotation(t, baseURL, "/start", 2, 2, 2)
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
	regions := guestGridRegionsForGrid(2, 2)
	active := make([]*guestTestProcess, 0, 5)

	for cycle := 1; cycle <= 5; cycle++ {
		process := startGuestProcess(t, script, cycle, 0)
		active = append(active, process)
		expectedTiles := cycle
		if expectedTiles > 4 {
			expectedTiles = 4
		}
		t.Logf("join: %d active guest(s), %d tile(s)", len(active), expectedTiles)
		waitForGuestTileCount(t, stream, baseline, regions, expectedTiles, guestJoinTimeout)
	}

	waitForMosaicGuests(t, baseURL, len(active), guestJoinTimeout)
	health := getHealth(t, baseURL)
	if got := health.Subsystems.Mosaic.RotationTimeoutSeconds; got != 2 {
		t.Fatalf("mosaic rotation timeout = %d, want 2", got)
	}

	referenceFrames := stream.collect(t, 45, 30*time.Second)
	referenceFrames = framesFromLastKeyframe(referenceFrames)
	reference := lastFrame(t, decodeGrayFrames(t, writeIVF(t, referenceFrames)))
	last := waitForGuestRotation(t, stream, baseline, reference, regions, regions[0], guestJoinTimeout)

	if count := countGuestTiles(last, baseline, regions); count != 4 {
		t.Fatalf("guest tile count = %d after rotation, want 4", count)
	}
	if difference := meanRegionDifference(last, reference, regions[0]); difference <= guestOverlayThreshold {
		t.Fatalf("rotated tile still matches the previous guest (mean luma difference %.3f)", difference)
	}

	for _, process := range active {
		stopGuestProcess(t, process)
	}
	postPipeline(t, baseURL, "/stop")
	stopped = true
	waitForNoComposerVideo(t, viewer, composerIdentity)
}

func guestScriptPath(t *testing.T) string {
	t.Helper()

	script := os.Getenv("MOSAIC_GUEST_SCRIPT")
	if script == "" {
		if _, err := os.Stat("/var/run/docker.sock"); err != nil {
			t.Skip("Docker socket is not available; run ./ci/run_tests.sh with Docker access")
		}
		script = filepath.Join("..", "ci", "mosaic", "run_guest.sh")
	}

	absolute, err := filepath.Abs(script)
	if err != nil {
		t.Fatalf("resolve guest script path %q: %v", script, err)
	}
	if _, err := os.Stat(absolute); err != nil {
		t.Fatalf("guest script is not available at %s: %v", absolute, err)
	}
	return absolute
}

func startGuestProcess(t *testing.T, script string, cycle int, timeoutSeconds int) *guestTestProcess {
	t.Helper()

	command := exec.Command(script)
	command.Args = append(command.Args, "--name", fmt.Sprintf("Guest %d", cycle))
	if timeoutSeconds > 0 {
		command.Args = append(command.Args, fmt.Sprintf("%d", timeoutSeconds))
	}
	command.Env = append(
		os.Environ(),
		fmt.Sprintf("MOSAIC_GUEST_IDENTITY=guest-e2e-%d-%d", time.Now().UnixNano(), cycle),
		"MOSAIC_GUEST_PATTERN="+guestPatternForCycle(cycle),
	)

	output := &bytes.Buffer{}
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		t.Fatalf("start guest script %s: %v", script, err)
	}

	process := &guestTestProcess{command: command, output: output}
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
			t.Logf("guest output:\n%s", output.String())
		}
	})
	return process
}

func stopGuestProcess(t *testing.T, process *guestTestProcess) {
	t.Helper()

	if process == nil || process.command.Process == nil {
		return
	}

	if err := process.command.Process.Signal(os.Interrupt); err != nil {
		t.Logf("send interrupt to guest script: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- process.command.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("guest script exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		_ = process.command.Process.Kill()
		<-done
		t.Fatalf("timed out stopping guest script\n%s", process.output.String())
	}

	if t.Failed() {
		t.Logf("guest output:\n%s", process.output.String())
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
			t.Fatalf("timed out waiting for guest test frames: received %d of %d", len(collected), count)
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

func guestPatternForCycle(cycle int) string {
	patterns := []string{"smpte", "smpte75", "colors", "gradient", "checkers-4", "smpte100"}
	return patterns[(cycle-1)%len(patterns)]
}

func guestGridRegions() []frameRegion {
	return guestGridRegionsForGrid(mosaicColumns, mosaicRows)
}

func guestGridRegionsForGrid(columns, rows int) []frameRegion {
	regions := make([]frameRegion, 0, columns*rows)
	for slot := 0; slot < columns*rows; slot++ {
		xpos, ypos, width, height := guestGridPositionForGrid(slot, columns, rows)
		regions = append(regions, frameRegion{xpos: xpos, ypos: ypos, width: width, height: height + mosaicLabelHeight})
	}
	return regions
}

func waitForMosaicGuests(t *testing.T, baseURL string, expected int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		health := getHealth(t, baseURL)
		if health.Subsystems.Mosaic.Guests == expected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"timed out waiting for %d mosaic guest(s), last count %d",
				expected,
				health.Subsystems.Mosaic.Guests,
			)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func waitForGuestRotation(t *testing.T, stream *studioFrameStream, baseline, reference []byte, regions []frameRegion, rotatedRegion frameRegion, timeout time.Duration) []byte {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastCount := -1
	lastDifference := -1.0

	for {
		if time.Now().After(deadline) {
			t.Fatalf(
				"timed out waiting for guest rotation (last tile count %d, last mean luma difference %.3f)",
				lastCount,
				lastDifference,
			)
		}

		frames := stream.collect(t, 45, 15*time.Second)
		frames = framesFromLastKeyframe(frames)
		decoded := decodeGrayFrames(t, writeIVF(t, frames))
		for _, frame := range decoded {
			lastCount = countGuestTiles(frame, baseline, regions)
			lastDifference = meanRegionDifference(frame, reference, rotatedRegion)
			if lastCount == len(regions) && lastDifference > guestOverlayThreshold {
				return frame
			}
		}
	}
}

func waitForGuestOverlay(t *testing.T, stream *studioFrameStream, baseline []byte, region frameRegion, present bool, timeout time.Duration) {
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
				"timed out waiting for the guest overlay to %s (last mean luma difference %.3f)",
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
		if present && lastDifference > guestOverlayThreshold {
			return
		}
		if !present && lastDifference < guestRestoredThreshold {
			return
		}
	}
}

func waitForGuestTileCount(t *testing.T, stream *studioFrameStream, baseline []byte, regions []frameRegion, expected int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastCount := -1

	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d guest tile(s), last count %d", expected, lastCount)
		}

		frames := stream.collect(t, 45, 15*time.Second)
		frames = framesFromLastKeyframe(frames)
		decoded := decodeGrayFrames(t, writeIVF(t, frames))
		if len(decoded) == 0 {
			continue
		}

		lastCount = countGuestTiles(decoded[len(decoded)-1], baseline, regions)
		if lastCount == expected {
			return
		}
	}
}

func waitForGuestTileCountStable(t *testing.T, stream *studioFrameStream, baseline []byte, regions []frameRegion, expected int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastCount := -1

	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d stable guest tile(s), last count %d", expected, lastCount)
		}

		frames := stream.collect(t, 45, 15*time.Second)
		frames = framesFromLastKeyframe(frames)
		decoded := decodeGrayFrames(t, writeIVF(t, frames))
		for _, frame := range decoded {
			count := countGuestTiles(frame, baseline, regions)
			if count < expected {
				t.Fatalf("guest tiles dropped to %d, want at least %d while a guest leaves", count, expected)
			}
			lastCount = count
		}
		if lastCount == expected {
			return
		}
	}
}

func countGuestTiles(frame, baseline []byte, regions []frameRegion) int {
	count := 0
	for _, region := range regions {
		if meanRegionDifference(frame, baseline, region) > guestOverlayThreshold {
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
