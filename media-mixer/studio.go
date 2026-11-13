package main

import (
	"sync"
	"time"
)

type studioVideo struct {
	participantIdentity string
	participantName     string
	participantSID      string
	publicationSID      string
	publishedAt         time.Time
}

type studioVideoTracker struct {
	mu     sync.Mutex
	videos []studioVideo
}

func (t *studioVideoTracker) add(video studioVideo) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, existing := range t.videos {
		if existing.publicationSID == video.publicationSID {
			return
		}
	}
	t.videos = append(t.videos, video)
}

func (t *studioVideoTracker) removePublication(publicationSID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for index, video := range t.videos {
		if video.publicationSID == publicationSID {
			t.videos = append(t.videos[:index], t.videos[index+1:]...)
			return
		}
	}
}

func (t *studioVideoTracker) removeParticipant(identity string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	videos := t.videos[:0]
	for _, video := range t.videos {
		if video.participantIdentity != identity {
			videos = append(videos, video)
		}
	}
	t.videos = videos
}

func (t *studioVideoTracker) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.videos)
}

func (t *studioVideoTracker) oldest() *studioVideo {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.videos) == 0 {
		return nil
	}
	oldest := t.videos[0]
	for _, video := range t.videos[1:] {
		if video.publishedAt.Before(oldest.publishedAt) {
			oldest = video
		}
	}
	return &oldest
}
