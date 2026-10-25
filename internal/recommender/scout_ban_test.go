package recommender

import (
	"testing"

	"cmnet/internal/blend"
	"cmnet/internal/draft"
)

func enemyRank(res Result, heroID int) int {
	for i, e := range res.EnemyPred {
		if e.HeroID == heroID {
			return i
		}
	}
	return -1
}

// On an enemy BAN step, their own comfort heroes (aff2) and the heroes they
// PICKED this event (aff3) must not be pushed up: teams do not ban what they want
// to play. What they banned this event and what WE are comfortable on must be.
func TestScoutBlendForEnemyBansUsesBanEvidence(t *testing.T) {
	r := zeroRecommender([]int{1, 2, 3, 4, 5, 6})
	d := draft.New(draft.Dire)            // Dire first: steps 0-1 are Dire (enemy) bans
	aff2 := []float64{0, 0, 0, 0, 0, 9}   // enemy players love hero 6
	aff3 := []float64{0, 0, 0, 0, 9, 0}   // enemy picked hero 5 in the event
	bans := []float64{0, 0, 9, 0, 0, 0}   // enemy banned hero 3 in the event
	allies := []float64{0, 9, 0, 0, 0, 0} // we are comfortable on hero 2
	r.WithScout(aff2, aff3, 2, blend.DefaultWeights()).WithScoutBans(bans).WithAllies(allies)

	res := r.Recommend(d, draft.Radiant, 6)
	if res.OurTurn || res.EnemyStep == nil || res.EnemyStep.IsPick {
		t.Fatalf("setup: want an enemy ban step, got ourTurn=%v step=%+v", res.OurTurn, res.EnemyStep)
	}
	top2 := map[int]bool{}
	for _, e := range res.EnemyPred[:2] {
		top2[e.HeroID] = true
	}
	if !top2[2] || !top2[3] {
		t.Fatalf("enemy ban prediction top-2 = %+v, want heroes 2 (our comfort) and 3 (their event ban)", res.EnemyPred[:2])
	}
	for _, e := range res.EnemyPred {
		if (e.HeroID == 5 || e.HeroID == 6) && enemyRank(res, e.HeroID) < 2 {
			t.Fatalf("hero %d (pick evidence) ranks %d among enemy bans", e.HeroID, enemyRank(res, e.HeroID))
		}
		if e.HeroID == 3 && e.Source != "event" {
			t.Errorf("hero 3 source = %q, want event", e.Source)
		}
		if e.HeroID == 2 && e.Source != "player" {
			t.Errorf("hero 2 source = %q, want player", e.Source)
		}
	}
}

// On an enemy PICK step the pick evidence applies as before.
func TestScoutBlendForEnemyPicksUsesPickEvidence(t *testing.T) {
	r := zeroRecommender([]int{1, 2, 3, 4, 5, 6})
	r.WithScout([]float64{0, 0, 0, 0, 0, 9}, nil, 0, blend.DefaultWeights()).
		WithScoutBans([]float64{0, 0, 9, 0, 0, 0})
	snap := &draft.Snapshot{
		CurrentTeam: draft.Dire, CurrentIsPick: true, FirstPick: draft.Radiant,
		RadiantBans: []int{101, 102, 103}, DireBans: []int{104, 105, 106, 107},
		RadiantPicks: []int{108},
	}
	res := r.Recommend(snap, draft.Radiant, 6)
	if res.EnemyStep == nil || !res.EnemyStep.IsPick {
		t.Fatalf("setup: want an enemy pick step, got %+v", res.EnemyStep)
	}
	if res.EnemyPred[0].HeroID != 6 {
		t.Fatalf("top enemy pick = %+v, want hero 6 (their comfort)", res.EnemyPred[0])
	}
}
