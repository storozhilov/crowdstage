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
)

var gstInitOnce sync.Once

type mosaicPipeline struct {
	config                config
	pipeline              gst.Pipeline
	participantSource     gst.Element
	participantSourceName string
	cancel                context.CancelFunc
	done                  chan struct{}

	mu           sync.Mutex
	closed       bool
	nextTile     int
	nextSource   int
	participants map[string]*participantTile
}

type participantTile struct {
	mixerPad       gst.Pad
	sourcePad      gst.Pad
	participantSID string
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

	participantSrc := pipeline.GetByName("participant-src")
	mixer := pipeline.GetByName("mixer")
	if participantSrc == nil || mixer == nil {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("create GStreamer mosaic pipeline: participant-src or mixer is missing")
	}

	mosaic := &mosaicPipeline{
		config:                config,
		pipeline:              pipeline,
		participantSource:     participantSrc,
		participantSourceName: participantSrc.GetName(),
		nextTile:              1,
		nextSource:            2,
		participants:          make(map[string]*participantTile),
	}
	participantSrc.ConnectPadAdded(mosaic.participantPadAdded)
	participantSrc.ConnectPadRemoved(mosaic.participantPadRemoved)

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

func (p *mosaicPipeline) participantPadAdded(source gst.Element, sourcePad gst.Pad) {
	sourcePadName := sourcePad.GetName()
	if !strings.HasPrefix(sourcePadName, "video_") {
		return
	}

	p.mu.Lock()
	if p.closed || p.participantSourceName != source.GetName() {
		p.mu.Unlock()
		return
	}
	if _, exists := p.participants[sourcePadName]; exists {
		p.mu.Unlock()
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
		log.Printf("participant %s: could not request compositor pad", sourcePadName)
		return
	}

	tile := p.newParticipantTile(sourcePadName)
	tile.sourcePad = sourcePad
	tile.participantSID = participantSIDFromPad(sourcePad)
	mixerPad.SetObjectProperties(map[string]any{
		"xpos":   int32(participantXPos()),
		"ypos":   int32(participantYPos()),
		"width":  int32(participantWidth),
		"height": int32(participantHeight),
		"zorder": uint32(1),
	})
	tile.mixerPad = mixerPad
	p.participants[sourcePadName] = tile
	p.mu.Unlock()

	watchParticipantSID(sourcePad, func(participantSID string) {
		p.setParticipantSID(sourcePadName, participantSID)
	})

	if err := p.startParticipantTile(tile, sourcePad); err != nil {
		log.Printf("participant %s: %v", sourcePadName, err)
		p.removeParticipantTile(sourcePadName)
		return
	}

	log.Printf("participant video track joined: pad=%q participant=%q", sourcePadName, tile.participantSID)
}

func (p *mosaicPipeline) setParticipantSID(sourcePadName, participantSID string) {
	if participantSID == "" {
		return
	}

	p.mu.Lock()
	tile := p.participants[sourcePadName]
	if tile == nil || tile.participantSID == participantSID {
		p.mu.Unlock()
		return
	}
	tile.participantSID = participantSID
	p.mu.Unlock()

	log.Printf("participant video track identified: pad=%q participant=%q", sourcePadName, participantSID)
}

func (p *mosaicPipeline) removeParticipant(participantSID string) {
	if participantSID == "" {
		return
	}

	p.mu.Lock()
	sourcePadNames := make([]string, 0, 1)
	for sourcePadName, tile := range p.participants {
		if tile.participantSID == participantSID {
			sourcePadNames = append(sourcePadNames, sourcePadName)
		}
	}
	p.mu.Unlock()

	for _, sourcePadName := range sourcePadNames {
		p.hideParticipantTile(sourcePadName)
	}
	if len(sourcePadNames) > 0 {
		p.resetParticipantSource()
	}
}

func (p *mosaicPipeline) resetParticipantSource() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	oldSource := p.participantSource
	sourceName := fmt.Sprintf("participant-src-%d", p.nextSource)
	p.nextSource++
	p.participantSource = nil
	p.participantSourceName = ""
	tileKeys := make([]string, 0, len(p.participants))
	for tileKey := range p.participants {
		tileKeys = append(tileKeys, tileKey)
	}
	p.mu.Unlock()

	if oldSource != nil {
		_ = oldSource.SetState(gst.StateNull)
	}
	for _, tileKey := range tileKeys {
		p.removeParticipantTile(tileKey)
	}
	if oldSource != nil {
		p.pipeline.Remove(oldSource)
	}

	element, err := gst.ParseLaunch(participantSourceDescription(p.config, sourceName))
	if err != nil {
		log.Printf("recreate participant source: %v", err)
		return
	}
	source, ok := element.(gst.Element)
	if !ok {
		log.Printf("recreate participant source: parsed element is not an element")
		return
	}
	source.ConnectPadAdded(p.participantPadAdded)
	source.ConnectPadRemoved(p.participantPadRemoved)

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = source.SetState(gst.StateNull)
		return
	}
	p.participantSource = source
	p.participantSourceName = sourceName
	p.mu.Unlock()

	if !p.pipeline.Add(source) {
		log.Printf("recreate participant source: could not add source to pipeline")
		return
	}
	source.SyncStateWithParent()
	log.Printf("participant source recreated")
}

func (p *mosaicPipeline) hideParticipantTile(sourcePadName string) {
	p.mu.Lock()
	tile := p.participants[sourcePadName]
	if tile == nil {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	tile.mixerPad.SetObjectProperties(map[string]any{"alpha": 0.0})
	log.Printf("participant video track left: pad=%q participant=%q", sourcePadName, tile.participantSID)
}

func (p *mosaicPipeline) newParticipantTile(sourcePadName string) *participantTile {
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

func (p *mosaicPipeline) participantPadRemoved(source gst.Element, sourcePad gst.Pad) {
	p.mu.Lock()
	current := !p.closed && p.participantSourceName == source.GetName()
	p.mu.Unlock()
	if !current {
		return
	}
	p.removeParticipantTile(sourcePad.GetName())
}

func (p *mosaicPipeline) removeParticipantTile(sourcePadName string) {
	p.mu.Lock()
	tile := p.participants[sourcePadName]
	if tile == nil {
		p.mu.Unlock()
		return
	}
	delete(p.participants, sourcePadName)
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
}

func participantXPos() int {
	return backgroundWidth - participantWidth - participantMargin
}

func participantYPos() int {
	return participantMargin
}

func participantOverlayPosition() (xpos, ypos, width, height int) {
	return participantXPos(), participantYPos(), participantWidth, participantHeight
}

func participantSIDFromPad(pad gst.Pad) string {
	if participantSID := padStringProperty(pad, "participant-sid"); participantSID != "" {
		return participantSID
	}
	return participantSIDFromCaps(pad.GetCurrentCaps())
}

func participantSIDFromCaps(caps *gst.Caps) string {
	if caps == nil || caps.GetSize() == 0 {
		return ""
	}
	structure := caps.GetStructure(0)
	if structure == nil {
		return ""
	}
	return participantSIDFromMSID(structure.GetString("a-msid"))
}

func participantSIDFromMSID(msid string) string {
	participantSID, _, _ := strings.Cut(msid, "|")
	return strings.TrimSpace(participantSID)
}

func padStringProperty(pad gst.Pad, name string) string {
	value, _ := pad.ObjectProperty(name).(string)
	return value
}

func watchParticipantSID(sourcePad gst.Pad, update func(string)) {
	if participantSIDFromPad(sourcePad) != "" {
		return
	}

	sourcePad.AddProbe(gst.PadProbeTypeEventDownstream, func(pad gst.Pad, info *gst.PadProbeInfo) gst.PadProbeReturn {
		event := info.GetEvent()
		if event == nil || event.GetType() != gst.EventCaps {
			return gst.PadProbeOK
		}
		participantSID := participantSIDFromCaps(event.ParseCaps())
		if participantSID == "" {
			return gst.PadProbeOK
		}
		update(participantSID)
		return gst.PadProbeRemove
	})
}

func participantSourceDescription(config config, name string) string {
	identity := config.Identity + "-participant"

	return fmt.Sprintf(`livekitwebrtcsrc name=%s
  signaller::ws-url=%s
  signaller::api-key=%s
  signaller::secret-key=%s
  signaller::room-name=%s
  signaller::identity=%s
  signaller::participant-name=%s
  stun-server=""`,
		gstString(name),
		gstString(config.LiveKitURL),
		gstString(config.APIKey),
		gstString(config.APISecret),
		gstString(config.RoomName),
		gstString(identity),
		gstString(identity),
	)
}

func pipelineDescription(config config) string {
	participantIdentity := config.Identity + "-participant"

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
  stun-server=""
livekitwebrtcsrc name=participant-src
  signaller::ws-url=%s
  signaller::api-key=%s
  signaller::secret-key=%s
  signaller::room-name=%s
  signaller::identity=%s
  signaller::participant-name=%s
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
		gstString(config.LiveKitURL),
		gstString(config.APIKey),
		gstString(config.APISecret),
		gstString(config.RoomName),
		gstString(participantIdentity),
		gstString(participantIdentity),
	)
}

func gstString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
