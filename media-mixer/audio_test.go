package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
)

func TestAudioSourceTrackerAddsNewSourcesUnmuted(t *testing.T) {
	tracker := &audioSourceTracker{}
	tracker.add(audioSource{
		participantIdentity: "audio-a",
		publicationSID:      "TR_a",
		publishedAt:         time.Unix(10, 0),
	})

	sources := tracker.list()
	if len(sources) != 1 {
		t.Fatalf("audio sources = %d, want 1", len(sources))
	}
	source := sources[0]
	if source.participantIdentity != "audio-a" {
		t.Fatalf("participant identity = %q, want %q", source.participantIdentity, "audio-a")
	}
	if source.muted {
		t.Fatal("a new audio source is muted")
	}
	if source.solo {
		t.Fatal("a new audio source is soloed")
	}
	if source.gain != defaultAudioGain {
		t.Fatalf("gain = %v, want %v", source.gain, defaultAudioGain)
	}
	if source.signalLevel != 0 {
		t.Fatalf("signal level = %v, want 0", source.signalLevel)
	}
}

func TestAudioSourceTrackerKeepsTheFirstSourcePerParticipant(t *testing.T) {
	tracker := &audioSourceTracker{}
	tracker.add(audioSource{
		participantIdentity: "audio-a",
		publicationSID:      "TR_a2",
		publishedAt:         time.Unix(20, 0),
	})
	tracker.add(audioSource{
		participantIdentity: "audio-a",
		publicationSID:      "TR_a1",
		publishedAt:         time.Unix(10, 0),
	})

	source := tracker.findByIdentity("audio-a")
	if source == nil {
		t.Fatal("audio source is nil for a tracked participant")
	}
	if source.publicationSID != "TR_a2" {
		t.Fatalf("publication = %q, want the first detected publication TR_a2", source.publicationSID)
	}
	if got := tracker.count(); got != 1 {
		t.Fatalf("audio sources = %d, want 1 per participant", got)
	}
}

func TestMediaMixerTracksAudioOnlyStudioParticipant(t *testing.T) {
	mixer := &mediaMixer{config: config{InputRoom: "studio"}}

	mixer.refreshStudioAudioSnapshot(studioAudioSnapshot{
		participantIdentity: "audio-a",
		participantName:     "Audio A",
		participantSID:      "PA_a",
		publications: []studioAudioPublication{
			{kind: lksdk.TrackKindAudio, sid: "TR_a"},
		},
	})

	source := mixer.audios.findByIdentity("audio-a")
	if source == nil {
		t.Fatal("audio-only studio participant was not tracked")
	}
	if source.publicationSID != "TR_a" || source.muted || source.solo || source.gain != defaultAudioGain {
		t.Fatalf("audio source = %+v, want the initial unmuted publication", source)
	}
}

func TestMediaMixerRemovesAudioWhenParticipantPublishesVideo(t *testing.T) {
	mixer := &mediaMixer{config: config{InputRoom: "studio"}}
	mixer.refreshStudioAudioSnapshot(studioAudioSnapshot{
		participantIdentity: "participant-a",
		publications: []studioAudioPublication{
			{kind: lksdk.TrackKindAudio, sid: "TR_a"},
		},
	})
	if mixer.audios.count() != 1 {
		t.Fatal("audio source was not tracked before the video publication")
	}

	mixer.refreshStudioAudioSnapshot(studioAudioSnapshot{
		participantIdentity: "participant-a",
		publications: []studioAudioPublication{
			{kind: lksdk.TrackKindAudio, sid: "TR_a"},
			{kind: lksdk.TrackKindVideo, sid: "TR_v"},
		},
	})

	if source := mixer.audios.findByIdentity("participant-a"); source != nil {
		t.Fatalf("audio source = %+v, want it removed while video is published", source)
	}
}

func TestMediaMixerRestoresAudioWhenVideoIsUnpublished(t *testing.T) {
	mixer := &mediaMixer{config: config{InputRoom: "studio"}}
	mixer.refreshStudioAudioSnapshot(studioAudioSnapshot{
		participantIdentity: "participant-a",
		publications: []studioAudioPublication{
			{kind: lksdk.TrackKindAudio, sid: "TR_a"},
			{kind: lksdk.TrackKindVideo, sid: "TR_v"},
		},
	})
	if mixer.audios.count() != 0 {
		t.Fatal("audio source was tracked while video was published")
	}

	mixer.refreshStudioAudioSnapshot(studioAudioSnapshot{
		participantIdentity: "participant-a",
		publications: []studioAudioPublication{
			{kind: lksdk.TrackKindAudio, sid: "TR_a"},
		},
	})

	source := mixer.audios.findByIdentity("participant-a")
	if source == nil {
		t.Fatal("audio source was not restored after the video was unpublished")
	}
	if source.publicationSID != "TR_a" {
		t.Fatalf("publication = %q, want TR_a", source.publicationSID)
	}
}

func TestMediaMixerReplacesAudioPublicationAndPreservesControls(t *testing.T) {
	mixer := &mediaMixer{config: config{InputRoom: "studio"}}
	mixer.refreshStudioAudioSnapshot(studioAudioSnapshot{
		participantIdentity: "audio-a",
		publications: []studioAudioPublication{
			{kind: lksdk.TrackKindAudio, sid: "TR_a1"},
		},
	})
	mixer.audios.update("audio-a", func(source *audioSource) {
		source.muted = true
		source.solo = true
		source.gain = 0.25
	})

	mixer.refreshStudioAudioSnapshot(studioAudioSnapshot{
		participantIdentity: "audio-a",
		publications: []studioAudioPublication{
			{kind: lksdk.TrackKindAudio, sid: "TR_a2"},
		},
	})

	source := mixer.audios.findByIdentity("audio-a")
	if source == nil {
		t.Fatal("audio source is nil after the publication replacement")
	}
	if source.publicationSID != "TR_a2" {
		t.Fatalf("publication = %q, want TR_a2", source.publicationSID)
	}
	if !source.muted || !source.solo || source.gain != 0.25 {
		t.Fatalf("controls = %+v, want them preserved across the publication replacement", source)
	}
	if got := mixer.audios.count(); got != 1 {
		t.Fatalf("audio sources = %d, want 1", got)
	}
}

func TestAudioSourceTrackerRemovesUnpublishedAndDisconnectedSources(t *testing.T) {
	tracker := &audioSourceTracker{}
	tracker.add(audioSource{participantIdentity: "audio-a", publicationSID: "TR_a"})
	tracker.add(audioSource{participantIdentity: "audio-b", publicationSID: "TR_b"})

	tracker.removePublication("TR_a")
	if got := tracker.count(); got != 1 {
		t.Fatalf("audio sources after unpublish = %d, want 1", got)
	}
	if source := tracker.findByIdentity("audio-a"); source != nil {
		t.Fatalf("audio-a = %+v, want nil after unpublish", source)
	}

	tracker.removeParticipant("audio-b")
	if got := tracker.count(); got != 0 {
		t.Fatalf("audio sources after disconnect = %d, want 0", got)
	}
}

func TestAudioSourceTrackerUpdatesControls(t *testing.T) {
	tracker := &audioSourceTracker{}
	tracker.add(audioSource{participantIdentity: "audio-a", publicationSID: "TR_a"})

	if !tracker.update("audio-a", func(source *audioSource) {
		source.muted = true
		source.solo = true
		source.gain = 0.25
	}) {
		t.Fatal("update reported a missing participant")
	}

	source := tracker.findByIdentity("audio-a")
	if source == nil {
		t.Fatal("audio source is nil after the update")
	}
	if !source.muted || !source.solo || source.gain != 0.25 {
		t.Fatalf("source controls = %+v, want muted, soloed, gain 0.25", source)
	}
	if tracker.update("audio-missing", func(*audioSource) {}) {
		t.Fatal("update reported success for a missing participant")
	}
}

func TestAudioSourceTrackerStoresSignalLevels(t *testing.T) {
	tracker := &audioSourceTracker{}
	tracker.add(audioSource{participantIdentity: "audio-a", publicationSID: "TR_a"})

	tracker.setSignalLevel("TR_a", 0.75)

	if source := tracker.findByIdentity("audio-a"); source == nil || source.signalLevel != 0.75 {
		t.Fatalf("audio-a signal level = %+v, want 0.75", source)
	}

	tracker.setSignalLevel("TR_missing", 0.5)
	if source := tracker.findByIdentity("audio-a"); source == nil || source.signalLevel != 0.75 {
		t.Fatalf("audio-a signal level = %+v, want the missing update to be ignored", source)
	}
}

func TestAudioRMSReportsNormalizedSignalLevel(t *testing.T) {
	if level := audioRMS(nil); level != 0 {
		t.Fatalf("empty RMS = %v, want 0", level)
	}

	const amplitude = 0.25
	samples := []float32{amplitude, -amplitude, amplitude, -amplitude}
	want := amplitude
	if level := audioRMS(samples); math.Abs(level-want) > 1e-6 {
		t.Fatalf("RMS = %v, want %v", level, want)
	}

	if level := audioRMS([]float32{2, 2}); level != 1 {
		t.Fatalf("clipped RMS = %v, want 1", level)
	}
}

func TestEffectiveAudioControlsAppliesSolo(t *testing.T) {
	first := audioSource{participantIdentity: "audio-a", muted: false, solo: false, gain: 0.5}
	second := audioSource{participantIdentity: "audio-b", muted: true, solo: true, gain: 0.25}

	withoutSolo := effectiveAudioControls(first, false)
	if withoutSolo.muted || withoutSolo.gain != 0.5 {
		t.Fatalf("controls without solo = %+v, want unmuted at gain 0.5", withoutSolo)
	}

	withSolo := effectiveAudioControls(first, true)
	if !withSolo.muted {
		t.Fatalf("controls while another source is soloed = %+v, want muted", withSolo)
	}

	soloed := effectiveAudioControls(second, true)
	if soloed.muted || soloed.gain != 0.25 {
		t.Fatalf("soloed controls = %+v, want unmuted at gain 0.25", soloed)
	}
}

func TestAudioMuteHandlerUpdatesTheSource(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.audios.add(audioSource{participantIdentity: "audio-a", publicationSID: "TR_a"})

	request := httptest.NewRequest(
		http.MethodPost,
		"/audio/audio-a/mute",
		strings.NewReader(`{"muted":true}`),
	)
	request.SetPathValue("identity", "audio-a")
	response := httptest.NewRecorder()

	audioMuteHandler(mixer).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	source := mixer.audios.findByIdentity("audio-a")
	if source == nil || !source.muted {
		t.Fatalf("audio source = %+v, want muted", source)
	}
}

func TestAudioSoloHandlerUpdatesTheSource(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.audios.add(audioSource{participantIdentity: "audio-a", publicationSID: "TR_a"})

	request := httptest.NewRequest(
		http.MethodPost,
		"/audio/audio-a/solo",
		strings.NewReader(`{"solo":true}`),
	)
	request.SetPathValue("identity", "audio-a")
	response := httptest.NewRecorder()

	audioSoloHandler(mixer).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	source := mixer.audios.findByIdentity("audio-a")
	if source == nil || !source.solo {
		t.Fatalf("audio source = %+v, want soloed", source)
	}
}

func TestAudioGainHandlerUpdatesTheSource(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.audios.add(audioSource{participantIdentity: "audio-a", publicationSID: "TR_a"})

	request := httptest.NewRequest(
		http.MethodPost,
		"/audio/audio-a/gain",
		strings.NewReader(`{"gain":0.4}`),
	)
	request.SetPathValue("identity", "audio-a")
	response := httptest.NewRecorder()

	audioGainHandler(mixer).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	source := mixer.audios.findByIdentity("audio-a")
	if source == nil || source.gain != 0.4 {
		t.Fatalf("audio source = %+v, want gain 0.4", source)
	}
}

func TestAudioGainHandlerNormalizesNegativeZero(t *testing.T) {
	mixer := &mediaMixer{}
	mixer.audios.add(audioSource{participantIdentity: "audio-a", publicationSID: "TR_a"})

	request := httptest.NewRequest(
		http.MethodPost,
		"/audio/audio-a/gain",
		strings.NewReader(`{"gain":-0.0}`),
	)
	request.SetPathValue("identity", "audio-a")
	response := httptest.NewRecorder()

	audioGainHandler(mixer).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	source := mixer.audios.findByIdentity("audio-a")
	if source == nil || source.gain != 0 || math.Signbit(source.gain) {
		t.Fatalf("audio source = %+v, want a positive zero gain", source)
	}
}

func TestAudioHandlersRejectUnknownSourcesAndInvalidValues(t *testing.T) {
	cases := []struct {
		name     string
		handler  func(*mediaMixer) http.HandlerFunc
		path     string
		identity string
		body     string
		status   int
	}{
		{
			name:     "missing mute source",
			handler:  audioMuteHandler,
			path:     "/audio/audio-missing/mute",
			identity: "audio-missing",
			body:     `{"muted":true}`,
			status:   http.StatusNotFound,
		},
		{
			name:     "invalid mute body",
			handler:  audioMuteHandler,
			path:     "/audio/audio-a/mute",
			identity: "audio-a",
			body:     `not-json`,
			status:   http.StatusBadRequest,
		},
		{
			name:     "negative gain",
			handler:  audioGainHandler,
			path:     "/audio/audio-a/gain",
			identity: "audio-a",
			body:     `{"gain":-1}`,
			status:   http.StatusBadRequest,
		},
		{
			name:     "excessive gain",
			handler:  audioGainHandler,
			path:     "/audio/audio-a/gain",
			identity: "audio-a",
			body:     `{"gain":11}`,
			status:   http.StatusBadRequest,
		},
		{
			name:     "invalid solo body",
			handler:  audioSoloHandler,
			path:     "/audio/audio-a/solo",
			identity: "audio-a",
			body:     `{"solo":"yes"}`,
			status:   http.StatusBadRequest,
		},
		{
			name:     "trailing JSON value",
			handler:  audioGainHandler,
			path:     "/audio/audio-a/gain",
			identity: "audio-a",
			body:     `{"gain":1} {"gain":10}`,
			status:   http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mixer := &mediaMixer{}
			mixer.audios.add(audioSource{participantIdentity: "audio-a", publicationSID: "TR_a"})

			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			request.SetPathValue("identity", tc.identity)
			response := httptest.NewRecorder()

			tc.handler(mixer).ServeHTTP(response, request)

			if response.Code != tc.status {
				t.Fatalf("status code = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}

			var body map[string]any
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if body["status"] != "error" {
				t.Fatalf("status = %v, want error", body["status"])
			}
		})
	}
}
