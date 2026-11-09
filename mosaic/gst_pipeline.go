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

	participantWidth  = 320
	participantHeight = 180
	participantMargin = 20

	maxParticipantTiles = 4
)

var gstInitOnce sync.Once

type mosaicPipeline struct {
	config   config
	pipeline gst.Pipeline
	cancel   context.CancelFunc
	done     chan struct{}

	mu       sync.Mutex
	closed   bool
	nextTile int
	sources  map[string]*participantSource
}

type participantSource struct {
	element        gst.Element
	name           string
	identity       string
	participantSID string
	slot           int
	tile           *participantTile
}

type participantTile struct {
	mixerPad       gst.Pad
	sourcePad      gst.Pad
	participantSID string
	slot           int
	queue          gst.Element
	decodebin      gst.Element
	convert        gst.Element
	elements       []gst.Element
	removed        bool
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

	mixer := pipeline.GetByName("mixer")
	if mixer == nil {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("create GStreamer mosaic pipeline: mixer is missing")
	}

	mosaic := &mosaicPipeline{
		config:   config,
		pipeline: pipeline,
		nextTile: 1,
		sources:  make(map[string]*participantSource),
	}

	if ret := pipeline.SetState(gst.StatePlaying); ret == gst.StateChangeFailure {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("start GStreamer mosaic pipeline")
	}

	ctx, cancel := context.WithCancel(context.Background())
	mosaic.cancel = cancel
	mosaic.done = make(chan struct{})
	go mosaic.watchBus(ctx)

	return mosaic, nil
}

func (p *mosaicPipeline) stop() error {
	p.cancel()
	<-p.done

	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

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

func (p *mosaicPipeline) addParticipant(identity, participantSID, sourceName string) bool {
	if identity == "" {
		return false
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false
	}
	if _, exists := p.sources[identity]; exists {
		p.mu.Unlock()
		return false
	}
	slot, available := p.nextParticipantSlotLocked()
	if !available {
		p.mu.Unlock()
		log.Printf("participant identity=%q participant=%q: maximum of %d participant sources reached", identity, participantSID, maxParticipantTiles)
		return false
	}
	participant := &participantSource{
		name:           sourceName,
		identity:       identity,
		participantSID: participantSID,
		slot:           slot,
	}
	p.sources[identity] = participant
	p.mu.Unlock()

	element, err := gst.ParseLaunch(participantSourceDescription(p.config, sourceName, participantSID))
	if err != nil {
		log.Printf("participant identity=%q: create source: %v", identity, err)
		p.removeParticipant(identity)
		return false
	}
	source, ok := element.(gst.Element)
	if !ok {
		log.Printf("participant identity=%q: parsed source is not an element", identity)
		p.removeParticipant(identity)
		return false
	}
	participant.element = source
	source.ConnectPadAdded(func(_ gst.Element, pad gst.Pad) {
		p.participantPadAdded(participant, pad)
	})
	source.ConnectPadRemoved(func(_ gst.Element, pad gst.Pad) {
		p.participantPadRemoved(participant, pad)
	})

	if !p.pipeline.Add(source) {
		log.Printf("participant identity=%q: could not add source to pipeline", identity)
		p.removeParticipant(identity)
		return false
	}

	p.mu.Lock()
	current := !p.closed && p.sources[identity] == participant
	p.mu.Unlock()
	if !current {
		_ = source.SetState(gst.StateNull)
		p.pipeline.Remove(source)
		return false
	}

	source.SyncStateWithParent()
	log.Printf("participant source joined: participant=%q identity=%q", participantSID, identity)
	return true
}

func (p *mosaicPipeline) removeParticipant(identity string) {
	if identity == "" {
		return
	}

	p.mu.Lock()
	participant := p.sources[identity]
	if participant != nil {
		delete(p.sources, identity)
	}
	p.mu.Unlock()
	if participant == nil {
		return
	}

	if participant.element != nil {
		_ = participant.element.SetState(gst.StateNull)
	}
	p.removeParticipantTile(participant, nil)
	if participant.element != nil {
		p.pipeline.Remove(participant.element)
	}
	log.Printf("participant source left: participant=%q identity=%q", participant.participantSID, identity)
}

func (p *mosaicPipeline) participantPadAdded(participant *participantSource, sourcePad gst.Pad) {
	if !strings.HasPrefix(sourcePad.GetName(), "video_") {
		return
	}

	p.mu.Lock()
	if p.closed || p.sources[participant.identity] != participant {
		p.mu.Unlock()
		return
	}
	if participant.tile != nil {
		p.mu.Unlock()
		log.Printf("participant identity=%q: ignoring additional video pad %q", participant.identity, sourcePad.GetName())
		return
	}

	mixer := p.pipeline.GetByName("mixer")
	if mixer == nil {
		p.mu.Unlock()
		return
	}
	mixerPad := mixer.RequestPadSimple("sink_%u")
	if mixerPad == nil {
		p.mu.Unlock()
		log.Printf("participant identity=%q: could not request compositor pad", participant.identity)
		return
	}

	tile := p.newParticipantTile(participant.slot)
	tile.sourcePad = sourcePad
	tile.participantSID = participant.participantSID
	xpos, ypos, width, height := participantGridPosition(participant.slot)
	mixerPad.SetObjectProperties(map[string]any{
		"xpos":   int32(xpos),
		"ypos":   int32(ypos),
		"width":  int32(width),
		"height": int32(height),
		"zorder": uint32(1),
	})
	tile.mixerPad = mixerPad
	participant.tile = tile
	p.mu.Unlock()

	if err := p.startParticipantTile(tile, sourcePad); err != nil {
		log.Printf("participant identity=%q: %v", participant.identity, err)
		p.removeParticipantTile(participant, sourcePad)
		return
	}

	log.Printf("participant video track joined: participant=%q identity=%q pad=%q", tile.participantSID, participant.identity, sourcePad.GetName())
}

func (p *mosaicPipeline) newParticipantTile(slot int) *participantTile {
	suffix := fmt.Sprintf("%d", p.nextTile)
	p.nextTile++

	queue := gst.ElementFactoryMake("queue", "participant-"+suffix+"-queue")
	decodebin := gst.ElementFactoryMake("decodebin", "participant-"+suffix+"-decodebin")
	convert := gst.ElementFactoryMake("videoconvert", "participant-"+suffix+"-convert")

	elements := make([]gst.Element, 0, 3)
	for _, element := range []gst.Element{queue, decodebin, convert} {
		if element != nil {
			elements = append(elements, element)
		}
	}

	return &participantTile{
		slot:      slot,
		queue:     queue,
		decodebin: decodebin,
		convert:   convert,
		elements:  elements,
	}
}

func (p *mosaicPipeline) startParticipantTile(tile *participantTile, sourcePad gst.Pad) error {
	if tile.queue == nil || tile.decodebin == nil || tile.convert == nil {
		return fmt.Errorf("could not create participant pipeline elements")
	}

	for _, element := range tile.elements {
		if !p.pipeline.Add(element) {
			return fmt.Errorf("could not add %s to pipeline", element.GetName())
		}
	}

	if ret := sourcePad.Link(tile.queue.GetStaticPad("sink")); ret != gst.PadLinkOK {
		return fmt.Errorf("link participant source to queue: %s", ret)
	}
	if !tile.queue.Link(tile.decodebin) {
		return fmt.Errorf("link queue to decodebin")
	}

	tile.decodebin.ConnectPadAdded(func(_ gst.Element, pad gst.Pad) {
		p.linkParticipantVideo(tile, pad)
	})

	for _, element := range tile.elements {
		element.SyncStateWithParent()
	}
	return nil
}

func (p *mosaicPipeline) linkParticipantVideo(tile *participantTile, pad gst.Pad) {
	caps := pad.GetCurrentCaps()
	if caps == nil || caps.GetSize() == 0 || !caps.GetStructure(0).HasName("video/x-raw") {
		return
	}

	p.mu.Lock()
	removed := tile.removed || p.closed
	p.mu.Unlock()
	if removed {
		return
	}

	if ret := pad.Link(tile.convert.GetStaticPad("sink")); ret != gst.PadLinkOK {
		log.Printf("participant %s: link decoded video to converter: %s", tile.decodebin.GetName(), ret)
		return
	}
	if ret := tile.convert.GetStaticPad("src").Link(tile.mixerPad); ret != gst.PadLinkOK {
		log.Printf("participant %s: link converter to compositor: %s", tile.convert.GetName(), ret)
	}
}

func (p *mosaicPipeline) participantPadRemoved(participant *participantSource, sourcePad gst.Pad) {
	p.mu.Lock()
	current := !p.closed && p.sources[participant.identity] == participant
	p.mu.Unlock()
	if !current {
		return
	}
	p.removeParticipantTile(participant, sourcePad)
}

func (p *mosaicPipeline) removeParticipantTile(participant *participantSource, sourcePad gst.Pad) {
	p.mu.Lock()
	tile := participant.tile
	if tile == nil {
		p.mu.Unlock()
		return
	}
	if sourcePad != nil && tile.sourcePad != sourcePad {
		p.mu.Unlock()
		return
	}
	participant.tile = nil
	tile.removed = true
	p.mu.Unlock()

	for _, element := range tile.elements {
		_ = element.SetState(gst.StateNull)
	}
	if tile.sourcePad != nil {
		if queueSink := tile.queue.GetStaticPad("sink"); queueSink != nil {
			tile.sourcePad.Unlink(queueSink)
		}
	}
	if source := tile.convert.GetStaticPad("src"); source != nil {
		source.Unlink(tile.mixerPad)
	}
	p.pipeline.Remove(tile.convert)
	p.pipeline.Remove(tile.decodebin)
	p.pipeline.Remove(tile.queue)

	if mixer := p.pipeline.GetByName("mixer"); mixer != nil {
		mixer.ReleaseRequestPad(tile.mixerPad)
	}
	log.Printf("participant video track left: participant=%q identity=%q pad=%q", tile.participantSID, participant.identity, tile.sourcePad.GetName())
}

func (p *mosaicPipeline) nextParticipantSlotLocked() (int, bool) {
	var used [maxParticipantTiles]bool
	for _, participant := range p.sources {
		if participant.slot >= 0 && participant.slot < maxParticipantTiles {
			used[participant.slot] = true
		}
	}
	for slot, occupied := range used {
		if !occupied {
			return slot, true
		}
	}
	return 0, false
}

func participantXPos() int {
	return backgroundWidth - participantWidth - participantMargin
}

func participantYPos() int {
	return participantMargin
}

func participantOverlayPosition() (xpos, ypos, width, height int) {
	return participantGridPosition(0)
}

func participantGridPosition(slot int) (xpos, ypos, width, height int) {
	column := slot % 2
	row := slot / 2
	xpos = participantXPos() - column*(participantWidth+participantMargin)
	ypos = participantYPos() + row*(participantHeight+participantMargin)
	return xpos, ypos, participantWidth, participantHeight
}

func participantSourceDescription(config config, name, producerPeerID string) string {
	identity := participantSourceIdentity(config, name)

	return fmt.Sprintf(`livekitwebrtcsrc name=%s
  signaller::ws-url=%s
  signaller::api-key=%s
  signaller::secret-key=%s
  signaller::room-name=%s
  signaller::identity=%s
  signaller::participant-name=%s
  signaller::producer-peer-id=%s
  stun-server=""`,
		gstString(name),
		gstString(config.LiveKitURL),
		gstString(config.APIKey),
		gstString(config.APISecret),
		gstString(config.RoomName),
		gstString(identity),
		gstString(identity),
		gstString(producerPeerID),
	)
}

func participantSourceIdentity(config config, name string) string {
	return config.Identity + "-" + name
}

func pipelineDescription(config config) string {
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
		backgroundWidth,
		backgroundHeight,
		backgroundFrameRate,
		backgroundWidth,
		backgroundHeight,
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
	)
}

func gstString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
