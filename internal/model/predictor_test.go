package model

import (
	"math"
	"reflect"
	"testing"

	"cmnet/internal/features"
)

func testPredictor(headPick, headBan *SoftmaxReg) *Predictor {
	p := &Predictor{
		Pick: headPick, Ban: headBan,
		HeroMap: features.NewHeroMap([]int{10, 20, 30, 40}),
	}
	for _, head := range []*SoftmaxReg{headPick, headBan} {
		if head == nil {
			continue
		}
		for i := range head.Weights {
			head.Weights[i] = make([]float64, p.HeroMap.PredictorLen())
		}
	}
	return p
}

func biasHead(bias ...float64) *SoftmaxReg {
	return &SoftmaxReg{Weights: make([][]float64, len(bias)), Bias: bias}
}

func TestDistributionMasksTakenAndNormalizes(t *testing.T) {
	p := testPredictor(biasHead(0, 1, 2, 3), nil)
	got := p.Distribution(features.PredictorState{
		RadiantPicks: []int{10}, DireBans: []int{30}, IsPick: true,
	})
	if got[0] != 0 || got[2] != 0 {
		t.Fatalf("taken probabilities must be exact zero: %v", got)
	}
	var sum float64
	for _, v := range got {
		sum += v
	}
	if math.Abs(sum-1) > 1e-3 {
		t.Fatalf("legal probability sum = %v, want approximately 1", sum)
	}
}

func TestDistributionUsesSeparateHeadsAndFallback(t *testing.T) {
	pick := biasHead(4, 0, 0, 0)
	ban := biasHead(0, 0, 0, 4)
	p := testPredictor(pick, ban)
	pickDist := p.Distribution(features.PredictorState{IsPick: true})
	banDist := p.Distribution(features.PredictorState{IsPick: false})
	if reflect.DeepEqual(pickDist, banDist) {
		t.Fatalf("pick and ban heads produced the same distribution: %v", pickDist)
	}
	if got := testPredictor(pick, nil).Distribution(features.PredictorState{IsPick: false}); !reflect.DeepEqual(got, pickDist) {
		t.Fatalf("ban fallback = %v, want pick distribution %v", got, pickDist)
	}
	if got := testPredictor(nil, ban).Distribution(features.PredictorState{IsPick: true}); !reflect.DeepEqual(got, banDist) {
		t.Fatalf("pick fallback = %v, want ban distribution %v", got, banDist)
	}
}

func TestDistributionAllBlockedReturnsZeros(t *testing.T) {
	p := testPredictor(biasHead(1, 2, 3, 4), nil)
	got := p.DistributionMaskedIdx(features.PredictorState{IsPick: true}, []bool{true, true, true, true})
	for i, v := range got {
		if v != 0 || math.IsNaN(v) {
			t.Fatalf("probability %d = %v, want exact zero", i, v)
		}
	}
}

func TestDistributionDeterministic(t *testing.T) {
	p := testPredictor(biasHead(0.1, 0.2, 0.3, 0.4), nil)
	s := features.PredictorState{RadiantPicks: []int{20}, IsPick: true, Ord: 7}
	a, b := p.Distribution(s), p.Distribution(s)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("successive distributions differ: %v vs %v", a, b)
	}
}
