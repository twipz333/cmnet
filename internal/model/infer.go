// Package model holds the layer-1 models (next-pick predictor and win-prob) plus
// their deterministic forward pass. Inference is single-threaded and scores are
// rounded before ranking so results are byte-identical across platforms (§6).
package model

import "math"

// ScorePrecision is the number of decimal places scores are rounded to before
// ranking. Rounding neutralizes sub-ULP float differences (including FMA) between
// platforms.
const ScorePrecision = 5

// RoundScore rounds f to ScorePrecision decimal places.
func RoundScore(f float64) float64 {
	p := math.Pow(10, ScorePrecision)
	return math.Round(f*p) / p
}

// dot computes the dot product of w and x in fixed index order. They must be the
// same length.
func dot(w, x []float64) float64 {
	var s float64
	for i := range x {
		s += w[i] * x[i]
	}
	return s
}

func sigmoid(z float64) float64 {
	return 1 / (1 + math.Exp(-z))
}

// softmax converts logits to probabilities in place-safe fashion (returns a new
// slice), iterating in fixed index order.
func softmax(logits []float64) []float64 {
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
		out[i] /= sum
	}
	return out
}
