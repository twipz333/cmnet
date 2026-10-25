package recommender

import (
	"reflect"
	"testing"

	"cmnet/internal/draft"
	"cmnet/internal/features"
	"cmnet/internal/model"
)

// beamRecommender builds a recommender over heroes 1..n with given win-prob
// weights and identity-ish synergy/counter matrices, with beam enabled.
func beamRecommender(n int, wpWeights []float64) *Recommender {
	ids := make([]int, n)
	for i := range ids {
		ids[i] = i + 1
	}
	hm := features.NewHeroMap(ids)
	wp := &model.WinProb{Model: &model.BinaryLogReg{Weights: wpWeights, Bias: 0}, HeroMap: hm}
	pred := &model.Predictor{
		Pick: &model.SoftmaxReg{Weights: make([][]float64, n), Bias: make([]float64, n)},
		HeroMap: hm,
	}
	for i := range pred.Pick.Weights {
		pred.Pick.Weights[i] = make([]float64, hm.PredictorLen())
	}
	pred.Ban = pred.Pick

	syn := make([][]float64, n)
	cnt := make([][]float64, n)
	for i := range syn {
		syn[i] = make([]float64, n)
		cnt[i] = make([]float64, n)
		for j := range syn[i] {
			syn[i][j] = 0.5
			cnt[i][j] = 0.5
		}
	}
	r := New(wp, pred).WithMatrices(syn, cnt)
	return r
}

func firstPickDraft(t *testing.T) *draft.DraftState {
	t.Helper()
	d := draft.New(draft.Radiant)
	// Advance through the four opening bans so the next step is a pick.
	for i := 0; i < 4; i++ {
		if err := d.Apply(900 + i); err != nil { // ids outside the model's hero set
			t.Fatalf("apply ban %d: %v", i, err)
		}
	}
	return d
}

func TestBeamEnabledByMatrices(t *testing.T) {
	r := beamRecommender(8, make([]float64, 16))
	if !r.beamEnabled() {
		t.Fatal("beam should be enabled when matrices are attached")
	}
	plain := New(r.WinProb, r.Predictor)
	if plain.beamEnabled() {
		t.Error("plain recommender (no matrices) must not enable beam")
	}
}

func TestBeamRecommendDeterministic(t *testing.T) {
	r := beamRecommender(12, randomishWeights(24))
	mk := func() Result { return r.Recommend(firstPickDraft(t), draft.Radiant, 8) }
	a, b := mk(), mk()
	if !reflect.DeepEqual(a, b) {
		t.Errorf("beam recommend non-deterministic:\n a=%+v\n b=%+v", a, b)
	}
}

func TestBeamPruningLimitsRecs(t *testing.T) {
	r := beamRecommender(40, randomishWeights(80))
	r.SetBeamConfig(BeamConfig{PruneN: 10, Depth: 2, Width: 2, LambdaSyn: 0.1, LambdaCnt: 0.1})
	res := r.Recommend(firstPickDraft(t), draft.Radiant, 100)
	// Only the pruned meta pool (10) can appear as recommendations.
	if len(res.Recs) > 10 {
		t.Errorf("recs = %d, want <= PruneN (10)", len(res.Recs))
	}
}

func TestBeamScoresDescending(t *testing.T) {
	r := beamRecommender(12, randomishWeights(24))
	res := r.Recommend(firstPickDraft(t), draft.Radiant, 8)
	for i := 1; i < len(res.Recs); i++ {
		if res.Recs[i-1].Score < res.Recs[i].Score {
			t.Errorf("not descending at %d: %v < %v", i, res.Recs[i-1].Score, res.Recs[i].Score)
		}
	}
}

// randomishWeights returns deterministic non-trivial weights.
func randomishWeights(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = float64((i*37)%11-5) * 0.1
	}
	return w
}
