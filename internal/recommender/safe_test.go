package recommender

import (
	"reflect"
	"testing"

	"cmnet/internal/draft"
)

// safeDraft advances a fresh draft through the opening bans and the first two
// picks, so both sides hold a hero and threat/fit are actually computable.
func safeDraft(t *testing.T) *draft.DraftState {
	t.Helper()
	d := draft.New(draft.Radiant)
	// The opening ban phase (heroes outside the model set), then Radiant and Dire
	// each pick one (7.34+: F pick, S pick).
	ids := []int{}
	for i := 0; i < openingBans; i++ {
		ids = append(ids, 901+i)
	}
	for _, id := range append(ids, 1, 2) {
		if err := d.Apply(id); err != nil {
			t.Fatalf("apply %d: %v", id, err)
		}
	}
	return d
}

func TestThreatDecompositionZeroWithoutMatrices(t *testing.T) {
	r := beamRecommender(10, randomishWeights(20))
	plain := New(r.WinProb, r.Predictor) // no matrices attached

	threat, fit, danger := plain.threatOf(3, []int{1}, []int{2})
	if threat != 0 || fit != 0 || danger != 0 {
		t.Errorf("without matrices got (%v, %v, %v), want all zero", threat, fit, danger)
	}
	if got := plain.safeToLet(safeDraft(t), draft.Radiant, make([]float64, 10), DefaultTopN); got != nil {
		t.Errorf("safeToLet without matrices = %v, want nil", got)
	}
}

// TestThreatSignReflectsMatchup: a hero that beats our picks reports positive
// threat, one that loses to them reports negative.
func TestThreatSignReflectsMatchup(t *testing.T) {
	r := beamRecommender(10, randomishWeights(20))
	// Hero 5 beats hero 1 (our pick); hero 6 loses to it.
	i5, _ := r.heroMap.Index(5)
	i6, _ := r.heroMap.Index(6)
	i1, _ := r.heroMap.Index(1)
	r.counter[i5][i1] = 0.60
	r.counter[i6][i1] = 0.40

	strong, _, dStrong := r.threatOf(5, []int{1}, nil)
	weak, _, dWeak := r.threatOf(6, []int{1}, nil)

	if strong <= 0 {
		t.Errorf("threat of a counter hero = %v, want > 0", strong)
	}
	if weak >= 0 {
		t.Errorf("threat of a countered hero = %v, want < 0", weak)
	}
	if dStrong <= dWeak {
		t.Errorf("danger not ordered: strong %v <= weak %v", dStrong, dWeak)
	}
}

// TestSafeToLetPicksHarmlessHeroes: with one clearly threatening hero and several
// harmless ones, the threatening hero must not appear in the "don't fear" list.
func TestSafeToLetPicksHarmlessHeroes(t *testing.T) {
	r := beamRecommender(12, randomishWeights(24))
	d := safeDraft(t)
	ourPick := 1 // Radiant's hero from safeDraft

	oi, _ := r.heroMap.Index(ourPick)
	scary, _ := r.heroMap.Index(7)
	for _, id := range []int{8, 9, 10, 11, 12} {
		hi, _ := r.heroMap.Index(id)
		r.counter[hi][oi] = 0.45 // harmless: loses to our pick
	}
	r.counter[scary][oi] = 0.65 // dangerous

	probs := make([]float64, r.heroMap.Len())
	for i := range probs {
		probs[i] = 1.0 / float64(len(probs))
	}

	safe := r.safeToLet(d, draft.Radiant, probs, DefaultTopN)
	if len(safe) == 0 {
		t.Fatal("no safe heroes found, want the harmless ones")
	}
	for _, e := range safe {
		if e.HeroID == 7 {
			t.Errorf("hero 7 counters us (0.65) but was listed as safe: %+v", e)
		}
		if e.Threat > 0 {
			t.Errorf("hero %d listed as safe with positive threat %v", e.HeroID, e.Threat)
		}
	}
}

// TestSafeToLetDeterministic guards §6: two runs over the same state must agree
// exactly, including ordering.
func TestSafeToLetDeterministic(t *testing.T) {
	r := beamRecommender(16, randomishWeights(32))
	d := safeDraft(t)
	probs := make([]float64, r.heroMap.Len())
	for i := range probs {
		probs[i] = float64((i*17)%7+1) / 100
	}
	a := r.safeToLet(d, draft.Radiant, probs, DefaultTopN)
	b := r.safeToLet(d, draft.Radiant, probs, DefaultTopN)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("safeToLet non-deterministic:\n a=%+v\n b=%+v", a, b)
	}
}

// TestSafeToLetExcludesTakenHeroes: a hero already on the board can never be a
// "let them have it" suggestion.
func TestSafeToLetExcludesTakenHeroes(t *testing.T) {
	r := beamRecommender(12, randomishWeights(24))
	d := safeDraft(t)
	probs := make([]float64, r.heroMap.Len())
	for i := range probs {
		probs[i] = 0.05
	}
	for _, e := range r.safeToLet(d, draft.Radiant, probs, DefaultTopN) {
		if d.IsTaken(e.HeroID) {
			t.Errorf("hero %d is already taken but listed as safe", e.HeroID)
		}
	}
}

// TestEnemyPredCarriesThreat: the prediction panel must receive the decomposition
// too, otherwise the front end has nothing to explain the ban ranking with.
func TestEnemyPredCarriesThreat(t *testing.T) {
	r := beamRecommender(12, randomishWeights(24))
	d := safeDraft(t)
	oi, _ := r.heroMap.Index(1)
	hi, _ := r.heroMap.Index(9)
	r.counter[hi][oi] = 0.70

	res := r.Recommend(d, draft.Radiant, DefaultTopN)
	var found bool
	for _, e := range res.EnemyPred {
		if e.HeroID == 9 {
			found = true
			if e.Threat <= 0 {
				t.Errorf("hero 9 threat = %v, want > 0", e.Threat)
			}
			if e.Danger <= 0 {
				t.Errorf("hero 9 danger = %v, want > 0", e.Danger)
			}
		}
	}
	if !found {
		t.Skip("hero 9 not in top-N prediction; nothing to assert")
	}
}
