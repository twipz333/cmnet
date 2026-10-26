package recommender

import (
	"sort"

	"cmnet/internal/draft"
	"cmnet/internal/model"
)

// searchState is a partial CM draft used by the §6.4 expectimax planner.
// Boards are stored in radiant/dire coordinates; whose turn comes from Seq[Cursor].
type searchState struct {
	rPicks, dPicks []int
	rBans, dBans   []int
	cursor         int
	firstPick      draft.Team
	seq            []draft.Step
}

func (s searchState) clone() searchState {
	return searchState{
		rPicks:    cloneInts(s.rPicks),
		dPicks:    cloneInts(s.dPicks),
		rBans:     cloneInts(s.rBans),
		dBans:     cloneInts(s.dBans),
		cursor:    s.cursor,
		firstPick: s.firstPick,
		seq:       s.seq, // immutable after build
	}
}

func (s searchState) complete() bool { return s.cursor >= len(s.seq) }

func (s searchState) step() (draft.Step, bool) {
	if s.complete() {
		return draft.Step{}, false
	}
	return s.seq[s.cursor], true
}

func (s searchState) taken(id int) bool {
	return usedIn(id, s.rPicks) || usedIn(id, s.dPicks) ||
		usedIn(id, s.rBans) || usedIn(id, s.dBans)
}

func (s searchState) apply(id int) searchState {
	ns := s.clone()
	step := ns.seq[ns.cursor]
	switch {
	case step.Team == draft.Radiant && step.Action == draft.Pick:
		ns.rPicks = append(ns.rPicks, id)
	case step.Team == draft.Dire && step.Action == draft.Pick:
		ns.dPicks = append(ns.dPicks, id)
	case step.Team == draft.Radiant && step.Action == draft.Ban:
		ns.rBans = append(ns.rBans, id)
	default:
		ns.dBans = append(ns.dBans, id)
	}
	ns.cursor++
	return ns
}

func (s searchState) myEnemyPicks(ourTeam draft.Team) (my, enemy []int) {
	if ourTeam == draft.Radiant {
		return s.rPicks, s.dPicks
	}
	return s.dPicks, s.rPicks
}

// beamPickScore scores adding cand on the current CM step. With EnemyPolicy it
// runs CM-aware expectimax; otherwise the legacy value-greedy pick rollout.
func (r *Recommender) beamPickScore(d draft.View, ourTeam draft.Team, cand int, pool []int) float64 {
	if !r.beam.EnemyPolicy {
		return r.beamPickScoreGreedy(ourTeam, d.Picks(draft.Radiant), d.Picks(draft.Dire), cand, pool)
	}
	st := r.searchStateFrom(d)
	if st.taken(cand) {
		my, enemy := st.myEnemyPicks(ourTeam)
		return r.evalDraft(ourTeam, my, enemy)
	}
	st = st.apply(cand)
	return r.expectimax(st, r.beam.Depth, ourTeam, pool)
}

func (r *Recommender) searchStateFrom(d draft.View) searchState {
	fp := d.FirstPickTeam()
	seq := r.CMOrder().Sequence(fp)
	cursor := len(d.Picks(draft.Radiant)) + len(d.Picks(draft.Dire)) +
		len(d.Bans(draft.Radiant)) + len(d.Bans(draft.Dire))
	if cursor > len(seq) {
		cursor = len(seq)
	}
	// Trust the live current step when the hardcoded CM pattern drifts (GSI/manual).
	if step, ok := d.Current(); ok && cursor < len(seq) {
		seq = append([]draft.Step(nil), seq...)
		seq[cursor] = step
	}
	return searchState{
		rPicks:    cloneInts(d.Picks(draft.Radiant)),
		dPicks:    cloneInts(d.Picks(draft.Dire)),
		rBans:     cloneInts(d.Bans(draft.Radiant)),
		dBans:     cloneInts(d.Bans(draft.Dire)),
		cursor:    cursor,
		firstPick: fp,
		seq:       seq,
	}
}

func (r *Recommender) expectimax(st searchState, depth int, ourTeam draft.Team, pool []int) float64 {
	my, enemy := st.myEnemyPicks(ourTeam)
	if depth <= 0 || st.complete() {
		return r.evalDraft(ourTeam, my, enemy)
	}
	step, ok := st.step()
	if !ok {
		return r.evalDraft(ourTeam, my, enemy)
	}
	if step.Team == ourTeam {
		return r.expectimaxOur(st, depth, ourTeam, pool)
	}
	return r.expectimaxEnemy(st, depth, ourTeam, pool)
}

func (r *Recommender) expectimaxOur(st searchState, depth int, ourTeam draft.Team, pool []int) float64 {
	type cand struct {
		id   int
		heur float64
	}
	// Which of our actions to expand. A pick is judged by the board it leaves. A
	// ban leaves our board exactly as it was, so judging it that way scored every
	// ban the same and the width cut kept whichever heroes had the lowest ids;
	// a ban is judged by how likely the enemy is to PICK the hero next instead —
	// the same base the live ban score uses.
	step, _ := st.step()
	var denyProb []float64
	var need *roleNeed
	if step.Action == draft.Ban {
		denyProb = r.searchEnemyPickPolicy(st, ourTeam)
	} else if r.hasRoles() {
		my, _ := st.myEnemyPicks(ourTeam)
		n := r.needOf(my, r.heroPosOurs)
		need = &n
	}
	cands := make([]cand, 0, len(pool))
	for _, id := range pool {
		if st.taken(id) {
			continue
		}
		if need != nil && need.fit(id) < MinRoleFit {
			continue // we would not draft a hero for a position we already hold
		}
		if denyProb != nil {
			h := 0.0
			if idx, ok := r.heroMap.Index(id); ok && idx < len(denyProb) {
				h = denyProb[idx]
			}
			cands = append(cands, cand{id: id, heur: h})
			continue
		}
		ns := st.apply(id)
		my, enemy := ns.myEnemyPicks(ourTeam)
		cands = append(cands, cand{id: id, heur: r.evalDraft(ourTeam, my, enemy)})
	}
	if len(cands) == 0 {
		my, enemy := st.myEnemyPicks(ourTeam)
		return r.evalDraft(ourTeam, my, enemy)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].heur != cands[j].heur {
			return cands[i].heur > cands[j].heur
		}
		return cands[i].id < cands[j].id
	})
	width := r.beam.Width
	if width > 0 && len(cands) > width {
		cands = cands[:width]
	}

	best := model.RoundScore(-1)
	bestID := -1
	for _, c := range cands {
		v := r.expectimax(st.apply(c.id), depth-1, ourTeam, pool)
		if bestID < 0 || v > best || (v == best && c.id < bestID) {
			best, bestID = v, c.id
		}
	}
	return best
}

func (r *Recommender) expectimaxEnemy(st searchState, depth int, ourTeam draft.Team, pool []int) float64 {
	my, enemy := st.myEnemyPicks(ourTeam)
	baseline := r.evalDraft(ourTeam, my, enemy)

	type wp struct {
		id int
		p  float64
	}
	dist := r.enemyStepPolicy(st)
	opts := make([]wp, 0, len(pool))
	for _, id := range pool {
		if st.taken(id) {
			continue
		}
		idx, ok := r.heroMap.Index(id)
		if !ok || idx >= len(dist) {
			continue
		}
		opts = append(opts, wp{id: id, p: dist[idx]})
	}
	sort.SliceStable(opts, func(i, j int) bool {
		if opts[i].p != opts[j].p {
			return opts[i].p > opts[j].p
		}
		return opts[i].id < opts[j].id
	})

	k := r.beam.EnemyTopK
	if k <= 0 {
		k = 8
	}
	floor := r.beam.MassFloor
	if floor <= 0 {
		floor = 0.85
	}

	var ev, mass float64
	kept := 0
	for _, o := range opts {
		if kept >= k {
			break
		}
		v := r.expectimax(st.apply(o.id), depth-1, ourTeam, pool)
		ev += o.p * v
		mass += o.p
		kept++
		if mass >= floor {
			break
		}
	}
	if kept == 0 {
		return baseline
	}
	ev += (1 - mass) * baseline
	return model.RoundScore(ev)
}

// enemyStepPolicy is P(hero | state) for the side about to act on st's current step.
func (r *Recommender) enemyStepPolicy(st searchState) []float64 {
	n := r.heroMap.Len()
	step, ok := st.step()
	if !ok || r.Predictor == nil {
		return make([]float64, n)
	}
	b := boardState{rPicks: st.rPicks, dPicks: st.dPicks, rBans: st.rBans, dBans: st.dBans, firstPick: st.firstPick}
	dist := r.Predictor.Distribution(predictorState(b, step.Team, step.Action, st.cursor))
	dist = r.roleAdjust(dist, b.picksOf(step.Team), r.pinsForTeam(step.Team), step.Action)
	return r.blendEnemy(dist, step.Action)
}

// searchEnemyPickPolicy is the enemy's distribution for their next PICK after st's
// cursor, on st's board (scout-blended like every enemy policy). Nil when the
// enemy has no pick left.
func (r *Recommender) searchEnemyPickPolicy(st searchState, ourTeam draft.Team) []float64 {
	if r.Predictor == nil {
		return nil
	}
	enemy := ourTeam.Opponent()
	for i := st.cursor; i < len(st.seq); i++ {
		if st.seq[i].Team != enemy || st.seq[i].Action != draft.Pick {
			continue
		}
		b := boardState{rPicks: st.rPicks, dPicks: st.dPicks, rBans: st.rBans, dBans: st.dBans, firstPick: st.firstPick}
		dist := r.Predictor.Distribution(predictorState(b, enemy, draft.Pick, i))
		dist = r.roleAdjust(dist, b.picksOf(enemy), r.pinsForTeam(enemy), draft.Pick)
		return r.blendEnemy(dist, draft.Pick)
	}
	return nil
}
