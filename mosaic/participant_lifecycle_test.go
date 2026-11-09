package main

import (
	"sync"
	"testing"
	"time"

	"github.com/go-gst/go-glib/pkg/gobject/v2"
	"github.com/go-gst/go-gst/pkg/gst"
)

type fakeParticipantPipeline struct {
	gst.Pipeline

	addStarted chan struct{}
	addRelease chan struct{}

	mu      sync.Mutex
	removed []gst.Element
}

func (p *fakeParticipantPipeline) Add(element gst.Element) bool {
	close(p.addStarted)
	<-p.addRelease
	return true
}

func (p *fakeParticipantPipeline) Remove(element gst.Element) bool {
	p.mu.Lock()
	p.removed = append(p.removed, element)
	p.mu.Unlock()
	return true
}

func (p *fakeParticipantPipeline) removedElements() []gst.Element {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]gst.Element(nil), p.removed...)
}

type fakeParticipantSource struct {
	gst.Element
	state gst.State
}

func (s *fakeParticipantSource) ConnectPadAdded(func(gst.Element, gst.Pad)) gobject.SignalHandle {
	return 0
}

func (s *fakeParticipantSource) ConnectPadRemoved(func(gst.Element, gst.Pad)) gobject.SignalHandle {
	return 0
}

func (s *fakeParticipantSource) SetState(state gst.State) gst.StateChangeReturn {
	s.state = state
	return gst.StateChangeSuccess
}

func (s *fakeParticipantSource) SyncStateWithParent() bool {
	return true
}

func TestRemovalWaitsForParticipantSourceStart(t *testing.T) {
	source := &fakeParticipantSource{}
	pipeline := &fakeParticipantPipeline{
		addStarted: make(chan struct{}),
		addRelease: make(chan struct{}),
	}
	mosaic := &mosaicPipeline{
		pipeline: pipeline,
		sources:  make(map[string]*participantSource),
		parseSource: func(config config, name, producerIdentity string) (gst.Element, error) {
			return source, nil
		},
	}
	participant := &participantSource{
		identity:       "participant-a",
		name:           "participant-src-1",
		participantSID: "participant-sid",
		slot:           0,
	}
	mosaic.sources[participant.identity] = participant

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(pipeline.addRelease) })
	}
	defer release()

	startDone := make(chan error, 1)
	go func() {
		startDone <- mosaic.startParticipant(participant)
	}()

	select {
	case <-pipeline.addStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("participant source start did not reach pipeline.Add")
	}

	removeDone := make(chan struct{})
	go func() {
		mosaic.removeParticipant(participant.identity)
		close(removeDone)
	}()

	select {
	case <-removeDone:
		t.Fatal("participant removal completed while source start was in progress")
	case <-time.After(200 * time.Millisecond):
	}

	release()

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("start participant source: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("participant source start did not finish")
	}

	select {
	case <-removeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("participant removal did not finish after source start completed")
	}

	if source.state != gst.StateNull {
		t.Fatalf("participant source state = %v, want NULL", source.state)
	}
	if removed := pipeline.removedElements(); len(removed) != 1 {
		t.Fatalf("participant source removals = %d, want 1", len(removed))
	}
}
