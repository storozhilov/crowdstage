package main

import (
	"strings"
	"testing"
)

func TestPipelineDescriptionOnlyPublishesTheStudioComposition(t *testing.T) {
	config := config{
		LiveKitURL:     "ws://livekit:7880",
		APIKey:         "key",
		APISecret:      "secret",
		RoomName:       "participants",
		StudioRoomName: "studio",
		Identity:       "composer",
	}

	description := pipelineDescription(config)

	expected := []string{
		"livekitwebrtcsink",
		"compositor name=mixer",
		"sink_0::xpos=0",
		"sink_0::ypos=0",
		"sink_0::width=1280",
		"sink_0::height=720",
		"sink_0::zorder=0",
		`signaller::ws-url="ws://livekit:7880"`,
		`signaller::room-name="studio"`,
		`signaller::identity="composer"`,
		"video/x-raw,width=1280,height=720,framerate=30/1",
		`video-caps="video/x-vp8"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("pipeline description does not contain %q:\n%s", want, description)
		}
	}
	if strings.Contains(description, "livekitwebrtcsrc") {
		t.Errorf("pipeline description must not create a shared participant source:\n%s", description)
	}
}

func TestParticipantSourceDescriptionSubscribesToProducer(t *testing.T) {
	config := config{
		LiveKitURL: "ws://livekit:7880",
		APIKey:     "key",
		APISecret:  "secret",
		RoomName:   "participants",
		Identity:   "composer",
	}

	description := participantSourceDescription(config, "participant-src-2", "publisher-a")

	expected := []string{
		`livekitwebrtcsrc name="participant-src-2"`,
		`signaller::ws-url="ws://livekit:7880"`,
		`signaller::room-name="participants"`,
		`signaller::identity="composer-participant-src-2"`,
		`signaller::producer-peer-id="publisher-a"`,
	}
	for _, want := range expected {
		if !strings.Contains(description, want) {
			t.Errorf("participant source description does not contain %q:\n%s", want, description)
		}
	}
}

func TestParticipantOverlayIsPlacedInTheTopRightCorner(t *testing.T) {
	xpos, ypos, width, height := participantOverlayPosition()

	if width != participantWidth || height != participantHeight {
		t.Fatalf("overlay size = %dx%d, want %dx%d", width, height, participantWidth, participantHeight)
	}
	if xpos+width > backgroundWidth {
		t.Fatalf("overlay right edge = %d, exceeds canvas width %d", xpos+width, backgroundWidth)
	}
	if ypos < 0 || ypos+height > backgroundHeight {
		t.Fatalf("overlay top edge %d or bottom edge %d is outside the canvas", ypos, ypos+height)
	}
	if xpos < backgroundWidth/2 || ypos >= backgroundHeight/2 {
		t.Fatalf("overlay position (%d,%d) is not in the top-right corner", xpos, ypos)
	}
}

func TestParticipantGridHasFourNonOverlappingTiles(t *testing.T) {
	regions := make([]frameRegion, 0, maxParticipantTiles)
	for slot := 0; slot < maxParticipantTiles; slot++ {
		xpos, ypos, width, height := participantGridPosition(slot)
		region := frameRegion{xpos: xpos, ypos: ypos, width: width, height: height}

		if width != participantWidth || height != participantHeight {
			t.Fatalf("slot %d size = %dx%d, want %dx%d", slot, width, height, participantWidth, participantHeight)
		}
		if xpos < 0 || xpos+width > backgroundWidth || ypos < 0 || ypos+height > backgroundHeight {
			t.Fatalf("slot %d position (%d,%d) with size %dx%d is outside the canvas", slot, xpos, ypos, width, height)
		}
		for _, existing := range regions {
			if regionsOverlap(existing, region) {
				t.Fatalf("slot %d region %+v overlaps existing region %+v", slot, region, existing)
			}
		}
		regions = append(regions, region)
	}
}

func TestParticipantGridSlotsFormTwoColumnsAndTwoRows(t *testing.T) {
	xpos0, ypos0, _, _ := participantGridPosition(0)
	xpos1, ypos1, _, _ := participantGridPosition(1)
	xpos2, ypos2, _, _ := participantGridPosition(2)
	xpos3, ypos3, _, _ := participantGridPosition(3)

	if xpos0 == xpos1 || ypos0 != ypos1 {
		t.Fatalf("slots 0 and 1 do not share a row: (%d,%d) and (%d,%d)", xpos0, ypos0, xpos1, ypos1)
	}
	if xpos2 != xpos0 || ypos2 == ypos0 {
		t.Fatalf("slot 2 is not below slot 0: (%d,%d) and (%d,%d)", xpos2, ypos2, xpos0, ypos0)
	}
	if xpos3 != xpos1 || ypos3 != ypos2 {
		t.Fatalf("slot 3 is not below slot 1: (%d,%d) and (%d,%d)", xpos3, ypos3, xpos1, ypos1)
	}
}

func TestParticipantSlotsUseFourSourcesAndReuseFreedSlots(t *testing.T) {
	pipeline := &mosaicPipeline{sources: map[string]*participantSource{}}

	for slot, identity := range []string{"participant-a", "participant-b", "participant-c", "participant-d"} {
		got, ok := pipeline.nextParticipantSlotLocked()
		if !ok || got != slot {
			t.Fatalf("slot %d allocation = %d, %v; want %d, true", slot, got, ok, slot)
		}
		pipeline.sources[identity] = &participantSource{slot: got}
	}

	if slot, ok := pipeline.nextParticipantSlotLocked(); ok {
		t.Fatalf("allocated slot %d beyond the four available tiles", slot)
	}

	delete(pipeline.sources, "participant-b")
	if got, ok := pipeline.nextParticipantSlotLocked(); !ok || got != 1 {
		t.Fatalf("reused slot = %d, %v; want 1, true", got, ok)
	}
}

func regionsOverlap(left, right frameRegion) bool {
	return left.xpos < right.xpos+right.width &&
		right.xpos < left.xpos+left.width &&
		left.ypos < right.ypos+right.height &&
		right.ypos < left.ypos+left.height
}

func TestGstStringEscapesValues(t *testing.T) {
	if got, want := gstString(`a"b\c`), `"a\"b\\c"`; got != want {
		t.Errorf("gstString() = %s, want %s", got, want)
	}
}

func TestComposerFiltersOnlyItsGeneratedParticipantSourceIdentities(t *testing.T) {
	composer := &composer{
		config:           config{Identity: "composer"},
		nextSource:       1,
		sourceIdentities: make(map[string]struct{}),
	}

	name, identity := composer.newParticipantSource()
	if name != "participant-src-1" {
		t.Errorf("source name = %q, want %q", name, "participant-src-1")
	}
	if identity != "composer-participant-src-1" {
		t.Errorf("source identity = %q, want %q", identity, "composer-participant-src-1")
	}
	if !composer.isParticipantSourceIdentity(identity) {
		t.Errorf("generated source identity %q is not filtered", identity)
	}
	if composer.isParticipantSourceIdentity("composer-participant-src-99") {
		t.Error("an identity with the source prefix but not generated by this composer is filtered")
	}

	composer.forgetParticipantSource(identity)
	if composer.isParticipantSourceIdentity(identity) {
		t.Errorf("forgotten source identity %q is still filtered", identity)
	}
}
