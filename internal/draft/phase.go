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

// Slot is one Captains Mode step expressed relative to draft order rather than
// to a side: whether the first- or the second-pick team acts, and how. Expressing
// the order this way lets one pattern serve both seatings.
type Slot struct {
	Second bool   `json:"second"` // false = first-pick team, true = second-pick team
	Action Action `json:"action"`
}

// Order is a full Captains Mode schedule in first/second terms.
//
// It is a value, not a package constant, because Valve changes it between patches
// (7.34 rebuilt it from scratch) and a stale schedule is silently wrong
// everywhere at once: the manual board asks for the wrong action, the planner
// simulates the wrong replies, and the predictor's phase features describe a
// phase the game is not in. Training reads the schedule the cached drafts were
// actually played under (train.InferOrder) and ships it in the bundle; this
// default only covers the case where no bundle carries one.
type Order []Slot

// CMSteps is the length of a full Captains Mode draft.
const CMSteps = 24

// BansPerTeam is fixed by the mode (7 bans per side; PicksPerTeam is in view.go).
const BansPerTeam = 7

var (
	fBan  = Slot{Second: false, Action: Ban}
	sBan  = Slot{Second: true, Action: Ban}
	fPick = Slot{Second: false, Action: Pick}
	sPick = Slot{Second: true, Action: Pick}
)

// defaultOrder is the Captains Mode schedule introduced in 7.34 and still in force
// in 7.41 (first-pick team = F, second-pick team = S):
//
//	ban 1   F F S S F S S   (7)  — F bans 3, S bans 4
//	pick 1  F S             (2)
//	ban 2   F F S           (3)
//	pick 2  S F F S S F     (6)
//	ban 3   F S F S         (4)
//	pick 3  F S             (2)
//
// Per side that is bans 3-2-2 (F) / 4-1-2 (S) and picks 1-3-1 for both, with the
// last pick still going to the second-pick team.
var defaultOrder = Order{
	fBan, fBan, sBan, sBan, fBan, sBan, sBan,
	fPick, sPick,
	fBan, fBan, sBan,
	sPick, fPick, fPick, sPick, sPick, fPick,
	fBan, sBan, fBan, sBan,
	fPick, sPick,
}

// DefaultOrder returns a copy of the built-in schedule.
func DefaultOrder() Order { return append(Order(nil), defaultOrder...) }

// Valid reports whether o is a structurally possible Captains Mode schedule:
// 24 steps, each side gets exactly 5 picks and 7 bans, and the first step belongs
// to the first-pick team. (That last one is a convention, not a game rule: the
// cache does not record the seating, so the side that acts first is what
// "first" means everywhere in this codebase, from training to the GSI parser.)
func (o Order) Valid() bool {
	if len(o) != CMSteps || o[0].Second {
		return false
	}
	var picks, bans [2]int
	for _, s := range o {
		side := 0
		if s.Second {
			side = 1
		}
		if s.Action == Pick {
			picks[side]++
		} else {
			bans[side]++
		}
	}
	return picks == [2]int{PicksPerTeam, PicksPerTeam} && bans == [2]int{BansPerTeam, BansPerTeam}
}

// OrDefault returns o when it is valid and the built-in schedule otherwise, so
// callers can pass a bundle's (possibly absent) order straight through.
func (o Order) OrDefault() Order {
	if o.Valid() {
		return o
	}
	return DefaultOrder()
}

// Equal reports whether two schedules are step-for-step identical.
func (o Order) Equal(other Order) bool {
	if len(o) != len(other) {
		return false
	}
	for i := range o {
		if o[i] != other[i] {
			return false
		}
	}
	return true
}

// Phases returns the lengths of the maximal runs of same-action steps — the
// phases of the draft (for the default order: 7, 2, 3, 6, 4, 2).
func (o Order) Phases() []int {
	var out []int
	for i, s := range o {
		if i == 0 || s.Action != o[i-1].Action {
			out = append(out, 0)
		}
		out[len(out)-1]++
	}
	return out
}

// Sequence resolves the schedule into concrete sides for the given first-pick team.
func (o Order) Sequence(firstPick Team) []Step {
	seq := make([]Step, len(o))
	for i, p := range o {
		team := firstPick
		if p.Second {
			team = firstPick.Opponent()
		}
		seq[i] = Step{Team: team, Action: p.Action}
	}
	return seq
}

// CMSequence returns the full Captains Mode step sequence for the given
// first-pick team under the built-in schedule. Prefer Order.Sequence with the
// bundle's order where one is available.
func CMSequence(firstPick Team) []Step {
	return defaultOrder.Sequence(firstPick)
}

// NextStepOf returns the index of the first step at or after from that team acts
// on in seq, and that step. ok is false when the team has nothing left to do.
func NextStepOf(seq []Step, from int, team Team) (idx int, step Step, ok bool) {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(seq); i++ {
		if seq[i].Team == team {
			return i, seq[i], true
		}
	}
	return -1, Step{}, false
}
