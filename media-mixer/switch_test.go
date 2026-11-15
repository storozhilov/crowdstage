package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMediaMixerSwitchSourceRejectsParticipantsWithoutVideo(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.videos.add(studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a",
	})

	err := mixer.switchSource("publisher-b")

	if !errors.Is(err, errSourceNotPublishing) {
		t.Fatalf("error = %v, want %v", err, errSourceNotPublishing)
	}
}

func TestMediaMixerSwitchSourceSelectsAVideoWhileStopped(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.videos.add(studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a",
	})
	mixer.videos.add(studioVideo{
		participantIdentity: "publisher-b",
		publicationSID:      "TR_b",
	})

	if err := mixer.switchSource("publisher-b"); err != nil {
		t.Fatalf("switch source: %v", err)
	}

	if mixer.selected == nil || mixer.selected.participantIdentity != "publisher-b" {
		t.Fatalf("selected = %+v, want publisher-b", mixer.selected)
	}
}

func TestMediaMixerKeepsAnExplicitSelectionWhileItIsPublishing(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.videos.add(studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a",
	})
	mixer.videos.add(studioVideo{
		participantIdentity: "publisher-b",
		publicationSID:      "TR_b",
	})
	selected := mixer.videos.findByIdentity("publisher-b")

	next := mixer.nextVideo(selected)

	if next == nil || next.participantIdentity != "publisher-b" {
		t.Fatalf("next video = %+v, want publisher-b", next)
	}
}

func TestMediaMixerFallsBackToTheOldestVideoWhenTheSelectionDisappears(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.videos.add(studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a",
	})
	mixer.videos.add(studioVideo{
		participantIdentity: "publisher-b",
		publicationSID:      "TR_b",
	})
	selected := mixer.videos.findByIdentity("publisher-b")
	mixer.videos.removeParticipant("publisher-b")

	next := mixer.nextVideo(selected)

	if next == nil || next.participantIdentity != "publisher-a" {
		t.Fatalf("next video = %+v, want publisher-a", next)
	}
}

func TestSwitchSourceHandlerSwitchesTheBroadcastedVideo(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.videos.add(studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a",
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/switch",
		strings.NewReader(`{"participant_identity":"publisher-a"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	switchSourceHandler(mixer).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if mixer.selected == nil || mixer.selected.participantIdentity != "publisher-a" {
		t.Fatalf("selected = %+v, want publisher-a", mixer.selected)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "switched" {
		t.Fatalf("status = %q, want %q", body.Status, "switched")
	}
}

func TestSwitchSourceHandlerRejectsMissingParticipantIdentity(t *testing.T) {
	mixer := &mediaMixer{}

	for _, body := range []string{`{}`, `{"participant_identity":""}`, `not-json`} {
		request := httptest.NewRequest(http.MethodPost, "/switch", strings.NewReader(body))
		response := httptest.NewRecorder()

		switchSourceHandler(mixer).ServeHTTP(response, request)

		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want %d", body, response.Code, http.StatusBadRequest)
		}
	}
}

func TestSwitchSourceHandlerReportsMissingVideo(t *testing.T) {
	mixer := &mediaMixer{}

	request := httptest.NewRequest(
		http.MethodPost,
		"/switch",
		strings.NewReader(`{"participant_identity":"publisher-a"}`),
	)
	response := httptest.NewRecorder()

	switchSourceHandler(mixer).ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status code = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}
}

func TestSameStudioVideoComparesThePublication(t *testing.T) {
	selected := &studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a1",
	}
	samePublication := &studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a1",
		publishedAt:         selected.publishedAt.Add(1),
	}
	republished := &studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a2",
	}

	if !sameStudioVideo(selected, samePublication) {
		t.Fatal("the same publication should be considered unchanged")
	}
	if sameStudioVideo(selected, republished) {
		t.Fatal("a republished track under the same identity should require a pipeline replacement")
	}
	if sameStudioVideo(nil, selected) || sameStudioVideo(selected, nil) {
		t.Fatal("a missing video cannot match a selected publication")
	}
}

func TestMediaMixerUsesTheCurrentPublicationOfTheSelectedParticipant(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.videos.add(studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a2",
	})
	selected := &studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a1",
	}

	next := mixer.nextVideo(selected)

	if next == nil || next.publicationSID != "TR_a2" {
		t.Fatalf("next video = %+v, want the current publication TR_a2", next)
	}
	if sameStudioVideo(selected, next) {
		t.Fatal("a stale selected publication should not be considered current")
	}
}

func TestMediaMixerPreservesTheSelectedSourceAcrossStop(t *testing.T) {
	selected := &studioVideo{
		participantIdentity: "publisher-b",
		publicationSID:      "TR_b",
	}
	mixer := &mediaMixer{
		running:     true,
		selected:    selected,
		broadcasted: selected,
	}

	if err := mixer.stop(); err != nil {
		t.Fatalf("stop mixer: %v", err)
	}

	if mixer.running {
		t.Fatal("mixer is still running after stop")
	}
	if mixer.selected == nil || mixer.selected.participantIdentity != "publisher-b" {
		t.Fatalf("selected = %+v, want the publisher-b selection to be preserved", mixer.selected)
	}
	if mixer.broadcasted != nil {
		t.Fatalf("broadcasted = %+v, want nil after stop", mixer.broadcasted)
	}
}
