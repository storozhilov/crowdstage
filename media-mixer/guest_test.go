package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/twitchtv/twirp"
)

func TestGuestLookupFailureTreatsNotFoundAsMissingParticipant(t *testing.T) {
	err := guestLookupFailure("guest-a", "guests", twirp.NotFound.Error("participant not found"))

	if err != nil {
		t.Fatalf("error = %v, want nil for a missing participant", err)
	}
}

func TestGuestLookupFailureWrapsOtherErrors(t *testing.T) {
	err := guestLookupFailure("guest-a", "guests", errors.New("room service unavailable"))

	if err == nil {
		t.Fatal("error is nil for a room service failure")
	}
	if !strings.Contains(err.Error(), "room service unavailable") {
		t.Fatalf("error = %q, want the room service failure", err)
	}
}

func TestFindMosaicVideoSkipsAnUnconfiguredOrSharedGuestRoom(t *testing.T) {
	for _, mosaicRoom := range []string{"", "guests"} {
		mixer := &mediaMixer{config: config{GuestRoom: "guests", MosaicRoom: mosaicRoom}}

		video, err := mixer.findMosaicVideo("mosaic-composer")

		if err != nil {
			t.Fatalf("mosaic room %q: error = %v, want nil", mosaicRoom, err)
		}
		if video != nil {
			t.Fatalf("mosaic room %q: video = %+v, want nil", mosaicRoom, video)
		}
	}
}

func TestRoomServiceURLUsesHTTPForWebSocketURLs(t *testing.T) {
	cases := map[string]string{
		"ws://livekit:7880":   "http://livekit:7880",
		"wss://livekit:7880":  "https://livekit:7880",
		"http://livekit:7880": "http://livekit:7880",
	}
	for input, want := range cases {
		if got := roomServiceURL(input); got != want {
			t.Errorf("roomServiceURL(%q) = %q, want %q", input, got, want)
		}
	}
}
