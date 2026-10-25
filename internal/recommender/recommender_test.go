package recommender

import (
	"reflect"
	"testing"

	"cmnet/internal/draft"
	"cmnet/internal/features"
	"cmnet/internal/model"
)

func testRecommender(wpWeights []float64, wpBias float64) *Recommender {
	hm := features.NewHeroMap([]int{1, 2, 3})
	if len(wpWeights) != hm.WinProbLen() {
		padded := make([]float64, hm.WinProbLen())
		copy(padded, wpWeights)
		wpWeights = padded
	}
	wp := &model.WinProb{
		Model:   &model.BinaryLogReg{Weights: wpWeights, Bias: wpBias},
		HeroMap: hm,
	}
	pred := &model.Predictor{
		Pick: &model.SoftmaxReg{
			Weights: make([][]float64, hm.Len()), // zero weights -> uniform
			Bias:    make([]float64, hm.Len()),
		},
		HeroMap: hm,
	}
	for i := range pred.Pick.Weights {
		pred.Pick.Weights[i] = make([]float64, hm.PredictorLen())
	}
	pred.Ban = pred.Pick
	return New(wp, pred)
}

// With zero win-prob weights every candidate ties at 0.5; the deterministic
// tie-break must order them by hero_id ascending.
func TestRecommendTieBreakByHeroID(t *testing.T) {
	r := testRecommender(make([]float64, 6), 0)
	d := draft.New(draft.Radiant)
	// Advance to the first PICK step (the opening ban phase).
	for i := 0; i < openingBans; i++ {
		_ = d.Apply(100 + i) // dummy heroes outside the {1,2,3} map
	}
	res := r.Recommend(d, draft.Radiant, 8)
	if !res.OurTurn || !res.IsPick {
		t.Fatalf("expected our pick turn, got OurTurn=%v IsPick=%v", res.OurTurn, res.IsPick)
	}
	gotIDs := make([]int, len(res.Recs))
	for i, rec := range res.Recs {
		gotIDs[i] = rec.HeroID
	}
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(gotIDs, want) {
		t.Errorf("tie-break order = %v, want %v", gotIDs, want)
	}
}

// Same input must produce identical output across repeated calls (§6).
func TestRecommendDeterministic(t *testing.T) {
	r := testRecommender([]float64{0.7, -0.3, 0.1, -0.2, 0.4, 0.0}, 0.05)
	mk := func() Result {
		d := draft.New(draft.Radiant)
		for i := 0; i < openingBans; i++ {
			_ = d.Apply(100 + i)
		}
		return r.Recommend(d, draft.Radiant, 8)
	}
	a, b := mk(), mk()
	if !reflect.DeepEqual(a, b) {
		t.Errorf("non-deterministic result:\n a=%+v\n b=%+v", a, b)
	}
}

func TestRecommendCompleteDraftEmptyRecs(t *testing.T) {
	r := testRecommender(make([]float64, 6), 0)
	d := draft.New(draft.Radiant)
	for i := 0; i < len(d.Seq); i++ {
		_ = d.Apply(200 + i)
	}
	res := r.Recommend(d, draft.Radiant, 8)
	if len(res.Recs) != 0 {
		t.Errorf("complete draft should have no recs, got %d", len(res.Recs))
	}
	// The win-prob read must survive after the draft completes (CM in-game): there
	// is no current step, but the full lineup is still on the board.
	if res.Strength == nil {
		t.Error("complete draft should still report draft strength, got nil")
	}
}

// The draft-strength readout must fold in synergy/counter lift when matrices are
// present (eval showed the raw win-prob is near-random on CM), stay a no-op without
// them, and remain a valid probability in [0,1].
func TestDraftStrengthMatrixBlend(t *testing.T) {
	r := testRecommender(make([]float64, 6), 0) // zero win-prob => 0.5 for any board
	// Board: radiant has heroes 1,2 (dense 0,1); dire has hero 3 (dense 2).
	view := &draft.Snapshot{RadiantPicks: []int{1, 2}, DirePicks: []int{3}, Done: true}

	base := r.Recommend(view, draft.Radiant, 8).Strength
	if base == nil {
		t.Fatal("nil strength")
	}
	if base.RadiantProb != 0.5 {
		t.Fatalf("pure win-prob strength = %v, want 0.5", base.RadiantProb)
	}

	// Matrices strongly favouring radiant: teammate synergy 1-2 and both radiant
	// heroes countering dire's hero 3, everything else neutral (0.5).
	n := 3
	syn := make([][]float64, n)
	cnt := make([][]float64, n)
	for i := 0; i < n; i++ {
		syn[i] = make([]float64, n)
		cnt[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			syn[i][j], cnt[i][j] = 0.5, 0.5
		}
	}
	syn[0][1], syn[1][0] = 0.7, 0.7 // radiant pair synergy > neutral
	cnt[0][2], cnt[1][2] = 0.7, 0.7 // radiant heroes counter dire hero 3
	r.WithMatrices(syn, cnt)

	blended := r.Recommend(view, draft.Radiant, 8).Strength
	if blended.RadiantProb <= 0.5 {
		t.Errorf("blended strength = %v, want > 0.5 (matrices favour radiant)", blended.RadiantProb)
	}
	if blended.RadiantProb < 0 || blended.RadiantProb > 1 {
		t.Errorf("strength out of [0,1]: %v", blended.RadiantProb)
	}
	// Dire's perspective must mirror radiant's.
	if got := r.Recommend(view, draft.Dire, 8).Strength.OurProb; got != model.RoundScore(1-blended.RadiantProb) {
		t.Errorf("dire OurProb = %v, want %v", got, model.RoundScore(1-blended.RadiantProb))
	}
}

func TestRecommendScoresDescending(t *testing.T) {
	r := testRecommender([]float64{0.7, -0.3, 0.1, -0.2, 0.4, 0.0}, 0.05)
	d := draft.New(draft.Radiant)
	for i := 0; i < openingBans; i++ {
		_ = d.Apply(100 + i)
	}
	res := r.Recommend(d, draft.Radiant, 8)
	for i := 1; i < len(res.Recs); i++ {
		if res.Recs[i-1].Score < res.Recs[i].Score {
			t.Errorf("recs not sorted descending at %d: %v < %v", i, res.Recs[i-1].Score, res.Recs[i].Score)
		}
	}
}
