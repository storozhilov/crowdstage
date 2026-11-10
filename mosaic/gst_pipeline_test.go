package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-gst/go-gst/pkg/gst"
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

func TestParticipantGridDefinesFourColumnsAndThreeRows(t *testing.T) {
	if maxParticipantTiles != mosaicColumns*mosaicRows {
		t.Fatalf("maxParticipantTiles = %d, want %d", maxParticipantTiles, mosaicColumns*mosaicRows)
	}

	firstRowY := -1
	for slot := 0; slot < maxParticipantTiles; slot++ {
		xpos, ypos, _, _ := participantGridPosition(slot)
		column := slot % mosaicColumns
		row := slot / mosaicColumns

		if column == 0 {
			if firstRowY < 0 {
				firstRowY = ypos
			}
		}
		if row > 0 && column == 0 && ypos <= firstRowY {
			t.Fatalf("slot %d does not start a new row below the first row", slot)
		}
		if column > 0 {
			previousX, _, _, _ := participantGridPosition(slot - 1)
			if xpos <= previousX {
				t.Fatalf("slot %d is not to the right of slot %d", slot, slot-1)
			}
		}
	}
}

func TestParticipantGridTilesAreNonOverlappingAndInsideTheCanvas(t *testing.T) {
	regions := make([]frameRegion, 0, maxParticipantTiles)
	for slot := 0; slot < maxParticipantTiles; slot++ {
		xpos, ypos, width, height := participantGridPosition(slot)
		region := frameRegion{xpos: xpos, ypos: ypos, width: width, height: height}

		if width <= 0 || height <= 0 {
			t.Fatalf("slot %d size = %dx%d, want a positive size", slot, width, height)
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

func TestParticipantGridKeepsSpacingBetweenTilesAndBorders(t *testing.T) {
	firstX, firstY, width, _ := participantGridPosition(0)
	lastColumnX, _, _, _ := participantGridPosition(mosaicColumns - 1)
	lastSlot := (mosaicRows - 1) * mosaicColumns
	_, lastLabelY, _, lastLabelHeight := participantLabelPosition(lastSlot)

	if firstX != backgroundWidth-(lastColumnX+width) {
		t.Fatalf("grid is not horizontally centered: left margin %d, right margin %d", firstX, backgroundWidth-(lastColumnX+width))
	}
	if firstY != backgroundHeight-(lastLabelY+lastLabelHeight) {
		t.Fatalf("grid is not vertically centered: top margin %d, bottom margin %d", firstY, backgroundHeight-(lastLabelY+lastLabelHeight))
	}
	if firstX < mosaicGap || firstY < mosaicGap {
		t.Fatalf("grid touches the canvas border: left margin %d, top margin %d, want at least %d", firstX, firstY, mosaicGap)
	}

	for slot := 0; slot < maxParticipantTiles; slot++ {
		xpos, ypos, _, _ := participantGridPosition(slot)
		column := slot % mosaicColumns
		row := slot / mosaicColumns

		if column > 0 {
			previousX, _, previousWidth, _ := participantGridPosition(slot - 1)
			if gap := xpos - (previousX + previousWidth); gap != mosaicGap {
				t.Fatalf("slot %d horizontal gap = %d, want %d", slot, gap, mosaicGap)
			}
		}
		if row > 0 {
			_, previousLabelY, _, previousLabelHeight := participantLabelPosition(slot - mosaicColumns)
			if gap := ypos - (previousLabelY + previousLabelHeight); gap != mosaicGap {
				t.Fatalf("slot %d vertical gap after label = %d, want %d", slot, gap, mosaicGap)
			}
		}
	}
}

func TestParticipantGridReservesSpaceForNameLabelsBelowCells(t *testing.T) {
	for slot := 0; slot < maxParticipantTiles; slot++ {
		cellX, cellY, cellWidth, cellHeight := participantGridPosition(slot)
		labelX, labelY, labelWidth, labelHeight := participantLabelPosition(slot)

		if labelX != cellX || labelWidth != cellWidth {
			t.Fatalf("slot %d label position (%d,%d) size %dx%d does not match cell position (%d,%d) size %dx%d",
				slot, labelX, labelY, labelWidth, labelHeight, cellX, cellY, cellWidth, cellHeight)
		}
		if labelY != cellY+cellHeight {
			t.Fatalf("slot %d label y = %d, want %d directly below the cell", slot, labelY, cellY+cellHeight)
		}
		if labelHeight != mosaicLabelHeight {
			t.Fatalf("slot %d label height = %d, want %d", slot, labelHeight, mosaicLabelHeight)
		}
		if labelY < 0 || labelY+labelHeight > backgroundHeight {
			t.Fatalf("slot %d label region (%d,%d) size %dx%d is outside the canvas", slot, labelX, labelY, labelWidth, labelHeight)
		}

		if slot+mosaicColumns < maxParticipantTiles {
			_, nextCellY, _, _ := participantGridPosition(slot + mosaicColumns)
			if labelY+labelHeight > nextCellY {
				t.Fatalf("slot %d label bottom %d overlaps the next row at %d", slot, labelY+labelHeight, nextCellY)
			}
		}
	}
}

func TestParticipantLabelTextUsesNameAndFallsBackToIdentity(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
		identity    string
		want        string
	}{
		{name: "display name", displayName: "Vasya Pupkin", identity: "participant-a", want: "Vasya Pupkin"},
		{name: "normalizes whitespace", displayName: "  Vasya   Pupkin  ", identity: "participant-a", want: "Vasya Pupkin"},
		{name: "empty display name", displayName: "", identity: "participant-a", want: "participant-a"},
		{name: "blank display name", displayName: "   ", identity: "participant-a", want: "participant-a"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := participantLabelText(test.displayName, test.identity); got != test.want {
				t.Fatalf("participantLabelText(%q, %q) = %q, want %q", test.displayName, test.identity, got, test.want)
			}
		})
	}
}

func TestParticipantTileDrawsParticipantNameBelowVideo(t *testing.T) {
	gstInitOnce.Do(gst.Init)

	pipeline := &mosaicPipeline{
		columns:  mosaicColumns,
		rows:     mosaicRows,
		nextTile: 1,
	}
	participant := &participantSource{
		identity:    "participant-identity",
		displayName: "Vasya Pupkin",
		slot:        0,
	}

	tile, err := pipeline.newParticipantTile(participant)
	if err != nil {
		t.Fatalf("create participant tile: %v", err)
	}
	if tile.scale == nil || tile.cellCaps == nil || tile.box == nil || tile.overlay == nil {
		t.Fatalf("participant tile is missing label elements: %+v", tile)
	}
	if got := tile.overlay.ObjectProperty("text"); got != "Vasya Pupkin" {
		t.Fatalf("textoverlay text = %v, want %q", got, "Vasya Pupkin")
	}
	if got := fmt.Sprint(tile.box.ObjectProperty("bottom")); got != fmt.Sprint(-mosaicLabelHeight) {
		t.Fatalf("videobox bottom = %v, want %d", got, -mosaicLabelHeight)
	}

	participant.displayName = ""
	tile, err = pipeline.newParticipantTile(participant)
	if err != nil {
		t.Fatalf("create participant tile without display name: %v", err)
	}
	if got := tile.overlay.ObjectProperty("text"); got != "participant-identity" {
		t.Fatalf("textoverlay text = %v, want identity fallback", got)
	}
}

func TestParticipantGridSupportsConfiguredColumnsAndRows(t *testing.T) {
	pipeline := &mosaicPipeline{columns: 2, rows: 2, sources: map[string]*participantSource{}}
	if got, want := pipeline.gridCapacity(), 4; got != want {
		t.Fatalf("grid capacity = %d, want %d", got, want)
	}

	regions := make([]frameRegion, 0, pipeline.gridCapacity())
	for slot := 0; slot < pipeline.gridCapacity(); slot++ {
		xpos, ypos, width, height := pipeline.gridPosition(slot)
		region := frameRegion{xpos: xpos, ypos: ypos, width: width, height: height}
		if width <= 0 || height <= 0 {
			t.Fatalf("slot %d size = %dx%d, want a positive size", slot, width, height)
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

	firstX, firstY, width, _ := pipeline.gridPosition(0)
	lastColumnX, _, _, _ := pipeline.gridPosition(pipeline.columns - 1)
	lastSlot := (pipeline.rows - 1) * pipeline.columns
	_, lastLabelY, _, lastLabelHeight := pipeline.labelPosition(lastSlot)
	if firstX != backgroundWidth-(lastColumnX+width) {
		t.Fatalf("grid is not horizontally centered: left margin %d, right margin %d", firstX, backgroundWidth-(lastColumnX+width))
	}
	if firstY != backgroundHeight-(lastLabelY+lastLabelHeight) {
		t.Fatalf("grid is not vertically centered: top margin %d, bottom margin %d", firstY, backgroundHeight-(lastLabelY+lastLabelHeight))
	}
}

func TestParticipantSlotsUseConfiguredGridCapacity(t *testing.T) {
	pipeline := &mosaicPipeline{columns: 2, rows: 2, sources: map[string]*participantSource{}}

	for slot := 0; slot < pipeline.gridCapacity(); slot++ {
		identity := fmt.Sprintf("participant-%d", slot)
		got, ok := pipeline.nextParticipantSlotLocked()
		if !ok || got != slot {
			t.Fatalf("slot %d allocation = %d, %v; want %d, true", slot, got, ok, slot)
		}
		pipeline.sources[identity] = &participantSource{identity: identity, slot: got}
	}

	if slot, ok := pipeline.nextParticipantSlotLocked(); ok {
		t.Fatalf("allocated slot %d beyond the %d available tiles", slot, pipeline.gridCapacity())
	}
}

func TestParticipantSlotsUseTheWholeGridAndReuseFreedSlots(t *testing.T) {
	pipeline := &mosaicPipeline{sources: map[string]*participantSource{}}

	for slot := 0; slot < maxParticipantTiles; slot++ {
		identity := fmt.Sprintf("participant-%d", slot)
		got, ok := pipeline.nextParticipantSlotLocked()
		if !ok || got != slot {
			t.Fatalf("slot %d allocation = %d, %v; want %d, true", slot, got, ok, slot)
		}
		pipeline.sources[identity] = &participantSource{identity: identity, slot: got}
	}

	if slot, ok := pipeline.nextParticipantSlotLocked(); ok {
		t.Fatalf("allocated slot %d beyond the %d available tiles", slot, maxParticipantTiles)
	}

	freed := fmt.Sprintf("participant-%d", maxParticipantTiles/2)
	delete(pipeline.sources, freed)
	if got, ok := pipeline.nextParticipantSlotLocked(); !ok || got != maxParticipantTiles/2 {
		t.Fatalf("reused slot = %d, %v; want %d, true", got, ok, maxParticipantTiles/2)
	}
}

func TestWaitingParticipantsTakeFreedSlotsInJoinOrder(t *testing.T) {
	pipeline := &mosaicPipeline{sources: map[string]*participantSource{}}
	active := make([]string, 0, maxParticipantTiles)

	for slot := 0; slot < maxParticipantTiles; slot++ {
		identity := fmt.Sprintf("active-%d", slot)
		participant := &participantSource{identity: identity}
		if !pipeline.registerParticipantLocked(participant) {
			t.Fatalf("active participant %q did not receive a slot", identity)
		}
		active = append(active, identity)
	}

	waitingA := &participantSource{identity: "waiting-a"}
	waitingB := &participantSource{identity: "waiting-b"}
	if pipeline.registerParticipantLocked(waitingA) {
		t.Fatal("waiting-a received a slot beyond the grid capacity")
	}
	if pipeline.registerParticipantLocked(waitingB) {
		t.Fatal("waiting-b received a slot beyond the grid capacity")
	}
	if waitingA.slot != -1 || waitingB.slot != -1 {
		t.Fatalf("waiting participants have slots: %d and %d, want -1", waitingA.slot, waitingB.slot)
	}

	removed, next := pipeline.unregisterParticipantLocked(active[5])
	if removed == nil || removed.identity != active[5] {
		t.Fatalf("removed participant = %+v, want %q", removed, active[5])
	}
	if next != waitingA {
		t.Fatalf("next participant = %+v, want waiting-a", next)
	}
	if waitingA.slot != 5 {
		t.Fatalf("waiting-a slot = %d, want the freed slot 5", waitingA.slot)
	}

	removed, next = pipeline.unregisterParticipantLocked(active[2])
	if removed == nil || removed.identity != active[2] {
		t.Fatalf("removed participant = %+v, want %q", removed, active[2])
	}
	if next != waitingB {
		t.Fatalf("next participant = %+v, want waiting-b", next)
	}
	if waitingB.slot != 2 {
		t.Fatalf("waiting-b slot = %d, want the freed slot 2", waitingB.slot)
	}
}

func TestRemovingWaitingParticipantDoesNotFreeATile(t *testing.T) {
	pipeline := &mosaicPipeline{sources: map[string]*participantSource{}}

	for slot := 0; slot < maxParticipantTiles; slot++ {
		identity := fmt.Sprintf("active-%d", slot)
		pipeline.registerParticipantLocked(&participantSource{identity: identity})
	}
	waiting := &participantSource{identity: "waiting-a"}
	pipeline.registerParticipantLocked(waiting)

	removed, next := pipeline.unregisterParticipantLocked(waiting.identity)
	if removed != waiting {
		t.Fatalf("removed participant = %+v, want the waiting participant", removed)
	}
	if next != nil {
		t.Fatalf("removing a waiting participant promoted %+v", next)
	}
	if len(pipeline.waiting) != 0 {
		t.Fatalf("waiting queue length = %d, want 0", len(pipeline.waiting))
	}
	if slot, ok := pipeline.nextParticipantSlotLocked(); ok {
		t.Fatalf("removing a waiting participant freed slot %d", slot)
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
