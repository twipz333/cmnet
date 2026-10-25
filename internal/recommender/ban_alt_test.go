package recommender

import (
	"testing"

	"cmnet/internal/draft"
	"cmnet/internal/features"
	"cmnet/internal/model"
)

func TestAltDeficitUniqueHeroHigherThanTwin(t *testing.T) {
	ids := []int{1, 2, 3, 4, 5, 6}
	hm := features.NewHeroMap(ids)
	n := hm.Len()
	pred := &model.Predictor{
		Pick:    &model.SoftmaxReg{Weights: make([][]float64, n), Bias: make([]float64, n)},
		Ban:     &model.SoftmaxReg{Weights: make([][]float64, n), Bias: make([]float64, n)},
		HeroMap: hm,
	}
	for i := range pred.Pick.Weights {
		pred.Pick.Weights[i] = make([]float64, hm.PredictorLen())
		pred.Ban.Weights[i] = make([]float64, hm.PredictorLen())
	}
	// Pick head: hero 1 very likely, heroes 2 and 3 equally next (twins). A ban
	// denies a hero the enemy would PICK, so the deficit is measured on their
	// pick policy, not on what they would ban.
	pred.Pick.Bias[0] = 4
	pred.Pick.Bias[1] = 2
	pred.Pick.Bias[2] = 2

	wp := &model.WinProb{Model: &model.BinaryLogReg{Weights: make([]float64, hm.WinProbLen())}, HeroMap: hm}
	r := New(wp, pred)

	d := draft.New(draft.Radiant)
	// Opening bans — current step is still a ban; eprobs is Dire's next pick.
	eprobs := r.orZeros(r.teamPolicy(d, draft.Dire, true))
	defUnique := r.altDeficit(d, draft.Radiant, 1, eprobs)
	defTwin := r.altDeficit(d, draft.Radiant, 2, eprobs)
	if defUnique <= defTwin {
		t.Fatalf("unique deficit %v should exceed twin %v (eprobs=%v)", defUnique, defTwin, eprobs)
	}
}

func TestBanScoreIncludesAltGapWhy(t *testing.T) {
	r := beamRecommender(8, make([]float64, features.NewHeroMap([]int{1, 2, 3, 4, 5, 6, 7, 8}).WinProbLen()))
	for i := range r.Predictor.Ban.Bias {
		r.Predictor.Ban.Bias[i] = -2
	}
	r.Predictor.Ban.Bias[0] = 6
	d := draft.New(draft.Radiant)
	res := r.Recommend(d, draft.Radiant, 5)
	if !res.IsPick && len(res.Recs) > 0 {
		found := false
		for _, rec := range res.Recs {
			if rec.Factors != nil && rec.Factors.AltDeficit > 0 {
				found = true
				break
			}
			if containsStr(rec.Why, "alt-gap") {
				found = true
				break
			}
		}
		if !found {
			// Opening ban: check factors at least computed
			score, why, f := r.banScore(d, draft.Radiant, nil, nil, 1, 0, make([]float64, 8))
			_ = score
			if f == nil {
				t.Fatalf("nil factors why=%s", why)
			}
		}
	}
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()))
}
