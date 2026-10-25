package main

import (
	"context"
	"os"
	"testing"
	"time"

	"cmnet/internal/cache"
)

// openCacheForTest opens the cache the same way the operations do. The pass under
// test opens and closes its own handle, so the checks around it must not hold one
// across the call.
func openCacheForTest(t *testing.T, path string) (*cache.Store, error) {
	t.Helper()
	return cache.Open(path)
}

// TestRankfillAgainstARealCache runs a real pass — real cache, real OpenDota — and
// checks that the backlog actually shrinks.
//
// Opt-in, because it needs the network and a mature cache to be worth anything: an
// empty database would pass the assertions without exercising a single request.
// Point CMNET_REAL_DB at a COPY; the pass writes to it.
//
// What this catches that the unit tests cannot: whether the Explorer query shape
// is still accepted, whether public_matches still has rows for matches this old,
// and whether a batch of MaxRankTierBatch ids still comes back whole rather than
// truncated under HTTP 200.
func TestRankfillAgainstARealCache(t *testing.T) {
	path := os.Getenv("CMNET_REAL_DB")
	if path == "" {
		t.Skip("set CMNET_REAL_DB to a copy of a real cache to run this")
	}

	store, err := openCacheForTest(t, path)
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	before, err := store.MatchesMissingRank(defaultRankfillLimit)
	if err != nil {
		t.Fatalf("read backlog: %v", err)
	}
	withRank, total, err := store.RankCoverage()
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	t.Logf("before: %d rankable matches, %d with a tier, backlog head %d",
		total, withRank, len(before))
	store.Close()
	if len(before) == 0 {
		t.Skip("this cache has no matches missing a rank tier; nothing to exercise")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// Default limit on purpose: a pass is ten batches back to back, and whether the
	// Explorer endpoint tolerates that is exactly what only a real run can tell.
	summary, err := opRankfill(ctx, rankfillOpts{dbPath: path},
		func(line string) { t.Log(line) })
	if err != nil {
		t.Fatalf("rankfill: %v", err)
	}
	t.Log(summary)

	store, err = openCacheForTest(t, path)
	if err != nil {
		t.Fatalf("reopen cache: %v", err)
	}
	defer store.Close()
	after, err := store.MatchesMissingRank(defaultRankfillLimit)
	if err != nil {
		t.Fatalf("read backlog: %v", err)
	}
	// The backlog is longer than one pass on a mature cache, so its LENGTH proves
	// nothing — the ids are what matter. Every match the pass asked about must be
	// gone from the query, whether or not an answer came back, or the next pass
	// walks the same head forever.
	asked := make(map[int64]bool, len(before))
	for _, id := range before[:min(defaultRankfillLimit, len(before))] {
		asked[id] = true
	}
	for _, id := range after {
		if asked[id] {
			t.Fatalf("match %d is still in the backlog after the pass asked about it", id)
		}
	}
	nowWithRank, _, err := store.RankCoverage()
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	t.Logf("after: %d with a tier (+%d)", nowWithRank, nowWithRank-withRank)
	if nowWithRank <= withRank {
		t.Errorf("matches carrying a tier went from %d to %d — the pass asked but "+
			"stored nothing", withRank, nowWithRank)
	}
}
