// Package heroagg computes per-position, per-lane and per-player aggregates from
// the local match cache.
//
// Why from our own cache rather than from STRATZ's aggregate endpoints: those
// describe a different population. STRATZ's per-position meta is ranked all-pick
// across a rank bracket; the cache holds the drafts this advisor was actually
// trained on. Both are useful and they disagree, so the two are kept side by side
// and tagged with their source rather than pooled — an aggregate that silently
// mixes populations is worse than either of them.
//
// Determinism: everything ordered here is sorted with sort.SliceStable and an
// explicit hero-id tie-break, and nothing iterates a map where order matters. The
// per-position and per-lane results are baked into the bundle, so two machines
// must agree on them exactly (§6).
package heroagg

import (
	"sort"

	"cmnet/internal/cache"
)

// Lane is one of the three lanes a draft is evaluated over.
type Lane string

const (
	LaneSafe Lane = "safe" // positions 1 + 5
	LaneMid  Lane = "mid"  // position 2, solo
	LaneOff  Lane = "off"  // positions 3 + 4
)

// Options tune the aggregation.
type Options struct {
	// MinMatches is the floor below which a cell is dropped rather than reported
	// with a win rate computed from a handful of games.
	MinMatches int
	// Prior is the pseudo-count pulling small samples toward the base rate, the
	// same idea (and default) as the synergy matrices use.
	Prior float64
}

const (
	defaultMinMatches = 20
	// DefaultPrior matches matrices.DefaultPrior: both answer "how much evidence
	// before a rate is believed", and having two different answers in one bundle
	// would be arbitrary.
	DefaultPrior = 2.0
)

func (o Options) withDefaults() Options {
	if o.MinMatches <= 0 {
		o.MinMatches = defaultMinMatches
	}
	if o.Prior <= 0 {
		o.Prior = DefaultPrior
	}
	return o
}

// HeroPosStat is one hero's record in one lane position.
type HeroPosStat struct {
	HeroID   int     `json:"hero_id"`
	Position int     `json:"position"`
	Matches  int     `json:"matches"`
	Wins     int     `json:"wins"`
	PickRate float64 `json:"pick_rate"` // share of drafts where this hero took this position
	WinRate  float64 `json:"win_rate"`  // smoothed toward the position's base rate
}

// HeroBanStat is one hero's ban record.
//
// Bans carry no position: they happen before lanes exist, and attributing one to
// a role would be inventing a fact the data does not contain.
type HeroBanStat struct {
	HeroID  int     `json:"hero_id"`
	Bans    int     `json:"bans"`
	BanRate float64 `json:"ban_rate"` // share of drafts that banned this hero
}

// LaneStat is one lane pairing's record.
//
// Heroes is sorted ascending, and the solo lane leaves the second slot at 0, so a
// pairing has exactly one key regardless of which side reported it first.
type LaneStat struct {
	Lane    Lane    `json:"lane"`
	Heroes  [2]int  `json:"heroes"`
	Matches int     `json:"matches"`
	Wins    int     `json:"wins"`
	WinRate float64 `json:"win_rate"`
	// Lift is the smoothed win rate minus the lane's base rate: how much this
	// pairing is worth over an average one, which is the comparable number.
	Lift float64 `json:"lift"`
}

// PlayerHeroStat is one account's record on one hero.
type PlayerHeroStat struct {
	AccountID int64   `json:"account_id"`
	HeroID    int     `json:"hero_id"`
	Matches   int     `json:"matches"`
	Wins      int     `json:"wins"`
	WinRate   float64 `json:"win_rate"`
}

// PlayerPosStat is one account's record in one position.
type PlayerPosStat struct {
	AccountID int64   `json:"account_id"`
	Position  int     `json:"position"`
	Matches   int     `json:"matches"`
	Share     float64 `json:"share"` // fraction of this player's games in this position
}

// Stats is everything computed in one pass over the corpus.
type Stats struct {
	// Drafts is how many matches contributed, the denominator of the rates.
	Drafts int
	// Positions holds the per-(hero, position) rows, ordered by position then by
	// matches descending then hero id.
	Positions []HeroPosStat
	// Bans is ordered by bans descending then hero id.
	Bans []HeroBanStat
	// Lanes is ordered by lane, then matches descending, then hero ids.
	Lanes []LaneStat
	// PlayerHeroes / PlayerPositions are per-account and are NOT baked into the
	// bundle: they describe local knowledge about specific people, and a shared
	// bundle must not depend on whose cache it was built from (§6/§9).
	PlayerHeroes    []PlayerHeroStat
	PlayerPositions []PlayerPosStat
}

// counter accumulates games and wins.
type counter struct {
	matches int
	wins    int
}

func (c *counter) add(win bool) {
	c.matches++
	if win {
		c.wins++
	}
}

// smoothed returns the win rate pulled toward base by prior pseudo-games.
//
// Without this a hero with three games and three wins outranks every real pick in
// the position, which is how a per-position panel ends up recommending noise.
func smoothed(c counter, base, prior float64) float64 {
	if c.matches == 0 {
		return base
	}
	return (float64(c.wins) + prior*base) / (float64(c.matches) + prior)
}

// posKey identifies a (hero, position) cell.
type posKey struct {
	hero int
	pos  int
}

// laneKey identifies a lane pairing.
type laneKey struct {
	lane Lane
	a, b int
}

// Compute aggregates the corpus.
//
// Matches without participants contribute only to the ban rates: the draft rows
// say which heroes were taken, never who played them or where, so per-position
// and per-lane cells simply have nothing to add from them.
func Compute(matches []cache.Match, opts Options) *Stats {
	opts = opts.withDefaults()
	st := &Stats{}

	posCells := map[posKey]*counter{}
	posTotals := map[int]*counter{} // per position, for the base rate
	banCells := map[int]int{}
	laneCells := map[laneKey]*counter{}
	laneTotals := map[Lane]*counter{}
	playerHero := map[[2]int64]*counter{}
	playerPos := map[[2]int64]int{}
	playerTotal := map[int64]int{}

	for _, m := range matches {
		st.Drafts++
		for _, pb := range m.PickBans {
			if !pb.IsPick {
				banCells[pb.HeroID]++
			}
		}
		if len(m.Players) == 0 {
			continue
		}

		byPos := map[int][]cache.MatchPlayer{}
		for _, p := range m.Players {
			win := (p.IsRadiant && m.RadiantWin) || (!p.IsRadiant && !m.RadiantWin)

			if p.Position != nil {
				pos := *p.Position
				key := posKey{hero: p.HeroID, pos: pos}
				if posCells[key] == nil {
					posCells[key] = &counter{}
				}
				posCells[key].add(win)
				if posTotals[pos] == nil {
					posTotals[pos] = &counter{}
				}
				posTotals[pos].add(win)
				byPos[pos] = append(byPos[pos], p)

				if p.AccountID != nil {
					pk := [2]int64{*p.AccountID, int64(pos)}
					playerPos[pk]++
				}
			}
			if p.AccountID != nil {
				hk := [2]int64{*p.AccountID, int64(p.HeroID)}
				if playerHero[hk] == nil {
					playerHero[hk] = &counter{}
				}
				playerHero[hk].add(win)
				playerTotal[*p.AccountID]++
			}
		}
		accumulateLanes(m, byPos, laneCells, laneTotals)
	}

	st.Positions = buildPositions(posCells, posTotals, st.Drafts, opts)
	st.Bans = buildBans(banCells, st.Drafts, opts)
	st.Lanes = buildLanes(laneCells, laneTotals, opts)
	st.PlayerHeroes = buildPlayerHeroes(playerHero, opts)
	st.PlayerPositions = buildPlayerPositions(playerPos, playerTotal)
	return st
}

// accumulateLanes records the three lane pairings of one match, per side.
//
// A pairing needs both of its positions on the same side to exist; a match where
// STRATZ labelled only some players contributes the lanes it can and skips the
// rest, rather than pairing a carry with nobody.
func accumulateLanes(m cache.Match, byPos map[int][]cache.MatchPlayer,
	cells map[laneKey]*counter, totals map[Lane]*counter) {
	for _, side := range []bool{true, false} {
		win := (side && m.RadiantWin) || (!side && !m.RadiantWin)
		hero := func(pos int) int {
			for _, p := range byPos[pos] {
				if p.IsRadiant == side {
					return p.HeroID
				}
			}
			return 0
		}
		pairs := []struct {
			lane Lane
			a, b int
		}{
			{LaneSafe, hero(1), hero(5)},
			{LaneOff, hero(3), hero(4)},
			{LaneMid, hero(2), 0},
		}
		for _, pr := range pairs {
			if pr.a == 0 || (pr.lane != LaneMid && pr.b == 0) {
				continue
			}
			a, b := pr.a, pr.b
			if b != 0 && b < a {
				a, b = b, a
			}
			k := laneKey{lane: pr.lane, a: a, b: b}
			if cells[k] == nil {
				cells[k] = &counter{}
			}
			cells[k].add(win)
			if totals[pr.lane] == nil {
				totals[pr.lane] = &counter{}
			}
			totals[pr.lane].add(win)
		}
	}
}

func buildPositions(cells map[posKey]*counter, totals map[int]*counter, drafts int, opts Options) []HeroPosStat {
	out := make([]HeroPosStat, 0, len(cells))
	for k, c := range cells {
		if c.matches < opts.MinMatches {
			continue
		}
		base := 0.5
		if t := totals[k.pos]; t != nil && t.matches > 0 {
			base = float64(t.wins) / float64(t.matches)
		}
		row := HeroPosStat{
			HeroID: k.hero, Position: k.pos,
			Matches: c.matches, Wins: c.wins,
			WinRate: smoothed(*c, base, opts.Prior),
		}
		if drafts > 0 {
			row.PickRate = float64(c.matches) / float64(drafts)
		}
		out = append(out, row)
	}
	// Map iteration order is randomised in Go, so the slice is sorted into a total
	// order before it can be baked into a bundle two machines must agree on.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		if out[i].Matches != out[j].Matches {
			return out[i].Matches > out[j].Matches
		}
		return out[i].HeroID < out[j].HeroID
	})
	return out
}

func buildBans(cells map[int]int, drafts int, opts Options) []HeroBanStat {
	out := make([]HeroBanStat, 0, len(cells))
	for hero, bans := range cells {
		if bans < opts.MinMatches {
			continue
		}
		row := HeroBanStat{HeroID: hero, Bans: bans}
		if drafts > 0 {
			row.BanRate = float64(bans) / float64(drafts)
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Bans != out[j].Bans {
			return out[i].Bans > out[j].Bans
		}
		return out[i].HeroID < out[j].HeroID
	})
	return out
}

func buildLanes(cells map[laneKey]*counter, totals map[Lane]*counter, opts Options) []LaneStat {
	out := make([]LaneStat, 0, len(cells))
	for k, c := range cells {
		if c.matches < opts.MinMatches {
			continue
		}
		base := 0.5
		if t := totals[k.lane]; t != nil && t.matches > 0 {
			base = float64(t.wins) / float64(t.matches)
		}
		wr := smoothed(*c, base, opts.Prior)
		out = append(out, LaneStat{
			Lane: k.lane, Heroes: [2]int{k.a, k.b},
			Matches: c.matches, Wins: c.wins,
			WinRate: wr, Lift: wr - base,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Lane != out[j].Lane {
			return out[i].Lane < out[j].Lane
		}
		if out[i].Matches != out[j].Matches {
			return out[i].Matches > out[j].Matches
		}
		if out[i].Heroes[0] != out[j].Heroes[0] {
			return out[i].Heroes[0] < out[j].Heroes[0]
		}
		return out[i].Heroes[1] < out[j].Heroes[1]
	})
	return out
}

// playerMinMatches is deliberately lower than the corpus-wide floor: a scouted
// opponent has a handful of games, and demanding twenty would report nothing at
// all about the person we are about to draft against.
const playerMinMatches = 3

func buildPlayerHeroes(cells map[[2]int64]*counter, opts Options) []PlayerHeroStat {
	out := make([]PlayerHeroStat, 0, len(cells))
	for k, c := range cells {
		if c.matches < playerMinMatches {
			continue
		}
		out = append(out, PlayerHeroStat{
			AccountID: k[0], HeroID: int(k[1]),
			Matches: c.matches, Wins: c.wins,
			WinRate: smoothed(*c, 0.5, opts.Prior),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AccountID != out[j].AccountID {
			return out[i].AccountID < out[j].AccountID
		}
		if out[i].Matches != out[j].Matches {
			return out[i].Matches > out[j].Matches
		}
		return out[i].HeroID < out[j].HeroID
	})
	return out
}

func buildPlayerPositions(cells map[[2]int64]int, totals map[int64]int) []PlayerPosStat {
	out := make([]PlayerPosStat, 0, len(cells))
	for k, n := range cells {
		row := PlayerPosStat{AccountID: k[0], Position: int(k[1]), Matches: n}
		if t := totals[k[0]]; t > 0 {
			row.Share = float64(n) / float64(t)
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AccountID != out[j].AccountID {
			return out[i].AccountID < out[j].AccountID
		}
		if out[i].Matches != out[j].Matches {
			return out[i].Matches > out[j].Matches
		}
		return out[i].Position < out[j].Position
	})
	return out
}

// TopByPosition returns the n best heroes for a position by smoothed win rate,
// breaking ties by popularity and then hero id.
func (s *Stats) TopByPosition(pos, n int) []HeroPosStat {
	var rows []HeroPosStat
	for _, r := range s.Positions {
		if r.Position == pos {
			rows = append(rows, r)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].WinRate != rows[j].WinRate {
			return rows[i].WinRate > rows[j].WinRate
		}
		if rows[i].Matches != rows[j].Matches {
			return rows[i].Matches > rows[j].Matches
		}
		return rows[i].HeroID < rows[j].HeroID
	})
	if n > 0 && len(rows) > n {
		rows = rows[:n]
	}
	return rows
}

// LaneLift returns the lift of a pairing, and whether it was observed often
// enough to have one. Order of the two heroes does not matter.
func (s *Stats) LaneLift(lane Lane, a, b int) (float64, bool) {
	if b != 0 && b < a {
		a, b = b, a
	}
	for _, r := range s.Lanes {
		if r.Lane == lane && r.Heroes[0] == a && r.Heroes[1] == b {
			return r.Lift, true
		}
	}
	return 0, false
}
