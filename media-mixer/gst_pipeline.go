package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/go-gst/go-gst/pkg/gst"
	gstutil "github.com/storozhilov/crowdstage/shared/gstutil"
)

const (
	pictureWidth     = 1280
	pictureHeight    = 720
	pictureFrameRate = 30
)

var gstInitOnce sync.Once

type mediaMixerPipeline struct {
	config   config
	pipeline gst.Pipeline
	cancel   context.CancelFunc
	done     chan struct{}

	failureMu  sync.Mutex
	failureErr error
}

func startMediaMixerPipeline(config config) (*mediaMixerPipeline, error) {
	gstInitOnce.Do(gst.Init)

	element, err := gst.ParseLaunch(mediaMixerPipelineDescription(config))
	if err != nil {
		return nil, fmt.Errorf("create GStreamer media mixer pipeline: %w", err)
	}

	pipeline, ok := element.(gst.Pipeline)
	if !ok {
		return nil, fmt.Errorf("create GStreamer media mixer pipeline: parsed element is not a pipeline")
	}

	mixer := &mediaMixerPipeline{
		config:   config,
		pipeline: pipeline,
	}

	if ret := pipeline.SetState(gst.StatePlaying); ret == gst.StateChangeFailure {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("start GStreamer media mixer pipeline")
	}

	ctx, cancel := context.WithCancel(context.Background())
	mixer.cancel = cancel
	mixer.done = make(chan struct{})
	go mixer.watchBus(ctx)

	log.Printf("streaming picture to LiveKit room %q", config.OutputRoom)
	return mixer, nil
}

func (p *mediaMixerPipeline) stop() error {
	p.cancel()
	<-p.done

	if ret := p.pipeline.SetState(gst.StateNull); ret == gst.StateChangeFailure {
		return fmt.Errorf("stop GStreamer media mixer pipeline")
	}
	log.Printf("stopped streaming picture to LiveKit room %q", p.config.OutputRoom)
	return nil
}

func (p *mediaMixerPipeline) watchBus(ctx context.Context) {
	defer close(p.done)

	for message := range p.pipeline.GetBus().Messages(ctx) {
		switch message.Type() {
		case gst.MessageError:
			debug, err := message.ParseError()
			log.Printf("GStreamer media mixer pipeline error: %v (%s)", err, debug)
			p.markFailed(fmt.Errorf("pipeline error: %w", err))
		case gst.MessageEOS:
			log.Printf("GStreamer media mixer pipeline reached end of stream")
			p.markFailed(fmt.Errorf("pipeline reached end of stream"))
		}
	}
}

func (p *mediaMixerPipeline) markFailed(err error) {
	if err == nil {
		return
	}

	p.failureMu.Lock()
	if p.failureErr == nil {
		p.failureErr = err
	}
	p.failureMu.Unlock()
}

func (p *mediaMixerPipeline) failure() error {
	p.failureMu.Lock()
	defer p.failureMu.Unlock()
	return p.failureErr
}

func mediaMixerPipelineDescription(config config) string {
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
  stun-server=""`,
		pictureWidth,
		pictureHeight,
		pictureFrameRate,
		pictureFrameRate,
		gstutil.String(config.LiveKitURL),
		gstutil.String(config.APIKey),
		gstutil.String(config.APISecret),
		gstutil.String(config.OutputRoom),
		gstutil.String(config.Identity),
		gstutil.String(config.Identity),
	)
}
