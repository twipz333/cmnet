package session

import (
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/draft"
	"cmnet/internal/model"
	"cmnet/internal/recommender"
	"cmnet/internal/train"
)

// testRecommender builds a real recommender over a tiny synthetic dataset, so
// ApplyGSI can be exercised end to end without a bundle file.
func testRecommender(t *testing.T) *recommender.Recommender {
	t.Helper()
	pb := func(ord, hero, team int, pick bool) cache.PickBan {
		return cache.PickBan{Ord: ord, HeroID: hero, Team: team, IsPick: pick}
	}
	matches := []cache.Match{
		{ID: 1, StartTime: 100, RadiantWin: true, PickBans: []cache.PickBan{
			pb(0, 1, 0, false), pb(1, 2, 1, false),
			pb(2, 3, 0, true), pb(3, 4, 1, true), pb(4, 5, 0, true), pb(5, 6, 1, true),
		}},
		{ID: 2, StartTime: 200, RadiantWin: false, PickBans: []cache.PickBan{
			pb(0, 3, 0, false), pb(1, 4, 1, false),
			pb(2, 1, 0, true), pb(3, 6, 1, true), pb(4, 2, 0, true), pb(5, 5, 1, true),
		}},
	}
	b := train.Bundle(matches, []int{1, 2, 3, 4, 5, 6}, train.Options{
		WindowDays: 31, PatchTag: "test", MMRTier: "test", CreatedAt: "2026-01-01T00:00:00Z",
	})
	hm := b.HeroMap()
	return recommender.New(
		&model.WinProb{Model: b.WinProb, HeroMap: hm},
		&model.Predictor{Pick: b.PickPredictor, Ban: b.BanPredictor, HeroMap: hm},
	)
}

// Two snapshots that differ only in the live clock must share a signature: that is
// what lets ApplyGSI skip the recommender while a match is in progress and Dota is
// posting ten events a second.
func TestDraftSignatureIgnoresClock(t *testing.T) {
	base := func() *draft.Snapshot {
		return &draft.Snapshot{
			RadiantPicks:  []int{1, 2},
			DirePicks:     []int{3},
			RadiantBans:   []int{4},
			DireBans:      []int{5},
			CurrentTeam:   draft.Dire,
			CurrentIsPick: true,
		}
	}
	a, b := base(), base()
	a.Live = &draft.Live{ClockTime: 100}
	b.Live = &draft.Live{ClockTime: 101}

	if draftSignature(a) != draftSignature(b) {
		t.Error("clock-only change altered the draft signature; the recommender would rerun for nothing")
	}
}

func TestDraftSignatureDetectsDraftChanges(t *testing.T) {
	base := draft.Snapshot{
		RadiantPicks:  []int{1, 2},
		DirePicks:     []int{3},
		RadiantBans:   []int{4},
		DireBans:      []int{5},
		CurrentTeam:   draft.Dire,
		CurrentIsPick: true,
	}
	sig := draftSignature(&base)

	tests := []struct {
		name   string
		change func(*draft.Snapshot)
	}{
		{"new radiant pick", func(s *draft.Snapshot) { s.RadiantPicks = []int{1, 2, 9} }},
		{"new dire pick", func(s *draft.Snapshot) { s.DirePicks = []int{3, 9} }},
		{"new radiant ban", func(s *draft.Snapshot) { s.RadiantBans = []int{4, 9} }},
		{"new dire ban", func(s *draft.Snapshot) { s.DireBans = []int{5, 9} }},
		{"turn passes", func(s *draft.Snapshot) { s.CurrentTeam = draft.Radiant }},
		{"pick becomes ban", func(s *draft.Snapshot) { s.CurrentIsPick = false }},
		{"draft finishes", func(s *draft.Snapshot) { s.Done = true }},
		{"first pick differs", func(s *draft.Snapshot) { s.FirstPick = draft.Dire }},
		{"different hero, same count", func(s *draft.Snapshot) { s.RadiantPicks = []int{1, 7} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := base
			tt.change(&changed)
			if draftSignature(&changed) == sig {
				t.Error("draft change did not alter the signature; the board would go stale")
			}
		})
	}
}

// Skipping the recompute must not skip the live state: the clock, and the
// objective alerts derived from it, still have to advance on every event.
func TestApplyGSIKeepsClockLiveWhenDraftIsUnchanged(t *testing.T) {
	sess := New(Config{
		GSIEvents: make(chan *draft.Snapshot),
		FirstPick: draft.Radiant,
		Rec:       testRecommender(t),
	})

	snap := func(clock int) *draft.Snapshot {
		return &draft.Snapshot{
			RadiantPicks: []int{1},
			CurrentTeam:  draft.Radiant,
			Live:         &draft.Live{ClockTime: clock},
		}
	}

	// First event: new draft, so this one does recompute and records the signature.
	sess.ApplyGSI(snap(239))
	first := sess.Result()

	// Every event after this differs only in the clock, i.e. the skip path.
	sess.ApplyGSI(snap(240))
	if live := sess.Live(); live == nil || live.ClockTime != 240 {
		t.Fatalf("clock = %v, want 240", live)
	}
	sess.ApplyGSI(snap(241))
	if live := sess.Live(); live == nil || live.ClockTime != 241 {
		t.Fatalf("clock = %v, want 241 (live state must advance even without a recompute)", live)
	}
	if !sess.GSISeen() {
		t.Error("GSISeen should be true after events arrived")
	}
	// The skipped recomputes must leave the advice exactly as it was, not empty it.
	if got := sess.Result(); len(got.Recs) != len(first.Recs) {
		t.Errorf("recommendations changed across clock-only events: %d then %d",
			len(first.Recs), len(got.Recs))
	}
}
