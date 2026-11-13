package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/go-gst/go-gst/pkg/gst"
	"github.com/storozhilov/crowdstage/shared/livekitgst"
)

const (
	mediaMixerFrameRate = 30
	mediaMixerBitrate   = 1000000

	staticPictureWidth  = 1280
	staticPictureHeight = 720
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

func startMediaMixerPipeline(config config, source *studioVideo, sourceName string) (*mediaMixerPipeline, error) {
	gstInitOnce.Do(gst.Init)

	element, err := gst.ParseLaunch(mediaMixerPipelineDescription(config, source, sourceName))
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

	if source != nil {
		log.Printf(
			"forwarding studio video to LiveKit room %q: participant=%q identity=%q",
			config.OutputRoom,
			source.participantSID,
			source.participantIdentity,
		)
	} else {
		log.Printf("streaming static picture to LiveKit room %q", config.OutputRoom)
	}
	return mixer, nil
}

func (p *mediaMixerPipeline) stop() error {
	p.cancel()
	<-p.done

	if ret := p.pipeline.SetState(gst.StateNull); ret == gst.StateChangeFailure {
		return fmt.Errorf("stop GStreamer media mixer pipeline")
	}
	log.Printf("stopped publishing to LiveKit room %q", p.config.OutputRoom)
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

func mediaMixerPipelineDescription(config config, source *studioVideo, sourceName string) string {
	sink := livekitgst.SinkDescription(livekitgst.Endpoint{
		URL:       config.LiveKitURL,
		APIKey:    config.APIKey,
		APISecret: config.APISecret,
		Room:      config.OutputRoom,
		Identity:  config.Identity,
	}, "video/x-vp8")

	if source == nil {
		return fmt.Sprintf(`videotestsrc is-live=true pattern=gradient !
video/x-raw,width=%d,height=%d,framerate=%d/1 !
videoconvert !
vp8enc deadline=1 keyframe-max-dist=%d target-bitrate=%d !
queue !
%s`,
			staticPictureWidth,
			staticPictureHeight,
			mediaMixerFrameRate,
			mediaMixerFrameRate,
			mediaMixerBitrate,
			sink,
		)
	}

	return fmt.Sprintf(`%s !
queue !
decodebin !
videoconvert !
vp8enc deadline=1 keyframe-max-dist=%d target-bitrate=%d !
queue !
%s`,
		livekitgst.SourceDescription(sourceName, livekitgst.Endpoint{
			URL:       config.LiveKitURL,
			APIKey:    config.APIKey,
			APISecret: config.APISecret,
			Room:      config.InputRoom,
			Identity:  config.Identity,
		}, source.participantIdentity),
		mediaMixerFrameRate,
		mediaMixerBitrate,
		sink,
	)
}
