package recommender

import (
	"reflect"
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/draft"
	"cmnet/internal/features"
	"cmnet/internal/model"
	"cmnet/internal/train"
)

// zeroPredictor is a predictor over ids whose every weight is zero (uniform), so a
// test can switch on exactly the feature it is about.
func zeroPredictor(ids []int) *model.Predictor {
	hm := features.NewHeroMap(ids)
	n := hm.Len()
	head := func() *model.SoftmaxReg {
		h := &model.SoftmaxReg{Weights: make([][]float64, n), Bias: make([]float64, n)}
		for i := range h.Weights {
			h.Weights[i] = make([]float64, hm.PredictorLen())
		}
		return h
	}
	return &model.Predictor{Pick: head(), Ban: head(), HeroMap: hm}
}

func zeroRecommender(ids []int) *Recommender {
	pred := zeroPredictor(ids)
	wp := &model.WinProb{Model: &model.BinaryLogReg{Weights: make([]float64, pred.HeroMap.WinProbLen())}, HeroMap: pred.HeroMap}
	return New(wp, pred)
}

// The guard against train/serve skew: replay a full draft through the live board
// and check that, at every step, the predictor input the advisor builds is
// byte-identical to the row training built for the same step.
func TestLivePredictorInputMatchesTrainingRows(t *testing.T) {
	ids := make([]int, 30)
	for i := range ids {
		ids[i] = i + 1
	}
	r := zeroRecommender(ids)
	hm := r.heroMap

	for _, fp := range []draft.Team{draft.Radiant, draft.Dire} {
		d := draft.New(fp)
		m := cache.Match{ID: 1, StartTime: 1, Source: cache.SourcePublicCM}
		var live [][]float64
		for ord := 0; ; ord++ {
			step, ok := d.Current()
			if !ok {
				break
			}
			hero := ord + 1
			live = append(live, hm.EncodePredictor(predictorState(boardOf(d), step.Team, step.Action, boardCursor(d))))
			m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: hero, IsPick: step.Action == draft.Pick, Team: int(step.Team)})
			if err := d.Apply(hero); err != nil {
				t.Fatal(err)
			}
		}
		s := train.BuildSamples([]cache.Match{m}, hm, -1, train.SampleOptions{})
		var rows [][]float64
		pi, bi := 0, 0
		for _, pb := range m.PickBans { // training splits rows by head; re-interleave
			if pb.IsPick {
				rows = append(rows, s.Pick[pi].X)
				pi++
			} else {
				rows = append(rows, s.Ban[bi].X)
				bi++
			}
		}
		if len(rows) != len(live) {
			t.Fatalf("fp=%v: %d training rows vs %d live states", fp, len(rows), len(live))
		}
		for i := range rows {
			if !reflect.DeepEqual(rows[i], live[i]) {
				t.Fatalf("fp=%v step %d: live predictor input differs from the training row", fp, i)
			}
		}
	}
}

// When Dire acts, "the actor's picks" are Dire's. The weight below says "an actor
// who already holds hero 1 takes hero 2"; Radiant holds hero 1, so Dire's pick
// must not be pulled to hero 2. Before the fix the live path always encoded
// Radiant as the actor and did exactly that.
func TestEnemyPredUsesTheActingSide(t *testing.T) {
	r := zeroRecommender([]int{1, 2, 3, 4})
	hm := r.heroMap
	i1, _ := hm.Index(1)
	i2, _ := hm.Index(2)
	r.Predictor.Pick.Weights[i2][0*hm.Len()+i1] = 5 // actor-picks block

	snap := &draft.Snapshot{
		RadiantPicks: []int{1}, CurrentTeam: draft.Dire, CurrentIsPick: true, FirstPick: draft.Radiant,
		RadiantBans: []int{101, 102, 103}, DireBans: []int{104, 105, 106, 107},
	}
	res := r.Recommend(snap, draft.Radiant, 8)
	for _, e := range res.EnemyPred {
		if e.HeroID == 2 && e.Prob > 0.5 {
			t.Fatalf("P(hero 2) = %v: Dire's pick was predicted from Radiant's lineup", e.Prob)
		}
	}
}

// On our turn the enemy panel must show the ENEMY's next action, not ours.
func TestEnemyPredOnOurTurnIsTheirNextStep(t *testing.T) {
	r := zeroRecommender([]int{1, 2, 3, 4, 5})
	hm := r.heroMap
	i5, _ := hm.Index(5)
	// Only the second-pick seat favours hero 5: +3 bias, -6 when the actor drafts
	// first. Layout tail: pick/ban flag, "actor drafts first" flag, 4 counts.
	firstFlag := hm.PredictorLen() - 5
	r.Predictor.Ban.Bias[i5] = 3
	r.Predictor.Ban.Weights[i5][firstFlag] = -6

	d := draft.New(draft.Radiant) // we are Radiant and first: steps 0-1 are ours
	res := r.Recommend(d, draft.Radiant, 8)
	if !res.OurTurn {
		t.Fatal("step 0 should be ours")
	}
	if res.EnemyStep == nil || res.EnemyStep.Ord != 2 || res.EnemyStep.IsPick {
		t.Fatalf("enemy step = %+v, want their first ban at index 2", res.EnemyStep)
	}
	var top EnemyPick
	if len(res.EnemyPred) > 0 {
		top = res.EnemyPred[0]
	}
	if top.HeroID != 5 {
		t.Fatalf("top enemy prediction = %+v, want hero 5 (what the second seat bans)", top)
	}
}

// The expected completion for a side must be predicted at that side's own next
// pick, with that side as the actor.
func TestRolloutPredictsEachSideAtItsOwnPick(t *testing.T) {
	r := zeroRecommender([]int{1, 2, 3})
	d := draft.New(draft.Radiant)
	for i := 0; i < openingBans; i++ { // through the opening bans: Radiant to pick
		_ = d.Apply(100 + i)
	}
	ord, step, ok := r.nextAction(d, draft.Dire, true)
	if !ok || ord != openingBans+1 || step.Team != draft.Dire || step.Action != draft.Pick {
		t.Fatalf("Dire's next pick = %d %+v %v, want index %d", ord, step, ok, openingBans+1)
	}
}

// When the enemy has nothing left to do, there is no enemy move to predict.
func TestNoEnemyPredictionWhenTheyAreDone(t *testing.T) {
	r := zeroRecommender([]int{1, 2, 3})
	d := draft.New(draft.Dire) // Radiant picks last (step 23)
	for i := 0; i < draft.CMSteps-1; i++ {
		_ = d.Apply(100 + i)
	}
	res := r.Recommend(d, draft.Radiant, 8)
	if !res.OurTurn || !res.IsPick {
		t.Fatal("setup: the last step should be our pick")
	}
	if res.EnemyStep != nil || len(res.EnemyPred) != 0 {
		t.Fatalf("enemy step %+v, %d predictions; want none", res.EnemyStep, len(res.EnemyPred))
	}
}
