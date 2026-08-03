// Package gsi receives Dota 2 Game State Integration events over a local HTTP
// server and turns the draft block into a draft.Snapshot the recommender can use.
//
// The exact GSI draft schema shifts between client versions and is not officially
// documented (§12). The parser is therefore tolerant: it accepts either
// radiant/dire or team2/team3 containers and scans for pickN_id / banN_id keys
// rather than relying on fixed field names.
package gsi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"cmnet/internal/draft"
)

// Dota team ids inside GSI: radiant=2, dire=3.
const (
	teamRadiant = 2
	teamDire    = 3
)

type rawEvent struct {
	Draft *rawDraft `json:"draft"`
	Map   *rawMap   `json:"map"`
}

// rawMap mirrors the GSI map block fields we use for the objective timers (M6).
type rawMap struct {
	ClockTime int    `json:"clock_time"`
	GameState string `json:"game_state"`
	Daytime   bool   `json:"daytime"`
}

type rawDraft struct {
	ActiveTeam int                        `json:"activeteam"`
	Pick       bool                       `json:"pick"`
	Radiant    map[string]json.RawMessage `json:"radiant"`
	Dire       map[string]json.RawMessage `json:"dire"`
	Team2      map[string]json.RawMessage `json:"team2"`
	Team3      map[string]json.RawMessage `json:"team3"`
}

var (
	pickKey = regexp.MustCompile(`^pick(\d+)_id$`)
	banKey  = regexp.MustCompile(`^ban(\d+)_id$`)
)

// ParseDraft extracts a draft.Snapshot from a GSI event body. It returns
// (nil, nil) only when the body carries neither a draft nor a map block (a wholly
// uninteresting event). Map-only events (post-draft / All Pick) yield a snapshot
// with HasDraft=false and a populated Live, so the server can retain the last known
// picks while still updating the live clock (M6).
func ParseDraft(body []byte) (*draft.Snapshot, error) {
	var ev rawEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("gsi decode: %w", err)
	}
	if ev.Draft == nil && ev.Map == nil {
		return nil, nil
	}

	snap := &draft.Snapshot{}

	if d := ev.Draft; d != nil {
		snap.HasDraft = true
		radiant := firstNonNil(d.Radiant, d.Team2)
		dire := firstNonNil(d.Dire, d.Team3)
		snap.RadiantPicks = extract(radiant, pickKey)
		snap.RadiantBans = extract(radiant, banKey)
		snap.DirePicks = extract(dire, pickKey)
		snap.DireBans = extract(dire, banKey)
		snap.CurrentIsPick = d.Pick
		switch d.ActiveTeam {
		case teamRadiant:
			snap.CurrentTeam = draft.Radiant
		case teamDire:
			snap.CurrentTeam = draft.Dire
		default:
			// No active team -> the draft phase is over (or not started).
			snap.Done = true
		}
	} else {
		// Map-only event: no draft block, so the draft phase is not in progress.
		snap.Done = true
	}

	if mp := ev.Map; mp != nil {
		snap.Live = &draft.Live{
			ClockTime: mp.ClockTime,
			GameState: mp.GameState,
			Daytime:   mp.Daytime,
		}
	}
	return snap, nil
}

func firstNonNil(a, b map[string]json.RawMessage) map[string]json.RawMessage {
	if a != nil {
		return a
	}
	return b
}

// indexedID pairs a slot index with its hero id, used to preserve per-team order.
type indexedID struct {
	idx int
	id  int
}

// extract collects hero ids from keys matching re (pickN_id / banN_id), ordered by
// the slot index N. Empty slots (id <= 0) are skipped.
func extract(container map[string]json.RawMessage, re *regexp.Regexp) []int {
	if container == nil {
		return nil
	}
	var found []indexedID
	for key, raw := range container {
		m := re.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		slot, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		var id int
		if err := json.Unmarshal(raw, &id); err != nil {
			continue
		}
		if id <= 0 {
			continue
		}
		found = append(found, indexedID{idx: slot, id: id})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].idx < found[j].idx })

	out := make([]int, len(found))
	for i, f := range found {
		out[i] = f.id
	}
	return out
}
