// Package eval provides offline, deterministic quality metrics for the layer-1
// models (win-prob + next-pick predictor). It exists to answer the question the
// project could previously only eyeball: "is the model any good, and did a change
// make it better or worse?" — via a time-based train/test split and standard
// scoring (log-loss, Brier, ROC AUC, ECE, top-k). It writes nothing: models are
// trained into memory on the train split and scored on the held-out test split,
// so running eval never touches bundle.json.
package eval

import (
	"math"
	"sort"
)

// clampProb keeps a probability strictly inside (0,1) so log-loss never hits ±Inf.
func clampProb(p float64) float64 {
	const eps = 1e-12
	if p < eps {
		return eps
	}
	if p > 1-eps {
		return 1 - eps
	}
	return p
}

// LogLoss is the mean binary cross-entropy of probs vs labels (lower is better).
func LogLoss(probs, labels []float64) float64 {
	if len(probs) == 0 {
		return 0
	}
	var s float64
	for i := range probs {
		p := clampProb(probs[i])
		if labels[i] >= 0.5 {
			s += -math.Log(p)
		} else {
			s += -math.Log(1 - p)
		}
	}
	return s / float64(len(probs))
}

// Brier is the mean squared error of probabilities vs labels (lower is better).
func Brier(probs, labels []float64) float64 {
	if len(probs) == 0 {
		return 0
	}
	var s float64
	for i := range probs {
		d := probs[i] - labels[i]
		s += d * d
	}
	return s / float64(len(probs))
}

// AUC is the area under the ROC curve, computed via the rank-sum (Mann-Whitney)
// identity with average ranks for ties. Deterministic. Returns 0.5 (chance) when
// either class is empty. A pair with equal score contributes 0.5 (tie), which the
// average-rank formulation handles exactly.
func AUC(scores, labels []float64) float64 {
	n := len(scores)
	if n == 0 {
		return 0.5
	}
	type ps struct {
		score float64
		label float64
	}
	arr := make([]ps, n)
	var npos, nneg float64
	for i := range scores {
		arr[i] = ps{scores[i], labels[i]}
		if labels[i] >= 0.5 {
			npos++
		} else {
			nneg++
		}
	}
	if npos == 0 || nneg == 0 {
		return 0.5
	}
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].score < arr[j].score })

	// Assign average ranks (1-based) to tied score groups.
	ranks := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j < n && arr[j].score == arr[i].score {
			j++
		}
		// average of ranks (i+1 .. j) in 1-based indexing
		avg := (float64(i+1) + float64(j)) / 2
		for k := i; k < j; k++ {
			ranks[k] = avg
		}
		i = j
	}
	var sumRankPos float64
	for i := range arr {
		if arr[i].label >= 0.5 {
			sumRankPos += ranks[i]
		}
	}
	// Mann-Whitney U for positives, normalized to AUC.
	u := sumRankPos - npos*(npos+1)/2
	return u / (npos * nneg)
}

// ECE is the expected calibration error with `bins` equal-width probability bins:
// the sample-weighted average gap between average predicted probability and the
// empirical positive rate in each bin. Lower is better (0 = perfectly calibrated).
func ECE(probs, labels []float64, bins int) float64 {
	if len(probs) == 0 || bins <= 0 {
		return 0
	}
	sumP := make([]float64, bins)
	sumY := make([]float64, bins)
	cnt := make([]int, bins)
	for i := range probs {
		p := probs[i]
		b := int(p * float64(bins))
		if b >= bins {
			b = bins - 1
		}
		if b < 0 {
			b = 0
		}
		sumP[b] += p
		sumY[b] += labels[i]
		cnt[b]++
	}
	n := float64(len(probs))
	var ece float64
	for b := 0; b < bins; b++ {
		if cnt[b] == 0 {
			continue
		}
		avgP := sumP[b] / float64(cnt[b])
		avgY := sumY[b] / float64(cnt[b])
		ece += float64(cnt[b]) / n * math.Abs(avgP-avgY)
	}
	return ece
}

// BaseRate is the fraction of positive labels — the constant-predictor baseline.
func BaseRate(labels []float64) float64 {
	if len(labels) == 0 {
		return 0
	}
	var s float64
	for _, y := range labels {
		s += y
	}
	return s / float64(len(labels))
}
