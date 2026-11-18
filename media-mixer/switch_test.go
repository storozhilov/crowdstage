package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestMediaMixerSwitchSourceBroadcastsAGuestTemporarily(t *testing.T) {
	studio := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	guest := &studioVideo{
		participantIdentity: "guest-a",
		publicationSID:      "TR_guest",
		room:                "guests",
	}
	pipeline := &fakeMixerPipeline{source: studio}
	mixer := &mediaMixer{
		config:                config{GuestRoom: "guests"},
		running:               true,
		pipeline:              pipeline,
		selected:              studio,
		broadcasted:           studio,
		guestBroadcastSeconds: 3600,
		guestLookup: func(participantIdentity string) (*studioVideo, error) {
			if participantIdentity == guest.participantIdentity {
				return guest, nil
			}
			return nil, nil
		},
	}
	mixer.videos.add(*studio)

	if err := mixer.switchSource("guest-a"); err != nil {
		t.Fatalf("switch source: %v", err)
	}

	if pipeline.source == nil || pipeline.source.participantIdentity != "guest-a" {
		t.Fatalf("pipeline source = %+v, want guest-a", pipeline.source)
	}
	if mixer.broadcasted == nil || mixer.broadcasted.participantIdentity != "guest-a" {
		t.Fatalf("broadcasted = %+v, want guest-a", mixer.broadcasted)
	}
	if mixer.selected == nil || mixer.selected.participantIdentity != "publisher-a" {
		t.Fatalf("selected = %+v, want the studio selection to stay publisher-a", mixer.selected)
	}
	if mixer.guestBroadcast == nil || mixer.guestBroadcast.participantIdentity != "guest-a" {
		t.Fatalf("guest broadcast = %+v, want guest-a", mixer.guestBroadcast)
	}

	mixer.mu.Lock()
	mixer.clearGuestBroadcastLocked()
	mixer.mu.Unlock()
}

func TestMediaMixerGuestBroadcastFallsBackToTheStudioSource(t *testing.T) {
	studio := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	guest := &studioVideo{
		participantIdentity: "guest-a",
		publicationSID:      "TR_guest",
		room:                "guests",
	}
	pipeline := &fakeMixerPipeline{source: studio}
	mixer := &mediaMixer{
		config:                config{GuestRoom: "guests"},
		running:               true,
		pipeline:              pipeline,
		selected:              studio,
		broadcasted:           studio,
		guestBroadcastSeconds: 1,
		guestLookup: func(string) (*studioVideo, error) {
			return guest, nil
		},
	}
	mixer.videos.add(*studio)

	if err := mixer.switchSource("guest-a"); err != nil {
		t.Fatalf("switch source: %v", err)
	}
	if mixer.broadcasted == nil || mixer.broadcasted.participantIdentity != "guest-a" {
		t.Fatalf("broadcasted = %+v, want guest-a", mixer.broadcasted)
	}

	waitForBroadcastedParticipant(t, mixer, "publisher-a", 5*time.Second)
}

func TestMediaMixerRepeatedGuestSwitchKeepsTheOriginalTimeout(t *testing.T) {
	studio := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	guest := &studioVideo{
		participantIdentity: "guest-a",
		publicationSID:      "TR_guest",
		room:                "guests",
	}
	pipeline := &fakeMixerPipeline{source: studio}
	mixer := &mediaMixer{
		config:                config{GuestRoom: "guests"},
		running:               true,
		pipeline:              pipeline,
		selected:              studio,
		broadcasted:           studio,
		guestBroadcastSeconds: 4,
		guestLookup: func(string) (*studioVideo, error) {
			return guest, nil
		},
	}
	mixer.videos.add(*studio)

	if err := mixer.switchSource("guest-a"); err != nil {
		t.Fatalf("switch source: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if err := mixer.switchSource("guest-a"); err != nil {
		t.Fatalf("switch source again: %v", err)
	}

	waitForBroadcastedParticipant(t, mixer, "publisher-a", 3200*time.Millisecond)
}

func TestMediaMixerGuestDurationUpdateReschedulesTheTimeout(t *testing.T) {
	studio := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	guest := &studioVideo{
		participantIdentity: "guest-a",
		publicationSID:      "TR_guest",
		room:                "guests",
	}
	pipeline := &fakeMixerPipeline{source: studio}
	mixer := &mediaMixer{
		config:                config{GuestRoom: "guests"},
		running:               true,
		pipeline:              pipeline,
		selected:              studio,
		broadcasted:           studio,
		guestBroadcastSeconds: 5,
		guestLookup: func(string) (*studioVideo, error) {
			return guest, nil
		},
	}
	mixer.videos.add(*studio)

	if err := mixer.switchSource("guest-a"); err != nil {
		t.Fatalf("switch source: %v", err)
	}
	mixer.updateGuestBroadcastSeconds(1)

	waitForBroadcastedParticipant(t, mixer, "publisher-a", 3*time.Second)
}

func TestMediaMixerSwitchingToAStudioSourceCancelsTheGuestBroadcast(t *testing.T) {
	studio := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	otherStudio := &studioVideo{participantIdentity: "publisher-b", publicationSID: "TR_b"}
	guest := &studioVideo{
		participantIdentity: "guest-a",
		publicationSID:      "TR_guest",
		room:                "guests",
	}
	pipeline := &fakeMixerPipeline{source: studio}
	mixer := &mediaMixer{
		config:                config{GuestRoom: "guests"},
		running:               true,
		pipeline:              pipeline,
		selected:              studio,
		broadcasted:           studio,
		guestBroadcastSeconds: 3600,
		guestLookup: func(string) (*studioVideo, error) {
			return guest, nil
		},
	}
	mixer.videos.add(*studio)
	mixer.videos.add(*otherStudio)

	if err := mixer.switchSource("guest-a"); err != nil {
		t.Fatalf("switch to guest: %v", err)
	}
	if err := mixer.switchSource("publisher-b"); err != nil {
		t.Fatalf("switch to studio: %v", err)
	}

	if mixer.guestBroadcast != nil {
		t.Fatalf("guest broadcast = %+v, want nil after a studio switch", mixer.guestBroadcast)
	}
	if pipeline.source == nil || pipeline.source.participantIdentity != "publisher-b" {
		t.Fatalf("pipeline source = %+v, want publisher-b", pipeline.source)
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

func TestMediaMixerSwitchSourceAdjustsTheRunningPipeline(t *testing.T) {
	first := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	second := &studioVideo{participantIdentity: "publisher-b", publicationSID: "TR_b"}
	pipeline := &fakeMixerPipeline{}
	mixer := &mediaMixer{
		running:     true,
		pipeline:    pipeline,
		selected:    first,
		broadcasted: first,
	}
	mixer.videos.add(*first)
	mixer.videos.add(*second)

	if err := mixer.switchSource("publisher-b"); err != nil {
		t.Fatalf("switch source: %v", err)
	}

	if pipeline.stopCalls != 0 {
		t.Fatalf("pipeline stop calls = %d, want 0; switching must not recreate the pipeline", pipeline.stopCalls)
	}
	if pipeline.setCalls != 1 {
		t.Fatalf("pipeline set source calls = %d, want 1", pipeline.setCalls)
	}
	if pipeline.source == nil || pipeline.source.participantIdentity != "publisher-b" {
		t.Fatalf("pipeline source = %+v, want publisher-b", pipeline.source)
	}
	if mixer.pipeline != pipeline {
		t.Fatal("the running pipeline was replaced")
	}
	if mixer.broadcasted == nil || mixer.broadcasted.participantIdentity != "publisher-b" {
		t.Fatalf("broadcasted = %+v, want publisher-b", mixer.broadcasted)
	}
	if mixer.selected == nil || mixer.selected.participantIdentity != "publisher-b" {
		t.Fatalf("selected = %+v, want publisher-b", mixer.selected)
	}
}

func TestMediaMixerFallsBackWithoutReplacingThePipeline(t *testing.T) {
	first := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	second := &studioVideo{participantIdentity: "publisher-b", publicationSID: "TR_b"}
	pipeline := &fakeMixerPipeline{source: second}
	mixer := &mediaMixer{
		running:     true,
		pipeline:    pipeline,
		selected:    second,
		broadcasted: second,
	}
	mixer.videos.add(*first)

	if err := mixer.reconcile(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if pipeline.stopCalls != 0 {
		t.Fatalf("pipeline stop calls = %d, want 0", pipeline.stopCalls)
	}
	if pipeline.source == nil || pipeline.source.participantIdentity != "publisher-a" {
		t.Fatalf("pipeline source = %+v, want publisher-a", pipeline.source)
	}
	if mixer.broadcasted == nil || mixer.broadcasted.participantIdentity != "publisher-a" {
		t.Fatalf("broadcasted = %+v, want publisher-a", mixer.broadcasted)
	}
}

func TestMediaMixerClearsTheSourceWhenNoStudioVideoRemains(t *testing.T) {
	selected := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	pipeline := &fakeMixerPipeline{source: selected}
	mixer := &mediaMixer{
		running:     true,
		pipeline:    pipeline,
		selected:    selected,
		broadcasted: selected,
	}

	if err := mixer.reconcile(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if pipeline.stopCalls != 0 {
		t.Fatalf("pipeline stop calls = %d, want 0", pipeline.stopCalls)
	}
	if pipeline.clearCalls != 1 {
		t.Fatalf("pipeline clear source calls = %d, want 1", pipeline.clearCalls)
	}
	if pipeline.source != nil {
		t.Fatalf("pipeline source = %+v, want no source", pipeline.source)
	}
	if mixer.broadcasted != nil {
		t.Fatalf("broadcasted = %+v, want nil", mixer.broadcasted)
	}
}

func TestMediaMixerStartStartsThePipelineOnceAndAttachesTheSelectedSource(t *testing.T) {
	first := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	pipeline := &fakeMixerPipeline{}
	attempts := 0
	mixer := &mediaMixer{
		startPipeline: func(config config) (mediaMixerPipelineAPI, error) {
			attempts++
			return pipeline, nil
		},
	}
	mixer.videos.add(*first)

	if err := mixer.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("pipeline start attempts = %d, want 1", attempts)
	}
	if pipeline.setCalls != 1 {
		t.Fatalf("pipeline set source calls = %d, want 1", pipeline.setCalls)
	}
	if mixer.broadcasted == nil || mixer.broadcasted.participantIdentity != "publisher-a" {
		t.Fatalf("broadcasted = %+v, want publisher-a", mixer.broadcasted)
	}

	if err := mixer.start(); err != nil {
		t.Fatalf("start the running mixer: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("pipeline start attempts after a repeated start = %d, want 1", attempts)
	}
}

func TestMediaMixerStartRetriesAfterAFailedPipelineStart(t *testing.T) {
	first := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	pipeline := &fakeMixerPipeline{}
	attempts := 0
	mixer := &mediaMixer{
		startPipeline: func(config config) (mediaMixerPipelineAPI, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("pipeline start failed")
			}
			return pipeline, nil
		},
	}
	mixer.videos.add(*first)

	if err := mixer.start(); err == nil {
		t.Fatal("start succeeded with a failing pipeline")
	}
	if mixer.running {
		t.Fatal("mixer is marked running after a failed pipeline start")
	}

	if err := mixer.start(); err != nil {
		t.Fatalf("retry start: %v", err)
	}

	if attempts != 2 {
		t.Fatalf("pipeline start attempts = %d, want 2", attempts)
	}
	if mixer.pipeline != pipeline {
		t.Fatal("pipeline was not restored by the retry start")
	}
	if mixer.broadcasted == nil || mixer.broadcasted.participantIdentity != "publisher-a" {
		t.Fatalf("broadcasted = %+v, want publisher-a", mixer.broadcasted)
	}
}

func TestMediaMixerKeepsTheCurrentSourceAfterAFailedSwitch(t *testing.T) {
	first := &studioVideo{participantIdentity: "publisher-a", publicationSID: "TR_a"}
	second := &studioVideo{participantIdentity: "publisher-b", publicationSID: "TR_b"}
	pipeline := &fakeMixerPipeline{source: first, setErr: errors.New("source switch failed")}
	mixer := &mediaMixer{
		running:     true,
		pipeline:    pipeline,
		selected:    first,
		broadcasted: first,
	}
	mixer.videos.add(*first)
	mixer.videos.add(*second)

	if err := mixer.switchSource("publisher-b"); err == nil {
		t.Fatal("switch source succeeded with a failing pipeline")
	}
	if mixer.pipeline != pipeline {
		t.Fatal("the running pipeline was replaced after a failed source switch")
	}
	if pipeline.stopCalls != 0 {
		t.Fatalf("pipeline stop calls = %d, want 0", pipeline.stopCalls)
	}
	if pipeline.source == nil || pipeline.source.participantIdentity != "publisher-a" {
		t.Fatalf("pipeline source = %+v, want the current publisher-a source", pipeline.source)
	}
	if mixer.broadcasted == nil || mixer.broadcasted.participantIdentity != "publisher-a" {
		t.Fatalf("broadcasted = %+v, want the current publisher-a source", mixer.broadcasted)
	}
	if mixer.selected == nil || mixer.selected.participantIdentity != "publisher-b" {
		t.Fatalf("selected = %+v, want the desired publisher-b selection", mixer.selected)
	}
}

func waitForBroadcastedParticipant(t *testing.T, mixer *mediaMixer, want string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		mixer.mu.Lock()
		broadcasted := mixer.broadcasted
		mixer.mu.Unlock()
		if broadcasted != nil && broadcasted.participantIdentity == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("broadcasted = %+v, want %q after the guest timeout", broadcasted, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type fakeMixerPipeline struct {
	source     *studioVideo
	sourceName string
	setCalls   int
	clearCalls int
	stopCalls  int
	setErr     error
	failureErr error
}

func (p *fakeMixerPipeline) setSource(source *studioVideo, sourceName string) error {
	p.setCalls++
	if p.setErr != nil {
		return p.setErr
	}
	p.source = source
	p.sourceName = sourceName
	return nil
}

func (p *fakeMixerPipeline) clearSource() {
	p.clearCalls++
	p.source = nil
	p.sourceName = ""
}

func (p *fakeMixerPipeline) stop() error {
	p.stopCalls++
	p.source = nil
	p.sourceName = ""
	return nil
}

func (p *fakeMixerPipeline) failure() error {
	return p.failureErr
}
