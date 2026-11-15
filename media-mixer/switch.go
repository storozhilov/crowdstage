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

	video := m.videos.findByIdentity(participantIdentity)
	if video == nil {
		return fmt.Errorf("%w: %q", errSourceNotPublishing, participantIdentity)
	}

	m.mu.Lock()
	selected := m.selected
	running := m.running
	m.mu.Unlock()

	if selected != nil && selected.participantIdentity == participantIdentity {
		m.mu.Lock()
		m.selected = video
		m.mu.Unlock()
		return nil
	}

	if !running {
		m.mu.Lock()
		m.selected = video
		m.mu.Unlock()
		return nil
	}

	return m.replacePipelineLocked(video)
}
