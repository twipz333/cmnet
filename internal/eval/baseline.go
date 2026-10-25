package eval

import (
	"sort"

	"cmnet/internal/cache"
	"cmnet/internal/features"
)

// Baseline is a frequency-only next-action predictor. It exists to answer the
// question a top-k number alone cannot: is the model learning the draft, or is it
// reciting the meta?
//
// "6x better than uniform random" sounds impressive and means nothing — nobody
// would ever guess uniformly. The honest reference is how well you do by counting
// what heroes get taken, and the strongest cheap version of that conditions on the
// draft step and masks heroes already on the board. A model that cannot clear this
// bar is adding nothing over a lookup table.
type Baseline struct {
	Name string
	// counts[key][heroIdx] is how often that hero was taken under the key.
	counts map[baselineKey][]float64
	keyOf  func(ord int, isPick bool, actor int) baselineKey
	n      int
}

type baselineKey struct {
	a, b int
}

// BaselineKinds returns the frequency baselines, weakest first.
func BaselineKinds(hm *features.HeroMap) []*Baseline {
	nop := func(ord int, isPick bool, actor int) baselineKey { return baselineKey{} }
	byAction := func(ord int, isPick bool, actor int) baselineKey {
		if isPick {
			return baselineKey{a: 1}
		}
		return baselineKey{}
	}
	byOrd := func(ord int, isPick bool, actor int) baselineKey { return baselineKey{a: ord} }
	byPhase := func(ord int, isPick bool, actor int) baselineKey {
		phase, _ := features.PhaseOf(ord)
		b := 0
		if isPick {
			b = 1
		}
		return baselineKey{a: phase, b: b}
	}
	return []*Baseline{
		{Name: "popularity", counts: map[baselineKey][]float64{}, keyOf: nop},
		{Name: "popularity|pick-ban", counts: map[baselineKey][]float64{}, keyOf: byAction},
		{Name: "popularity|phase", counts: map[baselineKey][]float64{}, keyOf: byPhase},
		{Name: "popularity|step", counts: map[baselineKey][]float64{}, keyOf: byOrd},
	}
}

// Fit accumulates counts from the training matches (CM drafts only — they are the
// ones with a real action order).
func (b *Baseline) Fit(matches []cache.Match, hm *features.HeroMap) {
	n := hm.Len()
	for _, m := range matches {
		if !hasBan(m) {
			continue
		}
		for ord, pb := range orderedPickBans(m) {
			idx, ok := hm.Index(pb.HeroID)
			if !ok {
				continue
			}
			k := b.keyOf(ord, pb.IsPick, pb.Team)
			row := b.counts[k]
			if row == nil {
				row = make([]float64, n)
				b.counts[k] = row
			}
			row[idx]++
			b.n++
		}
	}
}

// Score returns the baseline's ranking score per dense hero index for one action,
// with taken heroes zeroed out. Not normalized: only the ordering is used.
func (b *Baseline) Score(ord int, isPick bool, actor int, taken []bool) []float64 {
	row := b.counts[b.keyOf(ord, isPick, actor)]
	if row == nil {
		// Unseen key (e.g. a step index never observed): fall back to the pooled
		// counts so the baseline degrades instead of returning nothing.
		row = b.counts[baselineKey{}]
	}
	out := make([]float64, len(taken))
	for i := range out {
		if i < len(row) && !taken[i] {
			out[i] = row[i]
		}
	}
	return out
}

// orderedPickBans returns a match's actions sorted by draft order.
func orderedPickBans(m cache.Match) []cache.PickBan {
	pbs := append([]cache.PickBan(nil), m.PickBans...)
	sort.SliceStable(pbs, func(i, j int) bool { return pbs[i].Ord < pbs[j].Ord })
	return pbs
}
