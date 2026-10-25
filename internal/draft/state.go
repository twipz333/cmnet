package draft

import "fmt"

// NoHero is the sentinel hero id for an unfilled step. Real Dota hero ids are >= 1.
const NoHero = 0

// DraftState is the live state of a single Captains Mode draft: the fixed step
// sequence plus the hero chosen at each filled step. It is order-preserving and
// contains no maps, so iteration is deterministic (see §6).
type DraftState struct {
	FirstPick  Team
	Seq        []Step
	Selections []int // hero id per step; NoHero until filled. len == len(Seq).
	Cursor     int   // index of the next step to fill; == len(Seq) when complete.
	redo       []int // heroes undone and available to redo (LIFO); cleared by a fresh Apply.
}

// New creates an empty Captains Mode draft for the given first-pick team.
func New(firstPick Team) *DraftState {
	seq := CMSequence(firstPick)
	return &DraftState{
		FirstPick:  firstPick,
		Seq:        seq,
		Selections: make([]int, len(seq)),
		Cursor:     0,
	}
}

// Complete reports whether every step has been filled.
func (d *DraftState) Complete() bool {
	return d.Cursor >= len(d.Seq)
}

// Current returns the step that must be filled next. ok is false when the draft
// is complete.
func (d *DraftState) Current() (step Step, ok bool) {
	if d.Complete() {
		return Step{}, false
	}
	return d.Seq[d.Cursor], true
}

// SetCurrentStep overrides the team and action of the NEXT step to fill, before a
// hero is applied to it. This makes manual input correct under any patch's draft
// order without trusting a hardcoded sequence (the Valve CM order shifts between
// patches, §12): the user marks each action as the game actually presents it. No-op
// when the draft is complete. Already-applied steps are untouched.
func (d *DraftState) SetCurrentStep(team Team, action Action) {
	if d.Complete() {
		return
	}
	d.Seq[d.Cursor] = Step{Team: team, Action: action}
}

// IsTaken reports whether a hero already appears as any pick or ban.
func (d *DraftState) IsTaken(heroID int) bool {
	if heroID == NoHero {
		return false
	}
	for i := 0; i < d.Cursor; i++ {
		if d.Selections[i] == heroID {
			return true
		}
	}
	return false
}

// Apply fills the current step with heroID and advances the cursor. It errors if
// the draft is complete, the id is invalid, or the hero is already taken. A fresh
// Apply clears the redo history (it starts a new branch).
func (d *DraftState) Apply(heroID int) error {
	if err := d.applyInternal(heroID); err != nil {
		return err
	}
	d.redo = d.redo[:0]
	return nil
}

func (d *DraftState) applyInternal(heroID int) error {
	if d.Complete() {
		return fmt.Errorf("draft already complete")
	}
	if heroID == NoHero {
		return fmt.Errorf("invalid hero id %d", heroID)
	}
	if d.IsTaken(heroID) {
		return fmt.Errorf("hero %d already picked or banned", heroID)
	}
	d.Selections[d.Cursor] = heroID
	d.Cursor++
	return nil
}

// Undo reverts the most recent Apply and pushes the cleared hero onto the redo
// stack. It errors if nothing has been applied yet.
func (d *DraftState) Undo() error {
	if d.Cursor == 0 {
		return fmt.Errorf("nothing to undo")
	}
	d.Cursor--
	d.redo = append(d.redo, d.Selections[d.Cursor])
	d.Selections[d.Cursor] = NoHero
	return nil
}

// CanRedo reports whether an undone step is available to redo.
func (d *DraftState) CanRedo() bool { return len(d.redo) > 0 }

// Redo re-applies the most recently undone step. It errors if there is nothing to
// redo. Redo preserves the rest of the redo stack (so repeated redo walks forward).
func (d *DraftState) Redo() error {
	if len(d.redo) == 0 {
		return fmt.Errorf("nothing to redo")
	}
	hero := d.redo[len(d.redo)-1]
	if err := d.applyInternal(hero); err != nil {
		return err
	}
	d.redo = d.redo[:len(d.redo)-1]
	return nil
}

// heroesFor returns, in draft order, the filled hero ids matching team+action.
func (d *DraftState) heroesFor(team Team, action Action) []int {
	out := make([]int, 0, len(d.Seq))
	for i := 0; i < d.Cursor; i++ {
		s := d.Seq[i]
		if s.Team == team && s.Action == action {
			out = append(out, d.Selections[i])
		}
	}
	return out
}

// FirstPickTeam implements View.
func (d *DraftState) FirstPickTeam() Team { return d.FirstPick }

// Picks returns the hero ids picked by team, in draft order.
func (d *DraftState) Picks(team Team) []int { return d.heroesFor(team, Pick) }

// Bans returns the hero ids banned by team, in draft order.
func (d *DraftState) Bans(team Team) []int { return d.heroesFor(team, Ban) }
