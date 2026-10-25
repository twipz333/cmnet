package blend

import "math"

// roundPrecision matches model.ScorePrecision (5 decimals). Duplicated here to
// keep blend dependency-free; both must agree for cross-layer determinism (§6).
const roundPrecision = 5

func round(f float64) float64 {
	p := math.Pow(10, roundPrecision)
	return math.Round(f*p) / p
}

// Blend combines the layer-1 enemy-pick prior p1 with optional player-affinity
// (aff2) and team-this-event (aff3) vectors into a probability distribution.
//
// All input slices are indexed by dense hero index and must share length with p1.
// aff2 and/or aff3 may be nil to skip that layer (graceful degradation). n is the
// number of opponent drafts observed in the current event (drives w3_eff).
//
// Affinity vectors are additively smoothed so an unseen hero is never driven to
// log(0). The result is a softmax over the log-linear score, rounded for platform
// stability, in dense index order (no map iteration).
func Blend(p1, aff2, aff3 []float64, n int, w Weights) []float64 {
	size := len(p1)
	logits := make([]float64, size)
	w3eff := w.W3Eff(n)

	for i := 0; i < size; i++ {
		// log P1 with a tiny floor so a zero prior does not produce -Inf.
		logit := math.Log(p1[i] + 1e-12)
		if aff2 != nil && w.W2 != 0 {
			logit += w.W2 * math.Log(smooth(aff2[i], w.Smoothing))
		}
		if aff3 != nil && w3eff != 0 {
			logit += w3eff * math.Log(smooth(aff3[i], w.Smoothing))
		}
		logits[i] = logit
	}
	return softmaxRounded(logits)
}

func smooth(v, s float64) float64 {
	r := v + s
	if r <= 0 {
		return 1e-12
	}
	return r
}

func softmaxRounded(logits []float64) []float64 {
	maxLogit := math.Inf(-1)
	for _, l := range logits {
		if l > maxLogit {
			maxLogit = l
		}
	}
	out := make([]float64, len(logits))
	var sum float64
	for i, l := range logits {
		e := math.Exp(l - maxLogit)
		out[i] = e
		sum += e
	}
	if sum == 0 {
		return out
	}
	for i := range out {
		out[i] = round(out[i] / sum)
	}
	return out
}
