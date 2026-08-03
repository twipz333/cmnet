package cache

import (
	"path/filepath"
	"testing"
	"time"
)

// corpusFixture builds a cache with a known shape: two CM drafts (bans present),
// one picks-only bulk match, and one match nobody recorded a rank for.
func corpusFixture(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	if err := store.UpsertHeroes([]Hero{
		{ID: 1, Name: "antimage", Localized: "Anti-Mage"},
		{ID: 2, Name: "axe", Localized: "Axe"},
	}); err != nil {
		t.Fatal(err)
	}

	day := time.Now().Add(-24 * time.Hour).Unix()
	tier60, tier70 := 62, 71

	// Two Captains Mode drafts: several picks AND a ban each. The ban is what makes
	// them CM as far as this cache is concerned.
	for i, m := range []Match{
		{ID: 100, StartTime: day, RadiantWin: true, Source: SourcePublicCM, GameMode: 2, RankTier: &tier60},
		{ID: 101, StartTime: day, RadiantWin: false, Source: SourcePublicCM, GameMode: 2, RankTier: &tier70},
	} {
		m.PickBans = []PickBan{
			{Ord: 0, HeroID: 1, IsPick: false, Team: 0},
			{Ord: 1, HeroID: 2, IsPick: true, Team: 0},
			{Ord: 2, HeroID: 1, IsPick: true, Team: 1},
		}
		if err := store.UpsertMatch(m); err != nil {
			t.Fatalf("upsert cm %d: %v", i, err)
		}
	}

	// One picks-only bulk match: no bans, so it must never be counted as a draft.
	if err := store.UpsertMatch(Match{
		ID: 200, StartTime: day, RadiantWin: true, Source: SourceBulk, RankTier: &tier60,
		PickBans: []PickBan{
			{Ord: 0, HeroID: 1, IsPick: true, Team: 0},
			{Ord: 1, HeroID: 2, IsPick: true, Team: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// And one with no rank recorded at all.
	if err := store.UpsertMatch(Match{
		ID: 201, StartTime: day, RadiantWin: false, Source: SourceBulk,
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

// The headline numbers must describe MATCHES. The first version of this screen
// counted a join against pick_bans, which multiplies every match by its draft
// actions — three sources added up to more than twice the size of the table.
func TestCorpusStatsCountsMatchesNotDraftActions(t *testing.T) {
	store := corpusFixture(t)

	st, err := store.CorpusStats(31)
	if err != nil {
		t.Fatalf("corpus stats: %v", err)
	}
	if st.Matches != 4 {
		t.Errorf("matches = %d, want 4", st.Matches)
	}
	if st.CMDrafts != 2 {
		t.Errorf("cm drafts = %d, want 2 (only the ones carrying a ban)", st.CMDrafts)
	}

	var sum int64
	bySource := map[string]SourceCount{}
	for _, s := range st.Sources {
		sum += s.Matches
		bySource[s.Source] = s
	}
	if sum != st.Matches {
		t.Errorf("sources sum to %d matches, but the cache holds %d — the join is multiplying rows", sum, st.Matches)
	}
	if got := bySource[SourceBulk]; got.CMDrafts != 0 {
		t.Errorf("bulk reported %d CM drafts; picks-only matches have no ban phase", got.CMDrafts)
	}
	if got := bySource[SourcePublicCM]; got.Matches != 2 || got.CMDrafts != 2 {
		t.Errorf("public-cm = %+v, want 2 matches / 2 drafts", got)
	}
}

// Daily intake is a per-day count of matches, and the CM series is a subset of it —
// the chart stacks them, so a CM count above the day's total would draw a bar
// taller than the day it describes.
func TestCorpusDailyIsPerMatchAndCMIsASubset(t *testing.T) {
	store := corpusFixture(t)

	st, err := store.CorpusStats(31)
	if err != nil {
		t.Fatal(err)
	}
	var total, cm int64
	for _, d := range st.Daily {
		if d.CMDrafts > d.Matches {
			t.Errorf("day %d: %d CM drafts out of %d matches — the subset is bigger than the set",
				d.Day, d.CMDrafts, d.Matches)
		}
		total += d.Matches
		cm += d.CMDrafts
	}
	if total != 4 || cm != 2 {
		t.Errorf("daily totals = %d matches / %d drafts, want 4 / 2", total, cm)
	}
}

// Days with no intake must be present as zeroes: a gap is what tells the user the
// collector was not running, and a chart that closes over missing days hides it.
func TestCorpusDailyKeepsEmptyDays(t *testing.T) {
	store := corpusFixture(t)

	st, err := store.CorpusStats(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Daily) != 10 {
		t.Fatalf("daily buckets = %d, want 10 (one per day in the window)", len(st.Daily))
	}
	var empty int
	for _, d := range st.Daily {
		if d.Matches == 0 {
			empty++
		}
	}
	if empty == 0 {
		t.Error("no empty days reported, but the fixture only has intake on one day")
	}
}

// "When it was played" and "when we downloaded it" are different questions, and
// the screen draws them as two charts. A sync pass pulls games spread over weeks,
// so reporting one as the other would tell the user the collector was idle on days
// it was working (or the reverse).
func TestCorpusSeparatesPlayedFromFetched(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// A game played a week ago, downloaded today — the ordinary case for a backfill.
	played := time.Now().Add(-7 * 24 * time.Hour).Unix()
	if err := store.UpsertMatch(Match{
		ID: 1, StartTime: played, RadiantWin: true, Source: SourceBulk,
	}); err != nil {
		t.Fatal(err)
	}

	st, err := store.CorpusStats(31)
	if err != nil {
		t.Fatal(err)
	}
	lastDay := func(d []DayCount) DayCount { return d[len(d)-1] }
	if len(st.Daily) == 0 || len(st.Fetched) == 0 {
		t.Fatal("both histograms should be populated")
	}
	// The match sits on the last day of the fetched chart (cached just now) and on
	// an earlier day of the played chart.
	if lastDay(st.Fetched).Matches != 1 {
		t.Errorf("fetched histogram: last day = %d matches, want 1 (it was cached now)",
			lastDay(st.Fetched).Matches)
	}
	if lastDay(st.Daily).Matches != 0 {
		t.Errorf("played histogram: last day = %d matches, want 0 (the game is a week old)",
			lastDay(st.Daily).Matches)
	}
}

// Matches whose rank was never recorded are kept in their own bucket rather than
// dropped — they are most of a cache filled before the provenance columns existed.
func TestCorpusRankBucketsIncludeUnknown(t *testing.T) {
	store := corpusFixture(t)

	st, err := store.CorpusStats(31)
	if err != nil {
		t.Fatal(err)
	}
	var sum, unknown int64
	for _, b := range st.Ranks {
		sum += b.Matches
		if b.Tier == unknownRank {
			unknown = b.Matches
		}
	}
	if sum != st.Matches {
		t.Errorf("rank buckets sum to %d, want %d", sum, st.Matches)
	}
	if unknown != 1 {
		t.Errorf("unknown-rank bucket = %d, want 1", unknown)
	}
}

// The meta lists are what the cache actually saw, named, and picks must not leak
// into bans.
func TestCorpusTopHeroesSplitPicksFromBans(t *testing.T) {
	store := corpusFixture(t)

	st, err := store.CorpusStats(31)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.TopPicks) == 0 || len(st.TopBans) == 0 {
		t.Fatalf("top picks = %d, top bans = %d; both should be populated", len(st.TopPicks), len(st.TopBans))
	}
	for _, h := range st.TopBans {
		if h.HeroID != 1 {
			t.Errorf("hero %d appears among bans; only hero 1 was ever banned", h.HeroID)
		}
		if h.Count != 2 {
			t.Errorf("hero %d banned %d times, want 2", h.HeroID, h.Count)
		}
	}
	for _, h := range st.TopPicks {
		if h.Name == "" {
			t.Errorf("hero %d has no display name", h.HeroID)
		}
	}
}

// An empty cache must answer with zeroes rather than an error: the screen exists
// precisely to tell a new user that there is nothing yet.
func TestCorpusStatsOnEmptyCache(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	st, err := store.CorpusStats(31)
	if err != nil {
		t.Fatalf("empty cache should not error: %v", err)
	}
	if st.Matches != 0 || len(st.Daily) != 0 {
		t.Errorf("empty cache reported %d matches / %d days", st.Matches, len(st.Daily))
	}
}
