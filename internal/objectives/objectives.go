package objectives

import "sort"

// Kind identifies an objective type (for labels, grouping, and bell de-dup in the UI).
type Kind string

const (
	Bounty    Kind = "bounty"
	Power     Kind = "power"
	Wisdom    Kind = "wisdom"
	Lotus     Kind = "lotus"
	Tormentor Kind = "tormentor"
	DayNight  Kind = "daynight"
)

// DefaultLead is the fallback pre-alert window when no override is set.
const DefaultLead = 15

// LeadDisabled (-1) turns a kind off: it is omitted from Upcoming / never Due.
const LeadDisabled = -1

// NoLowerBound means the alert kind has no lower clock limit (active from pre-horn).
const NoLowerBound = -1

// Spawn describes a periodic (or one-shot) objective. It first appears at
// FirstSpawn seconds of GAME CLOCK (the post-horn clock; clock 0 == the horn),
// then repeats every IntervalSec. IntervalSec == 0 means a single spawn (e.g.
// Tormentor, whose later respawns depend on its death time and cannot be predicted
// from the clock alone).
type Spawn struct {
	Kind        Kind   `json:"kind"`
	Label       string `json:"label"`
	FirstSpawn  int    `json:"first_spawn"`  // game-clock seconds of the first spawn
	IntervalSec int    `json:"interval_sec"` // repeat period in seconds; 0 = one-shot
}

// Alert is one objective's next occurrence relative to the current clock.
type Alert struct {
	Kind   Kind   `json:"kind"`
	Label  string `json:"label"`
	NextAt int    `json:"nextAt"`        // game-clock seconds of the next spawn
	NextIn int    `json:"nextIn"`        // seconds until the next spawn (NextAt - clock), always >= 0
	Due    bool   `json:"due"`           // NextIn <= lead for this kind
	TTS    string `json:"tts,omitempty"` // optional speak template ({{nextIn}} etc.); set by alerts package
}

// Leads is the pre-alert window configuration. Default applies to every kind;
// ByKind overrides are personal preference (config/flags) and are not part of the
// shared bundle — teammates may choose different lead times without breaking §6
// draft determinism.
type Leads struct {
	Default int
	ByKind  map[Kind]int // >0 override; -1 disabled; 0 / missing → Default
}

// For returns the lead window for k in seconds, or LeadDisabled when the kind is off.
func (l Leads) For(k Kind) int {
	if l.ByKind != nil {
		if v, ok := l.ByKind[k]; ok {
			if v < 0 {
				return LeadDisabled
			}
			if v > 0 {
				return v
			}
			// v == 0 in the map: fall through to Default
		}
	}
	if l.Default < 0 {
		return LeadDisabled
	}
	if l.Default > 0 {
		return l.Default
	}
	return DefaultLead
}

// Enabled reports whether kind still produces alerts (lead != -1).
func (l Leads) Enabled(k Kind) bool {
	return l.For(k) >= 0
}

// Window is the game-clock range where a kind's alerts are armed.
// From: NoLowerBound (-1) = no lower limit; 0 = from the horn.
// Until: 0 = no upper limit; >0 = inclusive end (e.g. 2100 = 35:00).
type Window struct {
	From  int `json:"from"`
	Until int `json:"until"`
}

// Windows holds per-kind active ranges. Missing kinds are unlimited.
type Windows struct {
	ByKind map[Kind]Window
}

// For returns the active window for k (unlimited when unset).
func (w Windows) For(k Kind) Window {
	if w.ByKind != nil {
		if win, ok := w.ByKind[k]; ok {
			return win
		}
	}
	return Window{From: NoLowerBound, Until: 0}
}

// Contains reports whether clock is inside the kind's active window.
func (w Windows) Contains(k Kind, clock int) bool {
	win := w.For(k)
	if win.From >= 0 && clock < win.From {
		return false
	}
	if win.Until > 0 && clock > win.Until {
		return false
	}
	return true
}

// DefaultObjectives returns the spawn table for the current patch. Values are in
// GAME-CLOCK seconds (clock 0 == the horn).
//
// PATCH-DEPENDENT — verify against the live patch before distributing a bundle.
// As of 7.4x:
//   - Bounty runes:  spawn at 0:00, then every 3:00.
//   - Power runes:   first at 6:00, then every 2:00 (two spawn in the river).
//   - Wisdom runes:  first at 7:00, then every 7:00.
//   - Lotus pools:   first at 3:00, then every 3:00.
//   - Tormentor:     spawns at 20:00 (respawn after death is not clock-predictable).
//   - Day/Night:     flips every 5:00 (first night at 5:00).
func DefaultObjectives() []Spawn {
	return []Spawn{
		{Kind: Bounty, Label: "Bounty runes", FirstSpawn: 0, IntervalSec: 180},
		{Kind: Power, Label: "Power runes", FirstSpawn: 360, IntervalSec: 120},
		{Kind: Wisdom, Label: "Wisdom runes", FirstSpawn: 420, IntervalSec: 420},
		{Kind: Lotus, Label: "Lotus pools", FirstSpawn: 180, IntervalSec: 180},
		{Kind: Tormentor, Label: "Tormentor", FirstSpawn: 1200, IntervalSec: 0},
		{Kind: DayNight, Label: "Day/Night flip", FirstSpawn: 300, IntervalSec: 300},
	}
}

// Upcoming returns, for each spawn whose next occurrence is still ahead, the time to
// that occurrence. clock is the current GAME-CLOCK in seconds (negative before the
// horn). leads.For(kind) is the pre-alert window: an alert is Due when its NextIn
// <= that window. windows silence a kind while the clock is outside its range
// (e.g. wisdom only until 35:00). The result is sorted deterministically by
// (NextIn, Kind). One-shot spawns already in the past are omitted.
func Upcoming(specs []Spawn, clock int, leads Leads, windows Windows) []Alert {
	out := make([]Alert, 0, len(specs))
	for _, s := range specs {
		lead := leads.For(s.Kind)
		if lead < 0 {
			continue
		}
		if !windows.Contains(s.Kind, clock) {
			continue
		}
		next, ok := nextSpawn(s, clock)
		if !ok {
			continue
		}
		// Drop a spawn that sits past the kind's Until (next wisdom at 42:00
		// while Until=35:00) — nothing useful to arm for.
		if win := windows.For(s.Kind); win.Until > 0 && next > win.Until {
			continue
		}
		in := max(next-clock, 0)
		out = append(out, Alert{
			Kind:   s.Kind,
			Label:  s.Label,
			NextAt: next,
			NextIn: in,
			Due:    in <= lead,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].NextIn != out[j].NextIn {
			return out[i].NextIn < out[j].NextIn
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// nextSpawn returns the smallest spawn time >= clock for s. ok is false when a
// one-shot spawn has already passed.
func nextSpawn(s Spawn, clock int) (int, bool) {
	if s.IntervalSec <= 0 { // one-shot
		if s.FirstSpawn >= clock {
			return s.FirstSpawn, true
		}
		return 0, false
	}
	if clock <= s.FirstSpawn {
		return s.FirstSpawn, true
	}
	diff := clock - s.FirstSpawn
	k := diff / s.IntervalSec
	if diff%s.IntervalSec != 0 {
		k++
	}
	return s.FirstSpawn + k*s.IntervalSec, true
}
