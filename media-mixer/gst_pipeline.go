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
	sourceElementName = "media-mixer-source"
	targetElementName = "media-mixer-target"

	mediaTypeAudio = "audio"
	mediaTypeVideo = "video"
)

var gstInitOnce sync.Once

type mediaMixerPipeline struct {
	config   config
	pipeline gst.Pipeline
	source   gst.Element
	target   gst.Element
	cancel   context.CancelFunc
	done     chan struct{}

	opsMu sync.Mutex
	mu    sync.Mutex

	closed   bool
	branches map[string]gst.Element
	tracks   map[string]*forwardedTrack
}

type forwardedTrack struct {
	mediaType string
	sourcePad gst.Pad
	branch    gst.Element
}

func startMediaMixerPipeline(config config) (*mediaMixerPipeline, error) {
	gstInitOnce.Do(gst.Init)

	pipeline, ok := gst.NewPipeline("media-mixer").(gst.Pipeline)
	if !ok {
		return nil, fmt.Errorf("create GStreamer media mixer pipeline")
	}

	source, err := newSource(config)
	if err != nil {
		return nil, fmt.Errorf("create GStreamer media mixer pipeline: %w", err)
	}
	if !pipeline.Add(source) {
		return nil, fmt.Errorf("create GStreamer media mixer pipeline: add source")
	}

	target, err := newTarget(config)
	if err != nil {
		return nil, fmt.Errorf("create GStreamer media mixer pipeline: %w", err)
	}
	if !pipeline.Add(target) {
		return nil, fmt.Errorf("create GStreamer media mixer pipeline: add target")
	}

	mixer := &mediaMixerPipeline{
		config:   config,
		pipeline: pipeline,
		source:   source,
		target:   target,
		branches: make(map[string]gst.Element),
		tracks:   make(map[string]*forwardedTrack),
	}

	for _, branchSpec := range []struct {
		mediaType string
		suffix    int
		padName   string
	}{
		{mediaType: mediaTypeVideo, suffix: 1, padName: "video_%u"},
		{mediaType: mediaTypeAudio, suffix: 2, padName: "audio_%u"},
	} {
		branch, err := newTrackQueue(branchSpec.suffix)
		if err != nil {
			_ = pipeline.SetState(gst.StateNull)
			return nil, fmt.Errorf("create GStreamer media mixer pipeline: %w", err)
		}
		if !pipeline.Add(branch) {
			_ = pipeline.SetState(gst.StateNull)
			return nil, fmt.Errorf("create GStreamer media mixer pipeline: add %s branch", branchSpec.mediaType)
		}

		sinkPad := target.RequestPadSimple(branchSpec.padName)
		if sinkPad == nil {
			_ = pipeline.SetState(gst.StateNull)
			return nil, fmt.Errorf("create GStreamer media mixer pipeline: request %s sink pad", branchSpec.mediaType)
		}
		branchSrc := branch.GetStaticPad("src")
		if branchSrc == nil {
			_ = pipeline.SetState(gst.StateNull)
			return nil, fmt.Errorf("create GStreamer media mixer pipeline: %s branch source pad is missing", branchSpec.mediaType)
		}
		if ret := branchSrc.Link(sinkPad); ret != gst.PadLinkOK {
			_ = pipeline.SetState(gst.StateNull)
			return nil, fmt.Errorf("create GStreamer media mixer pipeline: link %s branch to target: %s", branchSpec.mediaType, ret)
		}

		mixer.branches[branchSpec.mediaType] = branch
	}

	source.ConnectPadAdded(mixer.sourcePadAdded)
	source.ConnectPadRemoved(mixer.sourcePadRemoved)

	if ret := pipeline.SetState(gst.StatePlaying); ret == gst.StateChangeFailure {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("start GStreamer media mixer pipeline")
	}

	ctx, cancel := context.WithCancel(context.Background())
	mixer.cancel = cancel
	mixer.done = make(chan struct{})
	go mixer.watchBus(ctx)

	log.Printf("forwarding media from LiveKit room %q to room %q", config.SourceRoom, config.TargetRoom)
	return mixer, nil
}

func (p *mediaMixerPipeline) stop() error {
	p.cancel()
	<-p.done

	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	if ret := p.pipeline.SetState(gst.StateNull); ret == gst.StateChangeFailure {
		return fmt.Errorf("stop GStreamer media mixer pipeline")
	}
	log.Printf("stopped forwarding media from room %q to room %q", p.config.SourceRoom, p.config.TargetRoom)
	return nil
}

func (p *mediaMixerPipeline) watchBus(ctx context.Context) {
	defer close(p.done)

	for message := range p.pipeline.GetBus().Messages(ctx) {
		switch message.Type() {
		case gst.MessageError:
			debug, err := message.ParseError()
			log.Printf("GStreamer media mixer pipeline error: %v (%s)", err, debug)
		case gst.MessageEOS:
			log.Printf("GStreamer media mixer pipeline reached end of stream")
		}
	}
}

func (p *mediaMixerPipeline) sourcePadAdded(_ gst.Element, sourcePad gst.Pad) {
	mediaType := padMediaType(sourcePad.GetName())
	if mediaType == "" {
		return
	}

	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	if _, exists := p.tracks[sourcePad.GetName()]; exists {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	if err := p.startTrack(sourcePad, mediaType); err != nil {
		log.Printf("forward %s track %q: %v", mediaType, sourcePad.GetName(), err)
	}
}

func (p *mediaMixerPipeline) sourcePadRemoved(_ gst.Element, sourcePad gst.Pad) {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	p.mu.Lock()
	track := p.tracks[sourcePad.GetName()]
	p.mu.Unlock()
	if track == nil {
		return
	}
	p.removeTrack(track)
}

func (p *mediaMixerPipeline) startTrack(sourcePad gst.Pad, mediaType string) error {
	branch := p.branches[mediaType]
	if branch == nil {
		return fmt.Errorf("no %s forwarding branch is available", mediaType)
	}

	branchSink := branch.GetStaticPad("sink")
	if branchSink == nil {
		return fmt.Errorf("%s forwarding branch sink pad is missing", mediaType)
	}

	if ret := sourcePad.Link(branchSink); ret != gst.PadLinkOK {
		return fmt.Errorf("link source to %s forwarding branch: %s", mediaType, ret)
	}

	track := &forwardedTrack{
		mediaType: mediaType,
		sourcePad: sourcePad,
		branch:    branch,
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.discardTrack(track)
		return nil
	}
	p.tracks[sourcePad.GetName()] = track
	p.mu.Unlock()

	log.Printf("forwarding %s track %q as %q", mediaType, sourcePad.GetName(), p.config.Identity+"-target")
	return nil
}

func (p *mediaMixerPipeline) removeTrack(track *forwardedTrack) {
	p.mu.Lock()
	if p.tracks[track.sourcePad.GetName()] != track {
		p.mu.Unlock()
		return
	}
	delete(p.tracks, track.sourcePad.GetName())
	p.mu.Unlock()

	p.discardTrack(track)
	log.Printf("stopped forwarding %s track %q", track.mediaType, track.sourcePad.GetName())
}

func (p *mediaMixerPipeline) discardTrack(track *forwardedTrack) {
	if branchSink := track.branch.GetStaticPad("sink"); branchSink != nil {
		track.sourcePad.Unlink(branchSink)
	}
}

func (p *mediaMixerPipeline) trackCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.tracks)
}

func padMediaType(name string) string {
	switch {
	case strings.HasPrefix(name, mediaTypeVideo+"_"):
		return mediaTypeVideo
	case strings.HasPrefix(name, mediaTypeAudio+"_"):
		return mediaTypeAudio
	default:
		return ""
	}
}

func newSource(config config) (gst.Element, error) {
	source := gst.ElementFactoryMake("livekitwebrtcsrc", sourceElementName)
	if source == nil {
		return nil, fmt.Errorf("create LiveKit source")
	}

	if err := configureSignaller(source, config, config.SourceRoom, config.Identity); err != nil {
		return nil, fmt.Errorf("configure LiveKit source: %w", err)
	}

	return source, nil
}

func newTarget(config config) (gst.Element, error) {
	identity := config.Identity + "-target"
	description := fmt.Sprintf(
		`livekitwebrtcsink name=%s
  signaller::ws-url=%s
  signaller::api-key=%s
  signaller::secret-key=%s
  signaller::room-name=%s
  signaller::identity=%s
  signaller::participant-name=%s
  video-caps="video/x-vp8"
  audio-caps="audio/x-opus"
  stun-server=""`,
		targetElementName,
		gstString(config.LiveKitURL),
		gstString(config.APIKey),
		gstString(config.APISecret),
		gstString(config.TargetRoom),
		gstString(identity),
		gstString(identity),
	)

	target, err := gst.ParseLaunch(description)
	if err != nil {
		return nil, fmt.Errorf("create LiveKit target: %w", err)
	}

	return target, nil
}

func configureSignaller(element gst.Element, config config, roomName, identity string) error {
	proxy, ok := element.(gst.ChildProxy)
	if !ok {
		return fmt.Errorf("child proxy interface is missing")
	}

	proxy.SetProperty("signaller::ws-url", config.LiveKitURL)
	proxy.SetProperty("signaller::api-key", config.APIKey)
	proxy.SetProperty("signaller::secret-key", config.APISecret)
	proxy.SetProperty("signaller::room-name", roomName)
	proxy.SetProperty("signaller::identity", identity)
	proxy.SetProperty("signaller::participant-name", identity)
	element.SetObjectProperty("stun-server", "")

	return nil
}

func newTrackQueue(suffix int) (gst.Element, error) {
	queue := gst.ElementFactoryMake("queue", fmt.Sprintf("media-mixer-track-%d-queue", suffix))
	if queue == nil {
		return nil, fmt.Errorf("create forwarding queue")
	}
	return queue, nil
}

func gstString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
