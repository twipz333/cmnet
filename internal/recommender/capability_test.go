package recommender_test

import (
	"testing"

	"cmnet/internal/capability"
	"cmnet/internal/draft"
	"cmnet/internal/features"
	"cmnet/internal/model"
	"cmnet/internal/recommender"
	"cmnet/internal/scout"
)

func TestCapabilityPenaltyChangesScore(t *testing.T) {
	ids := []int{1, 2, 3, 4, 5, 6, 7, 8}
	hm := features.NewHeroMap(ids)
	w := make([]float64, hm.WinProbLen())
	wp := &model.WinProb{Model: &model.BinaryLogReg{Weights: w, Bias: 0}, HeroMap: hm}
	pred := &model.Predictor{
		Pick:    &model.SoftmaxReg{Weights: make([][]float64, len(ids)), Bias: make([]float64, len(ids))},
		HeroMap: hm,
	}
	for i := range pred.Pick.Weights {
		pred.Pick.Weights[i] = make([]float64, hm.PredictorLen())
	}
	pred.Ban = pred.Pick
	n := len(ids)
	syn := make([][]float64, n)
	cnt := make([][]float64, n)
	for i := range syn {
		syn[i] = make([]float64, n)
		cnt[i] = make([]float64, n)
		for j := range syn[i] {
			syn[i][j], cnt[i][j] = 0.5, 0.5
		}
	}
	r := recommender.New(wp, pred).WithMatrices(syn, cnt)
	cfg := recommender.DefaultBeamConfig()
	cfg.Depth = 1
	cfg.EnemyPolicy = false
	r.SetBeamConfig(cfg)

	l2 := &scout.Layer2{Players: []scout.PlayerAffinity{
		{AccountID: 1, Nickname: "carry", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 1, Games: 40}}},
		{AccountID: 2, Nickname: "mid", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 2, Games: 40}}},
		{AccountID: 3, Nickname: "off", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 3, Games: 40}}},
		{AccountID: 4, Nickname: "soft", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 4, Games: 40}}},
		{AccountID: 5, Nickname: "hard", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 5, Games: 40}}},
	}}
	rost := capability.FromAllies(l2, ids, 0.1)
	if rost == nil {
		t.Fatal("roster")
	}

	d := draft.New(draft.Radiant)
	for i := 0; i < draft.DefaultOrder().Phases()[0]; i++ {
		_ = d.Apply(900 + i)
	}
	base := r.Recommend(d, draft.Radiant, 5)
	r.WithCapability(rost, 0.5) // strong lambda so effect is visible
	with := r.Recommend(d, draft.Radiant, 5)
	if len(base.Recs) == 0 || len(with.Recs) == 0 {
		t.Fatal("empty")
	}
	// At least one score or order should differ, or a player tag appears.
	changed := false
	for i := range with.Recs {
		if with.Recs[i].Player != "" {
			changed = true
		}
		if i < len(base.Recs) && (with.Recs[i].Score != base.Recs[i].Score || with.Recs[i].HeroID != base.Recs[i].HeroID) {
			changed = true
		}
	}
	if !changed {
		t.Fatalf("capability had no effect:\n base=%v\n with=%v", base.Recs, with.Recs)
	}
}
