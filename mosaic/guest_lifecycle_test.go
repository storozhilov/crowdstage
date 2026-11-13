package main

import (
	"sync"
	"testing"
	"time"

	"github.com/go-gst/go-glib/pkg/gobject/v2"
	"github.com/go-gst/go-gst/pkg/gst"
)

type fakeGuestPipeline struct {
	gst.Pipeline

	addStarted chan struct{}
	addRelease chan struct{}

	mu      sync.Mutex
	removed []gst.Element
}

func (p *fakeGuestPipeline) Add(element gst.Element) bool {
	close(p.addStarted)
	<-p.addRelease
	return true
}

func (p *fakeGuestPipeline) Remove(element gst.Element) bool {
	p.mu.Lock()
	p.removed = append(p.removed, element)
	p.mu.Unlock()
	return true
}

func (p *fakeGuestPipeline) removedElements() []gst.Element {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]gst.Element(nil), p.removed...)
}

type fakeGuestSource struct {
	gst.Element
	state gst.State
}

func (s *fakeGuestSource) ConnectPadAdded(func(gst.Element, gst.Pad)) gobject.SignalHandle {
	return 0
}

func (s *fakeGuestSource) ConnectPadRemoved(func(gst.Element, gst.Pad)) gobject.SignalHandle {
	return 0
}

func (s *fakeGuestSource) SetState(state gst.State) gst.StateChangeReturn {
	s.state = state
	return gst.StateChangeSuccess
}

func (s *fakeGuestSource) SyncStateWithParent() bool {
	return true
}

func TestRemovalWaitsForGuestSourceStart(t *testing.T) {
	source := &fakeGuestSource{}
	pipeline := &fakeGuestPipeline{
		addStarted: make(chan struct{}),
		addRelease: make(chan struct{}),
	}
	mosaic := &mosaicPipeline{
		pipeline: pipeline,
		sources:  make(map[string]*guestSource),
		parseSource: func(config config, name, producerIdentity string) (gst.Element, error) {
			return source, nil
		},
	}
	guest := &guestSource{
		identity: "guest-a",
		name:     "guest-src-1",
		guestSID: "guest-sid",
		slot:     0,
	}
	mosaic.sources[guest.identity] = guest

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(pipeline.addRelease) })
	}
	defer release()

	startDone := make(chan error, 1)
	go func() {
		startDone <- mosaic.startGuest(guest)
	}()

	select {
	case <-pipeline.addStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("guest source start did not reach pipeline.Add")
	}

	removeDone := make(chan struct{})
	go func() {
		mosaic.removeGuest(guest.identity)
		close(removeDone)
	}()

	select {
	case <-removeDone:
		t.Fatal("guest removal completed while source start was in progress")
	case <-time.After(200 * time.Millisecond):
	}

	release()

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("start guest source: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("guest source start did not finish")
	}

	select {
	case <-removeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("guest removal did not finish after source start completed")
	}

	if source.state != gst.StateNull {
		t.Fatalf("guest source state = %v, want NULL", source.state)
	}
	if removed := pipeline.removedElements(); len(removed) != 1 {
		t.Fatalf("guest source removals = %d, want 1", len(removed))
	}
}
