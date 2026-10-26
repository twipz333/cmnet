package recommender

import (
	"testing"

	"cmnet/internal/draft"
	"cmnet/internal/roles"
)

// rolePriorOf builds a prior where each hero plays exactly one position.
func rolePriorOf(pos map[int]int) *roles.Prior {
	p := &roles.Prior{Prob: map[int][roles.NumPositions]float64{}}
	for h, ps := range pos {
		var row [roles.NumPositions]float64
		row[ps-1] = 1
		p.Prob[h] = row
	}
	return p
}

// Heroes 1-4 are carries, 5-8 mids, 9 offlane, 10 a four, 11 a five.
var testRoles = map[int]int{1: 1, 2: 1, 3: 1, 4: 1, 5: 2, 6: 2, 7: 2, 8: 2, 9: 3, 10: 4, 11: 5}

func roleRecommender(t *testing.T) *Recommender {
	t.Helper()
	ids := make([]int, 0, len(testRoles))
	for h := 1; h <= len(testRoles); h++ {
		ids = append(ids, h)
	}
	r := zeroRecommender(ids)
	r.WithRolePrior(rolePriorOf(testRoles))
	return r
}

// The bug report: the top recommendations were all for a slot we had already
// filled. With a carry on the board, no other carry may be recommended.
func TestNoRecommendationForAFilledPosition(t *testing.T) {
	r := roleRecommender(t)
	// Make carries the model's favourite by far.
	for _, h := range []int{2, 3, 4} {
		i, _ := r.heroMap.Index(h)
		r.WinProb.Model.Weights[i] = 3
	}
	snap := &draft.Snapshot{
		RadiantPicks: []int{1}, DirePicks: []int{9},
		CurrentTeam: draft.Radiant, CurrentIsPick: true, FirstPick: draft.Radiant,
		RadiantBans: []int{101, 102, 103, 104}, DireBans: []int{105, 106, 107},
	}
	res := r.Recommend(snap, draft.Radiant, 5)
	if !res.OurTurn || !res.IsPick || len(res.Recs) == 0 {
		t.Fatalf("setup: want our pick with recommendations, got %+v", res)
	}
	for _, rec := range res.Recs {
		if testRoles[rec.HeroID] == 1 {
			t.Fatalf("recommended carry %d while our carry slot is taken: %+v", rec.HeroID, res.Recs)
		}
		if rec.Role == 1 || rec.Role == 0 {
			t.Fatalf("rec %+v has role %d, want an open position", rec, rec.Role)
		}
	}
}

// The enemy does not pick a second hero for a position they hold either.
func TestEnemyPredictionRespectsTheirOpenRoles(t *testing.T) {
	r := roleRecommender(t)
	i2, _ := r.heroMap.Index(2)
	// The meta favours carry 2 twentyfold. Role fit must outweigh that; it is
	// floored (enemyFitFloor) so that a hero the model likes far more than that
	// — a genuine flex pick the prior underrates — can still surface.
	r.Predictor.Pick.Bias[i2] = 3
	snap := &draft.Snapshot{
		DirePicks: []int{1}, RadiantPicks: []int{9},
		CurrentTeam: draft.Dire, CurrentIsPick: true, FirstPick: draft.Radiant,
		RadiantBans: []int{101, 102, 103}, DireBans: []int{104, 105, 106, 107},
	}
	res := r.Recommend(snap, draft.Radiant, 5)
	for i, e := range res.EnemyPred {
		if e.HeroID == 2 && i == 0 {
			t.Fatalf("enemy predicted to pick carry 2 with their carry slot taken: %+v", res.EnemyPred)
		}
	}
}

// With several positions open, the list must not be five takes on one slot.
func TestRecommendationsSpreadAcrossOpenRoles(t *testing.T) {
	r := roleRecommender(t)
	for _, h := range []int{5, 6, 7, 8} { // mids score best
		i, _ := r.heroMap.Index(h)
		r.WinProb.Model.Weights[i] = 3
	}
	d := firstPickDraftOf(t, r)
	res := r.Recommend(d, draft.Radiant, 5)
	mids := 0
	for _, rec := range res.Recs {
		if rec.Role == 2 {
			mids++
		}
	}
	if mids > maxPerRole {
		t.Fatalf("%d mid recommendations in the top 5, want at most %d: %+v", mids, maxPerRole, res.Recs)
	}
	if len(res.Recs) == 0 || res.Recs[0].Role != 2 {
		t.Fatalf("best recommendation should still be the strongest (a mid): %+v", res.Recs)
	}
}

func firstPickDraftOf(t *testing.T, r *Recommender) *draft.DraftState {
	t.Helper()
	d := draft.NewWithOrder(draft.Radiant, r.CMOrder())
	for i := 0; i < openingBans; i++ {
		if err := d.Apply(900 + i); err != nil {
			t.Fatal(err)
		}
	}
	return d
}
