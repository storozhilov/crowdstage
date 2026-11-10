package main

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/go-gst/go-gst/pkg/gst"
)

type rotationTestPipeline struct {
	gst.Pipeline

	mu          sync.Mutex
	added       []gst.Element
	removed     []gst.Element
	addFailures int
	mixer       gst.Element
}

func (p *rotationTestPipeline) Add(element gst.Element) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.addFailures > 0 {
		p.addFailures--
		return false
	}
	p.added = append(p.added, element)
	return true
}

func (p *rotationTestPipeline) Remove(element gst.Element) bool {
	p.mu.Lock()
	p.removed = append(p.removed, element)
	p.mu.Unlock()
	return true
}

func (p *rotationTestPipeline) GetByName(name string) gst.Element {
	if name == "mixer" {
		return p.mixer
	}
	return nil
}

func (p *rotationTestPipeline) removedElements() []gst.Element {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]gst.Element(nil), p.removed...)
}

type rotationTestMixer struct {
	gst.Element

	requests int
}

func (m *rotationTestMixer) RequestPadSimple(string) gst.Pad {
	m.requests++
	return nil
}

func (m *rotationTestMixer) ReleaseRequestPad(gst.Pad) {
}

func newRotationTestPipeline(columns, rows int, rotationTimeout time.Duration) (*mosaicPipeline, *rotationTestPipeline) {
	gstInitOnce.Do(gst.Init)

	pipeline := &rotationTestPipeline{}
	return &mosaicPipeline{
		pipeline:        pipeline,
		columns:         columns,
		rows:            rows,
		rotationTimeout: rotationTimeout,
		sources:         make(map[string]*participantSource),
		parseSource: func(config config, name, producerIdentity string) (gst.Element, error) {
			return &fakeParticipantSource{}, nil
		},
	}, pipeline
}

func newRotationTestTile(t *testing.T, createdAt time.Time) *participantTile {
	t.Helper()

	queue := gst.ElementFactoryMake("identity", "rotation-test-queue")
	overlay := gst.ElementFactoryMake("identity", "rotation-test-overlay")
	source := gst.ElementFactoryMake("identity", "rotation-test-source")
	mixer := gst.ElementFactoryMake("identity", "rotation-test-mixer")
	if queue == nil || overlay == nil || source == nil || mixer == nil {
		t.Fatal("could not create rotation test elements")
	}

	return &participantTile{
		queue:     queue,
		overlay:   overlay,
		sourcePad: source.GetStaticPad("src"),
		mixerPad:  mixer.GetStaticPad("sink"),
		createdAt: createdAt,
		elements:  []gst.Element{queue, overlay},
	}
}

func TestParticipantPadAddedIgnoresWaitingParticipant(t *testing.T) {
	gstInitOnce.Do(gst.Init)

	pipeline, fakePipeline := newRotationTestPipeline(2, 2, 10*time.Second)
	mixer := &rotationTestMixer{}
	fakePipeline.mixer = mixer
	participant := &participantSource{identity: "waiting", slot: -1}
	pipeline.sources[participant.identity] = participant
	pad := gst.NewPad("video_test", gst.PadSrc)

	pipeline.participantPadAdded(participant, pad)

	if mixer.requests != 0 {
		t.Fatalf("mixer pad requests = %d, want 0 for a waiting participant", mixer.requests)
	}
	if participant.tile != nil {
		t.Fatal("waiting participant got a mosaic tile")
	}
}

func TestRotationDoesNotReplaceTileBeforeTimeout(t *testing.T) {
	pipeline, _ := newRotationTestPipeline(2, 2, 10*time.Second)
	active := &participantSource{
		identity: "active",
		slot:     0,
		element:  &fakeParticipantSource{},
		tile:     newRotationTestTile(t, time.Now()),
	}
	waiting := &participantSource{identity: "waiting", slot: -1}
	pipeline.sources[active.identity] = active
	pipeline.sources[waiting.identity] = waiting
	pipeline.waiting = []string{waiting.identity}

	pipeline.rotateParticipant()

	if active.slot != 0 {
		t.Fatalf("active slot = %d, want 0 before the rotation timeout", active.slot)
	}
	if waiting.slot != -1 {
		t.Fatalf("waiting slot = %d, want -1 before the rotation timeout", waiting.slot)
	}
	if want := []string{waiting.identity}; !reflect.DeepEqual(pipeline.waiting, want) {
		t.Fatalf("waiting queue = %v, want %v", pipeline.waiting, want)
	}
}

func TestRotationReplacesOldestTileAndQueuesItBehindWaitingParticipants(t *testing.T) {
	pipeline, fakePipeline := newRotationTestPipeline(2, 2, 10*time.Second)
	now := time.Now()
	oldestElement := &fakeParticipantSource{}
	newerElement := &fakeParticipantSource{}
	oldest := &participantSource{
		identity: "oldest",
		slot:     0,
		element:  oldestElement,
		tile:     newRotationTestTile(t, now.Add(-20*time.Second)),
	}
	newer := &participantSource{
		identity: "newer",
		slot:     1,
		element:  newerElement,
		tile:     newRotationTestTile(t, now.Add(-15*time.Second)),
	}
	waitingA := &participantSource{identity: "waiting-a", slot: -1}
	waitingB := &participantSource{identity: "waiting-b", slot: -1}
	for _, participant := range []*participantSource{oldest, newer, waitingA, waitingB} {
		pipeline.sources[participant.identity] = participant
	}
	pipeline.waiting = []string{waitingA.identity, waitingB.identity}

	pipeline.rotateParticipant()

	if waitingA.slot != 0 {
		t.Fatalf("waiting-a slot = %d, want the oldest slot 0", waitingA.slot)
	}
	if oldest.slot != -1 {
		t.Fatalf("oldest slot = %d, want -1 after rotation", oldest.slot)
	}
	if newer.slot != 1 {
		t.Fatalf("newer slot = %d, want 1 after rotation", newer.slot)
	}
	if want := []string{waitingB.identity, oldest.identity}; !reflect.DeepEqual(pipeline.waiting, want) {
		t.Fatalf("waiting queue = %v, want %v", pipeline.waiting, want)
	}
	if got := oldestElement.state; got != gst.StateNull {
		t.Fatalf("replaced source state = %v, want NULL", got)
	}
	if got := newerElement.state; got != gst.State(0) {
		t.Fatalf("newer source state = %v, want unchanged", got)
	}
	removedSource := false
	for _, element := range fakePipeline.removedElements() {
		if element == oldestElement {
			removedSource = true
		}
	}
	if !removedSource {
		t.Fatal("replaced participant source was not removed from the pipeline")
	}
}

func TestRotationPromotesWaitingParticipantsInOrder(t *testing.T) {
	pipeline, _ := newRotationTestPipeline(2, 2, 10*time.Second)
	now := time.Now()
	first := &participantSource{
		identity: "first",
		slot:     0,
		element:  &fakeParticipantSource{},
		tile:     newRotationTestTile(t, now.Add(-20*time.Second)),
	}
	second := &participantSource{
		identity: "second",
		slot:     1,
		element:  &fakeParticipantSource{},
		tile:     newRotationTestTile(t, now.Add(-15*time.Second)),
	}
	waitingA := &participantSource{identity: "waiting-a", slot: -1}
	waitingB := &participantSource{identity: "waiting-b", slot: -1}
	for _, participant := range []*participantSource{first, second, waitingA, waitingB} {
		pipeline.sources[participant.identity] = participant
	}
	pipeline.waiting = []string{waitingA.identity, waitingB.identity}

	pipeline.rotateParticipant()
	pipeline.rotateParticipant()

	if waitingA.slot != 0 {
		t.Fatalf("waiting-a slot = %d, want 0 after first rotation", waitingA.slot)
	}
	if waitingB.slot != 1 {
		t.Fatalf("waiting-b slot = %d, want 1 after second rotation", waitingB.slot)
	}
	if first.slot != -1 || second.slot != -1 {
		t.Fatalf("replaced slots = %d, %d, want both -1", first.slot, second.slot)
	}
	if want := []string{first.identity, second.identity}; !reflect.DeepEqual(pipeline.waiting, want) {
		t.Fatalf("waiting queue = %v, want %v", pipeline.waiting, want)
	}
}

func TestRotationRestoresReplacedParticipantWhenPromotionFails(t *testing.T) {
	pipeline, fakePipeline := newRotationTestPipeline(2, 2, 10*time.Second)
	fakePipeline.addFailures = 1
	active := &participantSource{
		identity: "active",
		slot:     0,
		element:  &fakeParticipantSource{},
		tile:     newRotationTestTile(t, time.Now().Add(-20*time.Second)),
	}
	waiting := &participantSource{identity: "waiting", slot: -1}
	pipeline.sources[active.identity] = active
	pipeline.sources[waiting.identity] = waiting
	pipeline.waiting = []string{waiting.identity}

	pipeline.rotateParticipant()

	if active.slot != 0 {
		t.Fatalf("active slot = %d, want 0 after failed promotion", active.slot)
	}
	if _, ok := pipeline.sources[waiting.identity]; ok {
		t.Fatalf("failed participant %q is still registered", waiting.identity)
	}
	if len(pipeline.waiting) != 0 {
		t.Fatalf("waiting queue = %v, want empty", pipeline.waiting)
	}
}

func TestRotationDoesNothingWithoutWaitingParticipants(t *testing.T) {
	pipeline, _ := newRotationTestPipeline(2, 2, 10*time.Second)
	active := &participantSource{
		identity: "active",
		slot:     0,
		element:  &fakeParticipantSource{},
		tile:     newRotationTestTile(t, time.Now().Add(-20*time.Second)),
	}
	pipeline.sources[active.identity] = active

	pipeline.rotateParticipant()

	if active.slot != 0 {
		t.Fatalf("active slot = %d, want 0 without waiting participants", active.slot)
	}
	if got := active.element.(*fakeParticipantSource).state; got != gst.State(0) {
		t.Fatalf("active source state = %v, want unchanged", got)
	}
}
