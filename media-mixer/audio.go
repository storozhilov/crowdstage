package main

import (
	"math"
	"sort"
	"sync"
	"time"
)

const (
	defaultAudioGain = 1.0
	maxAudioGain     = 10.0
)

type audioSource struct {
	participantIdentity string
	participantName     string
	participantSID      string
	publicationSID      string
	publishedAt         time.Time
	room                string
	muted               bool
	solo                bool
	gain                float64
	signalLevel         float64
}

type audioControls struct {
	muted bool
	gain  float64
}

type audioSourceTracker struct {
	mu      sync.Mutex
	sources []audioSource
}

func (t *audioSourceTracker) add(source audioSource) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, existing := range t.sources {
		if existing.publicationSID == source.publicationSID {
			return
		}
		if existing.participantIdentity == source.participantIdentity {
			return
		}
	}

	if source.gain == 0 {
		source.gain = defaultAudioGain
	}
	t.sources = append(t.sources, source)
}

func (t *audioSourceTracker) replace(participantIdentity string, source audioSource) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for index := range t.sources {
		existing := &t.sources[index]
		if existing.participantIdentity != participantIdentity {
			continue
		}
		// Keep the operator controls atomically when a participant republishes
		// its audio track.
		source.muted = existing.muted
		source.solo = existing.solo
		source.gain = existing.gain
		t.sources[index] = source
		return true
	}
	return false
}

func (t *audioSourceTracker) removePublication(publicationSID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for index, source := range t.sources {
		if source.publicationSID == publicationSID {
			t.sources = append(t.sources[:index], t.sources[index+1:]...)
			return
		}
	}
}

func (t *audioSourceTracker) removeParticipant(participantIdentity string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sources := t.sources[:0]
	for _, source := range t.sources {
		if source.participantIdentity != participantIdentity {
			sources = append(sources, source)
		}
	}
	t.sources = sources
}

func (t *audioSourceTracker) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sources)
}

func (t *audioSourceTracker) list() []audioSource {
	t.mu.Lock()
	defer t.mu.Unlock()

	sources := make([]audioSource, len(t.sources))
	copy(sources, t.sources)
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].participantIdentity < sources[right].participantIdentity
	})
	return sources
}

func (t *audioSourceTracker) findByIdentity(participantIdentity string) *audioSource {
	t.mu.Lock()
	defer t.mu.Unlock()

	for index := range t.sources {
		if t.sources[index].participantIdentity == participantIdentity {
			source := t.sources[index]
			return &source
		}
	}
	return nil
}

func (t *audioSourceTracker) update(participantIdentity string, update func(*audioSource)) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for index := range t.sources {
		if t.sources[index].participantIdentity == participantIdentity {
			update(&t.sources[index])
			return true
		}
	}
	return false
}

func (t *audioSourceTracker) setSignalLevel(publicationSID string, level float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for index := range t.sources {
		if t.sources[index].publicationSID == publicationSID {
			t.sources[index].signalLevel = level
			return
		}
	}
}

func audioRMS(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}

	var sum float64
	for _, sample := range samples {
		value := float64(sample)
		sum += value * value
	}
	level := math.Sqrt(sum / float64(len(samples)))
	if level > 1 {
		return 1
	}
	return level
}

func effectiveAudioControls(source audioSource, anySolo bool) audioControls {
	muted := source.muted || anySolo
	if source.solo {
		muted = false
	}
	return audioControls{
		muted: muted,
		gain:  source.gain,
	}
}

func (m *mediaMixer) setAudioSignalLevel(publicationSID string, level float64) {
	m.audios.setSignalLevel(publicationSID, level)
}

func audioSourcesHealth(sources []audioSource) []AudioSourceHealth {
	health := make([]AudioSourceHealth, 0, len(sources))
	for _, source := range sources {
		health = append(health, AudioSourceHealth{
			ParticipantIdentity: source.participantIdentity,
			Muted:               source.muted,
			Solo:                source.solo,
			Gain:                source.gain,
			SignalLevel:         source.signalLevel,
		})
	}
	return health
}
