package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"
)

var errSourceNotPublishing = errors.New("participant is not publishing video to the studio room")

type switchSourceRequest struct {
	ParticipantIdentity string `json:"participant_identity"`
}

func switchSourceHandler(mixer *mediaMixer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request switchSourceRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
			return
		}

		participantIdentity := strings.TrimSpace(request.ParticipantIdentity)
		if participantIdentity == "" {
			sharedhttp.WriteError(w, http.StatusBadRequest, errors.New("participant_identity is required"))
			return
		}

		if err := mixer.switchSource(participantIdentity); err != nil {
			if errors.Is(err, errSourceNotPublishing) {
				sharedhttp.WriteError(w, http.StatusConflict, err)
				return
			}
			sharedhttp.WriteError(w, http.StatusInternalServerError, err)
			return
		}

		sharedhttp.WriteResponse(w, "switched")
	}
}

func (m *mediaMixer) switchSource(participantIdentity string) error {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	// A studio participant takes precedence when the same identity exists in
	// both the studio and guest source rooms.
	video := m.videos.findByIdentity(participantIdentity)
	if video == nil {
		guest, err := m.findGuestVideo(participantIdentity)
		if err != nil {
			return err
		}
		if guest == nil {
			return fmt.Errorf("%w: %q", errSourceNotPublishing, participantIdentity)
		}
		return m.broadcastGuestLocked(guest)
	}

	m.mu.Lock()
	m.clearGuestBroadcastLocked()
	m.selected = video
	running := m.running
	broadcasted := m.broadcasted
	m.mu.Unlock()

	if !running {
		return nil
	}
	if sameStudioVideo(broadcasted, video) {
		return nil
	}

	if _, err := m.ensurePipelineLocked(); err != nil {
		return err
	}
	return m.applySourceLocked(video, true)
}
