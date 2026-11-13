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
		sources:         make(map[string]*guestSource),
		parseSource: func(config config, name, producerIdentity string) (gst.Element, error) {
			return &fakeGuestSource{}, nil
		},
	}, pipeline
}

func newRotationTestTile(t *testing.T, createdAt time.Time) *guestTile {
	t.Helper()

	queue := gst.ElementFactoryMake("identity", "rotation-test-queue")
	overlay := gst.ElementFactoryMake("identity", "rotation-test-overlay")
	source := gst.ElementFactoryMake("identity", "rotation-test-source")
	mixer := gst.ElementFactoryMake("identity", "rotation-test-mixer")
	if queue == nil || overlay == nil || source == nil || mixer == nil {
		t.Fatal("could not create rotation test elements")
	}

	return &guestTile{
		queue:     queue,
		overlay:   overlay,
		sourcePad: source.GetStaticPad("src"),
		mixerPad:  mixer.GetStaticPad("sink"),
		createdAt: createdAt,
		elements:  []gst.Element{queue, overlay},
	}
}

func TestGuestPadAddedIgnoresWaitingGuest(t *testing.T) {
	gstInitOnce.Do(gst.Init)

	pipeline, fakePipeline := newRotationTestPipeline(2, 2, 10*time.Second)
	mixer := &rotationTestMixer{}
	fakePipeline.mixer = mixer
	guest := &guestSource{identity: "waiting", slot: -1}
	pipeline.sources[guest.identity] = guest
	pad := gst.NewPad("video_test", gst.PadSrc)

	pipeline.guestPadAdded(guest, pad)

	if mixer.requests != 0 {
		t.Fatalf("mixer pad requests = %d, want 0 for a waiting guest", mixer.requests)
	}
	if guest.tile != nil {
		t.Fatal("waiting guest got a mosaic tile")
	}
}

func TestRotationDoesNotReplaceTileBeforeTimeout(t *testing.T) {
	pipeline, _ := newRotationTestPipeline(2, 2, 10*time.Second)
	active := &guestSource{
		identity: "active",
		slot:     0,
		element:  &fakeGuestSource{},
		tile:     newRotationTestTile(t, time.Now()),
	}
	waiting := &guestSource{identity: "waiting", slot: -1}
	pipeline.sources[active.identity] = active
	pipeline.sources[waiting.identity] = waiting
	pipeline.waiting = []string{waiting.identity}

	pipeline.rotateGuest()

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

func TestRotationReplacesOldestTileAndQueuesItBehindWaitingGuests(t *testing.T) {
	pipeline, fakePipeline := newRotationTestPipeline(2, 2, 10*time.Second)
	now := time.Now()
	oldestElement := &fakeGuestSource{}
	newerElement := &fakeGuestSource{}
	oldest := &guestSource{
		identity: "oldest",
		slot:     0,
		element:  oldestElement,
		tile:     newRotationTestTile(t, now.Add(-20*time.Second)),
	}
	newer := &guestSource{
		identity: "newer",
		slot:     1,
		element:  newerElement,
		tile:     newRotationTestTile(t, now.Add(-15*time.Second)),
	}
	waitingA := &guestSource{identity: "waiting-a", slot: -1}
	waitingB := &guestSource{identity: "waiting-b", slot: -1}
	for _, guest := range []*guestSource{oldest, newer, waitingA, waitingB} {
		pipeline.sources[guest.identity] = guest
	}
	pipeline.waiting = []string{waitingA.identity, waitingB.identity}

	pipeline.rotateGuest()

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
		t.Fatal("replaced guest source was not removed from the pipeline")
	}
}

func TestRotationPromotesWaitingGuestsInOrder(t *testing.T) {
	pipeline, _ := newRotationTestPipeline(2, 2, 10*time.Second)
	now := time.Now()
	first := &guestSource{
		identity: "first",
		slot:     0,
		element:  &fakeGuestSource{},
		tile:     newRotationTestTile(t, now.Add(-20*time.Second)),
	}
	second := &guestSource{
		identity: "second",
		slot:     1,
		element:  &fakeGuestSource{},
		tile:     newRotationTestTile(t, now.Add(-15*time.Second)),
	}
	waitingA := &guestSource{identity: "waiting-a", slot: -1}
	waitingB := &guestSource{identity: "waiting-b", slot: -1}
	for _, guest := range []*guestSource{first, second, waitingA, waitingB} {
		pipeline.sources[guest.identity] = guest
	}
	pipeline.waiting = []string{waitingA.identity, waitingB.identity}

	pipeline.rotateGuest()
	pipeline.rotateGuest()

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

func TestRotationRestoresReplacedGuestWhenPromotionFails(t *testing.T) {
	pipeline, fakePipeline := newRotationTestPipeline(2, 2, 10*time.Second)
	fakePipeline.addFailures = 1
	active := &guestSource{
		identity: "active",
		slot:     0,
		element:  &fakeGuestSource{},
		tile:     newRotationTestTile(t, time.Now().Add(-20*time.Second)),
	}
	waiting := &guestSource{identity: "waiting", slot: -1}
	pipeline.sources[active.identity] = active
	pipeline.sources[waiting.identity] = waiting
	pipeline.waiting = []string{waiting.identity}

	pipeline.rotateGuest()

	if active.slot != 0 {
		t.Fatalf("active slot = %d, want 0 after failed promotion", active.slot)
	}
	if _, ok := pipeline.sources[waiting.identity]; ok {
		t.Fatalf("failed guest %q is still registered", waiting.identity)
	}
	if len(pipeline.waiting) != 0 {
		t.Fatalf("waiting queue = %v, want empty", pipeline.waiting)
	}
}

func TestRotationDoesNothingWithoutWaitingGuests(t *testing.T) {
	pipeline, _ := newRotationTestPipeline(2, 2, 10*time.Second)
	active := &guestSource{
		identity: "active",
		slot:     0,
		element:  &fakeGuestSource{},
		tile:     newRotationTestTile(t, time.Now().Add(-20*time.Second)),
	}
	pipeline.sources[active.identity] = active

	pipeline.rotateGuest()

	if active.slot != 0 {
		t.Fatalf("active slot = %d, want 0 without waiting guests", active.slot)
	}
	if got := active.element.(*fakeGuestSource).state; got != gst.State(0) {
		t.Fatalf("active source state = %v, want unchanged", got)
	}
}
