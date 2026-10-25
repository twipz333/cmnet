package blend

import (
	"math"
	"reflect"
	"testing"
)

func sum(s []float64) float64 {
	var t float64
	for _, v := range s {
		t += v
	}
	return t
}

func TestW3EffRampsWithN(t *testing.T) {
	w := DefaultWeights()
	if w.W3Eff(0) != 0 {
		t.Errorf("W3Eff(0) = %v, want 0", w.W3Eff(0))
	}
	prev := 0.0
	for n := 1; n <= 5; n++ {
		got := w.W3Eff(n)
		if got <= prev {
			t.Errorf("W3Eff not increasing at n=%d: %v <= %v", n, got, prev)
		}
		if got > w.W3Max {
			t.Errorf("W3Eff(%d) = %v exceeds W3Max %v", n, got, w.W3Max)
		}
		prev = got
	}
}

func TestBlendNoAffinityIsNormalizedPrior(t *testing.T) {
	p1 := []float64{0.5, 0.3, 0.2}
	out := Blend(p1, nil, nil, 0, DefaultWeights())
	if math.Abs(sum(out)-1) > 1e-6 {
		t.Errorf("blend sum = %v, want 1", sum(out))
	}
	// Ordering must be preserved (no affinity to perturb it).
	if !(out[0] > out[1] && out[1] > out[2]) {
		t.Errorf("ordering not preserved: %v", out)
	}
}

func TestBlendDeterministic(t *testing.T) {
	p1 := []float64{0.4, 0.4, 0.2}
	aff2 := []float64{0.1, 0.5, 0.0}
	aff3 := []float64{0.0, 2.0, 0.0}
	a := Blend(p1, aff2, aff3, 2, DefaultWeights())
	b := Blend(p1, aff2, aff3, 2, DefaultWeights())
	if !reflect.DeepEqual(a, b) {
		t.Errorf("blend non-deterministic: %v vs %v", a, b)
	}
}

func TestBlendLayer3BoostsObservedHero(t *testing.T) {
	p1 := []float64{0.34, 0.33, 0.33}
	base := Blend(p1, nil, nil, 0, DefaultWeights())
	// Hero 1 (index 1) was picked twice by the team this event.
	aff3 := []float64{0, 2, 0}
	boosted := Blend(p1, nil, aff3, 2, DefaultWeights())
	if boosted[1] <= base[1] {
		t.Errorf("layer-3 hero prob not boosted: base %v, boosted %v", base[1], boosted[1])
	}
}

func TestBlendDegradationLayer3NilUsesLayer2Only(t *testing.T) {
	p1 := []float64{0.34, 0.33, 0.33}
	aff2 := []float64{0, 1.0, 0}
	withL2 := Blend(p1, aff2, nil, 0, DefaultWeights())
	if withL2[1] <= withL2[0] {
		t.Errorf("layer-2 affinity not applied: %v", withL2)
	}
	// n>0 must not matter when aff3 is nil.
	withN := Blend(p1, aff2, nil, 5, DefaultWeights())
	if !reflect.DeepEqual(withL2, withN) {
		t.Errorf("aff3 nil but n changed result: %v vs %v", withL2, withN)
	}
}
