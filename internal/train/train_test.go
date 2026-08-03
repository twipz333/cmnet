package train

import (
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/features"
)

func mkMatch(id, start int64, radWin bool, patch string, mmr *int, rPicks, dPicks []int) cache.Match {
	m := cache.Match{ID: id, StartTime: start, RadiantWin: radWin, Patch: patch, AvgMMR: mmr}
	ord := 0
	for _, h := range rPicks {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 0})
		ord++
	}
	for _, h := range dPicks {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 1})
		ord++
	}
	return m
}

func fixed() []cache.Match {
	return []cache.Match{
		mkMatch(1, 1000, true, "7.36", nil, []int{1, 2}, []int{3, 4}),
		mkMatch(2, 2000, false, "7.36", nil, []int{3, 4}, []int{1, 2}),
	}
}

// Retrain on an expanded hero list must resize the models, not error.
func TestRetrainResizesToHeroList(t *testing.T) {
	matches := fixed()
	b3 := Bundle(matches, []int{1, 2, 3}, Options{CreatedAt: "2026-01-01T00:00:00Z"})
	if len(b3.PickPredictor.Weights) != 3 {
		t.Errorf("predictor classes = %d, want 3", len(b3.PickPredictor.Weights))
	}
	if len(b3.WinProb.Weights) != 3 {
		t.Errorf("winprob features = %d, want 3", len(b3.WinProb.Weights))
	}

	b4 := Bundle(matches, []int{1, 2, 3, 4}, Options{CreatedAt: "2026-01-01T00:00:00Z"})
	if len(b4.PickPredictor.Weights) != 4 {
		t.Errorf("retrained predictor classes = %d, want 4", len(b4.PickPredictor.Weights))
	}
	if len(b4.WinProb.Weights) != 4 {
		t.Errorf("retrained winprob features = %d, want 4", len(b4.WinProb.Weights))
	}
	hm := features.NewHeroMap(b4.HeroIDs)
	if _, ok := hm.Index(4); !ok {
		t.Error("new hero 4 missing from retrained hero map")
	}
}

func TestBundleDeterministic(t *testing.T) {
	a := Bundle(fixed(), []int{1, 2, 3, 4}, Options{CreatedAt: "2026-01-01T00:00:00Z"})
	b := Bundle(fixed(), []int{1, 2, 3, 4}, Options{CreatedAt: "2026-01-01T00:00:00Z"})
	if a.BundleVersion != b.BundleVersion {
		t.Errorf("bundle version not deterministic: %q vs %q", a.BundleVersion, b.BundleVersion)
	}
}

func TestFilterMatches(t *testing.T) {
	mmr := 6000
	low := 3000
	matches := []cache.Match{
		mkMatch(1, 1, true, "7.36", &mmr, []int{1}, []int{2}),
		mkMatch(2, 2, true, "7.35", &mmr, []int{1}, []int{2}),
		mkMatch(3, 3, true, "7.36", &low, []int{1}, []int{2}),
		mkMatch(4, 4, true, "7.36", nil, []int{1}, []int{2}),
		mkMatch(5, 5, true, "", nil, []int{1}, []int{2}), // predates provenance entirely
	}
	byPatch := FilterMatches(matches, "7.36", 0, 0)
	// Three tagged 7.36, plus the untagged one: an unknown patch passes, exactly as
	// an unknown rank does. Most of a mature cache carries no patch tag at all, and
	// excluding it would leave the filter looking honoured while training on nothing.
	if len(byPatch) != 4 {
		t.Errorf("patch filter kept %d, want 4 (unknown patch must pass)", len(byPatch))
	}
	byMMR := FilterMatches(matches, "", 5000, 0)
	// Only the genuinely low match is dropped. avg_mmr is never written by any
	// collector, so requiring a value here dropped the entire corpus — the flag did
	// not narrow the training set, it emptied it.
	if len(byMMR) != 4 {
		t.Errorf("mmr filter kept %d, want 4 (unknown MMR must pass, 3000 must not)", len(byMMR))
	}
	for _, m := range byMMR {
		if m.ID == 3 {
			t.Error("a match below the MMR floor survived the filter")
		}
	}
	if len(FilterMatches(matches, "", 0, 0)) != 5 {
		t.Error("no-op filter must keep all")
	}
}

func TestRecencyWeightingIsDeterministicAndDecays(t *testing.T) {
	// Newer match (higher start_time) should weigh >= older after recency weighting.
	matches := []cache.Match{
		mkMatch(1, 0, true, "", nil, []int{1, 2}, []int{3, 4}),                 // old
		mkMatch(2, int64(30*86400), true, "", nil, []int{1, 2}, []int{3, 4}),   // ~30d newer
	}
	hm := features.NewHeroMap([]int{1, 2, 3, 4})
	wp := BuildSamples(matches, hm, DefaultHalfLifeDays, SampleOptions{}).WinProb
	if wp[0].Weight >= wp[1].Weight {
		t.Errorf("older sample weight %v should be < newer %v", wp[0].Weight, wp[1].Weight)
	}
	// Disabled weighting -> uniform.
	wpu := BuildSamples(matches, hm, -1, SampleOptions{}).WinProb
	if wpu[0].Weight != wpu[1].Weight {
		t.Errorf("disabled recency should be uniform: %v vs %v", wpu[0].Weight, wpu[1].Weight)
	}
}
