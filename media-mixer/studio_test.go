package main

import (
	"testing"
	"time"
)

func TestStudioVideoTrackerCountsPublishedVideosAndSelectsOldest(t *testing.T) {
	tracker := &studioVideoTracker{}

	tracker.add(studioVideo{
		participantIdentity: "publisher-b",
		publicationSID:      "TR_b",
		publishedAt:         time.Unix(20, 0),
	})
	tracker.add(studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a",
		publishedAt:         time.Unix(10, 0),
	})

	if got := tracker.count(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	oldest := tracker.oldest()
	if oldest == nil {
		t.Fatal("oldest video is nil with two published videos")
	}
	if oldest.participantIdentity != "publisher-a" {
		t.Fatalf("oldest participant = %q, want %q", oldest.participantIdentity, "publisher-a")
	}
	if oldest.publicationSID != "TR_a" {
		t.Fatalf("oldest publication = %q, want %q", oldest.publicationSID, "TR_a")
	}
}

func TestStudioVideoTrackerRemovesUnpublishedAndDisconnectedVideos(t *testing.T) {
	tracker := &studioVideoTracker{}
	tracker.add(studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a",
		publishedAt:         time.Unix(10, 0),
	})
	tracker.add(studioVideo{
		participantIdentity: "publisher-b",
		publicationSID:      "TR_b",
		publishedAt:         time.Unix(20, 0),
	})
	tracker.add(studioVideo{
		participantIdentity: "publisher-b",
		publicationSID:      "TR_b2",
		publishedAt:         time.Unix(30, 0),
	})

	tracker.removePublication("TR_a")
	if got := tracker.count(); got != 2 {
		t.Fatalf("count after unpublish = %d, want 2", got)
	}
	if oldest := tracker.oldest(); oldest == nil || oldest.participantIdentity != "publisher-b" {
		t.Fatalf("oldest after unpublish = %+v, want publisher-b", oldest)
	}

	tracker.removeParticipant("publisher-b")
	if got := tracker.count(); got != 0 {
		t.Fatalf("count after participant disconnect = %d, want 0", got)
	}
	if oldest := tracker.oldest(); oldest != nil {
		t.Fatalf("oldest after participant disconnect = %+v, want nil", oldest)
	}
}

func TestStudioVideoTrackerIgnoresDuplicatePublications(t *testing.T) {
	tracker := &studioVideoTracker{}
	video := studioVideo{
		participantIdentity: "publisher-a",
		publicationSID:      "TR_a",
		publishedAt:         time.Unix(10, 0),
	}

	tracker.add(video)
	tracker.add(video)

	if got := tracker.count(); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
}
