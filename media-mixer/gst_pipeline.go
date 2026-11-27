package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/pkg/gst"
	"github.com/storozhilov/crowdstage/shared/livekitgst"
)

const (
	mediaMixerFrameRate = 30
	mediaMixerBitrate   = 1000000

	staticPictureWidth  = 1280
	staticPictureHeight = 720

	sourceAttachTimeout = 10 * time.Second
)

var gstInitOnce sync.Once

type mediaMixerPipeline struct {
	config     config
	pipeline   gst.Pipeline
	mixer      gst.Element
	audioMixer gst.Element
	cancel     context.CancelFunc
	done       chan struct{}

	failureMu  sync.Mutex
	failureErr error

	opsMu sync.Mutex

	mu               sync.Mutex
	closed           bool
	source           *mediaMixerSource
	pending          *mediaMixerSource
	nextZOrder       uint32
	audioSources     map[string]*mediaMixerAudioSource
	audioLevelUpdate func(string, float64)
}

type mediaMixerSource struct {
	lifecycle    sync.Mutex
	name         string
	element      gst.Element
	queue        gst.Element
	decodebin    gst.Element
	convert      gst.Element
	scale        gst.Element
	caps         gst.Element
	elements     []gst.Element
	sourcePad    gst.Pad
	decodebinPad gst.Pad
	mixerPad     gst.Pad
	linked       bool
	removed      bool

	ready     chan struct{}
	readyOnce sync.Once
	attachErr error
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

	audioMixer := pipeline.GetByName("audio-mixer")
	if audioMixer == nil {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("create GStreamer media mixer pipeline: audio mixer is missing")
	}

	mixerPipeline := &mediaMixerPipeline{
		config:       config,
		pipeline:     pipeline,
		mixer:        mixer,
		audioMixer:   audioMixer,
		nextZOrder:   2,
		audioSources: make(map[string]*mediaMixerAudioSource),
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

func (p *mediaMixerPipeline) setAudioLevelListener(listener func(publicationSID string, level float64)) {
	p.mu.Lock()
	p.audioLevelUpdate = listener
	p.mu.Unlock()
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
	p.pending = nil
	audioSources := make([]*mediaMixerAudioSource, 0, len(p.audioSources))
	for _, source := range p.audioSources {
		audioSources = append(audioSources, source)
	}
	p.audioSources = make(map[string]*mediaMixerAudioSource)
	p.mu.Unlock()

	for _, source := range audioSources {
		p.stopAudioSource(source)
	}

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

	if source == nil {
		p.clearSourceLocked()
		return nil
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("media mixer pipeline is closed")
	}
	previous := p.source
	p.mu.Unlock()

	studioSource, err := newMediaMixerSource(p.config, source, sourceName)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.pending = studioSource
	p.mu.Unlock()

	if err := p.addSource(studioSource); err != nil {
		p.mu.Lock()
		if p.pending == studioSource {
			p.pending = nil
		}
		p.mu.Unlock()
		p.removeSourceElements(studioSource)
		return err
	}

	for _, element := range studioSource.elements {
		element.SyncStateWithParent()
	}

	select {
	case <-studioSource.ready:
		err = studioSource.attachErr
	case <-time.After(sourceAttachTimeout):
		err = fmt.Errorf("timed out waiting for the studio source to attach")
	}
	if err != nil {
		p.mu.Lock()
		if p.pending == studioSource {
			p.pending = nil
		}
		p.mu.Unlock()
		p.stopSource(studioSource)
		return err
	}

	p.mu.Lock()
	if p.pending == studioSource {
		p.pending = nil
	}
	p.source = studioSource
	p.mu.Unlock()

	if previous != nil {
		p.stopSource(previous)
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
		ready:     make(chan struct{}),
	}, nil
}

func (s *mediaMixerSource) markReady(err error) {
	s.readyOnce.Do(func() {
		s.attachErr = err
		close(s.ready)
	})
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
	if p.nextZOrder == 0 {
		p.nextZOrder = 2
	}
	mixerPad.SetObjectProperties(map[string]any{
		"xpos":             int32(0),
		"ypos":             int32(0),
		"width":            int32(staticPictureWidth),
		"height":           int32(staticPictureHeight),
		"zorder":           p.nextZOrder,
		"repeat-after-eos": true,
	})
	p.nextZOrder++
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
	current := !p.closed && (p.source == source || p.pending == source) && !source.removed
	p.mu.Unlock()
	if !current {
		return
	}
	if source.sourcePad != nil {
		log.Printf("studio source %q: ignoring additional video pad %q", source.name, sourcePad.GetName())
		return
	}

	if ret := sourcePad.Link(source.queue.GetStaticPad("sink")); ret != gst.PadLinkOK {
		err := fmt.Errorf("link video pad to queue: %s", ret)
		log.Printf("studio source %q: %v", source.name, err)
		source.markReady(err)
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
	current := !p.closed && (p.source == source || p.pending == source) && !source.removed
	linked := source.linked
	p.mu.Unlock()
	if !current {
		return
	}

	if ret := pad.Link(source.convert.GetStaticPad("sink")); ret != gst.PadLinkOK {
		err := fmt.Errorf("link decoded video to converter: %s", ret)
		log.Printf("studio source %q: %v", source.name, err)
		source.markReady(err)
		return
	}
	source.decodebinPad = pad
	if linked {
		return
	}

	source.mixerPad.AddProbe(gst.PadProbeTypeBuffer, func(gst.Pad, *gst.PadProbeInfo) gst.PadProbeReturn {
		source.markReady(nil)
		return gst.PadProbeRemove
	})
	if ret := source.caps.GetStaticPad("src").Link(source.mixerPad); ret != gst.PadLinkOK {
		err := fmt.Errorf("link scaled video to compositor: %s", ret)
		log.Printf("studio source %q: %v", source.name, err)
		source.markReady(err)
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
	source.markReady(fmt.Errorf("studio source was removed"))

	p.removeSourceElements(source)
	log.Printf("removed studio source %q", source.name)
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
	unlinkStaticPads(source.queue, "src", source.decodebin, "sink")
	if source.decodebinPad != nil && source.convert != nil {
		if convertSink := source.convert.GetStaticPad("sink"); convertSink != nil {
			source.decodebinPad.Unlink(convertSink)
		}
	}
	unlinkStaticPads(source.convert, "src", source.scale, "sink")
	unlinkStaticPads(source.scale, "src", source.caps, "sink")
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

func unlinkStaticPads(source gst.Element, sourcePadName string, sink gst.Element, sinkPadName string) {
	if source == nil || sink == nil {
		return
	}
	sourcePad := source.GetStaticPad(sourcePadName)
	sinkPad := sink.GetStaticPad(sinkPadName)
	if sourcePad != nil && sinkPad != nil {
		sourcePad.Unlink(sinkPad)
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
	sink := livekitgst.AudioVideoSinkDescription(
		"sink",
		livekitgst.Endpoint{
			URL:       config.LiveKitURL,
			APIKey:    config.APIKey,
			APISecret: config.APISecret,
			Room:      config.OutputRoom,
			Identity:  config.Identity,
		},
		"video/x-vp8",
		"audio/x-opus",
	)

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
%s

audiotestsrc is-live=true wave=silence !
audio/x-raw,rate=48000,channels=2 !
audioconvert !
audioresample !
audio/x-raw,format=F32LE,rate=48000,channels=2,layout=interleaved !
audiomixer name=audio-mixer !
audio/x-raw,format=F32LE,rate=48000,channels=2,layout=interleaved !
audioconvert !
audioresample !
opusenc !
queue !
sink.`,
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
	room := source.room
	if room == "" {
		room = config.InputRoom
	}
	return livekitgst.SourceDescription(sourceName, livekitgst.Endpoint{
		URL:       config.LiveKitURL,
		APIKey:    config.APIKey,
		APISecret: config.APISecret,
		Room:      room,
		Identity:  config.Identity,
	}, source.participantIdentity)
}

func mediaMixerAudioSourceDescription(config config, sourceName string, source *audioSource) string {
	room := source.room
	if room == "" {
		room = config.InputRoom
	}
	return livekitgst.SourceDescription(sourceName, livekitgst.Endpoint{
		URL:       config.LiveKitURL,
		APIKey:    config.APIKey,
		APISecret: config.APISecret,
		Room:      room,
		Identity:  config.Identity,
	}, source.participantIdentity)
}
