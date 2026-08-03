// Package draft models the Captains Mode draft as an ordered sequence of
// pick/ban steps. It is pure domain logic with no external dependencies: the
// recommender and the TUI read draft state from here, and GSI (M2) feeds it.
package draft

// Team identifies one side of the draft.
type Team int

const (
	Radiant Team = iota
	Dire
)

func (t Team) String() string {
	switch t {
	case Radiant:
		return "Radiant"
	case Dire:
		return "Dire"
	default:
		return "Unknown"
	}
}

// Opponent returns the other team.
func (t Team) Opponent() Team {
	if t == Radiant {
		return Dire
	}
	return Radiant
}

// Action is whether a step picks or bans a hero.
type Action int

const (
	Ban Action = iota
	Pick
)

func (a Action) String() string {
	if a == Pick {
		return "Pick"
	}
	return "Ban"
}

// Step is a single draft action: which team acts and whether it is a pick or ban.
type Step struct {
	Team   Team
	Action Action
}

// firstSecond expresses a step relative to draft order (first-pick vs second-pick
// team) so the concrete sequence can be built for whichever side picks first.
type firstSecond struct {
	second bool // false = first-pick team, true = second-pick team
	action Action
}

// cmPattern is the Captains Mode order in first/second terms: 7 bans + 5 picks
// per team, 24 steps total.
//
// NOTE: this is the documented default order and is balanced per team, but the
// exact Valve sequence shifts between patches. In-game, GSI (M2) is the source of
// truth for whose turn it is; this default drives manual input (MVP) and tests.
var cmPattern = []firstSecond{
	// phase 1 — 2 bans each
	{false, Ban}, {true, Ban}, {false, Ban}, {true, Ban},
	// phase 2 — 2 picks each
	{false, Pick}, {true, Pick}, {true, Pick}, {false, Pick},
	// phase 3 — 3 bans each
	{false, Ban}, {true, Ban}, {false, Ban}, {true, Ban}, {false, Ban}, {true, Ban},
	// phase 4 — 2 picks each
	{true, Pick}, {false, Pick}, {false, Pick}, {true, Pick},
	// phase 5 — 2 bans each
	{false, Ban}, {true, Ban}, {false, Ban}, {true, Ban},
	// phase 6 — 1 pick each
	{false, Pick}, {true, Pick},
}

// CMSequence returns the full Captains Mode step sequence for the given
// first-pick team.
func CMSequence(firstPick Team) []Step {
	seq := make([]Step, len(cmPattern))
	for i, p := range cmPattern {
		team := firstPick
		if p.second {
			team = firstPick.Opponent()
		}
		seq[i] = Step{Team: team, Action: p.action}
	}
	return seq
}
