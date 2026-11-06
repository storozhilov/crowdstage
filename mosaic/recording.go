package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-gst/go-gst/pkg/gst"
	"github.com/go-gst/go-gst/pkg/gstapp"
)

const defaultSampleDuration = 33 * time.Millisecond

var gstInitOnce sync.Once

type mediaRecorder struct {
	mu        sync.Mutex
	pipeline  gst.Pipeline
	source    gstapp.AppSrc
	timestamp gst.ClockTime
	closed    bool
}

func newMediaRecorder(path string, width, height int) (*mediaRecorder, error) {
	gstInitOnce.Do(gst.Init)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create recordings directory: %w", err)
	}

	element, err := gst.ParseLaunch("appsrc name=src ! matroskamux ! filesink name=sink")
	if err != nil {
		return nil, fmt.Errorf("create GStreamer recording pipeline: %w", err)
	}

	pipeline, ok := element.(gst.Pipeline)
	if !ok {
		return nil, fmt.Errorf("create GStreamer recording pipeline: parsed element is not a pipeline")
	}

	source, ok := pipeline.GetByName("src").(gstapp.AppSrc)
	if !ok {
		return nil, fmt.Errorf("create GStreamer recording pipeline: missing appsrc")
	}
	sink := pipeline.GetByName("sink")
	if sink == nil {
		return nil, fmt.Errorf("create GStreamer recording pipeline: missing filesink")
	}

	caps := "video/x-vp8"
	if width > 0 && height > 0 {
		caps = fmt.Sprintf("video/x-vp8,width=%d,height=%d", width, height)
	}
	source.SetObjectProperty("caps", gst.CapsFromString(caps))
	source.SetObjectProperty("format", gst.FormatTime)
	sink.SetObjectProperty("location", path)

	if ret := pipeline.SetState(gst.StatePlaying); ret == gst.StateChangeFailure {
		pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("start GStreamer recording pipeline")
	}

	return &mediaRecorder{
		pipeline: pipeline,
		source:   source,
	}, nil
}

func (r *mediaRecorder) writeSample(data []byte, duration time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("recording pipeline is closed")
	}
	if duration <= 0 {
		duration = defaultSampleDuration
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

	buffer.SetPTS(r.timestamp)
	buffer.SetDuration(gst.ClockTime(duration))
	r.timestamp += gst.ClockTime(duration)

	if ret := r.source.PushBuffer(buffer); ret != gst.FlowOK {
		return fmt.Errorf("push GStreamer buffer: %s", ret)
	}

	return nil
}

func (r *mediaRecorder) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	r.source.EndOfStream()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var closeErr error
	eos := false
	for message := range r.pipeline.GetBus().Messages(ctx) {
		switch message.Type() {
		case gst.MessageEOS:
			eos = true
		case gst.MessageError:
			debug, err := message.ParseError()
			closeErr = fmt.Errorf("GStreamer recording pipeline: %w (%s)", err, debug)
		}
		if eos || closeErr != nil {
			break
		}
	}

	r.pipeline.SetState(gst.StateNull)

	if closeErr != nil {
		return closeErr
	}
	if !eos {
		return fmt.Errorf("wait for GStreamer recording pipeline to finish")
	}
	return nil
}
