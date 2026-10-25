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
	if len(wpWeights) != hm.WinProbLen() {
		padded := make([]float64, hm.WinProbLen())
		copy(padded, wpWeights)
		wpWeights = padded
	}
	wp := &model.WinProb{Model: &model.BinaryLogReg{Weights: wpWeights, Bias: 0}, HeroMap: hm}
	pred := &model.Predictor{
		Pick:    &model.SoftmaxReg{Weights: make([][]float64, n), Bias: make([]float64, n)},
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
	// Advance through the opening ban phase so the next step is a pick.
	for i := 0; i < openingBans; i++ {
		if err := d.Apply(900 + i); err != nil { // ids outside the model's hero set
			t.Fatalf("apply ban %d: %v", i, err)
		}
	}
	return d
}

func TestBeamEnabledByMatrices(t *testing.T) {
	r := beamRecommender(8, make([]float64, features.NewHeroMap([]int{1, 2, 3, 4, 5, 6, 7, 8}).WinProbLen()))
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
	r.SetBeamConfig(BeamConfig{PruneN: 10, Depth: 2, Width: 2})
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

func TestEnemyPolicyChangesRanking(t *testing.T) {
	// Win-prob strongly prefers high ids for Radiant; predictor pushes Dire toward id 1.
	n := 8
	w := make([]float64, features.NewHeroMap([]int{1, 2, 3, 4, 5, 6, 7, 8}).WinProbLen())
	for i := 0; i < n; i++ {
		w[i] = float64(i) * 0.4 // hero 8 >> hero 1 for Radiant presence
	}
	r := beamRecommender(n, w)
	for i := range r.Predictor.Pick.Bias {
		r.Predictor.Pick.Bias[i] = -3
	}
	r.Predictor.Pick.Bias[0] = 8 // hero 1 dominates enemy policy
	r.Predictor.Ban = r.Predictor.Pick

	cfg := DefaultBeamConfig()
	cfg.Depth = 2
	cfg.Width = 3
	cfg.PruneN = 8
	cfg.EnemyTopK = 3
	cfg.MassFloor = 0.5

	d := firstPickDraft(t)
	cfg.EnemyPolicy = true
	r.SetBeamConfig(cfg)
	withPol := r.Recommend(d, draft.Radiant, 8)

	cfg.EnemyPolicy = false
	r.SetBeamConfig(cfg)
	greedy := r.Recommend(d, draft.Radiant, 8)

	if len(withPol.Recs) == 0 || len(greedy.Recs) == 0 {
		t.Fatal("empty recommendations")
	}
	same := true
	for i := 0; i < len(withPol.Recs) && i < len(greedy.Recs); i++ {
		if withPol.Recs[i].HeroID != greedy.Recs[i].HeroID {
			same = false
			break
		}
	}
	if same && withPol.Recs[0].HeroID == greedy.Recs[0].HeroID &&
		withPol.Recs[0].Score == greedy.Recs[0].Score {
		t.Fatalf("EnemyPolicy did not change ranking/scores:\n pol=%v\n greedy=%v",
			withPol.Recs, greedy.Recs)
	}
}

func TestExpectimaxDeterministic(t *testing.T) {
	r := beamRecommender(10, randomishWeights(features.NewHeroMap([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}).WinProbLen()))
	cfg := DefaultBeamConfig()
	cfg.Depth = 2
	cfg.EnemyPolicy = true
	r.SetBeamConfig(cfg)
	d := firstPickDraft(t)
	pool := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	a := r.beamPickScore(d, draft.Radiant, 3, pool)
	b := r.beamPickScore(d, draft.Radiant, 3, pool)
	if a != b {
		t.Fatalf("non-deterministic expectimax: %v vs %v", a, b)
	}
}

func TestEnemyMassTailUsesBaseline(t *testing.T) {
	n := 6
	w := make([]float64, features.NewHeroMap([]int{1, 2, 3, 4, 5, 6}).WinProbLen())
	r := beamRecommender(n, w)
	// Uniform-ish predictor; tiny TopK leaves most mass for the baseline term.
	for i := range r.Predictor.Pick.Bias {
		r.Predictor.Pick.Bias[i] = 0
	}
	r.Predictor.Ban = r.Predictor.Pick

	cfg := DefaultBeamConfig()
	cfg.Depth = 1
	cfg.Width = 2
	cfg.PruneN = 6
	cfg.EnemyPolicy = true
	cfg.EnemyTopK = 1
	cfg.MassFloor = 0.99 // force stop only by TopK=1
	r.SetBeamConfig(cfg)

	d := firstPickDraft(t)
	pool := []int{1, 2, 3, 4, 5, 6}
	// After our pick, next CM steps include enemy actions; score must be finite.
	got := r.beamPickScore(d, draft.Radiant, 1, pool)
	if got != model.RoundScore(got) {
		t.Fatalf("unrounded score %v", got)
	}
	if got < 0 || got > 1 {
		t.Fatalf("score %v out of [0,1]", got)
	}
}
