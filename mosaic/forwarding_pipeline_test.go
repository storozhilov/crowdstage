package main

import (
	"bytes"
	"testing"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/rtp"
)

type gstreamerPacketTarget struct {
	writes chan *rtp.Packet
}

func (t *gstreamerPacketTarget) WriteRTP(packet *rtp.Packet, _ *lksdk.SampleWriteOptions) error {
	t.writes <- packet
	return nil
}

func TestRTPForwarderDeliversPacketsThroughGStreamer(t *testing.T) {
	target := &gstreamerPacketTarget{writes: make(chan *rtp.Packet, 4)}
	forwarder, err := newRTPForwarder(target)
	if err != nil {
		t.Fatalf("create RTP forwarder: %v", err)
	}
	defer func() {
		if err := forwarder.close(); err != nil {
			t.Errorf("close RTP forwarder: %v", err)
		}
	}()

	packets := []*rtp.Packet{
		{
			Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: 100, Timestamp: 1000, SSRC: 42},
			Payload: []byte{0x10, 0x20, 0x30},
		},
		{
			Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: 101, Timestamp: 4000, SSRC: 42},
			Payload: []byte{0x40, 0x50, 0x60},
		},
	}

	for _, packet := range packets {
		if err := forwarder.WriteRTP(packet, nil); err != nil {
			t.Fatalf("write RTP packet through GStreamer pipeline: %v", err)
		}
	}

	for _, want := range packets {
		select {
		case got := <-target.writes:
			if got.SequenceNumber != want.SequenceNumber ||
				got.Timestamp != want.Timestamp ||
				got.SSRC != want.SSRC ||
				got.PayloadType != want.PayloadType ||
				!bytes.Equal(got.Payload, want.Payload) {
				t.Fatalf("forwarded RTP packet = %+v, want %+v", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for RTP packet to pass through the GStreamer pipeline")
		}
	}
}
