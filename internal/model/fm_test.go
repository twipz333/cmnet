package model

import (
	"math"
	"reflect"
	"testing"
)

func TestNewFMDeterministicAndNonzero(t *testing.T) {
	a, b := NewFM(5, 3), NewFM(5, 3)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("fixed initialization produced different models")
	}
	nonzero := false
	for _, row := range a.Emb {
		for _, v := range row {
			nonzero = nonzero || v != 0
		}
	}
	if !nonzero {
		t.Fatal("factor initialization is entirely zero")
	}
}

func TestFMLogitAntisymmetry(t *testing.T) {
	f := NewFM(8, 4)
	f.Bias = 0.17
	for i := range f.Main {
		f.Main[i] = float64(i-3) / 10
	}
	radiant, dire := []int{0, 2, 5}, []int{1, 4, 7}
	got := f.Logit(radiant, dire) + f.Logit(dire, radiant)
	if math.Abs(got-2*f.Bias) > 1e-9 {
		t.Fatalf("side-swap logit sum = %.12g, want %.12g", got, 2*f.Bias)
	}
}

func TestTrainFMLearnsSeparableHero(t *testing.T) {
	var samples []FMSample
	for i := 0; i < 20; i++ {
		samples = append(samples,
			FMSample{Radiant: []int{0, 1}, Dire: []int{2, 3}, Y: 1},
			FMSample{Radiant: []int{2, 3}, Dire: []int{0, 1}, Y: 0},
		)
	}
	f := TrainFM(samples, nil, 4, FMConfig{K: 2, Epochs: 200, LR: 0.2, EvalEvery: 20})
	if got := f.Predict([]int{0, 1}, []int{2, 3}); got <= 0.5 {
		t.Fatalf("winning composition probability = %v, want > 0.5", got)
	}
}

func TestTrainFMDeterministic(t *testing.T) {
	samples := []FMSample{
		{Radiant: []int{0, 1}, Dire: []int{2, 3}, Y: 1},
		{Radiant: []int{0, 2}, Dire: []int{1, 3}, Y: 0},
	}
	cfg := FMConfig{K: 2, Epochs: 25, LR: 0.1, EvalEvery: 5}
	a, b := TrainFM(samples, nil, 4, cfg), TrainFM(samples, nil, 4, cfg)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("identical training runs produced different weights")
	}
}

func TestTeamSynergyNeedsPair(t *testing.T) {
	f := NewFM(3, 2)
	if got := f.teamSynergy(nil); got != 0 {
		t.Fatalf("empty synergy = %v, want 0", got)
	}
	if got := f.teamSynergy([]int{1}); got != 0 {
		t.Fatalf("single-hero synergy = %v, want 0", got)
	}
}
