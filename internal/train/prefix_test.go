package train

import (
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/features"
)

// bulkMatch mimics what the Explorer scan stores: picks only, no bans, and a
// synthetic order of five Radiant heroes followed by five Dire ones.
func bulkMatch(id int64, radWin bool) cache.Match {
	m := cache.Match{ID: id, StartTime: 1_700_000_000 + id, RadiantWin: radWin, Source: cache.SourceBulk}
	ord := 0
	for _, h := range []int{1, 2, 3, 4, 5} {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 0})
		ord++
	}
	for _, h := range []int{6, 7, 8, 9, 10} {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 1})
		ord++
	}
	return m
}

// sideCounts reports how many Radiant and Dire heroes an encoded win-prob row
// holds, given which hero ids belong to Radiant in the fixture.
func sideCounts(t *testing.T, hm *features.HeroMap, x []float64, radiantIDs []int, board int) (nr, nd int) {
	t.Helper()
	for _, id := range radiantIDs {
		idx, ok := hm.Index(id)
		if !ok {
			t.Fatalf("hero %d missing from the hero map", id)
		}
		if x[idx] != 0 {
			nr++
		}
	}
	return nr, board - nr
}

// cmMatch is a real draft: bans present, teams alternating.
func cmMatch(id int64, radWin bool) cache.Match {
	m := cache.Match{ID: id, StartTime: 1_700_000_000 + id, RadiantWin: radWin, Source: cache.SourcePublicCM}
	ord := 0
	for i, h := range []int{11, 12, 13, 14} {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: false, Team: i % 2})
		ord++
	}
	for i, h := range []int{1, 6, 2, 7, 3, 8, 4, 9, 5, 10} {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: i % 2})
		ord++
	}
	return m
}

// Bulk matches are stored five-Radiant-then-five-Dire, so walking that stored
// sequence yields boards no draft passes through — up to "five heroes versus an
// empty side". Their prefixes must be rebuilt by alternating sides.
//
// They must NOT simply be dropped: measured, dropping them cost pick-quality AUC
// on CM 0.555 → 0.504 and pushed the 1-2 hero boards below 0.5. The 52k bulk
// matches are what teaches sparse boards; only their lopsidedness is fiction.
func TestBulkPrefixesAlternateSides(t *testing.T) {
	hm := features.NewHeroMap([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14})

	bulk := BuildSamples([]cache.Match{bulkMatch(1, true)}, hm, -1, SampleOptions{Prefix: true})
	if len(bulk.WinProb) != 10 {
		t.Fatalf("bulk match produced %d win-prob rows, want 10 (one per hero added)", len(bulk.WinProb))
	}
	// Board sizes still grow one hero at a time...
	for i, got := range bulk.Board {
		if want := i + 1; got != want {
			t.Errorf("row %d: board of %d heroes, want %d", i, got, want)
		}
	}
	// ...but the sides must stay within one hero of each other. The stored order
	// violates that (it reaches 5v0); a real draft never does. Heroes 1-5 are
	// Radiant's in the fixture and 6-10 are Dire's, so the encoded rows can be
	// counted directly.
	for i, s := range bulk.WinProb {
		nr, nd := sideCounts(t, hm, s.X, []int{1, 2, 3, 4, 5}, bulk.Board[i])
		if nr-nd > 1 || nd-nr > 1 {
			t.Errorf("board %d holds %dv%d; an alternating draft never gets more than one hero ahead", i+1, nr, nd)
		}
	}

	cm := BuildSamples([]cache.Match{cmMatch(2, true)}, hm, -1, SampleOptions{Prefix: true})
	if len(cm.WinProb) != 10 {
		t.Errorf("CM draft produced %d win-prob rows, want 10 (one per pick)", len(cm.WinProb))
	}

	// Lopsided rosters are where clamping ceil/floor went wrong: a 5v3 match used
	// to emit 4v3 twice and never reach its own full composition, silently losing
	// the one row that matters most.
	lopsided := cache.Match{ID: 10, StartTime: 1_700_000_000, RadiantWin: true, Source: cache.SourceBulk}
	ord := 0
	for _, h := range []int{1, 2, 3, 4, 5} {
		lopsided.PickBans = append(lopsided.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 0})
		ord++
	}
	for _, h := range []int{6, 7, 8} {
		lopsided.PickBans = append(lopsided.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 1})
		ord++
	}
	odd := BuildSamples([]cache.Match{lopsided}, hm, -1, SampleOptions{Prefix: true})
	if len(odd.WinProb) != 8 {
		t.Fatalf("5v3 match produced %d rows, want 8 (one per hero)", len(odd.WinProb))
	}
	for i, got := range odd.Board {
		if want := i + 1; got != want {
			t.Errorf("5v3 row %d: board of %d heroes, want %d — every step must add exactly one", i, got, want)
		}
	}
	if nr, nd := sideCounts(t, hm, odd.WinProb[7].X, []int{1, 2, 3, 4, 5}, odd.Board[7]); nr != 5 || nd != 3 {
		t.Errorf("last 5v3 row holds %dv%d, want 5v3 — the full composition must be emitted", nr, nd)
	}

	mixed := BuildSamples([]cache.Match{bulkMatch(3, true), cmMatch(4, false)}, hm, -1, SampleOptions{Prefix: true})
	for i, b := range mixed.Board {
		if b == 0 {
			t.Errorf("row %d has an empty board", i)
		}
	}
	if len(mixed.WinProb) != 20 {
		t.Errorf("mixed pool produced %d rows, want 20 (10 bulk + 10 CM)", len(mixed.WinProb))
	}
}
