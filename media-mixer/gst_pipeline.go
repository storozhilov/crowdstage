package main

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	mixer    gst.Element
	cancel   context.CancelFunc
	done     chan struct{}

	failureMu  sync.Mutex
	failureErr error

	opsMu sync.Mutex

	mu     sync.Mutex
	closed bool
	source *mediaMixerSource
}

type mediaMixerSource struct {
	lifecycle sync.Mutex
	name      string
	element   gst.Element
	queue     gst.Element
	decodebin gst.Element
	convert   gst.Element
	scale     gst.Element
	caps      gst.Element
	elements  []gst.Element
	sourcePad gst.Pad
	mixerPad  gst.Pad
	linked    bool
	removed   bool
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

	mixer := pipeline.GetByName("mixer")
	if mixer == nil {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("create GStreamer media mixer pipeline: mixer is missing")
	}

	mixerPipeline := &mediaMixerPipeline{
		config:   config,
		pipeline: pipeline,
		mixer:    mixer,
	}

	if ret := pipeline.SetState(gst.StatePlaying); ret == gst.StateChangeFailure {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("start GStreamer media mixer pipeline")
	}

	ctx, cancel := context.WithCancel(context.Background())
	mixerPipeline.cancel = cancel
	mixerPipeline.done = make(chan struct{})
	go mixerPipeline.watchBus(ctx)

	log.Printf("streaming static picture to LiveKit room %q", config.OutputRoom)
	return mixerPipeline, nil
}

func (p *mediaMixerPipeline) stop() error {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}

	p.mu.Lock()
	p.closed = true
	p.source = nil
	p.mu.Unlock()

	if p.pipeline != nil {
		if ret := p.pipeline.SetState(gst.StateNull); ret == gst.StateChangeFailure {
			return fmt.Errorf("stop GStreamer media mixer pipeline")
		}
	}
	log.Printf("stopped publishing to LiveKit room %q", p.config.OutputRoom)
	return nil
}

func (p *mediaMixerPipeline) setSource(source *studioVideo, sourceName string) error {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	p.clearSourceLocked()
	if source == nil {
		return nil
	}

	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return fmt.Errorf("media mixer pipeline is closed")
	}

	studioSource, err := newMediaMixerSource(p.config, source, sourceName)
	if err != nil {
		return err
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("media mixer pipeline is closed")
	}
	p.source = studioSource
	p.mu.Unlock()

	if err := p.addSource(studioSource); err != nil {
		p.mu.Lock()
		if p.source == studioSource {
			p.source = nil
		}
		p.mu.Unlock()
		p.removeSourceElements(studioSource)
		return err
	}

	for _, element := range studioSource.elements {
		element.SyncStateWithParent()
	}

	log.Printf(
		"forwarding studio video to LiveKit room %q: participant=%q identity=%q",
		p.config.OutputRoom,
		source.participantIdentity,
		p.config.Identity+"-"+sourceName,
	)
	return nil
}

func (p *mediaMixerPipeline) clearSource() {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	p.clearSourceLocked()
}

func (p *mediaMixerPipeline) clearSourceLocked() {
	p.mu.Lock()
	source := p.source
	p.source = nil
	p.mu.Unlock()

	if source != nil {
		p.stopSource(source)
	}
}

func newMediaMixerSource(config config, source *studioVideo, sourceName string) (*mediaMixerSource, error) {
	element, err := gst.ParseLaunch(mediaMixerSourceDescription(config, sourceName, source))
	if err != nil {
		return nil, fmt.Errorf("create studio source %q: %w", sourceName, err)
	}
	if element == nil {
		return nil, fmt.Errorf("create studio source %q: parsed element is nil", sourceName)
	}

	queue := gst.ElementFactoryMake("queue", sourceName+"-queue")
	decodebin := gst.ElementFactoryMake("decodebin", sourceName+"-decodebin")
	convert := gst.ElementFactoryMake("videoconvert", sourceName+"-convert")
	scale := gst.ElementFactoryMake("videoscale", sourceName+"-scale")
	caps := gst.ElementFactoryMake("capsfilter", sourceName+"-caps")
	if queue == nil || decodebin == nil || convert == nil || scale == nil || caps == nil {
		return nil, fmt.Errorf("create studio source chain for %q", sourceName)
	}

	caps.SetObjectProperty("caps", gst.CapsFromString(fmt.Sprintf(
		"video/x-raw,width=%d,height=%d,pixel-aspect-ratio=1/1",
		staticPictureWidth,
		staticPictureHeight,
	)))

	return &mediaMixerSource{
		name:      sourceName,
		element:   element,
		queue:     queue,
		decodebin: decodebin,
		convert:   convert,
		scale:     scale,
		caps:      caps,
		elements:  []gst.Element{element, queue, decodebin, convert, scale, caps},
	}, nil
}

func (p *mediaMixerPipeline) addSource(source *mediaMixerSource) error {
	for _, element := range source.elements {
		if !p.pipeline.Add(element) {
			return fmt.Errorf("could not add %s to pipeline", element.GetName())
		}
	}

	if !source.queue.Link(source.decodebin) {
		return fmt.Errorf("link studio source queue to decodebin")
	}
	if !source.convert.Link(source.scale) {
		return fmt.Errorf("link studio source converter to scaler")
	}
	if !source.scale.Link(source.caps) {
		return fmt.Errorf("link studio source scaler to caps")
	}

	mixerPad := p.mixer.RequestPadSimple("sink_%u")
	if mixerPad == nil {
		return fmt.Errorf("could not request compositor pad for the studio source")
	}
	mixerPad.SetObjectProperties(map[string]any{
		"xpos":   int32(0),
		"ypos":   int32(0),
		"width":  int32(staticPictureWidth),
		"height": int32(staticPictureHeight),
		"zorder": uint32(1),
	})
	source.mixerPad = mixerPad

	source.element.ConnectPadAdded(func(_ gst.Element, pad gst.Pad) {
		p.sourcePadAdded(source, pad)
	})
	source.decodebin.ConnectPadAdded(func(_ gst.Element, pad gst.Pad) {
		p.decodebinPadAdded(source, pad)
	})
	return nil
}

func (p *mediaMixerPipeline) sourcePadAdded(source *mediaMixerSource, sourcePad gst.Pad) {
	if !strings.HasPrefix(sourcePad.GetName(), "video_") {
		return
	}

	source.lifecycle.Lock()
	defer source.lifecycle.Unlock()

	p.mu.Lock()
	current := !p.closed && p.source == source && !source.removed
	p.mu.Unlock()
	if !current {
		return
	}
	if source.sourcePad != nil {
		log.Printf("studio source %q: ignoring additional video pad %q", source.name, sourcePad.GetName())
		return
	}

	if ret := sourcePad.Link(source.queue.GetStaticPad("sink")); ret != gst.PadLinkOK {
		log.Printf("studio source %q: link video pad to queue: %s", source.name, ret)
		return
	}
	source.sourcePad = sourcePad
}

func (p *mediaMixerPipeline) decodebinPadAdded(source *mediaMixerSource, pad gst.Pad) {
	caps := pad.GetCurrentCaps()
	if caps == nil || caps.GetSize() == 0 || !caps.GetStructure(0).HasName("video/x-raw") {
		return
	}

	source.lifecycle.Lock()
	defer source.lifecycle.Unlock()

	p.mu.Lock()
	current := !p.closed && p.source == source && !source.removed
	linked := source.linked
	p.mu.Unlock()
	if !current {
		return
	}

	if ret := pad.Link(source.convert.GetStaticPad("sink")); ret != gst.PadLinkOK {
		log.Printf("studio source %q: link decoded video to converter: %s", source.name, ret)
		return
	}
	if linked {
		return
	}

	if ret := source.caps.GetStaticPad("src").Link(source.mixerPad); ret != gst.PadLinkOK {
		log.Printf("studio source %q: link scaled video to compositor: %s", source.name, ret)
		return
	}
	p.mu.Lock()
	source.linked = true
	p.mu.Unlock()
}

func (p *mediaMixerPipeline) stopSource(source *mediaMixerSource) {
	source.lifecycle.Lock()
	defer source.lifecycle.Unlock()

	p.mu.Lock()
	source.removed = true
	p.mu.Unlock()

	p.removeSourceElements(source)
	log.Printf("stopped forwarding studio source %q", source.name)
}

func (p *mediaMixerPipeline) removeSourceElements(source *mediaMixerSource) {
	for _, element := range source.elements {
		_ = element.SetState(gst.StateNull)
	}
	if source.sourcePad != nil && source.queue != nil {
		if queueSink := source.queue.GetStaticPad("sink"); queueSink != nil {
			source.sourcePad.Unlink(queueSink)
		}
	}
	if source.caps != nil && source.mixerPad != nil {
		if capsSource := source.caps.GetStaticPad("src"); capsSource != nil {
			capsSource.Unlink(source.mixerPad)
		}
	}
	for _, element := range source.elements {
		p.pipeline.Remove(element)
	}
	if source.mixerPad != nil && p.mixer != nil {
		p.mixer.ReleaseRequestPad(source.mixerPad)
		source.mixerPad = nil
	}
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
	sink := livekitgst.SinkDescription(livekitgst.Endpoint{
		URL:       config.LiveKitURL,
		APIKey:    config.APIKey,
		APISecret: config.APISecret,
		Room:      config.OutputRoom,
		Identity:  config.Identity,
	}, "video/x-vp8")

	return fmt.Sprintf(`videotestsrc is-live=true pattern=gradient !
video/x-raw,width=%d,height=%d,framerate=%d/1 !
videoconvert !
compositor name=mixer
  sink_0::xpos=0
  sink_0::ypos=0
  sink_0::width=%d
  sink_0::height=%d
  sink_0::zorder=0
!
video/x-raw,width=%d,height=%d,framerate=%d/1 !
videoconvert !
vp8enc deadline=1 keyframe-max-dist=%d target-bitrate=%d !
queue !
%s`,
		staticPictureWidth,
		staticPictureHeight,
		mediaMixerFrameRate,
		staticPictureWidth,
		staticPictureHeight,
		staticPictureWidth,
		staticPictureHeight,
		mediaMixerFrameRate,
		mediaMixerFrameRate,
		mediaMixerBitrate,
		sink,
	)
}

func mediaMixerSourceDescription(config config, sourceName string, source *studioVideo) string {
	return livekitgst.SourceDescription(sourceName, livekitgst.Endpoint{
		URL:       config.LiveKitURL,
		APIKey:    config.APIKey,
		APISecret: config.APISecret,
		Room:      config.InputRoom,
		Identity:  config.Identity,
	}, source.participantIdentity)
}
