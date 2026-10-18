// Package track keeps live match memory across GSI ticks: who is visible,
// last seen, structures, Roshan observations, and recent events.
package track

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"cmnet/internal/draft"
	"cmnet/internal/gsi/observe"
)

// HeroRef is one roster slot. UnitName is the minimap identity
// (npc_dota_hero_*). ID is optional and unused for matching when empty.
type HeroRef struct {
	UnitName string
	ID       int
}

func (h HeroRef) Name() string { return h.UnitName }

// Roster is the 5+5 composition. It is not inferred from the minimap.
type Roster struct {
	AllySide observe.Team
	Allies   []HeroRef
	Enemies  []HeroRef
}

func (r Roster) Valid() bool {
	return r.AllySide == observe.TeamRadiant || r.AllySide == observe.TeamDire
}

func (r Roster) EnemySide() observe.Team {
	if r.AllySide == observe.TeamRadiant {
		return observe.TeamDire
	}
	if r.AllySide == observe.TeamDire {
		return observe.TeamRadiant
	}
	return observe.TeamUnknown
}

func (r Roster) allySet() map[string]int {
	out := make(map[string]int, len(r.Allies))
	for i, h := range r.Allies {
		if h.UnitName != "" {
			out[h.UnitName] = i
		}
	}
	return out
}

func (r Roster) enemySet() map[string]int {
	out := make(map[string]int, len(r.Enemies))
	for i, h := range r.Enemies {
		if h.UnitName != "" {
			out[h.UnitName] = i
		}
	}
	return out
}

// RosterProvider supplies the match roster. The tracker does not import TUI
// or draft UI types; callers adapt Snapshot / a JSON file onto this.
type RosterProvider interface {
	Roster() (Roster, bool)
}

// StaticRoster is a RosterProvider that always returns R.
type StaticRoster struct{ R Roster }

func (s StaticRoster) Roster() (Roster, bool) { return s.R, s.R.Valid() }

// FileRoster loads {ally_side, allies, enemies} from JSON.
type FileRoster struct {
	AllySide string   `json:"ally_side"`
	Allies   []string `json:"allies"`
	Enemies  []string `json:"enemies"`
}

func LoadRosterFile(path string) (Roster, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Roster{}, err
	}
	var f FileRoster
	if err := json.Unmarshal(b, &f); err != nil {
		return Roster{}, fmt.Errorf("roster json: %w", err)
	}
	return f.Roster(), nil
}

func (f FileRoster) Roster() Roster {
	side := observe.TeamUnknown
	switch strings.ToLower(strings.TrimSpace(f.AllySide)) {
	case "radiant":
		side = observe.TeamRadiant
	case "dire":
		side = observe.TeamDire
	}
	r := Roster{AllySide: side}
	for _, n := range f.Allies {
		r.Allies = append(r.Allies, HeroRef{UnitName: n})
	}
	for _, n := range f.Enemies {
		r.Enemies = append(r.Enemies, HeroRef{UnitName: n})
	}
	return r
}

// NameLookup maps a Valve hero id to npc_dota_hero_*.
type NameLookup interface {
	HeroName(id int) (string, bool)
}

// MapNames is a static id -> unit-name lookup.
type MapNames map[int]string

func (m MapNames) HeroName(id int) (string, bool) {
	if m == nil {
		return "", false
	}
	n, ok := m[id]
	return n, ok && n != ""
}

// DraftRoster adapts a completed draft.Snapshot into a Roster.
type DraftRoster struct {
	Snap  *draft.Snapshot
	Names NameLookup
}

func (d DraftRoster) Roster() (Roster, bool) {
	if d.Snap == nil || !d.Snap.LocalTeamOK || d.Names == nil {
		return Roster{}, false
	}
	allySide := observe.TeamRadiant
	if d.Snap.LocalTeam == draft.Dire {
		allySide = observe.TeamDire
	}
	our := d.Snap.Picks(d.Snap.LocalTeam)
	their := d.Snap.Picks(d.Snap.LocalTeam.Opponent())
	if len(our) < draft.PicksPerTeam || len(their) < draft.PicksPerTeam {
		return Roster{}, false
	}
	r := Roster{AllySide: allySide}
	for _, id := range our {
		name, ok := d.Names.HeroName(id)
		if !ok {
			return Roster{}, false
		}
		r.Allies = append(r.Allies, HeroRef{UnitName: name, ID: id})
	}
	for _, id := range their {
		name, ok := d.Names.HeroName(id)
		if !ok {
			return Roster{}, false
		}
		r.Enemies = append(r.Enemies, HeroRef{UnitName: name, ID: id})
	}
	return r, r.Valid()
}
