package eval

import (
	"math"
	"strings"
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/features"
	"cmnet/internal/model"
)

func TestMultiClassLogLossPerfect(t *testing.T) {
	probs := [][]float64{{0, 1, 0}, {1, 0, 0}}
	labels := []int{1, 0}
	if got := MultiClassLogLoss(probs, labels); got > 1e-9 {
		t.Fatalf("logloss=%v want ~0", got)
	}
}

func TestNormalizeScores(t *testing.T) {
	got := normalizeScores([]float64{2, 2, 0})
	if math.Abs(got[0]-0.5) > 1e-9 || math.Abs(got[1]-0.5) > 1e-9 || got[2] != 0 {
		t.Fatalf("%v", got)
	}
}

func TestLadderSmoke(t *testing.T) {
	// Tiny synthetic CM: model strongly prefers the true hero.
	ids := []int{1, 2, 3, 4}
	hm := features.NewHeroMap(ids)
	n := hm.Len()
	pred := &model.Predictor{
		Pick: &model.SoftmaxReg{
			Weights: make([][]float64, n),
			Bias:    []float64{5, 0, 0, 0}, // hero 1 dominates
		},
		HeroMap: hm,
	}
	for i := range pred.Pick.Weights {
		pred.Pick.Weights[i] = make([]float64, hm.PredictorLen())
	}
	pred.Ban = pred.Pick

	// One CM-ish match: 4 bans then pick hero 1
	m := cache.Match{
		ID: 1, StartTime: 100, RadiantWin: true,
		PickBans: []cache.PickBan{
			{Ord: 0, HeroID: 2, IsPick: false, Team: 0},
			{Ord: 1, HeroID: 3, IsPick: false, Team: 1},
			{Ord: 2, HeroID: 4, IsPick: false, Team: 0},
			{Ord: 3, HeroID: 2, IsPick: false, Team: 1}, // duplicate ok for test? IsTaken - hero 2 twice - second may fail index still
		},
	}
	// Fix: unique bans
	m.PickBans[3].HeroID = 3 // wait 3 already - use only heroes we have
	m.PickBans = []cache.PickBan{
		{Ord: 0, HeroID: 2, IsPick: false, Team: 0},
		{Ord: 1, HeroID: 3, IsPick: false, Team: 1},
		{Ord: 2, HeroID: 4, IsPick: false, Team: 0},
		{Ord: 3, HeroID: 2, IsPick: false, Team: 1},
	}
	// hero 2 banned twice - for eval we still score actions. Use 4 unique: only 4 heroes.
	// Skip full CM — just need hasBan and some actions with hero 1 pick
	m.PickBans = []cache.PickBan{
		{Ord: 0, HeroID: 2, IsPick: false, Team: 0},
		{Ord: 1, HeroID: 3, IsPick: false, Team: 1},
		{Ord: 2, HeroID: 1, IsPick: true, Team: 0},
	}

	// Empty train => step baseline has no counts and falls back to uniform-ish.
	train := []cache.Match{}
	test := []cache.Match{m, m, m, m, m, m, m, m, m, m}
	rep := evalLadder(test, train, test, pred, hm, nil, Options{Bootstrap: 20, TopK: []int{1, 3, 5}})
	if len(rep.Rungs) != 5 {
		t.Fatalf("rungs=%d want 5", len(rep.Rungs))
	}
	if rep.Rungs[1].N == 0 {
		t.Fatal("model scored nothing")
	}
	if out := formatLadder(rep); out == "" || !strings.Contains(out, "personalization ladder") {
		t.Fatalf("format: %q", out)
	}
}
