package train

import (
	"testing"

	"cmnet/internal/cache"
)

// cmDraft builds a CM-shaped draft (bans present, so it reaches the predictor's
// training pool) from one source, picking a hero block characteristic of that
// source. Pro and public drafts must favour DIFFERENT heroes, or down-weighting
// one population scales both sides equally and cannot be observed.
func cmDraft(id, start int64, radWin bool, source string, heroes []int) cache.Match {
	m := cache.Match{ID: id, StartTime: start, RadiantWin: radWin, Source: source}
	ord := 0
	for i := range 4 {
		m.PickBans = append(m.PickBans, cache.PickBan{
			Ord: ord, HeroID: heroes[i%len(heroes)], IsPick: false, Team: i % 2,
		})
		ord++
	}
	for i := range 10 {
		m.PickBans = append(m.PickBans, cache.PickBan{
			Ord: ord, HeroID: heroes[i%len(heroes)], IsPick: true, Team: i % 2,
		})
		ord++
	}
	return m
}

// mixedCorpus mimics today's cache: pro CM drafts alongside public CM drafts,
// each population favouring its own heroes.
func mixedCorpus() []cache.Match {
	var out []cache.Match
	proHeroes := []int{1, 2, 3, 4, 5}
	pubHeroes := []int{6, 7, 8, 9, 10}
	for i := range 20 {
		start := int64(1_700_000_000 + i*3600)
		out = append(out, cmDraft(int64(i*2+1), start, i%2 == 0, cache.SourcePro, proHeroes))
		out = append(out, cmDraft(int64(i*2+2), start+60, i%3 == 0, cache.SourcePublicCM, pubHeroes))
	}
	return out
}

// SourceWeights must reach the PREDICTOR, not just win-prob. They used to be
// dropped on the predictor's sample build, which silently turned every
// pro-weight experiment into a measurement of the plumbing: the predictor came
// back bit-identical no matter what weight was passed.
func TestProWeightReachesPredictor(t *testing.T) {
	heroIDs := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	opts := func(w float64) Options {
		return Options{
			CreatedAt:     "2026-01-01T00:00:00Z",
			SourceWeights: ProWeight(w),
		}
	}

	base := Bundle(mixedCorpus(), heroIDs, opts(1))
	down := Bundle(mixedCorpus(), heroIDs, opts(0.25))

	if equalWeights(base.PickPredictor.Weights, down.PickPredictor.Weights) {
		t.Error("pro-weight 0.25 left the pick predictor bit-identical to weight 1: " +
			"SourceWeights are not reaching predictor training")
	}

	// Weight 1 means "no source weighting at all" (ProWeight returns nil), so it
	// must stay a no-op — this is what keeps the shipped default and its golden
	// bundle unchanged.
	none := Bundle(mixedCorpus(), heroIDs, Options{CreatedAt: "2026-01-01T00:00:00Z"})
	if base.BundleVersion != none.BundleVersion {
		t.Errorf("pro-weight 1 changed the bundle: %q vs %q", base.BundleVersion, none.BundleVersion)
	}
}

func equalWeights(a, b [][]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}
