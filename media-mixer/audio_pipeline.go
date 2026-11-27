package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-gst/go-gst/pkg/gst"
)

const audioMixerCaps = "audio/x-raw,format=F32LE,rate=48000,channels=2,layout=interleaved"

var audioSourceSequence atomic.Uint64

type mediaMixerAudioSource struct {
	lifecycle sync.Mutex

	publicationSID string
	name           string
	channel        string
	pipeline       gst.Bin
	interSource    gst.Element
	element        gst.Element
	queue          gst.Element
	decodebin      gst.Element
	convert        gst.Element
	resample       gst.Element
	caps           gst.Element
	sink           gst.Element
	elements       []gst.Element
	sourcePad      gst.Pad
	decodebinPad   gst.Pad
	mixerPad       gst.Pad
	linked         bool
	removed        bool
	levelListener  func(string, float64)
	cancel         context.CancelFunc
	done           chan struct{}
}

func newMediaMixerAudioSource(config config, source *audioSource, sourceName string) (*mediaMixerAudioSource, error) {
	element, err := gst.ParseLaunch(mediaMixerAudioSourceDescription(config, sourceName, source))
	if err != nil {
		return nil, fmt.Errorf("create studio audio source %q: %w", sourceName, err)
	}
	if element == nil {
		return nil, fmt.Errorf("create studio audio source %q: parsed element is nil", sourceName)
	}

	queue := gst.ElementFactoryMake("queue", sourceName+"-queue")
	decodebin := gst.ElementFactoryMake("decodebin", sourceName+"-decodebin")
	convert := gst.ElementFactoryMake("audioconvert", sourceName+"-convert")
	resample := gst.ElementFactoryMake("audioresample", sourceName+"-resample")
	caps := gst.ElementFactoryMake("capsfilter", sourceName+"-caps")
	sink := gst.ElementFactoryMake("interaudiosink", sourceName+"-sink")
	if queue == nil || decodebin == nil || convert == nil || resample == nil || caps == nil || sink == nil {
		return nil, fmt.Errorf("create studio audio source chain for %q", sourceName)
	}

	channel := "crowdstage-" + sourceName
	caps.SetObjectProperty("caps", gst.CapsFromString(audioMixerCaps))
	sink.SetObjectProperty("channel", channel)

	pipelineElement := gst.NewPipeline(sourceName + "-pipeline")
	if pipelineElement == nil {
		return nil, fmt.Errorf("create studio audio source pipeline for %q", sourceName)
	}
	sourcePipeline, ok := pipelineElement.(gst.Bin)
	if !ok {
		return nil, fmt.Errorf("create studio audio source pipeline for %q: parsed element is not a bin", sourceName)
	}

	audioSource := &mediaMixerAudioSource{
		publicationSID: source.publicationSID,
		name:           sourceName,
		channel:        channel,
		pipeline:       sourcePipeline,
		element:        element,
		queue:          queue,
		decodebin:      decodebin,
		convert:        convert,
		resample:       resample,
		caps:           caps,
		sink:           sink,
		elements:       []gst.Element{element, queue, decodebin, convert, resample, caps, sink},
		done:           make(chan struct{}),
	}

	for _, element := range audioSource.elements {
		if !sourcePipeline.Add(element) {
			return nil, fmt.Errorf("could not add %s to the studio audio source pipeline", element.GetName())
		}
	}

	if !queue.Link(decodebin) {
		return nil, fmt.Errorf("link studio audio queue to decodebin")
	}
	if !convert.Link(resample) {
		return nil, fmt.Errorf("link studio audio converter to resampler")
	}
	if !resample.Link(caps) {
		return nil, fmt.Errorf("link studio audio resampler to caps")
	}
	if !caps.Link(sink) {
		return nil, fmt.Errorf("link studio audio caps to interaudiosink")
	}

	element.ConnectPadAdded(func(_ gst.Element, pad gst.Pad) {
		audioSource.sourcePadAdded(pad)
	})
	decodebin.ConnectPadAdded(func(_ gst.Element, pad gst.Pad) {
		audioSource.decodebinPadAdded(pad)
	})
	ctx, cancel := context.WithCancel(context.Background())
	audioSource.cancel = cancel
	go audioSource.watchBus(ctx)

	caps.GetStaticPad("src").AddProbe(gst.PadProbeTypeBuffer, func(_ gst.Pad, info *gst.PadProbeInfo) gst.PadProbeReturn {
		buffer := info.GetBuffer()
		if buffer == nil {
			return gst.PadProbeOK
		}
		mapped, ok := buffer.Map(gst.MapRead)
		if !ok {
			return gst.PadProbeOK
		}
		level := audioRMS(mapped.Float32Data(binary.LittleEndian))
		mapped.Unmap()

		audioSource.lifecycle.Lock()
		listener := audioSource.levelListener
		publicationSID := audioSource.publicationSID
		audioSource.lifecycle.Unlock()
		if listener != nil {
			listener(publicationSID, level)
		}
		return gst.PadProbeOK
	})

	return audioSource, nil
}

func (s *mediaMixerAudioSource) setLevelListener(listener func(publicationSID string, level float64)) {
	s.lifecycle.Lock()
	s.levelListener = listener
	s.lifecycle.Unlock()
}

func (s *mediaMixerAudioSource) watchBus(ctx context.Context) {
	defer close(s.done)

	for message := range s.pipeline.GetBus().Messages(ctx) {
		switch message.Type() {
		case gst.MessageError:
			debug, err := message.ParseError()
			log.Printf("GStreamer studio audio source %q error: %v (%s)", s.name, err, debug)
		case gst.MessageEOS:
			log.Printf("GStreamer studio audio source %q reached end of stream", s.name)
		}
	}
}

func (s *mediaMixerAudioSource) sourcePadAdded(sourcePad gst.Pad) {
	if !strings.HasPrefix(sourcePad.GetName(), "audio_") {
		return
	}

	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()

	if s.removed || s.sourcePad != nil {
		return
	}
	if ret := sourcePad.Link(s.queue.GetStaticPad("sink")); ret != gst.PadLinkOK {
		log.Printf("studio audio source %q: link audio pad to queue: %s", s.name, ret)
		return
	}
	s.sourcePad = sourcePad
}

func (s *mediaMixerAudioSource) decodebinPadAdded(pad gst.Pad) {
	caps := pad.GetCurrentCaps()
	if caps == nil || caps.GetSize() == 0 || !caps.GetStructure(0).HasName("audio/x-raw") {
		return
	}

	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()

	if s.removed || s.linked {
		return
	}
	if ret := pad.Link(s.convert.GetStaticPad("sink")); ret != gst.PadLinkOK {
		log.Printf("studio audio source %q: link decoded audio to converter: %s", s.name, ret)
		return
	}
	s.decodebinPad = pad
	s.linked = true
}

func (p *mediaMixerPipeline) syncAudioSources(sources []audioSource) error {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("media mixer pipeline is closed")
	}
	p.mu.Unlock()

	desired := make(map[string]audioSource, len(sources))
	anySolo := false
	for _, source := range sources {
		desired[source.publicationSID] = source
		if source.solo {
			anySolo = true
		}
	}

	p.mu.Lock()
	stale := make([]*mediaMixerAudioSource, 0)
	for publicationSID, source := range p.audioSources {
		if _, ok := desired[publicationSID]; ok {
			continue
		}
		delete(p.audioSources, publicationSID)
		stale = append(stale, source)
	}
	p.mu.Unlock()

	for _, source := range stale {
		p.stopAudioSource(source)
	}

	for publicationSID, source := range desired {
		p.mu.Lock()
		existing := p.audioSources[publicationSID]
		p.mu.Unlock()
		if existing != nil {
			continue
		}

		sourceName := fmt.Sprintf("audio-src-%d", audioSourceSequence.Add(1))
		pipelineSource, err := newMediaMixerAudioSource(p.config, &source, sourceName)
		if err != nil {
			return err
		}
		p.mu.Lock()
		levelListener := p.audioLevelUpdate
		p.mu.Unlock()
		pipelineSource.setLevelListener(levelListener)

		if err := p.addAudioSource(pipelineSource); err != nil {
			p.removeAudioSourceElements(pipelineSource)
			return err
		}

		pipelineSource.mixerPad.SetObjectProperties(map[string]any{
			"volume": source.gain,
			"mute":   source.muted,
		})

		p.mu.Lock()
		p.audioSources[publicationSID] = pipelineSource
		p.mu.Unlock()
	}

	p.mu.Lock()
	active := make([]*mediaMixerAudioSource, 0, len(p.audioSources))
	for _, source := range p.audioSources {
		active = append(active, source)
	}
	p.mu.Unlock()

	for _, pipelineSource := range active {
		source, ok := desired[pipelineSource.publicationSID]
		if !ok {
			continue
		}
		controls := effectiveAudioControls(source, anySolo)
		pipelineSource.mixerPad.SetObjectProperty("volume", controls.gain)
		pipelineSource.mixerPad.SetObjectProperty("mute", controls.muted)
	}

	return nil
}

func (p *mediaMixerPipeline) addAudioSource(source *mediaMixerAudioSource) error {
	interSource := gst.ElementFactoryMake("interaudiosrc", source.name+"-src")
	if interSource == nil {
		return fmt.Errorf("create interaudiosrc for the studio audio source")
	}
	interSource.SetObjectProperty("channel", source.channel)

	mixerPad := p.audioMixer.RequestPadSimple("sink_%u")
	if mixerPad == nil {
		return fmt.Errorf("could not request audiomixer pad for the studio audio source")
	}

	if !p.pipeline.Add(interSource) {
		p.audioMixer.ReleaseRequestPad(mixerPad)
		return fmt.Errorf("could not add the studio audio inter source to the pipeline")
	}

	source.interSource = interSource
	source.mixerPad = mixerPad
	if ret := interSource.GetStaticPad("src").Link(mixerPad); ret != gst.PadLinkOK {
		p.pipeline.Remove(interSource)
		p.audioMixer.ReleaseRequestPad(mixerPad)
		source.interSource = nil
		source.mixerPad = nil
		return fmt.Errorf("link the studio audio inter source to the audiomixer")
	}
	interSource.SyncStateWithParent()

	if ret := source.pipeline.SetState(gst.StatePlaying); ret == gst.StateChangeFailure {
		return fmt.Errorf("start the studio audio source pipeline for %q", source.name)
	}
	return nil
}

func (p *mediaMixerPipeline) stopAudioSource(source *mediaMixerAudioSource) {
	source.lifecycle.Lock()
	source.removed = true
	source.levelListener = nil
	source.lifecycle.Unlock()

	p.removeAudioSourceElements(source)
}

func (p *mediaMixerPipeline) removeAudioSourceElements(source *mediaMixerAudioSource) {
	if source.cancel != nil {
		source.cancel()
		<-source.done
		source.cancel = nil
	}
	if source.pipeline != nil {
		_ = source.pipeline.SetState(gst.StateNull)
	}

	if source.interSource != nil {
		_ = source.interSource.SetState(gst.StateNull)
		if interPad := source.interSource.GetStaticPad("src"); interPad != nil && source.mixerPad != nil {
			interPad.Unlink(source.mixerPad)
		}
		p.pipeline.Remove(source.interSource)
		source.interSource = nil
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
	unlinkStaticPads(source.convert, "src", source.resample, "sink")
	unlinkStaticPads(source.resample, "src", source.caps, "sink")
	unlinkStaticPads(source.caps, "src", source.sink, "sink")

	if source.pipeline != nil {
		for _, element := range source.elements {
			source.pipeline.Remove(element)
		}
	}
	if source.mixerPad != nil && p.audioMixer != nil {
		p.audioMixer.ReleaseRequestPad(source.mixerPad)
		source.mixerPad = nil
	}
}
