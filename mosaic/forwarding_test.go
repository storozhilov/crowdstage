package main

import "testing"

type recordingUnpublisher struct {
	sids []string
}

func (r *recordingUnpublisher) UnpublishTrack(sid string) error {
	r.sids = append(r.sids, sid)
	return nil
}

func TestCloseForwardsUnpublishesForwardedTracks(t *testing.T) {
	unpublisher := &recordingUnpublisher{}
	cancelled := make(map[string]bool)
	composer := &composer{
		forwards: map[string]*forward{
			"first": {
				cancel: func() { cancelled["first"] = true },
				sid:    "TR_first",
			},
			"second": {
				cancel: func() { cancelled["second"] = true },
				sid:    "TR_second",
			},
		},
		studioUnpublisher: unpublisher,
	}

	composer.closeForwards()

	if len(composer.forwards) != 0 {
		t.Fatalf("forwards after close = %d, want 0", len(composer.forwards))
	}
	if !cancelled["first"] || !cancelled["second"] {
		t.Fatalf("cancelled forwards = %v, want both entries cancelled", cancelled)
	}

	unpublished := make(map[string]bool, len(unpublisher.sids))
	for _, sid := range unpublisher.sids {
		unpublished[sid] = true
	}
	if len(unpublished) != 2 || !unpublished["TR_first"] || !unpublished["TR_second"] {
		t.Fatalf("unpublished track SIDs = %v, want TR_first and TR_second", unpublisher.sids)
	}
}
