package recommender

import (
	"math"

	"cmnet/internal/draft"
	"cmnet/internal/model"
	"cmnet/internal/roles"
)

// Role fit: does a hero fill a position this side still needs?
//
// The strength models cannot answer that. The win-prob head adds hero (and
// hero×position) contributions independently, so a second carry adds as much as
// the first; the stacker's lining feature has almost no variance in real drafts
// (real teams always field a 1-5), so its weight trains to ~0. The result was a
// top-5 that could be five heroes for the same slot, long after that slot was
// filled. Role fit is therefore applied as a CONSTRAINT on candidates and a
// LIKELIHOOD on predictions, not as one more feature for a model to ignore.
//
//	open[p]  = 1 − (expected occupancy of position p by the side's current picks)
//	fit(h)   = Σ_p P(p | h) · open[p]
//
// P(p|h) is the bundle's role prior (how often the hero is played in each
// position). Occupancy comes from the assignment posterior over the side's picks,
// with captain-pinned positions taking precedence.

// MinRoleFit is the smallest fit a hero needs to be recommended as OUR pick: it
// is played in a still-open position at least this share of the time.
const MinRoleFit = 0.15

// enemyFitFloor keeps a hero the role prior rules out from vanishing entirely
// from the enemy prediction: priors are estimates, and pubs flex.
const enemyFitFloor = 0.03

// roleNeed is the per-position open mass for one side, plus the prior used.
type roleNeed struct {
	open  [roles.NumPositions]float64
	prior *roles.Prior
	elig  map[int][]int
}

// hasRoles reports whether the bundle carries any role information at all.
func (r *Recommender) hasRoles() bool { return r.rolePrior != nil || len(r.posByID) > 0 }

// needOf computes which positions side still has to fill given its picks.
// pinned (hero → position) overrides the posterior for heroes the captain fixed.
func (r *Recommender) needOf(picks []int, pinned map[int]int) roleNeed {
	n := roleNeed{prior: r.rolePrior, elig: r.posByID}
	if n.prior == nil {
		n.prior = &roles.Prior{} // uniform over eligible positions
	}
	for i := range n.open {
		n.open[i] = 1
	}
	if len(picks) == 0 {
		return n
	}
	var free []int
	for _, h := range picks {
		if p, ok := pinned[h]; ok && p >= 1 && p <= roles.NumPositions {
			n.open[p-1] = 0
			continue
		}
		free = append(free, h)
	}
	if len(free) == 0 {
		return n
	}
	occ := roles.ExpectedOccupancy(free, n.prior, n.elig)
	for _, h := range free { // fixed order: summation must not depend on map order
		row := occ[h]
		for p := range n.open {
			n.open[p] -= row[p]
		}
	}
	for p := range n.open {
		if n.open[p] < 0 {
			n.open[p] = 0
		}
	}
	return n
}

// probOf is P(position p+1 | hero) under the need's prior, restricted to the
// hero's eligible positions and renormalized.
func (n roleNeed) probs(hero int) [roles.NumPositions]float64 {
	var row [roles.NumPositions]float64
	var sum float64
	for _, pos := range eligibleOf(hero, n.elig) {
		v := expLogProb(n.prior, hero, pos)
		row[pos-1] = v
		sum += v
	}
	if sum > 0 {
		for i := range row {
			row[i] /= sum
		}
	}
	return row
}

// fit is how well hero fills this side's open positions, in [0,1].
func (n roleNeed) fit(hero int) float64 {
	row := n.probs(hero)
	var s float64
	for p, v := range row {
		s += v * n.open[p]
	}
	return s
}

// bestRole is the open position hero most plausibly fills (1-5), 0 if none.
func (n roleNeed) bestRole(hero int) int {
	row := n.probs(hero)
	best, bestV := 0, 0.0
	for p, v := range row {
		if w := v * n.open[p]; w > bestV {
			best, bestV = p+1, w
		}
	}
	return best
}

// openCount is how many positions are still meaningfully open.
func (n roleNeed) openCount() int {
	c := 0
	for _, v := range n.open {
		if v > 0.5 {
			c++
		}
	}
	return c
}

// pinsFor returns the captain-pinned positions for team's heroes.
func (r *Recommender) pinsFor(team, ourTeam draft.Team) map[int]int {
	if team == ourTeam {
		return r.heroPosOurs
	}
	return r.heroPosEnemy
}

// roleAdjust reweights a pick distribution for the side that holds picks:
// P(h | open roles) ∝ P(h) · fit(h), floored so the prior never zeroes a hero.
// Bans and boards without role data pass through unchanged.
func (r *Recommender) roleAdjust(dist []float64, picks []int, pinned map[int]int, action draft.Action) []float64 {
	if action != draft.Pick || !r.hasRoles() || len(picks) == 0 || dist == nil {
		return dist
	}
	need := r.needOf(picks, pinned)
	out := make([]float64, len(dist))
	var sum float64
	for idx, p := range dist {
		if p <= 0 {
			continue
		}
		f := need.fit(r.heroMap.ID(idx))
		if f < enemyFitFloor {
			f = enemyFitFloor
		}
		out[idx] = p * f
		sum += out[idx]
	}
	if sum <= 0 {
		return dist
	}
	for i := range out {
		out[i] = roundProb(out[i] / sum)
	}
	return out
}

func eligibleOf(hero int, elig map[int][]int) []int {
	if ps, ok := elig[hero]; ok && len(ps) > 0 {
		out := make([]int, 0, len(ps))
		for _, p := range ps {
			if p >= 1 && p <= roles.NumPositions {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []int{1, 2, 3, 4, 5}
}

func expLogProb(p *roles.Prior, hero, pos int) float64 { return math.Exp(p.LogProb(hero, pos)) }

func roundProb(v float64) float64 { return model.RoundScore(v) }

// picksOf returns team's picks on a board.
func (b boardState) picksOf(team draft.Team) []int {
	if team == draft.Radiant {
		return b.rPicks
	}
	return b.dPicks
}
