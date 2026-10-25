package session

import (
	"fmt"

	"cmnet/internal/lineup"
)

// LineupView is the draft-screen roster panel payload.
type LineupView struct {
	Our     []lineup.Slot   `json:"our"`
	Enemy   []lineup.Slot   `json:"enemy"`
	Allies  []lineup.Player `json:"allies"`
	Enemies []lineup.Player `json:"enemies"`
}

// Lineup returns the current seats and scouted player pickers.
func (s *Session) Lineup() LineupView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lineupViewLocked()
}

func (s *Session) lineupViewLocked() LineupView {
	v := LineupView{
		Our:   slotsOf(s.ourLineup),
		Enemy: slotsOf(s.enemyLineup),
	}
	if rost := s.cfg.Rec.CapRoster(); rost != nil {
		for _, p := range rost.Players {
			v.Allies = append(v.Allies, lineup.Player{AccountID: p.AccountID, Name: p.Nickname})
		}
	}
	if er := s.cfg.Rec.EnemyRoster(); er != nil {
		for _, p := range er.Players {
			v.Enemies = append(v.Enemies, lineup.Player{AccountID: p.AccountID, Name: p.Nickname})
		}
	}
	// Also include header profiles that may not be in the scoring roster (e.g. <5 allies).
	for _, p := range s.cfg.LogHeader.Allies {
		if p.AccountID <= 0 || hasPlayer(v.Allies, p.AccountID) {
			continue
		}
		name := p.Persona
		if name == "" {
			name = "игрок"
		}
		v.Allies = append(v.Allies, lineup.Player{AccountID: p.AccountID, Name: name})
	}
	for _, p := range s.cfg.LogHeader.Enemies {
		if p.AccountID <= 0 || hasPlayer(v.Enemies, p.AccountID) {
			continue
		}
		name := p.Persona
		if name == "" {
			name = "игрок"
		}
		v.Enemies = append(v.Enemies, lineup.Player{AccountID: p.AccountID, Name: name})
	}
	return v
}

func slotsOf(side lineup.Side) []lineup.Slot {
	out := make([]lineup.Slot, lineup.NumPositions)
	for i := range side {
		out[i] = side[i]
	}
	return out
}

func hasPlayer(ps []lineup.Player, id int64) bool {
	for _, p := range ps {
		if p.AccountID == id {
			return true
		}
	}
	return false
}

// SetLineupSlot applies a captain override on one seat (our or enemy).
// side is "our" or "enemy". clear drops the player pin only (hero lock kept).
// heroID > 0 moves/swaps that hero onto position; accountID > 0 pins the player.
func (s *Session) SetLineupSlot(side string, position int, accountID int64, heroID int, clear bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if position < 1 || position > lineup.NumPositions {
		return fmt.Errorf("position must be 1..5")
	}
	apply := func(cur lineup.Side) lineup.Side {
		if clear {
			return cur.ClearAccount(position)
		}
		if heroID > 0 {
			cur = cur.AssignHero(position, heroID)
		}
		if accountID > 0 {
			cur = cur.SetAccount(position, accountID)
		}
		return cur
	}
	switch side {
	case "our", "ally", "allies":
		s.ourLineup = apply(s.ourLineup)
	case "enemy", "enemies":
		s.enemyLineup = apply(s.enemyLineup)
	default:
		return fmt.Errorf("side must be our|enemy")
	}
	s.recompute()
	return nil
}

// syncLineupLocked refreshes seats from the board. Callers hold mu.
func (s *Session) syncLineupLocked() {
	if s.view == nil || s.cfg.Rec == nil {
		return
	}
	ourPicks := s.view.Picks(s.cfg.OurTeam)
	enemyPicks := s.view.Picks(s.cfg.OurTeam.Opponent())
	// Sync on empty picks still keeps Locked seats (player pins before first pick).
	// Do not Empty() — that wiped SetLineupSlot on every recompute with a blank board.
	prior := s.cfg.Rec.RolePrior()
	elig := s.cfg.Rec.PosByID()
	s.ourLineup = lineup.Sync(s.ourLineup, ourPicks, prior, elig, s.cfg.Rec.CapRoster())
	s.enemyLineup = lineup.Sync(s.enemyLineup, enemyPicks, prior, elig, s.cfg.Rec.EnemyRoster())
	s.lineupInit = true
}

// applyLineupFixLocked pushes seat pins into the recommender. Callers hold mu.
func (s *Session) applyLineupFixLocked() {
	if s.cfg.Rec == nil {
		return
	}
	s.cfg.Rec.WithLineupFix(s.ourLineup.HeroPos(), s.enemyLineup.HeroPos(), s.ourLineup.HeroPlayer())
}
