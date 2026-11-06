package main

import (
	"context"
	"log"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
)

type forward struct {
	cancel      context.CancelFunc
	publication *lksdk.LocalTrackPublication
}

func (c *composer) forwardTrack(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
	if track == nil {
		return
	}

	localTrack, err := lksdk.NewLocalTrack(track.Codec().RTPCodecCapability)
	if err != nil {
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
		log.Printf("publish forwarded track %q to studio room: %v", publication.Name(), err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	entry := &forward{cancel: cancel, publication: localPublication}
	key := publication.SID()

	c.forwardsMu.Lock()
	previous := c.forwards[key]
	c.forwards[key] = entry
	c.forwardsMu.Unlock()

	if previous != nil {
		c.stopForwardEntry(previous)
	}

	log.Printf("forwarding track %q from participant %q to the studio room", publication.Name(), participant.Identity())
	go c.forwardRTP(ctx, key, entry, track, localTrack)
}

func (c *composer) forwardRTP(ctx context.Context, key string, entry *forward, source *webrtc.TrackRemote, target *lksdk.LocalTrack) {
	defer func() {
		c.forwardsMu.Lock()
		if c.forwards[key] != entry {
			c.forwardsMu.Unlock()
			return
		}
		delete(c.forwards, key)
		c.forwardsMu.Unlock()
		c.stopForwardEntry(entry)
	}()

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
		if err := target.WriteRTP(packet, nil); err != nil {
			return
		}
	}
}

func (c *composer) stopForward(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, participant *lksdk.RemoteParticipant) {
	key := publication.SID()

	c.forwardsMu.Lock()
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
	entry.cancel()
	if err := c.studioRoom.LocalParticipant.UnpublishTrack(entry.publication.SID()); err != nil {
		log.Printf("unpublish forwarded track: %v", err)
	}
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
		entry.cancel()
	}
}
