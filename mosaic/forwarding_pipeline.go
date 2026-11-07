package main

import (
	"fmt"
	"sync"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/rtp"

	"github.com/go-gst/go-gst/pkg/gst"
	"github.com/go-gst/go-gst/pkg/gstapp"
)

const forwarderPullTimeout = 100 * time.Millisecond

type rtpForwarder struct {
	mu         sync.Mutex
	pipeline   gst.Pipeline
	source     gstapp.AppSrc
	sink       gstapp.AppSink
	target     rtpTarget
	timestamp  gst.ClockTime
	packets    chan *rtp.Packet
	stop       chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
	closed     bool
	writeErrMu sync.Mutex
	writeErr   error
}

func newRTPForwarder(target rtpTarget) (*rtpForwarder, error) {
	gstInitOnce.Do(gst.Init)

	element, err := gst.ParseLaunch("appsrc name=src ! appsink name=sink sync=false")
	if err != nil {
		return nil, fmt.Errorf("create GStreamer forwarding pipeline: %w", err)
	}

	pipeline, ok := element.(gst.Pipeline)
	if !ok {
		return nil, fmt.Errorf("create GStreamer forwarding pipeline: parsed element is not a pipeline")
	}

	source, ok := pipeline.GetByName("src").(gstapp.AppSrc)
	if !ok {
		return nil, fmt.Errorf("create GStreamer forwarding pipeline: missing appsrc")
	}
	sink, ok := pipeline.GetByName("sink").(gstapp.AppSink)
	if !ok {
		return nil, fmt.Errorf("create GStreamer forwarding pipeline: missing appsink")
	}

	caps := gst.CapsFromString("application/x-rtp")
	source.SetObjectProperty("caps", caps)
	sink.SetObjectProperty("caps", caps)
	source.SetObjectProperty("format", gst.FormatTime)

	if ret := pipeline.SetState(gst.StatePlaying); ret == gst.StateChangeFailure {
		pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("start GStreamer forwarding pipeline")
	}

	forwarder := &rtpForwarder{
		pipeline: pipeline,
		source:   source,
		sink:     sink,
		target:   target,
		packets:  make(chan *rtp.Packet),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go forwarder.forward()
	go forwarder.write()

	return forwarder, nil
}

func (f *rtpForwarder) WriteRTP(packet *rtp.Packet, _ *lksdk.SampleWriteOptions) error {
	data, err := packet.Marshal()
	if err != nil {
		return fmt.Errorf("marshal RTP packet: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return fmt.Errorf("GStreamer forwarding pipeline is closed")
	}
	f.writeErrMu.Lock()
	writeErr := f.writeErr
	f.writeErrMu.Unlock()
	if writeErr != nil {
		return fmt.Errorf("forward RTP packet to studio room: %w", writeErr)
	}

	buffer := gst.NewBufferAllocate(nil, uint(len(data)), nil)
	mapped, ok := buffer.Map(gst.MapWrite)
	if !ok {
		return fmt.Errorf("map GStreamer buffer")
	}
	if _, err := mapped.Write(data); err != nil {
		mapped.Unmap()
		return fmt.Errorf("write GStreamer buffer: %w", err)
	}
	mapped.Unmap()

	buffer.SetPTS(f.timestamp)
	buffer.SetDuration(gst.ClockTime(defaultSampleDuration))
	f.timestamp += gst.ClockTime(defaultSampleDuration)

	if ret := f.source.PushBuffer(buffer); ret != gst.FlowOK {
		return fmt.Errorf("push GStreamer buffer: %s", ret)
	}

	return nil
}

func (f *rtpForwarder) forward() {
	defer close(f.done)

	for {
		select {
		case <-f.stop:
			return
		default:
		}

		sample := f.sink.TryPullSample(gst.ClockTime(forwarderPullTimeout))
		if sample == nil {
			if f.sink.IsEOS() {
				return
			}
			continue
		}

		buffer := sample.GetBuffer()
		if buffer == nil {
			continue
		}

		mapped, ok := buffer.Map(gst.MapRead)
		if !ok {
			continue
		}
		data := append([]byte(nil), mapped.Data()...)
		mapped.Unmap()

		packet := &rtp.Packet{}
		if err := packet.Unmarshal(data); err != nil {
			continue
		}

		select {
		case f.packets <- packet:
		case <-f.stop:
			return
		}
	}
}

func (f *rtpForwarder) write() {
	for {
		select {
		case <-f.stop:
			return
		default:
		}

		select {
		case <-f.stop:
			return
		case packet := <-f.packets:
			if err := f.target.WriteRTP(packet, nil); err != nil {
				f.writeErrMu.Lock()
				f.writeErr = err
				f.writeErrMu.Unlock()
			}
		}
	}
}

func (f *rtpForwarder) close() error {
	var closeErr error

	f.closeOnce.Do(func() {
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()

		close(f.stop)
		// Only wait for the appsink reader. The downstream write may be blocked
		// indefinitely, so it is cancelled through f.stop instead of awaited.
		<-f.done

		if ret := f.pipeline.SetState(gst.StateNull); ret == gst.StateChangeFailure {
			closeErr = fmt.Errorf("stop GStreamer forwarding pipeline")
		}
	})

	return closeErr
}
