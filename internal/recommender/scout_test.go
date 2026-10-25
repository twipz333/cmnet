package recommender

import (
	"testing"

	"cmnet/internal/blend"
	"cmnet/internal/draft"
)

// With scout blending, a hero strongly favored by layer-3 (team this event) must
// rank higher in the enemy prediction than it does on layer 1 alone.
func TestScoutBlendShiftsEnemyPrediction(t *testing.T) {
	r := beamRecommender(6, make([]float64, 12)) // uniform predictor prior

	base := r.Recommend(firstPickDraft(t), draft.Radiant, 6)

	// Layer-3 vector favoring hero id 3 (dense index 2), observed in 2 event drafts.
	aff3 := []float64{0, 0, 5, 0, 0, 0}
	r.WithScout(nil, aff3, 2, blend.DefaultWeights())
	blended := r.Recommend(firstPickDraft(t), draft.Radiant, 6)

	rank := func(res Result, heroID int) int {
		for i, e := range res.EnemyPred {
			if e.HeroID == heroID {
				return i
			}
		}
		return -1
	}
	if rank(blended, 3) >= rank(base, 3) && rank(base, 3) != 0 {
		t.Errorf("hero 3 should rank higher after layer-3 boost: base %d, blended %d",
			rank(base, 3), rank(blended, 3))
	}
	// Provenance must tag the boosted hero as coming from the event layer.
	for _, e := range blended.EnemyPred {
		if e.HeroID == 3 && e.Source != "event" {
			t.Errorf("hero 3 source = %q, want event", e.Source)
		}
	}
}

func TestScoutDegradationNilLeavesLayer1(t *testing.T) {
	r := beamRecommender(6, make([]float64, 12))
	r.WithScout(nil, nil, 0, blend.DefaultWeights()) // nothing to blend
	if r.scoutEnabled {
		t.Error("scout must stay disabled when both affinity vectors are nil")
	}
}

func TestEnemyPredSourceMetaWithoutScout(t *testing.T) {
	r := beamRecommender(6, make([]float64, 12))
	res := r.Recommend(firstPickDraft(t), draft.Radiant, 6)
	for _, e := range res.EnemyPred {
		if e.Source != "meta" {
			t.Errorf("source = %q, want meta without scout", e.Source)
		}
	}
}
