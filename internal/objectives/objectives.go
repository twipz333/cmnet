// Package objectives models Dota 2 map-objective spawn timers (bounty/power/wisdom
// runes, lotus pools, tormentor, day-night) and turns a live game clock — read from
// GSI's map block — into a deterministic list of upcoming-objective alerts.
//
// Spawn timers change between patches, so the table is NOT hardcoded at the call
// sites: it is produced by DefaultObjectives() and baked into the bundle
// (patch-tagged, shared, versioned — §6). Upcoming() is a pure function of the
// clock and the table, so every teammate with the same bundle and the same in-lobby
// game clock sees byte-identical alerts. No map iteration feeds the output ordering.
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
	NextAt int    `json:"nextAt"` // game-clock seconds of the next spawn
	NextIn int    `json:"nextIn"` // seconds until the next spawn (NextAt - clock), always >= 0
	Due    bool   `json:"due"`    // NextIn <= lead: inside the pre-alert window
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
// horn). lead is the pre-alert window in seconds: an alert is Due when its NextIn
// <= lead. The result is sorted deterministically by (NextIn, Kind) so the ordering
// is identical on every machine. One-shot spawns already in the past are omitted.
func Upcoming(specs []Spawn, clock, lead int) []Alert {
	out := make([]Alert, 0, len(specs))
	for _, s := range specs {
		next, ok := nextSpawn(s, clock)
		if !ok {
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
