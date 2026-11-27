package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"
)

var errAudioSourceNotFound = errors.New("audio source not found")

type audioMuteRequest struct {
	Muted *bool `json:"muted"`
}

type audioSoloRequest struct {
	Solo *bool `json:"solo"`
}

type audioGainRequest struct {
	Gain *float64 `json:"gain"`
}

func audioMuteHandler(mixer *mediaMixer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request audioMuteRequest
		if err := decodeAudioRequest(w, r, &request); err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, err)
			return
		}
		if request.Muted == nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, errors.New("muted is required"))
			return
		}
		if err := mixer.setAudioMuted(r.PathValue("identity"), *request.Muted); err != nil {
			writeAudioError(w, err)
			return
		}
		sharedhttp.WriteResponse(w, "updated")
	}
}

func audioSoloHandler(mixer *mediaMixer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request audioSoloRequest
		if err := decodeAudioRequest(w, r, &request); err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, err)
			return
		}
		if request.Solo == nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, errors.New("solo is required"))
			return
		}
		if err := mixer.setAudioSolo(r.PathValue("identity"), *request.Solo); err != nil {
			writeAudioError(w, err)
			return
		}
		sharedhttp.WriteResponse(w, "updated")
	}
}

func audioGainHandler(mixer *mediaMixer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request audioGainRequest
		if err := decodeAudioRequest(w, r, &request); err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, err)
			return
		}
		if request.Gain == nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, errors.New("gain is required"))
			return
		}
		gain := *request.Gain
		if math.IsNaN(gain) || math.IsInf(gain, 0) || gain < 0 || gain > maxAudioGain {
			sharedhttp.WriteError(
				w,
				http.StatusBadRequest,
				fmt.Errorf("gain must be between 0 and %v", maxAudioGain),
			)
			return
		}
		if err := mixer.setAudioGain(r.PathValue("identity"), gain); err != nil {
			writeAudioError(w, err)
			return
		}
		sharedhttp.WriteResponse(w, "updated")
	}
}

func decodeAudioRequest(w http.ResponseWriter, r *http.Request, target any) error {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(target); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func writeAudioError(w http.ResponseWriter, err error) {
	if errors.Is(err, errAudioSourceNotFound) {
		sharedhttp.WriteError(w, http.StatusNotFound, err)
		return
	}
	sharedhttp.WriteError(w, http.StatusInternalServerError, err)
}

func (m *mediaMixer) setAudioMuted(participantIdentity string, muted bool) error {
	return m.updateAudioSource(participantIdentity, func(source *audioSource) {
		source.muted = muted
	})
}

func (m *mediaMixer) setAudioSolo(participantIdentity string, solo bool) error {
	return m.updateAudioSource(participantIdentity, func(source *audioSource) {
		source.solo = solo
	})
}

func (m *mediaMixer) setAudioGain(participantIdentity string, gain float64) error {
	return m.updateAudioSource(participantIdentity, func(source *audioSource) {
		source.gain = gain
	})
}

func (m *mediaMixer) updateAudioSource(participantIdentity string, update func(*audioSource)) error {
	if !m.audios.update(participantIdentity, update) {
		return fmt.Errorf("%w: %q", errAudioSourceNotFound, participantIdentity)
	}
	return m.reconcile()
}
