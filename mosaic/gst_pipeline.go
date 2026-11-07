package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/go-gst/go-gst/pkg/gst"
)

const (
	backgroundWidth     = 1280
	backgroundHeight    = 720
	backgroundFrameRate = 30
)

var gstInitOnce sync.Once

type mosaicPipeline struct {
	pipeline gst.Pipeline
	cancel   context.CancelFunc
	done     chan struct{}
}

func startMosaicPipeline(config config) (*mosaicPipeline, error) {
	gstInitOnce.Do(gst.Init)

	element, err := gst.ParseLaunch(pipelineDescription(config))
	if err != nil {
		return nil, fmt.Errorf("create GStreamer mosaic pipeline: %w", err)
	}

	pipeline, ok := element.(gst.Pipeline)
	if !ok {
		return nil, fmt.Errorf("create GStreamer mosaic pipeline: parsed element is not a pipeline")
	}

	if ret := pipeline.SetState(gst.StatePlaying); ret == gst.StateChangeFailure {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("start GStreamer mosaic pipeline")
	}

	ctx, cancel := context.WithCancel(context.Background())
	mosaic := &mosaicPipeline{
		pipeline: pipeline,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	go mosaic.watchBus(ctx)

	return mosaic, nil
}

func (p *mosaicPipeline) stop() error {
	p.cancel()
	<-p.done
	if ret := p.pipeline.SetState(gst.StateNull); ret == gst.StateChangeFailure {
		return fmt.Errorf("stop GStreamer mosaic pipeline")
	}
	return nil
}

func (p *mosaicPipeline) watchBus(ctx context.Context) {
	defer close(p.done)

	for message := range p.pipeline.GetBus().Messages(ctx) {
		switch message.Type() {
		case gst.MessageError:
			debug, err := message.ParseError()
			log.Printf("GStreamer mosaic pipeline error: %v (%s)", err, debug)
		case gst.MessageEOS:
			log.Printf("GStreamer mosaic pipeline reached end of stream")
		}
	}
}

func pipelineDescription(config config) string {
	participantIdentity := config.Identity + "-participant"

	return fmt.Sprintf(`videotestsrc is-live=true pattern=gradient !
video/x-raw,width=%d,height=%d,framerate=%d/1 !
videoconvert !
vp8enc deadline=1 keyframe-max-dist=%d target-bitrate=1000000 !
queue !
livekitwebrtcsink
  signaller::ws-url=%s
  signaller::api-key=%s
  signaller::secret-key=%s
  signaller::room-name=%s
  signaller::identity=%s
  signaller::participant-name=%s
  video-caps="video/x-vp8"
  stun-server=""
livekitwebrtcsrc
  signaller::ws-url=%s
  signaller::api-key=%s
  signaller::secret-key=%s
  signaller::room-name=%s
  signaller::identity=%s
  signaller::participant-name=%s
  signaller::producer-peer-id=%s
  stun-server=""
! fakesink sync=false async=false`,
		backgroundWidth,
		backgroundHeight,
		backgroundFrameRate,
		backgroundFrameRate,
		gstString(config.LiveKitURL),
		gstString(config.APIKey),
		gstString(config.APISecret),
		gstString(config.StudioRoomName),
		gstString(config.Identity),
		gstString(config.Identity),
		gstString(config.LiveKitURL),
		gstString(config.APIKey),
		gstString(config.APISecret),
		gstString(config.RoomName),
		gstString(participantIdentity),
		gstString(participantIdentity),
		gstString("__ignored__"),
	)
}

func gstString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
