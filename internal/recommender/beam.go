package recommender

import (
	"sort"

	"cmnet/internal/draft"
	"cmnet/internal/model"
)

const picksPerTeam = 5

// prunePool returns the top-PruneN untaken heroes by predictor prior, in
// descending probability with a hero_id tie-break (deterministic). This bounds the
// branching factor (~127 heroes) the beam search must consider.
func (r *Recommender) prunePool(d draft.View, dist []float64) []int {
	type cand struct {
		id   int
		prob float64
	}
	cands := make([]cand, 0, len(dist))
	for idx, p := range dist {
		id := r.heroMap.ID(idx)
		if d.IsTaken(id) {
			continue
		}
		cands = append(cands, cand{id: id, prob: p})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].prob != cands[j].prob {
			return cands[i].prob > cands[j].prob
		}
		return cands[i].id < cands[j].id
	})
	n := r.beam.PruneN
	if n <= 0 || n > len(cands) {
		n = len(cands)
	}
	pool := make([]int, n)
	for i := 0; i < n; i++ {
		pool[i] = cands[i].id
	}
	return pool
}

// evalDraft scores a (partial) draft from ourTeam's perspective: win probability
// plus synergy and counter adjustments. Result is rounded for platform stability.
// ScoreDraft exposes the value the search ranks candidates by, for offline
// evaluation. It is not the raw win probability: it is what the advisor actually
// orders picks with, matrices and lining penalty included. Measuring win-prob
// alone would grade a component the user never sees on its own.
func (r *Recommender) ScoreDraft(ourTeam draft.Team, myPicks, enemyPicks []int) float64 {
	return r.evalDraft(ourTeam, myPicks, enemyPicks)
}

func (r *Recommender) evalDraft(ourTeam draft.Team, myPicks, enemyPicks []int) float64 {
	var base float64
	if ourTeam == draft.Radiant {
		base = model.RoundScore(r.WinProb.CalibrateAt(r.rawWinProb(myPicks, enemyPicks), len(myPicks)+len(enemyPicks)))
	} else {
		base = model.RoundScore(1 - r.WinProb.CalibrateAt(r.rawWinProb(enemyPicks, myPicks), len(myPicks)+len(enemyPicks)))
	}
	adj := r.beam.LambdaSyn*(r.meanSynergy(myPicks)-r.meanSynergy(enemyPicks)) +
		r.beam.LambdaCnt*(r.meanCounter(myPicks, enemyPicks)-0.5)
	// Lining penalty (#8): discount drafts whose heroes can't cover distinct
	// positions 1-5 (e.g. four hard carries). Only our own composition is checked.
	if r.posByID != nil && r.beam.LambdaPos > 0 {
		adj -= r.beam.LambdaPos * (1 - r.coverage(myPicks))
	}
	return model.RoundScore(base + adj)
}

// coverage returns the fraction of our heroes that can be assigned to distinct
// lane positions (1-5) — a max bipartite matching of heroes to positions divided
// by min(len, 5). 1.0 means every hero has a distinct plausible lane; lower means
// role overlap (a draft that is hard to actually realize). 1.0 when positions are
// unknown or there are no picks.
func (r *Recommender) coverage(picks []int) float64 {
	if len(picks) == 0 {
		return 1
	}
	need := len(picks)
	if need > 5 {
		need = 5
	}
	// posOfHero[i] = eligible positions for picks[i]; unknown heroes can fill any.
	posOfHero := make([][]int, len(picks))
	for i, id := range picks {
		if p, ok := r.posByID[id]; ok && len(p) > 0 {
			posOfHero[i] = p
		} else {
			posOfHero[i] = []int{1, 2, 3, 4, 5}
		}
	}
	posToHero := map[int]int{} // position -> matched hero index
	var augment func(hi int, seen map[int]bool) bool
	augment = func(hi int, seen map[int]bool) bool {
		for _, p := range posOfHero[hi] {
			if seen[p] {
				continue
			}
			seen[p] = true
			if cur, taken := posToHero[p]; !taken || augment(cur, seen) {
				posToHero[p] = hi
				return true
			}
		}
		return false
	}
	matched := 0
	for hi := range picks {
		if augment(hi, map[int]bool{}) {
			matched++
		}
	}
	return float64(matched) / float64(need)
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
