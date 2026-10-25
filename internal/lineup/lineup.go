package lineup

import (
	"sort"

	"cmnet/internal/capability"
	"cmnet/internal/roles"
)

// NumPositions is Captains Mode lanes 1-5.
const NumPositions = 5

// Slot is one lane seat: who plays what.
type Slot struct {
	Position  int   `json:"position"` // 1-5
	AccountID int64 `json:"accountId,omitempty"`
	HeroID    int   `json:"heroId,omitempty"`
	Locked    bool  `json:"locked"` // captain override; Sync must not clobber
}

// Side is the five seats for one team, indexed by position-1.
type Side [NumPositions]Slot

// Player is a scouted account shown in the lineup picker.
type Player struct {
	AccountID int64  `json:"accountId"`
	Name      string `json:"name"`
}

// Empty returns five unlocked empty seats.
func Empty() Side {
	var s Side
	for i := 0; i < NumPositions; i++ {
		s[i].Position = i + 1
	}
	return s
}

// RoleName is the Russian lane label for positions 1-5.
func RoleName(pos int) string {
	switch pos {
	case 1:
		return "керри"
	case 2:
		return "мид"
	case 3:
		return "офф"
	case 4:
		return "софт"
	case 5:
		return "хард"
	default:
		if pos > 0 {
			return "поз. " + itoa(pos)
		}
		return ""
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// HeroPos returns hero id → position for seats that have both.
func (s Side) HeroPos() map[int]int {
	out := map[int]int{}
	for i := range s {
		if s[i].HeroID > 0 && s[i].Position >= 1 {
			out[s[i].HeroID] = s[i].Position
		}
	}
	return out
}

// HeroPlayer returns hero id → account id for seats that have both.
func (s Side) HeroPlayer() map[int]int64 {
	out := map[int]int64{}
	for i := range s {
		if s[i].HeroID > 0 && s[i].AccountID > 0 {
			out[s[i].HeroID] = s[i].AccountID
		}
	}
	return out
}

// Sync reconciles seats with the side's current picks.
// Locked seats keep AccountID (and HeroID if that hero is still picked).
// Unlocked seats are reassigned deterministically via roles.MAP + capability.Assign.
func Sync(prev Side, picks []int, prior *roles.Prior, elig map[int][]int, roster *capability.Roster) Side {
	out := Empty()
	picked := map[int]bool{}
	for _, h := range picks {
		if h > 0 {
			picked[h] = true
		}
	}

	// Carry locked seats forward when their hero is still on the board (or no hero yet).
	usedPos := map[int]bool{}
	usedHero := map[int]bool{}
	usedAcc := map[int64]bool{}
	for i := range prev {
		ps := prev[i]
		if !ps.Locked {
			continue
		}
		if ps.HeroID > 0 && !picked[ps.HeroID] {
			ps.HeroID = 0 // hero got undrafted
		}
		idx := ps.Position - 1
		if idx < 0 || idx >= NumPositions {
			continue
		}
		out[idx] = Slot{Position: ps.Position, AccountID: ps.AccountID, HeroID: ps.HeroID, Locked: true}
		usedPos[ps.Position] = true
		if ps.HeroID > 0 {
			usedHero[ps.HeroID] = true
		}
		if ps.AccountID > 0 {
			usedAcc[ps.AccountID] = true
		}
	}

	// Free heroes (not locked to a seat).
	var free []int
	for _, h := range picks {
		if h > 0 && !usedHero[h] {
			free = append(free, h)
		}
	}
	sort.Ints(free)

	// Free positions.
	var freePos []int
	for p := 1; p <= NumPositions; p++ {
		if !usedPos[p] {
			freePos = append(freePos, p)
		}
	}

	// Assign free heroes → free positions via MAP on the full pick set, then
	// keep only mappings onto free seats / free heroes.
	asg := roles.MAP(append([]int(nil), picks...), prior, elig)
	heroToPos := map[int]int{}
	if asg != nil {
		for h, pos := range asg {
			heroToPos[h] = pos
		}
	} else if prior != nil {
		for _, h := range free {
			heroToPos[h] = prior.Mode(h)
		}
	}

	// Place free heroes into preferred free positions; conflicts → next free pos.
	placed := map[int]int{} // hero → pos
	for _, h := range free {
		want := heroToPos[h]
		if want >= 1 && !usedPos[want] {
			placed[h] = want
			usedPos[want] = true
			continue
		}
		for _, p := range freePos {
			if !usedPos[p] {
				placed[h] = p
				usedPos[p] = true
				break
			}
		}
	}
	for h, pos := range placed {
		out[pos-1].HeroID = h
		out[pos-1].Position = pos
		usedHero[h] = true
	}

	// Locked seats without a hero yet: fill from MAP when that lane is preferred
	// and the hero is still free (captain named the player before the pick landed).
	for _, h := range free {
		if usedHero[h] {
			continue
		}
		pos := heroToPos[h]
		if pos < 1 || pos > NumPositions {
			continue
		}
		i := pos - 1
		if out[i].Locked && out[i].HeroID == 0 {
			out[i].HeroID = h
			usedHero[h] = true
		}
	}

	// Player assignment for seats that have a hero but no locked account.
	if roster != nil {
		var heroes []int
		for i := range out {
			if out[i].HeroID > 0 {
				heroes = append(heroes, out[i].HeroID)
			}
		}
		fixed := map[int]int64{}
		for i := range out {
			if out[i].Locked && out[i].HeroID > 0 && out[i].AccountID > 0 {
				fixed[out[i].HeroID] = out[i].AccountID
			}
		}
		a := capability.AssignFixed(heroes, roster, fixed)
		for h, pi := range a.HeroToPlayer {
			if pi < 0 || pi >= len(roster.Players) {
				continue
			}
			acc := roster.Players[pi].AccountID
			for i := range out {
				if out[i].HeroID != h {
					continue
				}
				if out[i].Locked && out[i].AccountID > 0 {
					break
				}
				if usedAcc[acc] && out[i].AccountID != acc {
					// Prefer leaving empty over double-booking unlocked seats.
					break
				}
				out[i].AccountID = acc
				usedAcc[acc] = true
				break
			}
		}
	}
	return out
}

// AssignHero puts heroID on position and locks it so Sync will not reshuffle.
// If the hero sat elsewhere, swaps with whoever was on the destination; both
// seats stay locked so the captain's placement survives the next recompute.
func (s Side) AssignHero(position, heroID int) Side {
	if position < 1 || position > NumPositions || heroID <= 0 {
		return s
	}
	out := s
	dst := position - 1
	src := -1
	for j := range out {
		if out[j].HeroID == heroID {
			src = j
			break
		}
	}
	if src == dst {
		out[dst].Locked = true
		out[dst].Position = position
		return out
	}
	other := out[dst].HeroID
	out[dst].HeroID = heroID
	out[dst].Position = position
	out[dst].Locked = true
	if src >= 0 {
		out[src].HeroID = other
		out[src].Locked = true
	}
	return out
}

// SetAccount pins a player to a seat (and locks it). Removes the account from
// other seats on this side so SoftP does not double-book.
func (s Side) SetAccount(position int, accountID int64) Side {
	if position < 1 || position > NumPositions || accountID <= 0 {
		return s
	}
	out := s
	i := position - 1
	for j := range out {
		if j != i && out[j].AccountID == accountID {
			out[j].AccountID = 0
		}
	}
	out[i].Position = position
	out[i].AccountID = accountID
	out[i].Locked = true
	return out
}

// ClearAccount drops the player pin. Hero lock stays so a manual role placement
// is not undone by picking "Игрок…" in the UI.
func (s Side) ClearAccount(position int) Side {
	if position < 1 || position > NumPositions {
		return s
	}
	out := s
	i := position - 1
	out[i].AccountID = 0
	if out[i].HeroID == 0 {
		out[i].Locked = false
	}
	return out
}

// ResetSeat unlocks and empties a seat (full auto again).
func (s Side) ResetSeat(position int) Side {
	if position < 1 || position > NumPositions {
		return s
	}
	out := s
	out[position-1] = Slot{Position: position}
	return out
}
