package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/pkg/gst"
	gstutil "github.com/storozhilov/crowdstage/shared/gstutil"
	"github.com/storozhilov/crowdstage/shared/livekitgst"
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

	maxGuestTiles     = mosaicColumns * mosaicRows
	maxMosaicCapacity = 100
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
	sources  map[string]*guestSource
	waiting  []string
}

type guestSource struct {
	lifecycle   sync.Mutex
	element     gst.Element
	name        string
	identity    string
	displayName string
	guestSID    string
	slot        int
	tile        *guestTile
}

type guestTile struct {
	mixerPad  gst.Pad
	sourcePad gst.Pad
	guestSID  string
	slot      int
	queue     gst.Element
	decodebin gst.Element
	convert   gst.Element
	scale     gst.Element
	cellCaps  gst.Element
	box       gst.Element
	overlay   gst.Element
	elements  []gst.Element
	createdAt time.Time
	removed   bool
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
		parseSource:     parseGuestSource,
		columns:         columns,
		rows:            rows,
		rotationTimeout: rotationTimeout,
		nextTile:        1,
		sources:         make(map[string]*guestSource),
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
			p.rotateGuest()
		}
	}
}

func (p *mosaicPipeline) rotateGuest() {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	active := p.oldestRotatableGuestLocked(time.Now())
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

	log.Printf("rotating guest: replacing identity=%q with identity=%q in slot=%d", active.identity, next.identity, slot)
	p.stopGuest(active)
	if err := p.startGuest(next); err != nil {
		log.Printf("guest identity=%q: %v", next.identity, err)
		p.removeGuestLocked(next.identity)
	}
}

func (p *mosaicPipeline) oldestRotatableGuestLocked(now time.Time) *guestSource {
	var oldest *guestSource
	for _, guest := range p.sources {
		if guest.slot < 0 || guest.tile == nil {
			continue
		}
		if now.Sub(guest.tile.createdAt) < p.rotationTimeout {
			continue
		}
		if oldest == nil || guest.tile.createdAt.Before(oldest.tile.createdAt) {
			oldest = guest
		}
	}
	return oldest
}

func (p *mosaicPipeline) addGuest(identity, displayName, guestSID, sourceName string) bool {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	return p.addGuestLocked(identity, displayName, guestSID, sourceName)
}

func (p *mosaicPipeline) addGuestLocked(identity, displayName, guestSID, sourceName string) bool {
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
	guest := &guestSource{
		name:        sourceName,
		identity:    identity,
		displayName: displayName,
		guestSID:    guestSID,
		slot:        -1,
	}
	active := p.registerGuestLocked(guest)
	p.mu.Unlock()

	if !active {
		log.Printf("guest identity=%q guest=%q: all %d mosaic tiles are occupied; waiting for a free tile", identity, guestSID, p.gridCapacity())
		return true
	}

	if err := p.startGuest(guest); err != nil {
		log.Printf("guest identity=%q: %v", identity, err)
		p.removeGuestLocked(identity)
		return false
	}
	return true
}

func (p *mosaicPipeline) registerGuestLocked(guest *guestSource) bool {
	guest.slot = -1
	p.sources[guest.identity] = guest
	if slot, available := p.nextGuestSlotLocked(); available {
		guest.slot = slot
		return true
	}
	p.waiting = append(p.waiting, guest.identity)
	return false
}

func (p *mosaicPipeline) unregisterGuestLocked(identity string) (removed, next *guestSource) {
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

func (p *mosaicPipeline) takeNextWaitingLocked() *guestSource {
	guest := p.popWaitingLocked()
	if guest == nil {
		return nil
	}

	slot, available := p.nextGuestSlotLocked()
	if !available {
		p.waiting = append([]string{guest.identity}, p.waiting...)
		return nil
	}
	guest.slot = slot
	return guest
}

func (p *mosaicPipeline) popWaitingLocked() *guestSource {
	for len(p.waiting) > 0 {
		identity := p.waiting[0]
		p.waiting = p.waiting[1:]

		guest := p.sources[identity]
		if guest == nil || guest.slot >= 0 {
			continue
		}
		return guest
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

func parseGuestSource(config config, name, producerIdentity string) (gst.Element, error) {
	element, err := gst.ParseLaunch(guestSourceDescription(config, name, producerIdentity))
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

func (p *mosaicPipeline) startGuest(guest *guestSource) error {
	guest.lifecycle.Lock()
	defer guest.lifecycle.Unlock()

	source, err := p.parseSource(p.config, guest.name, guest.identity)
	if err != nil {
		return err
	}
	guest.element = source

	source.ConnectPadAdded(func(_ gst.Element, pad gst.Pad) {
		p.guestPadAdded(guest, pad)
	})
	source.ConnectPadRemoved(func(_ gst.Element, pad gst.Pad) {
		p.guestPadRemoved(guest, pad)
	})

	if !p.pipeline.Add(source) {
		return fmt.Errorf("could not add source to pipeline")
	}

	p.mu.Lock()
	current := !p.closed && p.sources[guest.identity] == guest && guest.slot >= 0
	p.mu.Unlock()
	if !current {
		_ = source.SetState(gst.StateNull)
		p.pipeline.Remove(source)
		guest.element = nil
		return nil
	}

	source.SyncStateWithParent()
	log.Printf("guest source joined: guest=%q identity=%q", guest.guestSID, guest.identity)
	return nil
}

func (p *mosaicPipeline) removeGuest(identity string) {
	p.opsMu.Lock()
	defer p.opsMu.Unlock()

	p.removeGuestLocked(identity)
}

func (p *mosaicPipeline) removeGuestLocked(identity string) {
	if identity == "" {
		return
	}

	p.mu.Lock()
	guest, next := p.unregisterGuestLocked(identity)
	p.mu.Unlock()
	if guest == nil {
		return
	}

	p.stopGuest(guest)
	if next != nil {
		if err := p.startGuest(next); err != nil {
			log.Printf("guest identity=%q: %v", next.identity, err)
			p.removeGuestLocked(next.identity)
		}
	}
}

func (p *mosaicPipeline) stopGuest(guest *guestSource) {
	guest.lifecycle.Lock()
	defer guest.lifecycle.Unlock()

	element := guest.element
	if element != nil {
		_ = element.SetState(gst.StateNull)
	}
	p.removeGuestTile(guest, nil)
	if element != nil {
		p.pipeline.Remove(element)
		guest.element = nil
	}
	log.Printf("guest source left: guest=%q identity=%q", guest.guestSID, guest.identity)
}

func (p *mosaicPipeline) guestPadAdded(guest *guestSource, sourcePad gst.Pad) {
	if !strings.HasPrefix(sourcePad.GetName(), "video_") {
		return
	}

	p.mu.Lock()
	if p.closed || p.sources[guest.identity] != guest {
		p.mu.Unlock()
		return
	}
	if guest.tile != nil {
		p.mu.Unlock()
		log.Printf("guest identity=%q: ignoring additional video pad %q", guest.identity, sourcePad.GetName())
		return
	}
	if guest.slot < 0 {
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
		log.Printf("guest identity=%q: could not request compositor pad", guest.identity)
		return
	}

	tile, err := p.newGuestTile(guest)
	if err != nil {
		p.mu.Unlock()
		mixer.ReleaseRequestPad(mixerPad)
		log.Printf("guest identity=%q: %v", guest.identity, err)
		return
	}
	tile.sourcePad = sourcePad
	tile.guestSID = guest.guestSID
	tile.createdAt = time.Now()
	xpos, ypos, width, height := p.gridPosition(guest.slot)
	mixerPad.SetObjectProperties(map[string]any{
		"xpos":   int32(xpos),
		"ypos":   int32(ypos),
		"width":  int32(width),
		"height": int32(height + mosaicLabelHeight),
		"zorder": uint32(1),
	})
	tile.mixerPad = mixerPad
	guest.tile = tile
	p.mu.Unlock()

	if err := p.startGuestTile(tile, sourcePad); err != nil {
		log.Printf("guest identity=%q: %v", guest.identity, err)
		p.removeGuestTile(guest, sourcePad)
		return
	}

	log.Printf("guest video track joined: guest=%q identity=%q pad=%q", tile.guestSID, guest.identity, sourcePad.GetName())
}

func (p *mosaicPipeline) newGuestTile(guest *guestSource) (*guestTile, error) {
	suffix := fmt.Sprintf("%d", p.nextTile)
	p.nextTile++

	_, _, width, height := p.gridPosition(guest.slot)
	label := guestLabelText(guest.displayName, guest.identity)

	queue := gst.ElementFactoryMake("queue", "guest-"+suffix+"-queue")
	decodebin := gst.ElementFactoryMake("decodebin", "guest-"+suffix+"-decodebin")
	convert := gst.ElementFactoryMake("videoconvert", "guest-"+suffix+"-convert")
	scale := gst.ElementFactoryMake("videoscale", "guest-"+suffix+"-scale")
	cellCaps := gst.ElementFactoryMake("capsfilter", "guest-"+suffix+"-cell-caps")

	box, err := parsePipelineElement(fmt.Sprintf(
		"videobox name=guest-%s-label-box bottom=-%d fill=black border-alpha=1.0",
		suffix,
		mosaicLabelHeight,
	))
	if err != nil {
		return nil, fmt.Errorf("create guest label box: %w", err)
	}
	// Bottom alignment plus ypad keeps the text visually centered in the
	// label strip added by videobox.
	overlay, err := parsePipelineElement(fmt.Sprintf(
		"textoverlay name=guest-%s-label text=%s font-desc=%s color=4294967295 halignment=center valignment=bottom ypad=%d wrap-mode=none",
		suffix,
		gstutil.String(label),
		gstutil.String(mosaicLabelFont),
		mosaicLabelYPad,
	))
	if err != nil {
		return nil, fmt.Errorf("create guest label overlay: %w", err)
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

	return &guestTile{
		slot:      guest.slot,
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

func (p *mosaicPipeline) startGuestTile(tile *guestTile, sourcePad gst.Pad) error {
	if tile.queue == nil || tile.decodebin == nil || tile.convert == nil ||
		tile.scale == nil || tile.cellCaps == nil || tile.box == nil || tile.overlay == nil {
		return fmt.Errorf("could not create guest pipeline elements")
	}

	for _, element := range tile.elements {
		if !p.pipeline.Add(element) {
			return fmt.Errorf("could not add %s to pipeline", element.GetName())
		}
	}

	if ret := sourcePad.Link(tile.queue.GetStaticPad("sink")); ret != gst.PadLinkOK {
		return fmt.Errorf("link guest source to queue: %s", ret)
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
		p.linkGuestVideo(tile, pad)
	})

	for _, element := range tile.elements {
		element.SyncStateWithParent()
	}
	return nil
}

func (p *mosaicPipeline) linkGuestVideo(tile *guestTile, pad gst.Pad) {
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
		log.Printf("guest %s: link decoded video to converter: %s", tile.decodebin.GetName(), ret)
		return
	}
	if ret := tile.overlay.GetStaticPad("src").Link(tile.mixerPad); ret != gst.PadLinkOK {
		log.Printf("guest %s: link name overlay to compositor: %s", tile.overlay.GetName(), ret)
	}
}

func (p *mosaicPipeline) guestPadRemoved(guest *guestSource, sourcePad gst.Pad) {
	p.mu.Lock()
	current := !p.closed && p.sources[guest.identity] == guest
	p.mu.Unlock()
	if !current {
		return
	}
	p.removeGuestTile(guest, sourcePad)
}

func (p *mosaicPipeline) removeGuestTile(guest *guestSource, sourcePad gst.Pad) {
	p.mu.Lock()
	tile := guest.tile
	if tile == nil {
		p.mu.Unlock()
		return
	}
	if sourcePad != nil && tile.sourcePad != sourcePad {
		p.mu.Unlock()
		return
	}
	guest.tile = nil
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
	log.Printf("guest video track left: guest=%q identity=%q pad=%q", tile.guestSID, guest.identity, tile.sourcePad.GetName())
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
	return guestGridPositionForGrid(slot, p.gridColumns(), p.gridRows())
}

func (p *mosaicPipeline) labelPosition(slot int) (xpos, ypos, width, height int) {
	return guestLabelPositionForGrid(slot, p.gridColumns(), p.gridRows())
}

func (p *mosaicPipeline) nextGuestSlotLocked() (int, bool) {
	capacity := p.gridCapacity()
	used := make([]bool, capacity)
	for _, guest := range p.sources {
		if guest.slot >= 0 && guest.slot < capacity {
			used[guest.slot] = true
		}
	}
	for slot, occupied := range used {
		if !occupied {
			return slot, true
		}
	}
	return 0, false
}

func guestGridPosition(slot int) (xpos, ypos, width, height int) {
	return guestGridPositionForGrid(slot, mosaicColumns, mosaicRows)
}

func guestGridPositionForGrid(slot, columns, rows int) (xpos, ypos, width, height int) {
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

func guestLabelPosition(slot int) (xpos, ypos, width, height int) {
	return guestLabelPositionForGrid(slot, mosaicColumns, mosaicRows)
}

func guestLabelPositionForGrid(slot, columns, rows int) (xpos, ypos, width, height int) {
	xpos, ypos, width, height = guestGridPositionForGrid(slot, columns, rows)
	return xpos, ypos + height, width, mosaicLabelHeight
}

func guestLabelText(displayName, identity string) string {
	if label := strings.Join(strings.Fields(displayName), " "); label != "" {
		return label
	}
	return strings.Join(strings.Fields(identity), " ")
}

func guestSourceDescription(config config, name, producerIdentity string) string {
	return livekitgst.SourceDescription(name, livekitEndpoint(config), producerIdentity)
}

func livekitEndpoint(config config) livekitgst.Endpoint {
	return livekitgst.Endpoint{
		URL:       config.LiveKitURL,
		APIKey:    config.APIKey,
		APISecret: config.APISecret,
		Room:      config.RoomName,
		Identity:  config.Identity,
	}
}

func guestSourceIdentity(config config, name string) string {
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
%s`,
		backgroundWidth,
		backgroundHeight,
		backgroundFrameRate,
		backgroundWidth,
		backgroundHeight,
		backgroundWidth,
		backgroundHeight,
		backgroundFrameRate,
		backgroundFrameRate,
		livekitgst.SinkDescription(livekitgst.Endpoint{
			URL:       config.LiveKitURL,
			APIKey:    config.APIKey,
			APISecret: config.APISecret,
			Room:      config.StudioRoomName,
			Identity:  config.Identity,
		}, "video/x-vp8"),
	)
}
