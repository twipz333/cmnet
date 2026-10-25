package eval

import "math"

// MultiClassLogLoss is mean −log p[true] over actions (lower is better).
func MultiClassLogLoss(probs [][]float64, labels []int) float64 {
	if len(probs) == 0 {
		return 0
	}
	var s float64
	for i, dist := range probs {
		lab := labels[i]
		p := 0.0
		if lab >= 0 && lab < len(dist) {
			p = dist[lab]
		}
		s += -math.Log(clampProb(p))
	}
	return s / float64(len(probs))
}

// MultiClassBrier is mean Σ_j (p_j − 1[j=y])^2 (lower is better).
func MultiClassBrier(probs [][]float64, labels []int) float64 {
	if len(probs) == 0 {
		return 0
	}
	var s float64
	for i, dist := range probs {
		lab := labels[i]
		for j, p := range dist {
			y := 0.0
			if j == lab {
				y = 1
			}
			d := p - y
			s += d * d
		}
	}
	return s / float64(len(probs))
}

// MultiClassECE is confidence ECE: bin by max_j p_j, label = 1[argmax == true].
func MultiClassECE(probs [][]float64, labels []int, bins int) float64 {
	if len(probs) == 0 || bins <= 0 {
		return 0
	}
	sumP := make([]float64, bins)
	sumHit := make([]float64, bins)
	cnt := make([]int, bins)
	for i, dist := range probs {
		if len(dist) == 0 {
			continue
		}
		best, bestP := 0, dist[0]
		for j, p := range dist {
			if p > bestP {
				best, bestP = j, p
			}
		}
		b := int(bestP * float64(bins))
		if b >= bins {
			b = bins - 1
		}
		if b < 0 {
			b = 0
		}
		sumP[b] += bestP
		if best == labels[i] {
			sumHit[b]++
		}
		cnt[b]++
	}
	n := float64(len(probs))
	var ece float64
	for b := 0; b < bins; b++ {
		if cnt[b] == 0 {
			continue
		}
		avgP := sumP[b] / float64(cnt[b])
		avgY := sumHit[b] / float64(cnt[b])
		ece += float64(cnt[b]) / n * math.Abs(avgP-avgY)
	}
	return ece
}

// normalizeScores turns non-negative ranking scores into a probability simplex.
func normalizeScores(scores []float64) []float64 {
	out := make([]float64, len(scores))
	var sum float64
	for _, v := range scores {
		if v > 0 {
			sum += v
		}
	}
	if sum <= 0 {
		if len(out) == 0 {
			return out
		}
		u := 1 / float64(len(out))
		for i := range out {
			out[i] = u
		}
		return out
	}
	if math.Abs(sum-1) < 1e-6 {
		copy(out, scores)
		return out
	}
	for i, v := range scores {
		if v > 0 {
			out[i] = v / sum
		}
	}
	return out
}
