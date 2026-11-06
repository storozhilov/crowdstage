package main

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type blockingRTPSource struct {
	packets chan *rtp.Packet

	readStarted         chan struct{}
	readStartedOnce     sync.Once
	interrupted         chan struct{}
	interruptedOnce     sync.Once
	deadlineCleared     chan struct{}
	deadlineClearedOnce sync.Once
}

func newBlockingRTPSource() *blockingRTPSource {
	return &blockingRTPSource{
		packets:         make(chan *rtp.Packet, 1),
		readStarted:     make(chan struct{}),
		interrupted:     make(chan struct{}),
		deadlineCleared: make(chan struct{}),
	}
}

func (s *blockingRTPSource) ReadRTP() (*rtp.Packet, interceptor.Attributes, error) {
	s.readStartedOnce.Do(func() { close(s.readStarted) })

	select {
	case packet := <-s.packets:
		return packet, nil, nil
	case <-s.interrupted:
		return nil, nil, io.EOF
	}
}

func (s *blockingRTPSource) Codec() webrtc.RTPCodecParameters {
	return webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: 90000,
		},
	}
}

func (s *blockingRTPSource) SetReadDeadline(deadline time.Time) error {
	if deadline.IsZero() {
		s.deadlineClearedOnce.Do(func() { close(s.deadlineCleared) })
		return nil
	}

	s.interruptedOnce.Do(func() { close(s.interrupted) })
	return nil
}

type capturingRTPTarget struct {
	writes chan *rtp.Packet
}

func (t *capturingRTPTarget) WriteRTP(packet *rtp.Packet, _ *lksdk.SampleWriteOptions) error {
	t.writes <- packet
	return nil
}

func TestStopForwardEntryInterruptsBlockedRTPRead(t *testing.T) {
	source := newBlockingRTPSource()
	target := &capturingRTPTarget{writes: make(chan *rtp.Packet, 4)}
	ctx, cancel := context.WithCancel(context.Background())
	entry := &forward{
		cancel: cancel,
		sid:    "TR_test",
		source: source,
		done:   make(chan struct{}),
	}
	unpublisher := &recordingUnpublisher{}
	composer := &composer{
		forwards: map[string]*forward{
			"track": entry,
		},
		studioUnpublisher: unpublisher,
	}

	go composer.forwardRTP(ctx, "track", entry, source, target)

	select {
	case <-source.readStarted:
	case <-time.After(time.Second):
		t.Fatal("forwarding did not start reading RTP")
	}

	stopped := make(chan struct{})
	go func() {
		composer.stopForwardEntry(entry)
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stopping forwarding did not interrupt the blocked RTP read")
	}

	select {
	case <-entry.done:
	default:
		t.Fatal("stop returned before the forwarding loop finished")
	}

	select {
	case <-source.deadlineCleared:
	case <-time.After(time.Second):
		t.Fatal("stop did not clear the track read deadline")
	}

	source.packets <- &rtp.Packet{
		Header:  rtp.Header{SequenceNumber: 42},
		Payload: []byte{0x10},
	}

	select {
	case packet := <-target.writes:
		t.Fatalf("forwarded RTP packet after stop: sequence number %d", packet.SequenceNumber)
	case <-time.After(200 * time.Millisecond):
	}

	if len(unpublisher.sids) != 1 || unpublisher.sids[0] != "TR_test" {
		t.Fatalf("unpublished track SIDs = %v, want [TR_test]", unpublisher.sids)
	}
}
