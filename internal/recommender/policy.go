package recommender

import (
	"cmnet/internal/draft"
	"cmnet/internal/features"
)

// This file is the single place the live advisor turns a board into predictor
// input. Training (train.BuildSamples) and offline eval fill every field of
// features.PredictorState — whose turn it is (Actor), which step of the draft it
// is (Ord) and which side drafts first (FirstPick). The live paths used to set
// only the board and the pick/ban flag, so the predictor saw Radiant as the actor
// on every query, "step 0 of the draft" on every query, and an always-true
// "actor drafts first" flag: a different input distribution from the one it was
// trained and evaluated on. Whenever Dire was acting, the actor/opponent blocks
// were swapped and the model predicted the hero that completes the OTHER team.
//
// Everything that queries the predictor goes through predictorState so that can
// not drift apart again.

// boardCursor is the index of the next draft step: in Captains Mode every action
// lands on the board, so it equals the number of picks and bans so far.
func boardCursor(d draft.View) int {
	return len(d.Picks(draft.Radiant)) + len(d.Picks(draft.Dire)) +
		len(d.Bans(draft.Radiant)) + len(d.Bans(draft.Dire))
}

// liveSequence is the full step sequence for d under the recommender's schedule,
// with the step the view reports as current written over the schedule's own idea
// of it. The live source (GSI, or the manual board) is the authority on whose turn
// it is now; the schedule is only needed to look past it.
func (r *Recommender) liveSequence(d draft.View) []draft.Step {
	seq := r.CMOrder().Sequence(d.FirstPickTeam())
	if cur, ok := d.Current(); ok {
		if c := boardCursor(d); c < len(seq) {
			seq[c] = cur
		}
	}
	return seq
}

// nextAction finds the next step team takes, at or after the current one. With
// pickOnly it skips that team's bans. ok is false when there is none left.
func (r *Recommender) nextAction(d draft.View, team draft.Team, pickOnly bool) (ord int, step draft.Step, ok bool) {
	seq := r.liveSequence(d)
	for i := boardCursor(d); i < len(seq); i++ {
		if seq[i].Team != team || (pickOnly && seq[i].Action != draft.Pick) {
			continue
		}
		return i, seq[i], true
	}
	return -1, draft.Step{}, false
}

// boardState is the board half of a predictor input, with the action-specific
// fields left for predictorState to fill.
type boardState struct {
	rPicks, dPicks, rBans, dBans []int
	firstPick                    draft.Team
}

func boardOf(d draft.View) boardState {
	return boardState{
		rPicks: d.Picks(draft.Radiant), dPicks: d.Picks(draft.Dire),
		rBans: d.Bans(draft.Radiant), dBans: d.Bans(draft.Dire),
		firstPick: d.FirstPickTeam(),
	}
}

// predictorState is the predictor input for team taking step (at index ord) on
// board b — exactly the fields train.BuildSamples sets.
//
// ord may lie beyond the board (the enemy's next action while it is our turn).
// The board is then the one we can see now: our own intervening actions are not
// known yet, and the step/phase features still tell the model which part of the
// draft that action belongs to.
func predictorState(b boardState, team draft.Team, action draft.Action, ord int) features.PredictorState {
	return features.PredictorState{
		RadiantPicks: b.rPicks, DirePicks: b.dPicks,
		RadiantBans: b.rBans, DireBans: b.dBans,
		IsPick:    action == draft.Pick,
		Actor:     int(team),
		Ord:       ord,
		FirstPick: int(b.firstPick),
	}
}

// policy is the predicted distribution for a concrete upcoming action, plus the
// action it is for. A zero policy (dist == nil) means that team has no such action
// left in the draft.
type policy struct {
	dist []float64
	step draft.Step
	ord  int
}

func (p policy) ok() bool { return p.dist != nil }

// teamPolicy predicts team's next action on d (its next pick, with pickOnly),
// layer 1 only.
func (r *Recommender) teamPolicy(d draft.View, team draft.Team, pickOnly bool) policy {
	ord, step, ok := r.nextAction(d, team, pickOnly)
	if !ok || r.Predictor == nil {
		return policy{}
	}
	return policy{dist: r.Predictor.Distribution(predictorState(boardOf(d), team, step.Action, ord)), step: step, ord: ord}
}

// enemyPolicy is teamPolicy for the opponent, with the scout layers blended in
// (they describe the opponent only, §9).
func (r *Recommender) enemyPolicy(d draft.View, ourTeam draft.Team, pickOnly bool) policy {
	p := r.teamPolicy(d, ourTeam.Opponent(), pickOnly)
	if p.ok() {
		p.dist = r.blendEnemy(p.dist, p.step.Action)
	}
	return p
}

// orZeros returns p's distribution, or an all-zero one when the team has no such
// action left (so callers indexing by hero never see a nil slice).
func (r *Recommender) orZeros(p policy) []float64 {
	if p.ok() {
		return p.dist
	}
	return make([]float64, r.heroMap.Len())
}

// firstPickState is the input for the first pick of the draft by the first-pick
// team, on an empty board — the closest the predictor has to "how popular is this
// hero as a pick", used by the per-position meta fallback.
func (r *Recommender) firstPickState() features.PredictorState {
	seq := r.CMOrder().Sequence(draft.Radiant)
	for i, st := range seq {
		if st.Action == draft.Pick {
			return predictorState(boardState{firstPick: draft.Radiant}, st.Team, draft.Pick, i)
		}
	}
	return predictorState(boardState{firstPick: draft.Radiant}, draft.Radiant, draft.Pick, -1)
}
