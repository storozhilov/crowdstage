package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/pkg/gst"
)

const (
	backgroundWidth     = 1280
	backgroundHeight    = 720
	backgroundFrameRate = 30

	mosaicColumns = 4
	mosaicRows    = 3
	mosaicGap     = 10

	mosaicLabelHeight = 24
	mosaicLabelFont   = "DejaVu Sans Bold 20"
	mosaicLabelYPad   = 5

	defaultRotationTimeout = 10 * time.Second

	maxParticipantTiles = mosaicColumns * mosaicRows
	maxMosaicCapacity   = 100
)

var gstInitOnce sync.Once

type mosaicPipeline struct {
	config          config
	pipeline        gst.Pipeline
	parseSource     func(config config, name, producerIdentity string) (gst.Element, error)
	cancel          context.CancelFunc
	done            chan struct{}
	columns         int
	rows            int
	rotationTimeout time.Duration

	opsMu        sync.Mutex
	rotationDone chan struct{}

	mu       sync.Mutex
	closed   bool
	nextTile int
	sources  map[string]*participantSource
	waiting  []string
}

type participantSource struct {
	lifecycle      sync.Mutex
	element        gst.Element
	name           string
	identity       string
	displayName    string
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
	scale          gst.Element
	cellCaps       gst.Element
	box            gst.Element
	overlay        gst.Element
	elements       []gst.Element
	createdAt      time.Time
	removed        bool
}

func startMosaicPipeline(config config, columns, rows int, rotationTimeout time.Duration) (*mosaicPipeline, error) {
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
		config:          config,
		pipeline:        pipeline,
		parseSource:     parseParticipantSource,
		columns:         columns,
		rows:            rows,
		rotationTimeout: rotationTimeout,
		nextTile:        1,
		sources:         make(map[string]*participantSource),
	}

	if ret := pipeline.SetState(gst.StatePlaying); ret == gst.StateChangeFailure {
		_ = pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("start GStreamer mosaic pipeline")
	}

	ctx, cancel := context.WithCancel(context.Background())
	mosaic.cancel = cancel
	mosaic.done = make(chan struct{})
	mosaic.rotationDone = make(chan struct{})
	go mosaic.watchBus(ctx)
	go mosaic.rotateLoop(ctx)

	return mosaic, nil
}

func (p *mosaicPipeline) stop() error {
	p.cancel()
	<-p.done
	<-p.rotationDone

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

func (p *mosaicPipeline) rotateLoop(ctx context.Context) {
	defer close(p.rotationDone)
	if p.rotationTimeout <= 0 {
		return
	}

	ticker := time.NewTicker(p.rotationTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.rotateParticipant()
		}
	}
}

func (p *mosaicPipeline) rotateParticipant() {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	active := p.oldestRotatableParticipantLocked(time.Now())
	if active == nil {
		p.mu.Unlock()
		return
	}
	next := p.popWaitingLocked()
	if next == nil {
		p.mu.Unlock()
		return
	}

	slot := active.slot
	active.slot = -1
	next.slot = slot
	p.waiting = append(p.waiting, active.identity)
	p.mu.Unlock()

	log.Printf("rotating participant: replacing identity=%q with identity=%q in slot=%d", active.identity, next.identity, slot)
	p.stopParticipant(active)
	if err := p.startParticipant(next); err != nil {
		log.Printf("participant identity=%q: %v", next.identity, err)
		p.removeParticipantLocked(next.identity)
	}
}

func (p *mosaicPipeline) oldestRotatableParticipantLocked(now time.Time) *participantSource {
	var oldest *participantSource
	for _, participant := range p.sources {
		if participant.slot < 0 || participant.tile == nil {
			continue
		}
		if now.Sub(participant.tile.createdAt) < p.rotationTimeout {
			continue
		}
		if oldest == nil || participant.tile.createdAt.Before(oldest.tile.createdAt) {
			oldest = participant
		}
	}
	return oldest
}

func (p *mosaicPipeline) addParticipant(identity, displayName, participantSID, sourceName string) bool {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	return p.addParticipantLocked(identity, displayName, participantSID, sourceName)
}

func (p *mosaicPipeline) addParticipantLocked(identity, displayName, participantSID, sourceName string) bool {
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
	participant := &participantSource{
		name:           sourceName,
		identity:       identity,
		displayName:    displayName,
		participantSID: participantSID,
		slot:           -1,
	}
	active := p.registerParticipantLocked(participant)
	p.mu.Unlock()

	if !active {
		log.Printf("participant identity=%q participant=%q: all %d mosaic tiles are occupied; waiting for a free tile", identity, participantSID, p.gridCapacity())
		return true
	}

	if err := p.startParticipant(participant); err != nil {
		log.Printf("participant identity=%q: %v", identity, err)
		p.removeParticipantLocked(identity)
		return false
	}
	return true
}

func (p *mosaicPipeline) registerParticipantLocked(participant *participantSource) bool {
	participant.slot = -1
	p.sources[participant.identity] = participant
	if slot, available := p.nextParticipantSlotLocked(); available {
		participant.slot = slot
		return true
	}
	p.waiting = append(p.waiting, participant.identity)
	return false
}

func (p *mosaicPipeline) unregisterParticipantLocked(identity string) (removed, next *participantSource) {
	removed = p.sources[identity]
	if removed == nil {
		return nil, nil
	}
	delete(p.sources, identity)
	if removed.slot < 0 {
		p.removeWaitingLocked(identity)
		return removed, nil
	}
	return removed, p.takeNextWaitingLocked()
}

func (p *mosaicPipeline) takeNextWaitingLocked() *participantSource {
	participant := p.popWaitingLocked()
	if participant == nil {
		return nil
	}

	slot, available := p.nextParticipantSlotLocked()
	if !available {
		p.waiting = append([]string{participant.identity}, p.waiting...)
		return nil
	}
	participant.slot = slot
	return participant
}

func (p *mosaicPipeline) popWaitingLocked() *participantSource {
	for len(p.waiting) > 0 {
		identity := p.waiting[0]
		p.waiting = p.waiting[1:]

		participant := p.sources[identity]
		if participant == nil || participant.slot >= 0 {
			continue
		}
		return participant
	}
	return nil
}

func (p *mosaicPipeline) removeWaitingLocked(identity string) {
	for index, waitingIdentity := range p.waiting {
		if waitingIdentity == identity {
			p.waiting = append(p.waiting[:index], p.waiting[index+1:]...)
			return
		}
	}
}

func parseParticipantSource(config config, name, producerIdentity string) (gst.Element, error) {
	element, err := gst.ParseLaunch(participantSourceDescription(config, name, producerIdentity))
	if err != nil {
		return nil, fmt.Errorf("create source: %w", err)
	}
	source, ok := element.(gst.Element)
	if !ok {
		return nil, fmt.Errorf("parsed source is not an element")
	}
	return source, nil
}

func parsePipelineElement(description string) (gst.Element, error) {
	element, err := gst.ParseLaunch(description)
	if err != nil {
		return nil, err
	}
	if element == nil {
		return nil, fmt.Errorf("parsed element is nil")
	}
	return element, nil
}

func (p *mosaicPipeline) startParticipant(participant *participantSource) error {
	participant.lifecycle.Lock()
	defer participant.lifecycle.Unlock()

	source, err := p.parseSource(p.config, participant.name, participant.identity)
	if err != nil {
		return err
	}
	participant.element = source

	source.ConnectPadAdded(func(_ gst.Element, pad gst.Pad) {
		p.participantPadAdded(participant, pad)
	})
	source.ConnectPadRemoved(func(_ gst.Element, pad gst.Pad) {
		p.participantPadRemoved(participant, pad)
	})

	if !p.pipeline.Add(source) {
		return fmt.Errorf("could not add source to pipeline")
	}

	p.mu.Lock()
	current := !p.closed && p.sources[participant.identity] == participant && participant.slot >= 0
	p.mu.Unlock()
	if !current {
		_ = source.SetState(gst.StateNull)
		p.pipeline.Remove(source)
		participant.element = nil
		return nil
	}

	source.SyncStateWithParent()
	log.Printf("participant source joined: participant=%q identity=%q", participant.participantSID, participant.identity)
	return nil
}

func (p *mosaicPipeline) removeParticipant(identity string) {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	p.removeParticipantLocked(identity)
}

func (p *mosaicPipeline) removeParticipantLocked(identity string) {
	if identity == "" {
		return
	}

	p.mu.Lock()
	participant, next := p.unregisterParticipantLocked(identity)
	p.mu.Unlock()
	if participant == nil {
		return
	}

	p.stopParticipant(participant)
	if next != nil {
		if err := p.startParticipant(next); err != nil {
			log.Printf("participant identity=%q: %v", next.identity, err)
			p.removeParticipantLocked(next.identity)
		}
	}
}

func (p *mosaicPipeline) stopParticipant(participant *participantSource) {
	participant.lifecycle.Lock()
	defer participant.lifecycle.Unlock()

	element := participant.element
	if element != nil {
		_ = element.SetState(gst.StateNull)
	}
	p.removeParticipantTile(participant, nil)
	if element != nil {
		p.pipeline.Remove(element)
		participant.element = nil
	}
	log.Printf("participant source left: participant=%q identity=%q", participant.participantSID, participant.identity)
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
	if participant.slot < 0 {
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
		log.Printf("participant identity=%q: could not request compositor pad", participant.identity)
		return
	}

	tile, err := p.newParticipantTile(participant)
	if err != nil {
		p.mu.Unlock()
		mixer.ReleaseRequestPad(mixerPad)
		log.Printf("participant identity=%q: %v", participant.identity, err)
		return
	}
	tile.sourcePad = sourcePad
	tile.participantSID = participant.participantSID
	tile.createdAt = time.Now()
	xpos, ypos, width, height := p.gridPosition(participant.slot)
	mixerPad.SetObjectProperties(map[string]any{
		"xpos":   int32(xpos),
		"ypos":   int32(ypos),
		"width":  int32(width),
		"height": int32(height + mosaicLabelHeight),
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

func (p *mosaicPipeline) newParticipantTile(participant *participantSource) (*participantTile, error) {
	suffix := fmt.Sprintf("%d", p.nextTile)
	p.nextTile++

	_, _, width, height := p.gridPosition(participant.slot)
	label := participantLabelText(participant.displayName, participant.identity)

	queue := gst.ElementFactoryMake("queue", "participant-"+suffix+"-queue")
	decodebin := gst.ElementFactoryMake("decodebin", "participant-"+suffix+"-decodebin")
	convert := gst.ElementFactoryMake("videoconvert", "participant-"+suffix+"-convert")
	scale := gst.ElementFactoryMake("videoscale", "participant-"+suffix+"-scale")
	cellCaps := gst.ElementFactoryMake("capsfilter", "participant-"+suffix+"-cell-caps")

	box, err := parsePipelineElement(fmt.Sprintf(
		"videobox name=participant-%s-label-box bottom=-%d fill=black border-alpha=1.0",
		suffix,
		mosaicLabelHeight,
	))
	if err != nil {
		return nil, fmt.Errorf("create participant label box: %w", err)
	}
	// Bottom alignment plus ypad keeps the text visually centered in the
	// label strip added by videobox.
	overlay, err := parsePipelineElement(fmt.Sprintf(
		"textoverlay name=participant-%s-label text=%s font-desc=%s color=4294967295 halignment=center valignment=bottom ypad=%d wrap-mode=none",
		suffix,
		gstString(label),
		gstString(mosaicLabelFont),
		mosaicLabelYPad,
	))
	if err != nil {
		return nil, fmt.Errorf("create participant label overlay: %w", err)
	}

	if cellCaps != nil {
		cellCaps.SetObjectProperty("caps", gst.CapsFromString(fmt.Sprintf(
			"video/x-raw,width=%d,height=%d,pixel-aspect-ratio=1/1,format=RGBA",
			width,
			height,
		)))
	}

	elements := make([]gst.Element, 0, 7)
	for _, element := range []gst.Element{queue, decodebin, convert, scale, cellCaps, box, overlay} {
		if element != nil {
			elements = append(elements, element)
		}
	}

	return &participantTile{
		slot:      participant.slot,
		queue:     queue,
		decodebin: decodebin,
		convert:   convert,
		scale:     scale,
		cellCaps:  cellCaps,
		box:       box,
		overlay:   overlay,
		elements:  elements,
	}, nil
}

func (p *mosaicPipeline) startParticipantTile(tile *participantTile, sourcePad gst.Pad) error {
	if tile.queue == nil || tile.decodebin == nil || tile.convert == nil ||
		tile.scale == nil || tile.cellCaps == nil || tile.box == nil || tile.overlay == nil {
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
	if !tile.convert.Link(tile.scale) {
		return fmt.Errorf("link converter to scaler")
	}
	if !tile.scale.Link(tile.cellCaps) {
		return fmt.Errorf("link scaler to cell caps")
	}
	if !tile.cellCaps.Link(tile.box) {
		return fmt.Errorf("link cell caps to label box")
	}
	if !tile.box.Link(tile.overlay) {
		return fmt.Errorf("link label box to name overlay")
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
	if ret := tile.overlay.GetStaticPad("src").Link(tile.mixerPad); ret != gst.PadLinkOK {
		log.Printf("participant %s: link name overlay to compositor: %s", tile.overlay.GetName(), ret)
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
	if source := tile.overlay.GetStaticPad("src"); source != nil {
		source.Unlink(tile.mixerPad)
	}
	for _, element := range tile.elements {
		p.pipeline.Remove(element)
	}

	if mixer := p.pipeline.GetByName("mixer"); mixer != nil {
		mixer.ReleaseRequestPad(tile.mixerPad)
	}
	log.Printf("participant video track left: participant=%q identity=%q pad=%q", tile.participantSID, participant.identity, tile.sourcePad.GetName())
}

func (p *mosaicPipeline) gridColumns() int {
	if p.columns > 0 {
		return p.columns
	}
	return mosaicColumns
}

func (p *mosaicPipeline) gridRows() int {
	if p.rows > 0 {
		return p.rows
	}
	return mosaicRows
}

func (p *mosaicPipeline) gridCapacity() int {
	return p.gridColumns() * p.gridRows()
}

func (p *mosaicPipeline) gridPosition(slot int) (xpos, ypos, width, height int) {
	return participantGridPositionForGrid(slot, p.gridColumns(), p.gridRows())
}

func (p *mosaicPipeline) labelPosition(slot int) (xpos, ypos, width, height int) {
	return participantLabelPositionForGrid(slot, p.gridColumns(), p.gridRows())
}

func (p *mosaicPipeline) nextParticipantSlotLocked() (int, bool) {
	capacity := p.gridCapacity()
	used := make([]bool, capacity)
	for _, participant := range p.sources {
		if participant.slot >= 0 && participant.slot < capacity {
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

func participantGridPosition(slot int) (xpos, ypos, width, height int) {
	return participantGridPositionForGrid(slot, mosaicColumns, mosaicRows)
}

func participantGridPositionForGrid(slot, columns, rows int) (xpos, ypos, width, height int) {
	width = (backgroundWidth - mosaicGap*(columns+1)) / columns
	height = (backgroundHeight - mosaicGap*(rows+1) - rows*mosaicLabelHeight) / rows

	totalWidth := columns*width + (columns-1)*mosaicGap
	totalHeight := rows*height + rows*mosaicLabelHeight + (rows-1)*mosaicGap
	left := (backgroundWidth - totalWidth) / 2
	top := (backgroundHeight - totalHeight) / 2

	column := slot % columns
	row := slot / columns
	xpos = left + column*(width+mosaicGap)
	ypos = top + row*(height+mosaicLabelHeight+mosaicGap)
	return xpos, ypos, width, height
}

func participantLabelPosition(slot int) (xpos, ypos, width, height int) {
	return participantLabelPositionForGrid(slot, mosaicColumns, mosaicRows)
}

func participantLabelPositionForGrid(slot, columns, rows int) (xpos, ypos, width, height int) {
	xpos, ypos, width, height = participantGridPositionForGrid(slot, columns, rows)
	return xpos, ypos + height, width, mosaicLabelHeight
}

func participantLabelText(displayName, identity string) string {
	if label := strings.Join(strings.Fields(displayName), " "); label != "" {
		return label
	}
	return strings.Join(strings.Fields(identity), " ")
}

func participantSourceDescription(config config, name, producerIdentity string) string {
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
		gstString(producerIdentity),
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
