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
	"strings"

	"cmnet/internal/draft"
)

// Dota team ids inside GSI: radiant=2, dire=3.
const (
	teamRadiant = 2
	teamDire    = 3
)

type rawEvent struct {
	Auth   *rawAuth   `json:"auth"`
	Draft  *rawDraft  `json:"draft"`
	Map    *rawMap    `json:"map"`
	Player *rawPlayer `json:"player"`
	Hero   *rawHero   `json:"hero"`
}

// rawAuth is the token block Dota echoes back from the config file.
//
// It was written into the config and never checked, so the server accepted any
// POST to the port from anything running on the machine. See Server.token.
type rawAuth struct {
	Token string `json:"token"`
}

// rawMap mirrors the GSI map block.
//
// Deliberately absent: roshan_state / roshan_state_end_seconds. Every catalogue
// of the schema puts those in the spectator payload — the client sends the full
// picture while observing a match and only the local player's slice while
// playing — so a timer built on them would be permanently blank in the mode this
// advisor is used in. Not parsing them is the honest version of that; see
// SCHEMA.md.
type rawMap struct {
	ClockTime    int    `json:"clock_time"`
	GameState    string `json:"game_state"`
	Daytime      bool   `json:"daytime"`
	MatchID      string `json:"matchid"`
	RadiantScore int    `json:"radiant_score"`
	DireScore    int    `json:"dire_score"`
	Paused       bool   `json:"paused"`
	WinTeam      string `json:"win_team"`
}

// rawPlayer is the local player's own line. While playing, this is all the client
// sends about anybody: the opponents' numbers are not withheld by us, they are
// simply not in the payload.
type rawPlayer struct {
	Kills    int `json:"kills"`
	Deaths   int `json:"deaths"`
	Assists  int `json:"assists"`
	LastHits int `json:"last_hits"`
	Denies   int `json:"denies"`
	GPM      int `json:"gpm"`
	XPM      int `json:"xpm"`
	NetWorth int `json:"net_worth"`
	Gold     int `json:"gold"`
	// TeamName is radiant|dire|spectator when the client sends it. This is how we
	// learn "our side" during CM — without it the UI stuck on --side until toggled.
	TeamName string `json:"team_name"`
	// Team is the numeric seat (2=radiant, 3=dire) on some client builds.
	Team int `json:"team"`
}

// rawHero is the local player's hero.
type rawHero struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Level           int    `json:"level"`
	Alive           bool   `json:"alive"`
	RespawnSeconds  int    `json:"respawn_seconds"`
	BuybackCost     int    `json:"buyback_cost"`
	BuybackCooldown int    `json:"buyback_cooldown"`
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
		radiant := firstNonNil(d.Radiant, d.Team2)
		dire := firstNonNil(d.Dire, d.Team3)
		snap.RadiantPicks = extract(radiant, pickKey)
		snap.RadiantBans = extract(radiant, banKey)
		snap.DirePicks = extract(dire, pickKey)
		snap.DireBans = extract(dire, banKey)
		snap.CurrentIsPick = d.Pick
		snap.FirstPick = firstPickFrom(radiant, dire)
		active := d.ActiveTeam == teamRadiant || d.ActiveTeam == teamDire
		filled := len(snap.RadiantPicks)+len(snap.DirePicks)+len(snap.RadiantBans)+len(snap.DireBans) > 0
		// An empty "draft":{} still arrives in-game once we subscribe to the
		// draft section. That is not a new draft; HasDraft=false lets merge
		// keep the completed lineup.
		snap.HasDraft = active || filled
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
			ClockTime:    mp.ClockTime,
			GameState:    mp.GameState,
			Daytime:      mp.Daytime,
			MatchID:      mp.MatchID,
			RadiantScore: mp.RadiantScore,
			DireScore:    mp.DireScore,
			Paused:       mp.Paused,
			WinTeam:      mp.WinTeam,
		}
	}
	// The local player's own line. Attached to Live, which the front ends render
	// and the recommender never reads — live telemetry differs between two people
	// watching the same draft, so anything derived from it cannot be part of a
	// score that must match on both machines (§6).
	if ev.Player != nil || ev.Hero != nil {
		if snap.Live == nil {
			snap.Live = &draft.Live{}
		}
		self := &draft.SelfState{}
		if p := ev.Player; p != nil {
			self.Kills, self.Deaths, self.Assists = p.Kills, p.Deaths, p.Assists
			self.LastHits, self.Denies = p.LastHits, p.Denies
			self.GPM, self.XPM = p.GPM, p.XPM
			self.NetWorth, self.Gold = p.NetWorth, p.Gold
		}
		if h := ev.Hero; h != nil {
			self.HeroID, self.HeroName, self.Level = h.ID, h.Name, h.Level
			self.Alive, self.RespawnSeconds = h.Alive, h.RespawnSeconds
			self.BuybackCost, self.BuybackCooldown = h.BuybackCost, h.BuybackCooldown
		}
		snap.Live.Self = self
	}
	if t, ok := localTeamFromPlayer(ev.Player); ok {
		snap.LocalTeam, snap.LocalTeamOK = t, true
	} else if t, ok := localTeamFromHero(snap); ok {
		snap.LocalTeam, snap.LocalTeamOK = t, true
	}
	return snap, nil
}

// localTeamFromPlayer reads GSI player.team_name / player.team.
func localTeamFromPlayer(p *rawPlayer) (draft.Team, bool) {
	if p == nil {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(p.TeamName)) {
	case "radiant":
		return draft.Radiant, true
	case "dire":
		return draft.Dire, true
	}
	switch p.Team {
	case teamRadiant:
		return draft.Radiant, true
	case teamDire:
		return draft.Dire, true
	}
	return 0, false
}

// localTeamFromHero infers our seat once our hero appears on a pick list
// (fallback when team_name is missing mid-draft).
func localTeamFromHero(snap *draft.Snapshot) (draft.Team, bool) {
	if snap == nil || snap.Live == nil || snap.Live.Self == nil {
		return 0, false
	}
	hid := snap.Live.Self.HeroID
	if hid <= 0 {
		return 0, false
	}
	for _, id := range snap.RadiantPicks {
		if id == hid {
			return draft.Radiant, true
		}
	}
	for _, id := range snap.DirePicks {
		if id == hid {
			return draft.Dire, true
		}
	}
	return 0, false
}

// AuthToken returns the token Dota echoed back, or "" when the payload carries no
// auth block.
func AuthToken(body []byte) string {
	var ev rawEvent
	if err := json.Unmarshal(body, &ev); err != nil || ev.Auth == nil {
		return ""
	}
	return ev.Auth.Token
}

// firstPickFrom infers which side drafts first from the state of the board.
//
// GSI does not label it, and Snapshot.FirstPick was consequently never set at all
// — it stayed at its zero value, Radiant, and the CM schedule was read off that
// whether or not it was true. Half the time the phase and the whose-turn display
// were wrong for the entire draft.
//
// The inference is the first ban: CM alternates strictly, so whichever side has
// more actions on the board acted first. Equal counts mean either nothing has
// happened yet or the two are in step, and the zero value stands — which is no
// worse than what it did before, and correct as soon as the first action lands.
func firstPickFrom(radiant, dire map[string]json.RawMessage) draft.Team {
	r := len(extract(radiant, pickKey)) + len(extract(radiant, banKey))
	d := len(extract(dire, pickKey)) + len(extract(dire, banKey))
	if d > r {
		return draft.Dire
	}
	return draft.Radiant
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
