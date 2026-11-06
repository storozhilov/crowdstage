package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/livekit/server-sdk-go/v2/pkg/samplebuilder"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

type forward struct {
	cancel   context.CancelFunc
	sid      string
	recorder *mediaRecorder
	source   rtpSource
	done     chan struct{}
	stopOnce sync.Once
}

type rtpSource interface {
	ReadRTP() (*rtp.Packet, interceptor.Attributes, error)
	SetReadDeadline(deadline time.Time) error
	Codec() webrtc.RTPCodecParameters
}

type rtpTarget interface {
	WriteRTP(packet *rtp.Packet, options *lksdk.SampleWriteOptions) error
}

type trackSource struct {
	track       *webrtc.TrackRemote
	publication *lksdk.RemoteTrackPublication
	participant *lksdk.RemoteParticipant
}

type trackUnpublisher interface {
	UnpublishTrack(sid string) error
}

func (c *composer) trackSubscribed(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
	if track == nil {
		return
	}

	key := publication.SID()

	c.forwardsMu.Lock()
	if c.sources == nil {
		c.sources = make(map[string]trackSource)
	}
	c.sources[key] = trackSource{
		track:       track,
		publication: publication,
		participant: participant,
	}
	active := c.pipelineActive
	c.forwardsMu.Unlock()

	if active {
		c.startForward(key)
	}
}

func (c *composer) startPipeline() {
	c.forwardsMu.Lock()
	c.pipelineActive = true
	keys := make([]string, 0, len(c.sources))
	for key := range c.sources {
		keys = append(keys, key)
	}
	c.forwardsMu.Unlock()

	for _, key := range keys {
		c.startForward(key)
	}
}

func (c *composer) stopPipeline() {
	c.forwardsMu.Lock()
	c.pipelineActive = false
	entries := make([]*forward, 0, len(c.forwards))
	for _, entry := range c.forwards {
		entries = append(entries, entry)
	}
	c.forwards = make(map[string]*forward)
	c.forwardsMu.Unlock()

	for _, entry := range entries {
		c.stopForwardEntry(entry)
	}
}

func (c *composer) startForward(key string) {
	c.forwardsMu.Lock()
	source, ok := c.sources[key]
	active := c.pipelineActive
	_, running := c.forwards[key]
	c.forwardsMu.Unlock()

	if !ok || !active || running {
		return
	}

	c.forwardTrack(key, source)
}

func (c *composer) forwardTrack(key string, source trackSource) {
	track := source.track
	publication := source.publication
	participant := source.participant

	recorder, err := c.startRecording(publication, track)
	if err != nil {
		log.Printf("start recording track %q from participant %q: %v", publication.Name(), participant.Identity(), err)
	}

	localTrack, err := lksdk.NewLocalTrack(track.Codec().RTPCodecCapability)
	if err != nil {
		if recorder != nil {
			_ = recorder.close()
		}
		log.Printf("create forwarded track for participant %q: %v", participant.Identity(), err)
		return
	}

	options := &lksdk.TrackPublicationOptions{
		Name:   publication.Name(),
		Source: publication.Source(),
	}
	if info := publication.TrackInfo(); info != nil {
		options.VideoWidth = int(info.Width)
		options.VideoHeight = int(info.Height)
	}

	localPublication, err := c.studioRoom.LocalParticipant.PublishTrack(localTrack, options)
	if err != nil {
		if recorder != nil {
			_ = recorder.close()
		}
		log.Printf("publish forwarded track %q to studio room: %v", publication.Name(), err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	entry := &forward{
		cancel:   cancel,
		sid:      localPublication.SID(),
		recorder: recorder,
		source:   track,
		done:     make(chan struct{}),
	}

	c.forwardsMu.Lock()
	if !c.pipelineActive {
		c.forwardsMu.Unlock()
		close(entry.done)
		c.stopForwardEntry(entry)
		return
	}
	previous := c.forwards[key]
	c.forwards[key] = entry
	c.forwardsMu.Unlock()

	if previous != nil {
		c.stopForwardEntry(previous)
	}

	log.Printf("forwarding track %q from participant %q to the studio room", publication.Name(), participant.Identity())
	go c.forwardRTP(ctx, key, entry, track, localTrack)
}

func (c *composer) startRecording(publication *lksdk.RemoteTrackPublication, track *webrtc.TrackRemote) (*mediaRecorder, error) {
	if c.config.RecordingsDir == "" || track.Kind() != webrtc.RTPCodecTypeVideo {
		return nil, nil
	}
	if track.Codec().MimeType != webrtc.MimeTypeVP8 {
		return nil, fmt.Errorf("unsupported video codec %q", track.Codec().MimeType)
	}

	width, height := 0, 0
	if info := publication.TrackInfo(); info != nil {
		width = int(info.Width)
		height = int(info.Height)
	}

	path := filepath.Join(c.config.RecordingsDir, publication.SID()+".mkv")
	recorder, err := newMediaRecorder(path, width, height)
	if err != nil {
		return nil, err
	}

	log.Printf("recording track %q (%s) to %s", publication.Name(), publication.SID(), path)
	return recorder, nil
}

func (c *composer) forwardRTP(ctx context.Context, key string, entry *forward, source rtpSource, target rtpTarget) {
	defer func() {
		c.forwardsMu.Lock()
		if c.forwards[key] == entry {
			delete(c.forwards, key)
		}
		c.forwardsMu.Unlock()
		close(entry.done)
		c.stopForwardEntry(entry)
	}()

	var samples *samplebuilder.SampleBuilder
	if entry.recorder != nil {
		samples = samplebuilder.New(1000, &codecs.VP8Packet{}, source.Codec().ClockRate)
	}
	recordingFailed := false

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		packet, _, err := source.ReadRTP()
		if err != nil {
			return
		}

		if samples != nil && !recordingFailed {
			samples.Push(packet)
			for {
				sample := samples.Pop()
				if sample == nil {
					break
				}
				if err := entry.recorder.writeSample(sample.Data, sample.Duration); err != nil {
					log.Printf("record track %q: %v", key, err)
					_ = entry.recorder.close()
					recordingFailed = true
					break
				}
			}
		}

		if err := target.WriteRTP(packet, nil); err != nil {
			return
		}
	}
}

func (c *composer) trackUnsubscribed(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
	key := publication.SID()

	c.forwardsMu.Lock()
	delete(c.sources, key)
	entry := c.forwards[key]
	if entry != nil {
		delete(c.forwards, key)
	}
	c.forwardsMu.Unlock()

	if entry != nil {
		c.stopForwardEntry(entry)
	}
}

func (c *composer) stopForwardEntry(entry *forward) {
	entry.stopOnce.Do(func() {
		entry.cancel()
		if entry.source != nil {
			if err := entry.source.SetReadDeadline(time.Now()); err != nil {
				log.Printf("interrupt forwarding source for track %q: %v", entry.sid, err)
			}
		}
		if entry.done != nil {
			<-entry.done
			if entry.source != nil {
				if err := entry.source.SetReadDeadline(time.Time{}); err != nil {
					log.Printf("restore forwarding source read deadline for track %q: %v", entry.sid, err)
				}
			}
		}
		if entry.recorder != nil {
			if err := entry.recorder.close(); err != nil {
				log.Printf("stop recording for forwarded track %q: %v", entry.sid, err)
			}
		}
		if err := c.studioUnpublisher.UnpublishTrack(entry.sid); err != nil {
			log.Printf("unpublish forwarded track %q: %v", entry.sid, err)
		}
	})
}

func (c *composer) closeForwards() {
	c.forwardsMu.Lock()
	entries := make([]*forward, 0, len(c.forwards))
	for _, entry := range c.forwards {
		entries = append(entries, entry)
	}
	c.forwards = make(map[string]*forward)
	c.forwardsMu.Unlock()

	for _, entry := range entries {
		c.stopForwardEntry(entry)
	}
}
