package train

import (
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/features"
	"cmnet/internal/matrices"
)

// pairMatch is a picks-only match with heroes a,b on Radiant and c,d on Dire.
func pairMatch(id int64, a, b, c, d int, radiantWin bool) cache.Match {
	m := cache.Match{ID: id, StartTime: 1_700_000_000 + id, RadiantWin: radiantWin, Source: cache.SourceBulk}
	for i, h := range []int{a, b} {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: i, HeroID: h, IsPick: true, Team: 0})
	}
	for i, h := range []int{c, d} {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: 2 + i, HeroID: h, IsPick: true, Team: 1})
	}
	return m
}

// The stacker is fitted on the validation block, so the matrices it sees must not
// have counted those matches. Here pair (5,6) only ever plays — and always wins —
// in the validation block: the bundle's matrices know it, the stacker's must not.
func TestStackerMatricesExcludeValidationOutcomes(t *testing.T) {
	hm := features.NewHeroMap([]int{1, 2, 3, 4, 5, 6})
	var fit, val []cache.Match
	for i := 0; i < 40; i++ {
		fit = append(fit, pairMatch(int64(i), 1, 2, 3, 4, i%2 == 0))
	}
	for i := 0; i < 10; i++ {
		val = append(val, pairMatch(int64(100+i), 5, 6, 3, 4, true))
	}
	o := matrices.Options{NoShrink: true}
	full := matrices.Compute(append(append([]cache.Match(nil), fit...), val...), hm, o)
	i5, _ := hm.Index(5)
	i6, _ := hm.Index(6)
	if full.Synergy[i5][i6] == 0.5 {
		t.Fatal("setup: the full matrices should carry the validation pair")
	}
	got := stackerMatrices(fit, true, hm, o, full)
	if got.Synergy[i5][i6] != 0.5 {
		t.Fatalf("stacker matrices hold synergy %.3f for a pair seen only in validation", got.Synergy[i5][i6])
	}
	if stackerMatrices(fit, false, hm, o, full) != full {
		t.Fatal("without a validation block the full matrices should be used")
	}
}
