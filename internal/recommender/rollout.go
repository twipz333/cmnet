package recommender

import (
	"sort"

	"cmnet/internal/draft"
)

// RolloutTopK is how many candidate heroes per unfilled slot carry the expected
// completion. Truncating the tail keeps a long list of near-zero-probability
// heroes from smearing the estimate, and keeps the sum interpretable.
const RolloutTopK = 20

// rolloutStrength estimates P(radiant win) for an UNFINISHED draft by
// marginalizing over how the draft is likely to finish, instead of scoring the
// board as if the empty slots simply did not exist.
//
// The problem it fixes: the win-prob model is trained on lineups, but mid-draft
// the advisor asks it about a board with three heroes on it. Encoding the missing
// picks as zeros does not mean "unknown", it means "no hero", which is a state the
// model has never seen and cannot reason about — the reading drifts toward the
// intercept and the number stops meaning anything.
//
// Because the model is linear in its features, the expected logit over random
// completions equals the logit of the EXPECTED feature vector. So the whole
// marginalization collapses to filling each unfilled slot with the predictor's
// probability distribution over available heroes — one forward pass, exact under
// the linear model, and fully deterministic.
//
// The approximation: the same distribution is reused for each of a side's
// remaining slots (conditioning each future pick on the previous one would need a
// tree). It holds up because those distributions are similar in practice, and it
// degrades gracefully — with zero unfilled slots this is exactly EncodeWinProb.
func (r *Recommender) rolloutStrength(d draft.View, rPicks, dPicks []int) (float64, bool) {
	if r.Predictor == nil || r.WinProb == nil {
		return 0, false
	}
	missR := draft.PicksPerTeam - len(rPicks)
	missD := draft.PicksPerTeam - len(dPicks)
	if missR <= 0 && missD <= 0 {
		return 0, false // nothing to marginalize: the plain encoding is exact
	}

	expR := r.expectedPicks(d, rPicks, dPicks, draft.Radiant, missR)
	expD := r.expectedPicks(d, rPicks, dPicks, draft.Dire, missD)
	x := r.heroMap.EncodeWinProbExpected(rPicks, dPicks, expR, expD)
	return r.WinProb.Model.Predict(x), true
}

// expectedPicks returns the expected multi-hot contribution of a team's remaining
// picks: the predictor's masked distribution scaled by how many slots are left.
func (r *Recommender) expectedPicks(d draft.View, rPicks, dPicks []int, team draft.Team, missing int) []float64 {
	if missing <= 0 {
		return nil
	}
	// The team's next pick in the schedule, not the current step: Ord -1 used to
	// stamp every side's expected picks with the index of whatever step was
	// current, which on the other side's turn is a step this team never takes.
	ord, _, ok := r.nextAction(d, team, true)
	if !ok {
		ord = -1
	}
	st := predictorState(boardState{
		rPicks: rPicks, dPicks: dPicks,
		rBans: d.Bans(draft.Radiant), dBans: d.Bans(draft.Dire),
		firstPick: d.FirstPickTeam(),
	}, team, draft.Pick, ord)
	blocked := make([]bool, r.heroMap.Len())
	for _, id := range append(append([]int(nil), rPicks...), dPicks...) {
		if i, ok := r.heroMap.Index(id); ok {
			blocked[i] = true
		}
	}
	for _, id := range append(append([]int(nil), st.RadiantBans...), st.DireBans...) {
		if i, ok := r.heroMap.Index(id); ok {
			blocked[i] = true
		}
	}
	dist := r.Predictor.DistributionMaskedIdx(st, blocked)
	trimmed := topKNormalized(dist, RolloutTopK)
	for i := range trimmed {
		trimmed[i] *= float64(missing)
	}
	return trimmed
}

// topKNormalized zeroes everything outside the k highest entries and renormalizes
// the survivors to sum to 1. Ties are broken by index so the result is stable.
func topKNormalized(dist []float64, k int) []float64 {
	if k <= 0 || k >= len(dist) {
		return normalized(dist)
	}
	// Selection by threshold: find the k-th largest value without sorting pairs,
	// then keep entries above it (and the earliest ties up to k).
	sorted := append([]float64(nil), dist...)
	sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))
	cutoff := sorted[k-1]

	out := make([]float64, len(dist))
	kept := 0
	for i, v := range dist {
		if v > cutoff {
			out[i] = v
			kept++
		}
	}
	for i, v := range dist {
		if kept >= k {
			break
		}
		if v == cutoff && out[i] == 0 {
			out[i] = v
			kept++
		}
	}
	return normalized(out)
}

// normalized scales a non-negative vector to sum to 1 (all-zero stays all-zero).
func normalized(v []float64) []float64 {
	out := append([]float64(nil), v...)
	var sum float64
	for _, x := range out {
		sum += x
	}
	if sum <= 0 {
		return out
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}
