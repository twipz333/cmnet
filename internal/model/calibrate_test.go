package model

import (
	"math"
	"testing"
)

func TestIdentityCalibratorIsNoOp(t *testing.T) {
	id := Identity()
	for _, p := range []float64{0.1, 0.5, 0.9} {
		if math.Abs(id.Apply(p)-p) > 1e-9 {
			t.Errorf("Identity.Apply(%v) = %v, want %v", p, id.Apply(p), p)
		}
	}
}

func TestFitPlattEmptyReturnsIdentity(t *testing.T) {
	p := FitPlatt(nil, nil, 100, 0.5)
	if p.A != 1 || p.B != 0 {
		t.Errorf("empty fit = %+v, want identity", p)
	}
}

// Platt scaling on miscalibrated probabilities should not worsen, and typically
// improves, the Brier score.
func TestFitPlattImprovesBrier(t *testing.T) {
	// Construct overconfident raw probabilities: label 1 -> 0.95, label 0 -> 0.05,
	// but the true base rate per bucket is only 0.7 / 0.3.
	var raw, labels []float64
	add := func(p, frac float64, n int) {
		pos := int(frac * float64(n))
		for i := 0; i < n; i++ {
			raw = append(raw, p)
			if i < pos {
				labels = append(labels, 1)
			} else {
				labels = append(labels, 0)
			}
		}
	}
	add(0.95, 0.7, 100) // overconfident positives
	add(0.05, 0.3, 100) // overconfident negatives

	before := BrierScore(raw, labels)

	cal := FitPlatt(raw, labels, 500, 0.3)
	calibrated := make([]float64, len(raw))
	for i, r := range raw {
		calibrated[i] = cal.Apply(r)
	}
	after := BrierScore(calibrated, labels)

	if after > before {
		t.Errorf("calibration worsened Brier: before %v, after %v", before, after)
	}
}

func TestFitPlattDeterministic(t *testing.T) {
	raw := []float64{0.9, 0.8, 0.2, 0.1}
	labels := []float64{1, 0, 1, 0}
	a := FitPlatt(raw, labels, 200, 0.3)
	b := FitPlatt(raw, labels, 200, 0.3)
	if a != b {
		t.Errorf("FitPlatt non-deterministic: %+v vs %+v", a, b)
	}
}
