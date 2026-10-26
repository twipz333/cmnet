package recommender

import (
	"sort"

	"cmnet/internal/draft"
	"cmnet/internal/model"
)

const picksPerTeam = 5

// ScoreDraft exposes the value the search ranks candidates by, for offline
// evaluation (eval's "advisor" ranker): our-perspective boardStrength, the same
// number the strength bar shows.
func (r *Recommender) ScoreDraft(ourTeam draft.Team, myPicks, enemyPicks []int) float64 {
	return r.evalDraft(ourTeam, myPicks, enemyPicks)
}

// evalDraft is the planner's leaf value: P(we win) for a (partial) board, read
// through boardStrength — exactly the function behind the strength readout.
//
// It used to be its own formula: calibrated win-prob plus fixed 0.10 synergy and
// counter terms and a 0.15 lining penalty, while the strength bar used the
// bundle's fitted stacker. The search therefore ranked picks by one quantity and
// the user judged them by another, and the "win ~X%" next to a pick was not a
// probability at all. One function now serves both, so the number beside a
// recommendation is the expected strength the bar will show if the draft goes
// the way the planner expects.
func (r *Recommender) evalDraft(ourTeam draft.Team, myPicks, enemyPicks []int) float64 {
	rp, dp := myPicks, enemyPicks
	if ourTeam == draft.Dire {
		rp, dp = enemyPicks, myPicks
	}
	p := r.boardStrength(rp, dp, r.rawWinProb(rp, dp))
	if ourTeam == draft.Dire {
		p = model.RoundScore(1 - p)
	}
	return p
}

// meanSynergy is the average teammate-synergy win rate among ids (0.5 if < 2).
func (r *Recommender) meanSynergy(ids []int) float64 {
	if len(ids) < 2 {
		return 0.5
	}
	var sum float64
	var pairs int
	for i := 0; i < len(ids); i++ {
		ii, ok := r.heroMap.Index(ids[i])
		if !ok {
			continue
		}
		for j := i + 1; j < len(ids); j++ {
			jj, ok := r.heroMap.Index(ids[j])
			if !ok {
				continue
			}
			sum += r.synergy[ii][jj]
			pairs++
		}
	}
	if pairs == 0 {
		return 0.5
	}
	return sum / float64(pairs)
}

// meanCounter is the average counter win rate of my heroes versus enemy heroes
// (0.5 if either side is empty).
func (r *Recommender) meanCounter(my, enemy []int) float64 {
	if len(my) == 0 || len(enemy) == 0 {
		return 0.5
	}
	var sum float64
	var pairs int
	for _, mid := range my {
		mi, ok := r.heroMap.Index(mid)
		if !ok {
			continue
		}
		for _, eid := range enemy {
			ei, ok := r.heroMap.Index(eid)
			if !ok {
				continue
			}
			sum += r.counter[mi][ei]
			pairs++
		}
	}
	if pairs == 0 {
		return 0.5
	}
	return sum / float64(pairs)
}

// beamState is a partial draft during rollout.
type beamState struct {
	my    []int
	enemy []int
	score float64 // our-perspective eval of the partial draft
}

// beamPickScoreGreedy is the pre-§6.4 rollout: pick-only, value-greedy enemy reply,
// max over the beam. Kept as the EnemyPolicy=false path for gate tests.
func (r *Recommender) beamPickScoreGreedy(ourTeam draft.Team, rPicks, dPicks []int, cand int, pool []int) float64 {
	var myStart, enemyStart []int
	if ourTeam == draft.Radiant {
		myStart, enemyStart = rPicks, dPicks
	} else {
		myStart, enemyStart = dPicks, rPicks
	}

	start := beamState{
		my:    appendCopy(myStart, cand),
		enemy: cloneInts(enemyStart),
	}
	start.score = r.evalDraft(ourTeam, start.my, start.enemy)

	beam := []beamState{start}
	enemyTeam := ourTeam.Opponent()

	for ply := 0; ply < r.beam.Depth; ply++ {
		if full(beam[0].my) && full(beam[0].enemy) {
			break
		}
		var next []beamState
		for _, st := range beam {
			// The side with fewer picks acts next; ties go to the enemy (responding
			// to our candidate).
			if len(st.enemy) <= len(st.my) && !full(st.enemy) {
				next = append(next, r.enemyBestResponse(enemyTeam, ourTeam, st, pool))
			} else if !full(st.my) {
				next = append(next, r.myExpansions(ourTeam, st, pool)...)
			} else {
				next = append(next, st) // this side full; keep state
			}
		}
		beam = keepTopBeam(next, r.beam.Width)
	}

	best := beam[0].score
	for _, st := range beam[1:] {
		if st.score > best {
			best = st.score
		}
	}
	return best
}

// enemyBestResponse adds the enemy's single best pick (greedy, deterministic).
func (r *Recommender) enemyBestResponse(enemyTeam, ourTeam draft.Team, st beamState, pool []int) beamState {
	bestID := -1
	bestVal := -1.0
	for _, id := range pool {
		if usedIn(id, st.my) || usedIn(id, st.enemy) {
			continue
		}
		val := r.evalDraft(enemyTeam, appendCopy(st.enemy, id), st.my)
		if val > bestVal || (val == bestVal && (bestID == -1 || id < bestID)) {
			bestVal, bestID = val, id
		}
	}
	if bestID == -1 {
		return st
	}
	ns := beamState{my: cloneInts(st.my), enemy: appendCopy(st.enemy, bestID)}
	ns.score = r.evalDraft(ourTeam, ns.my, ns.enemy)
	return ns
}

// myExpansions branches our next pick across the pool (beam will prune to Width).
func (r *Recommender) myExpansions(ourTeam draft.Team, st beamState, pool []int) []beamState {
	var out []beamState
	for _, id := range pool {
		if usedIn(id, st.my) || usedIn(id, st.enemy) {
			continue
		}
		ns := beamState{my: appendCopy(st.my, id), enemy: cloneInts(st.enemy)}
		ns.score = r.evalDraft(ourTeam, ns.my, ns.enemy)
		out = append(out, ns)
	}
	if len(out) == 0 {
		return []beamState{st}
	}
	return out
}

// keepTopBeam keeps the top-width states by score, with a deterministic tie-break
// on the pick composition.
func keepTopBeam(states []beamState, width int) []beamState {
	sort.SliceStable(states, func(i, j int) bool {
		if states[i].score != states[j].score {
			return states[i].score > states[j].score
		}
		return lessInts(states[i].my, states[j].my)
	})
	if width > 0 && len(states) > width {
		states = states[:width]
	}
	return states
}

func full(picks []int) bool { return len(picks) >= picksPerTeam }

func usedIn(id int, s []int) bool { return containsInt(s, id) }

func cloneInts(s []int) []int {
	out := make([]int, len(s))
	copy(out, s)
	return out
}

// lessInts gives a total order on int slices for deterministic tie-breaks.
func lessInts(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
