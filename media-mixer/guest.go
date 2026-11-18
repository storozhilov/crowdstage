package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/livekit/protocol/livekit"
)

const (
	defaultGuestBroadcastSeconds = 10
	maxGuestBroadcastSeconds     = int((1<<63 - 1) / int64(time.Second))

	guestLookupTimeout = 5 * time.Second
)

func (m *mediaMixer) setGuestBroadcastSeconds(seconds int) {
	m.mu.Lock()
	m.guestBroadcastSeconds = seconds
	m.mu.Unlock()
}

func (m *mediaMixer) broadcastGuestLocked(guest *studioVideo) error {
	m.mu.Lock()
	m.guestBroadcast = guest
	m.mu.Unlock()

	m.scheduleGuestBroadcastExpiry()

	m.mu.Lock()
	running := m.running
	m.mu.Unlock()
	if !running {
		return nil
	}

	if _, err := m.ensurePipelineLocked(); err != nil {
		return err
	}
	return m.reconcileLocked()
}

func (m *mediaMixer) scheduleGuestBroadcastExpiry() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.guestBroadcastTimerID++
	timerID := m.guestBroadcastTimerID
	if m.guestBroadcastTimer != nil {
		m.guestBroadcastTimer.Stop()
	}

	seconds := m.guestBroadcastSeconds
	if seconds <= 0 {
		seconds = defaultGuestBroadcastSeconds
	}
	m.guestBroadcastTimer = time.AfterFunc(time.Duration(seconds)*time.Second, func() {
		m.expireGuestBroadcast(timerID)
	})
}

func (m *mediaMixer) expireGuestBroadcast(timerID uint64) {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	m.mu.Lock()
	if m.guestBroadcastTimerID != timerID || m.guestBroadcast == nil {
		m.mu.Unlock()
		return
	}
	m.guestBroadcast = nil
	m.guestBroadcastTimer = nil
	m.mu.Unlock()

	if err := m.reconcileLocked(); err != nil {
		log.Printf("stop guest broadcast: %v", err)
	}
}

func (m *mediaMixer) clearGuestBroadcastLocked() {
	m.guestBroadcastTimerID++
	if m.guestBroadcastTimer != nil {
		m.guestBroadcastTimer.Stop()
		m.guestBroadcastTimer = nil
	}
	m.guestBroadcast = nil
}

func (m *mediaMixer) findGuestVideo(participantIdentity string) (*studioVideo, error) {
	if m.guestLookup != nil {
		return m.guestLookup(participantIdentity)
	}
	if m.guestClient == nil {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), guestLookupTimeout)
	defer cancel()

	participant, err := m.guestClient.GetParticipant(ctx, &livekit.RoomParticipantIdentity{
		Room:     m.config.GuestRoom,
		Identity: participantIdentity,
	})
	if err != nil {
		return nil, fmt.Errorf("look up guest %q in room %q: %w", participantIdentity, m.config.GuestRoom, err)
	}

	for _, track := range participant.Tracks {
		if track.Type != livekit.TrackType_VIDEO {
			continue
		}
		return &studioVideo{
			participantIdentity: participant.Identity,
			participantName:     participant.Name,
			participantSID:      participant.Sid,
			publicationSID:      track.Sid,
			publishedAt:         time.Now(),
			room:                m.config.GuestRoom,
		}, nil
	}
	return nil, nil
}

func roomServiceURL(livekitURL string) string {
	switch {
	case strings.HasPrefix(livekitURL, "ws://"):
		return "http://" + strings.TrimPrefix(livekitURL, "ws://")
	case strings.HasPrefix(livekitURL, "wss://"):
		return "https://" + strings.TrimPrefix(livekitURL, "wss://")
	default:
		return livekitURL
	}
}
