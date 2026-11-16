package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	sharedconfig "github.com/storozhilov/crowdstage/shared/config"
)

const mixerBackgroundDifference = 5.0

func TestMediaMixerDoesNotShowTheBackgroundWhileSwitching(t *testing.T) {
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
	viewerIdentity := fmt.Sprintf("media-mixer-flicker-viewer-%d", time.Now().UnixNano())

	postMixer(t, baseURL, "/stop")
	postMixer(t, baseURL, "/start")
	defer postMixer(t, baseURL, "/stop")

	viewer, stream := joinMixerViewer(t, url, apiKey, apiSecret, targetRoom, viewerIdentity, mixerIdentity)
	defer viewer.Disconnect()
	trackGeneration := stream.waitForFirstTrack(t, mixerTrackTimeout)

	backgroundFrames := waitForMixerStaticPicture(t, stream, trackGeneration)
	backgroundPath := writeMixerIVF(t, backgroundFrames, mixerStaticTestWidth, mixerStaticTestHeight)
	background := decodeMixerGrayFrames(t, backgroundPath)
	backgroundFrameSize := mixerStaticTestWidth * mixerStaticTestHeight
	if len(background) < backgroundFrameSize {
		t.Fatal("could not capture the static background picture")
	}
	backgroundReference := background[:backgroundFrameSize]

	first := startMixerStudioProcess(t, script, "first", "smpte")
	waitForMixerInputs(t, baseURL, 1, mixerTrackTimeout)
	waitForMixerBroadcastedIdentity(t, baseURL, first.identity, mixerTrackTimeout)
	waitForMixerStaticPicture(t, stream, trackGeneration)

	second := startMixerStudioProcess(t, script, "second", "ball")
	waitForMixerInputs(t, baseURL, 2, mixerTrackTimeout)

	postMixerSwitch(t, baseURL, second.identity)
	health := getMixerHealth(t, baseURL)
	assertMixerBroadcastedIdentity(t, health, second.identity)
	assertMixerTrackGeneration(t, stream, trackGeneration)
	assertMixerKeepsTheBackgroundHidden(t, stream.collect(t, 90, mixerTrackTimeout, trackGeneration), backgroundReference)

	waitForMixerMovingPicture(t, stream, trackGeneration)

	pauseMixerStudioProcess(t, first)
	defer resumeMixerStudioProcess(t, first)
	stopMixerStudioProcess(t, second)
	assertMixerKeepsTheBackgroundHidden(t, stream.collect(t, 60, mixerTrackTimeout, trackGeneration), backgroundReference)
	resumeMixerStudioProcess(t, first)

	waitForMixerInputs(t, baseURL, 1, mixerTrackTimeout)
	waitForMixerBroadcastedIdentity(t, baseURL, first.identity, mixerTrackTimeout)
	waitForMixerStaticPicture(t, stream, trackGeneration)
}

func assertMixerKeepsTheBackgroundHidden(t *testing.T, frames [][]byte, background []byte) {
	t.Helper()

	path := writeMixerIVF(t, frames, mixerStaticTestWidth, mixerStaticTestHeight)
	decoded := decodeMixerGrayFrames(t, path)
	frameSize := mixerStaticTestWidth * mixerStaticTestHeight
	frameCount := len(decoded) / frameSize
	if frameCount < 8 {
		t.Fatalf("decoded frame count = %d, want at least 8", frameCount)
	}

	for index := 0; index < frameCount; index++ {
		frame := decoded[index*frameSize : (index+1)*frameSize]
		var totalDifference uint64
		for offset := range frame {
			totalDifference += uint64(mixerAbsoluteDifference(frame[offset], background[offset]))
		}
		meanDifference := float64(totalDifference) / float64(frameSize)
		if meanDifference < mixerBackgroundDifference {
			t.Fatalf(
				"decoded frame %d matches the static background picture (mean luma difference %.3f); the broadcast flickered during the source switch",
				index,
				meanDifference,
			)
		}
	}
}
