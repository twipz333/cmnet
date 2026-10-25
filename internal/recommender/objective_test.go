package recommender

import (
	"testing"

	"cmnet/internal/draft"
	"cmnet/internal/model"
)

// The pick score and the strength bar must be one quantity: after we take the
// one-ply recommendation, the strength bar shows exactly the score the pick was
// ranked by. Before, the search used fixed 0.10 lambdas while the bar used the
// fitted stacker, so the two disagreed whenever a stacker was present.
func TestPickScoreEqualsStrengthAfterThePick(t *testing.T) {
	r := beamRecommender(10, randomishWeights(20))
	r.SetBeamConfig(BeamConfig{}) // one-ply: score = strength after the pick
	i1, _ := r.heroMap.Index(1)
	i2, _ := r.heroMap.Index(2)
	r.synergy[i1][i2], r.synergy[i2][i1] = 0.6, 0.6
	r.WithStacker(&model.Stacker{Weights: []float64{1, 3, 2, 0}, Bias: 0.05})

	d := firstPickDraft(t)
	if err := d.Apply(1); err != nil { // Radiant picks hero 1
		t.Fatal(err)
	}
	if err := d.Apply(3); err != nil { // Dire picks hero 3
		t.Fatal(err)
	}
	// Radiant's next action is a ban phase, so score candidate 2 directly.
	score := r.pickScore(draft.Radiant, d.Picks(draft.Radiant), d.Picks(draft.Dire), 2)
	after := &draft.Snapshot{
		RadiantPicks: append(append([]int(nil), d.Picks(draft.Radiant)...), 2),
		DirePicks:    d.Picks(draft.Dire),
		FirstPick:    draft.Radiant,
	}
	got := r.draftStrength(after, draft.Radiant)
	if got == nil || got.OurProb != score {
		t.Fatalf("pick score %v but strength after the pick %+v", score, got)
	}
}

// Every pick score the planner produces is a probability.
func TestPlannerScoresAreProbabilities(t *testing.T) {
	r := beamRecommender(12, randomishWeights(24))
	r.WithStacker(&model.Stacker{Weights: []float64{1, 2, 2, 0}})
	res := r.Recommend(firstPickDraft(t), draft.Radiant, 8)
	if !res.IsPick || len(res.Recs) == 0 {
		t.Fatal("setup: expected pick recommendations")
	}
	for _, rec := range res.Recs {
		if rec.Score < 0 || rec.Score > 1 {
			t.Fatalf("pick score %v outside [0,1]: %+v", rec.Score, rec)
		}
	}
}
