package capability

import (
	"math"
	"sort"

	"cmnet/internal/scout"
)

// MinRoster is how many resolved allies are required before player→hero
// assignment is trusted. Fewer seats make the matching wrong by construction (§6.5).
const MinRoster = 5

// DefaultAlpha is Laplace smoothing for soft P(player plays hero).
const DefaultAlpha = 1.0

// DefaultLambda is how much (1 - feasibility) is subtracted from a pick score.
const DefaultLambda = 0.08

// Player is one confirmed ally with a soft distribution over heroes.
type Player struct {
	AccountID int64
	Nickname  string
	// SoftP maps Valve hero id → P(this player plays that hero), smoothed, >0.
	SoftP map[int]float64
}

// Roster is the human-confirmed ally side used for capability scoring.
type Roster struct {
	Players []Player // sorted by AccountID for deterministic matching
}

// FromAllies builds a Roster from a scouted Allies layer.
// Returns nil when fewer than MinRoster players are resolved with data.
func FromAllies(l2 *scout.Layer2, heroUniverse []int, alpha float64) *Roster {
	return FromLayer2(l2, heroUniverse, alpha, MinRoster)
}

// FromLayer2 builds a Roster from any layer-2 block. minN is the minimum number of
// resolved players required (use MinRoster for scoring, 1 for enemy lineup guesses).
func FromLayer2(l2 *scout.Layer2, heroUniverse []int, alpha float64, minN int) *Roster {
	if l2 == nil {
		return nil
	}
	if alpha <= 0 {
		alpha = DefaultAlpha
	}
	if minN <= 0 {
		minN = 1
	}
	nH := len(heroUniverse)
	if nH == 0 {
		return nil
	}

	type cand struct {
		p    Player
		mass float64
	}
	var cands []cand
	for _, p := range l2.Players {
		if !p.Resolved || p.AccountID == 0 {
			continue
		}
		games := make(map[int]int, len(p.Heroes))
		total := 0
		for _, h := range p.Heroes {
			if h.Games <= 0 || h.HeroID <= 0 {
				continue
			}
			games[h.HeroID] += h.Games
			total += h.Games
		}
		soft := make(map[int]float64, nH)
		denom := float64(total) + alpha*float64(nH)
		for _, id := range heroUniverse {
			soft[id] = (float64(games[id]) + alpha) / denom
		}
		pl := Player{AccountID: p.AccountID, Nickname: p.Nickname, SoftP: soft}
		cands = append(cands, cand{p: pl, mass: float64(total)})
	}
	if len(cands) < minN {
		return nil
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].mass != cands[j].mass {
			return cands[i].mass > cands[j].mass
		}
		return cands[i].p.AccountID < cands[j].p.AccountID
	})
	take := len(cands)
	if minN >= MinRoster && take > MinRoster {
		take = MinRoster // scoring roster stays at five seats
	}
	players := make([]Player, take)
	for i := 0; i < take; i++ {
		players[i] = cands[i].p
	}
	sort.SliceStable(players, func(i, j int) bool {
		return players[i].AccountID < players[j].AccountID
	})
	return &Roster{Players: players}
}

// SetNicknames replaces display names by account id (persona cache). Empty names
// are ignored so a partial map does not wipe what we already have.
func (r *Roster) SetNicknames(names map[int64]string) {
	if r == nil || len(names) == 0 {
		return
	}
	for i := range r.Players {
		if n := names[r.Players[i].AccountID]; n != "" {
			r.Players[i].Nickname = n
		}
	}
}

// Assignment maps each hero id to the roster player index that should play it.
type Assignment struct {
	HeroToPlayer map[int]int // hero id → index into Roster.Players
	Feasibility  float64     // geometric mean of softP over assigned pairs, in (0,1]
}

// Assign finds a maximum-weight injection of heroes onto distinct players.
// Weight is log(softP); feasibility is the geometric mean of the softPs used.
func Assign(heroes []int, roster *Roster) Assignment {
	out := Assignment{HeroToPlayer: map[int]int{}, Feasibility: 1}
	if roster == nil || len(roster.Players) == 0 {
		return out
	}
	hs := uniqueSorted(heroes)
	if len(hs) == 0 {
		return out
	}
	nP := len(roster.Players)
	if len(hs) > nP {
		hs = hs[:nP]
	}

	type edge struct {
		hero, pi int
	}
	used := make([]bool, nP)
	var cur []edge
	bestEdges := []edge(nil)
	bestLog := math.Inf(-1)

	var search func(hi int, logSum float64)
	search = func(hi int, logSum float64) {
		if hi == len(hs) {
			if logSum > bestLog {
				bestLog = logSum
				bestEdges = append([]edge(nil), cur...)
			}
			return
		}
		hero := hs[hi]
		for pi := 0; pi < nP; pi++ {
			if used[pi] {
				continue
			}
			p := roster.Players[pi].SoftP[hero]
			if p <= 0 {
				p = 1e-12
			}
			used[pi] = true
			cur = append(cur, edge{hero, pi})
			search(hi+1, logSum+math.Log(p))
			cur = cur[:len(cur)-1]
			used[pi] = false
		}
	}
	search(0, 0)

	if len(bestEdges) == 0 {
		return out
	}
	out.HeroToPlayer = make(map[int]int, len(bestEdges))
	prod := 1.0
	for _, e := range bestEdges {
		out.HeroToPlayer[e.hero] = e.pi
		p := roster.Players[e.pi].SoftP[e.hero]
		if p <= 0 {
			p = 1e-12
		}
		prod *= p
	}
	out.Feasibility = math.Pow(prod, 1/float64(len(bestEdges)))
	return out
}

// AssignFixed is Assign with hard hero→account pins (captain lineup overrides).
// Pins consume roster seats first; remaining heroes are matched as usual.
func AssignFixed(heroes []int, roster *Roster, fixed map[int]int64) Assignment {
	if roster == nil || len(roster.Players) == 0 {
		return Assignment{HeroToPlayer: map[int]int{}, Feasibility: 1}
	}
	if len(fixed) == 0 {
		return Assign(heroes, roster)
	}
	accIdx := map[int64]int{}
	for i, p := range roster.Players {
		accIdx[p.AccountID] = i
	}
	pinned := map[int]int{} // hero → player index
	used := make([]bool, len(roster.Players))
	var rest []int
	for _, h := range uniqueSorted(heroes) {
		acc, ok := fixed[h]
		if !ok || acc <= 0 {
			rest = append(rest, h)
			continue
		}
		pi, ok := accIdx[acc]
		if !ok || used[pi] {
			rest = append(rest, h)
			continue
		}
		pinned[h] = pi
		used[pi] = true
	}
	// Match the remainder on free seats by temporarily zeroing used players'
	// SoftP for pinned heroes only — simpler: build a sub-roster of free players.
	freePlayers := make([]Player, 0, len(roster.Players))
	freeIdx := make([]int, 0, len(roster.Players))
	for i, p := range roster.Players {
		if used[i] {
			continue
		}
		freePlayers = append(freePlayers, p)
		freeIdx = append(freeIdx, i)
	}
	sub := &Roster{Players: freePlayers}
	subAsg := Assign(rest, sub)
	out := Assignment{HeroToPlayer: map[int]int{}, Feasibility: 1}
	prod := 1.0
	n := 0
	for h, pi := range pinned {
		out.HeroToPlayer[h] = pi
		p := roster.Players[pi].SoftP[h]
		if p <= 0 {
			p = 1e-12
		}
		prod *= p
		n++
	}
	for h, subPi := range subAsg.HeroToPlayer {
		if subPi < 0 || subPi >= len(freeIdx) {
			continue
		}
		pi := freeIdx[subPi]
		out.HeroToPlayer[h] = pi
		p := roster.Players[pi].SoftP[h]
		if p <= 0 {
			p = 1e-12
		}
		prod *= p
		n++
	}
	if n > 0 {
		out.Feasibility = pow(prod, 1/float64(n))
	}
	return out
}

func pow(a, e float64) float64 { return math.Pow(a, e) }

// Feasibility is Assign(...).Feasibility.
func Feasibility(heroes []int, roster *Roster) float64 {
	return Assign(heroes, roster).Feasibility
}

// FeasibilityFixed is AssignFixed(...).Feasibility.
func FeasibilityFixed(heroes []int, roster *Roster, fixed map[int]int64) float64 {
	return AssignFixed(heroes, roster, fixed).Feasibility
}

// PlayerFor returns the nickname assigned to hero under the best matching of
// heroes onto the roster, or "" when unknown.
func PlayerFor(hero int, heroes []int, roster *Roster) string {
	return PlayerForFixed(hero, heroes, roster, nil)
}

// PlayerForFixed is PlayerFor with captain-pinned hero→account seats.
func PlayerForFixed(hero int, heroes []int, roster *Roster, fixed map[int]int64) string {
	if roster == nil {
		return ""
	}
	a := AssignFixed(heroes, roster, fixed)
	pi, ok := a.HeroToPlayer[hero]
	if !ok || pi < 0 || pi >= len(roster.Players) {
		return ""
	}
	n := roster.Players[pi].Nickname
	if n != "" {
		return n
	}
	return formatAccount(roster.Players[pi].AccountID)
}

func formatAccount(id int64) string {
	if id <= 0 {
		return ""
	}
	return "игрок"
}

func uniqueSorted(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}
	seen := map[int]bool{}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}
