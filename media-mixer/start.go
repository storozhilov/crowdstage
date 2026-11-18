package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	sharedhttp "github.com/storozhilov/crowdstage/shared/httpapi"
)

type startRequest struct {
	GuestBroadcastSeconds *int `json:"guest_broadcast_seconds"`
}

func startHandler(mixer *mediaMixer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		seconds, err := decodeGuestBroadcastSeconds(r)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, err)
			return
		}
		if seconds != nil {
			mixer.setGuestBroadcastSeconds(*seconds)
		}
		if err := mixer.start(); err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, err)
			return
		}
		sharedhttp.WriteResponse(w, "started")
	}
}

func decodeGuestBroadcastSeconds(r *http.Request) (*int, error) {
	if r.Body == nil {
		return nil, nil
	}

	var request startRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("decode start request: %w", err)
	}
	if request.GuestBroadcastSeconds == nil {
		return nil, nil
	}
	seconds := *request.GuestBroadcastSeconds
	if seconds <= 0 {
		return nil, fmt.Errorf("guest broadcast duration must be a positive number of seconds")
	}
	if seconds > maxGuestBroadcastSeconds {
		return nil, fmt.Errorf(
			"guest broadcast duration %d is too large; maximum is %d seconds",
			seconds,
			maxGuestBroadcastSeconds,
		)
	}
	return &seconds, nil
}
