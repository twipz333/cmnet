package cache

import (
	"path/filepath"
	"testing"
	"time"
)

// rankFixture builds a cache holding one match per interesting state: rank known,
// rank missing, rank missing on a pro match, and rank missing but already asked
// about.
func rankFixture(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	day := time.Now().Add(-24 * time.Hour).Unix()
	tier := 62
	draft := []PickBan{
		{Ord: 0, HeroID: 1, IsPick: false, Team: 0},
		{Ord: 1, HeroID: 2, IsPick: true, Team: 0},
		{Ord: 2, HeroID: 3, IsPick: true, Team: 1},
	}
	for _, m := range []Match{
		{ID: 10, StartTime: day, Source: SourceBulk, RankTier: &tier, PickBans: draft},
		{ID: 11, StartTime: day - 1, Source: SourceBulk, PickBans: draft},
		{ID: 12, StartTime: day - 2, Source: SourcePublicCM, PickBans: draft},
		{ID: 13, StartTime: day - 3, Source: SourcePro, PickBans: draft},
	} {
		if err := store.UpsertMatch(m); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// matchByID reads one cached match back, drafts included.
func matchByID(t *testing.T, store *Store, id int64) Match {
	t.Helper()
	all, err := store.Matches(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("match %d is not in the cache", id)
	return Match{}
}

// The backlog is the matches that can have a rank and do not yet, and nothing
// else. A pro game has no average rank tier at all, so asking about it would spend
// a request to learn nothing.
func TestMatchesMissingRankSkipsWhatCannotBeFilled(t *testing.T) {
	store := rankFixture(t)

	ids, err := store.MatchesMissingRank(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("backlog = %v, want the two rankable matches without a tier (11, 12)", ids)
	}
	// Newest first: an interrupted pass should have improved the part the training
	// window weights most.
	if ids[0] != 11 || ids[1] != 12 {
		t.Errorf("backlog = %v, want [11 12] newest first", ids)
	}
}

// Asking is what retires a match from the backlog, not finding. Otherwise the
// matches public_matches has no row for are re-queried on every pass forever and
// the real backlog behind them never drains.
func TestSetRankTiersRetiresEveryIdItWasAskedAbout(t *testing.T) {
	store := rankFixture(t)

	// Answered for one of the two, nothing for the other.
	if err := store.SetRankTiers(map[int64]int{11: 45}, []int64{11, 12}); err != nil {
		t.Fatal(err)
	}

	ids, err := store.MatchesMissingRank(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("backlog = %v after both ids were asked about, want empty", ids)
	}

	m := matchByID(t, store, 11)
	if m.RankTier == nil || *m.RankTier != 45 {
		t.Errorf("match 11 rank = %v, want 45", m.RankTier)
	}
	m = matchByID(t, store, 12)
	if m.RankTier != nil {
		t.Errorf("match 12 rank = %v, want it left unknown — nothing was found for it", *m.RankTier)
	}
}

// The whole reason this is a narrow UPDATE and not a Match upsert: a rank-only
// write must not touch the draft, which is the part that made the match worth
// caching.
func TestSetRankTiersLeavesTheDraftAlone(t *testing.T) {
	store := rankFixture(t)

	before := matchByID(t, store, 11)
	if len(before.PickBans) == 0 {
		t.Fatal("fixture match has no draft, so this test would pass vacuously")
	}

	if err := store.SetRankTiers(map[int64]int{11: 45}, []int64{11}); err != nil {
		t.Fatal(err)
	}

	after := matchByID(t, store, 11)
	if len(after.PickBans) != len(before.PickBans) {
		t.Fatalf("draft went from %d rows to %d — the rank write erased it",
			len(before.PickBans), len(after.PickBans))
	}
	for i, pb := range after.PickBans {
		if pb != before.PickBans[i] {
			t.Errorf("pick/ban %d = %+v, was %+v", i, pb, before.PickBans[i])
		}
	}
}

// A tier already recorded is the better one: it came from the source that
// collected the match. A later pass must not overwrite it with whatever the
// lookup table says today.
func TestSetRankTiersDoesNotOverwriteAKnownTier(t *testing.T) {
	store := rankFixture(t)

	if err := store.SetRankTiers(map[int64]int{10: 11}, []int64{10}); err != nil {
		t.Fatal(err)
	}
	m := matchByID(t, store, 10)
	if m.RankTier == nil || *m.RankTier != 62 {
		t.Errorf("rank = %v, want the original 62 kept", m.RankTier)
	}
}

// Coverage counts what could carry a tier, so the pro matches that never can do
// not drag the percentage down forever.
func TestRankCoverageExcludesProMatches(t *testing.T) {
	store := rankFixture(t)

	withRank, total, err := store.RankCoverage()
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("rankable total = %d, want 3 (the pro match is not rankable)", total)
	}
	if withRank != 1 {
		t.Errorf("with rank = %d, want 1", withRank)
	}
}
