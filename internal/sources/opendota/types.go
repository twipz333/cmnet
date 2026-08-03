package opendota

import (
	"context"
	"strconv"
	"strings"
	"unicode"
)

// Hero is the OpenDota /heroes entry (subset we use).
type Hero struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`           // npc_dota_hero_*
	LocalizedName string   `json:"localized_name"` // display name
	Roles         []string `json:"roles"`          // e.g. ["Carry","Escape","Nuker"]
}

// Heroes fetches the full hero reference list (GET /heroes).
func (c *Client) Heroes(ctx context.Context) ([]Hero, error) {
	var heroes []Hero
	if err := c.getJSON(ctx, "/heroes", &heroes); err != nil {
		return nil, err
	}
	return heroes, nil
}

// pickBanRaw mirrors an entry of the match "picks_bans" array.
type pickBanRaw struct {
	IsPick bool `json:"is_pick"`
	HeroID int  `json:"hero_id"`
	Team   int  `json:"team"`  // 0 radiant / 1 dire
	Order  int  `json:"order"` // draft order
}

type playerRaw struct {
	AccountID  int64 `json:"account_id"`
	PlayerSlot int   `json:"player_slot"` // <128 => radiant
}

// matchRaw mirrors the subset of GET /matches/{id} we cache.
type matchRaw struct {
	MatchID    int64        `json:"match_id"`
	StartTime  int64        `json:"start_time"`
	RadiantWin bool         `json:"radiant_win"`
	GameMode   int          `json:"game_mode"` // 2 == Captains Mode
	PicksBans  []pickBanRaw `json:"picks_bans"`
	Players    []playerRaw  `json:"players"`
}

// PickBan is a single draft action from a match.
type PickBan struct {
	IsPick bool
	HeroID int
	Team   int
	Order  int
}

// MatchPlayer identifies a participant and which side they were on.
type MatchPlayer struct {
	AccountID int64
	Radiant   bool
}

// Match is the detailed match data we extract from OpenDota.
type Match struct {
	MatchID    int64
	StartTime  int64
	RadiantWin bool
	GameMode   int
	PickBans   []PickBan
	Players    []MatchPlayer
}

// Match fetches detailed match data including picks_bans (GET /matches/{id}).
func (c *Client) Match(ctx context.Context, id int64) (*Match, error) {
	var raw matchRaw
	if err := c.getJSON(ctx, "/matches/"+itoa(id), &raw); err != nil {
		return nil, err
	}
	m := &Match{
		MatchID:    raw.MatchID,
		StartTime:  raw.StartTime,
		RadiantWin: raw.RadiantWin,
		GameMode:   raw.GameMode,
	}
	for _, pb := range raw.PicksBans {
		m.PickBans = append(m.PickBans, PickBan{
			IsPick: pb.IsPick,
			HeroID: pb.HeroID,
			Team:   pb.Team,
			Order:  pb.Order,
		})
	}
	for _, p := range raw.Players {
		m.Players = append(m.Players, MatchPlayer{
			AccountID: p.AccountID,
			Radiant:   p.PlayerSlot < 128,
		})
	}
	return m, nil
}

// PlayerHero is one row of GET /players/{id}/heroes.
type PlayerHero struct {
	HeroID int `json:"hero_id"`
	Games  int `json:"games"`
	Win    int `json:"win"`
}

// PlayerHeroes fetches a player's per-hero performance (GET /players/{id}/heroes).
// When days > 0 the result is limited to that recency window (OpenDota ?date=N).
func (c *Client) PlayerHeroes(ctx context.Context, accountID int64, days int) ([]PlayerHero, error) {
	path := "/players/" + itoa(accountID) + "/heroes"
	if days > 0 {
		path += "?date=" + strconv.Itoa(days)
	}
	var out []PlayerHero
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// profileRaw mirrors the subset of GET /players/{id} we need. The whole profile
// block is absent for accounts that never enabled public match data, which is why
// it is a pointer: "no profile" is a normal answer here, not an error.
type profileRaw struct {
	Profile *struct {
		PersonaName string `json:"personaname"`
	} `json:"profile"`
}

// PersonaName fetches a player's display name (GET /players/{id}). It returns an
// empty string, and no error, when the account has no public profile — the caller
// is expected to fall back to showing the account id.
//
// The name is sanitised before it is returned: it is attacker-chosen text that
// ends up printed straight into a terminal by the TUI, where an embedded escape
// sequence could repaint or reprogram it.
func (c *Client) PersonaName(ctx context.Context, accountID int64) (string, error) {
	var out profileRaw
	if err := c.getJSON(ctx, "/players/"+itoa(accountID), &out); err != nil {
		return "", err
	}
	if out.Profile == nil {
		return "", nil
	}
	return sanitizeDisplayName(out.Profile.PersonaName), nil
}

// maxDisplayName bounds a persona name so one absurdly long nickname cannot break
// the layout of either front end.
const maxDisplayName = 64

// sanitizeDisplayName strips control characters (ESC, CR/LF, NUL, and the C1
// range) from a user-chosen name and trims it to a sane length. Printable text,
// including non-Latin scripts and emoji, is left alone.
func sanitizeDisplayName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == ' ':
			b.WriteRune(' ')
		case unicode.IsControl(r):
			// dropped: ESC and friends have no place in a display name
		default:
			b.WriteRune(r)
		}
	}
	name := strings.TrimSpace(b.String())
	if len([]rune(name)) > maxDisplayName {
		name = string([]rune(name)[:maxDisplayName])
	}
	return name
}

// PlayerMatch is one row of GET /players/{id}/recentMatches.
type PlayerMatch struct {
	MatchID   int64 `json:"match_id"`
	StartTime int64 `json:"start_time"`
}

// RecentMatches fetches a player's recent matches (GET /players/{id}/recentMatches).
func (c *Client) RecentMatches(ctx context.Context, accountID int64) ([]PlayerMatch, error) {
	var out []PlayerMatch
	if err := c.getJSON(ctx, "/players/"+itoa(accountID)+"/recentMatches", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ProMatch is one row of GET /proMatches (recent professional/league matches,
// predominantly Captains Mode — a clean drafting signal).
type ProMatch struct {
	MatchID   int64 `json:"match_id"`
	StartTime int64 `json:"start_time"`
}

// ProMatches fetches a page of recent pro matches. When lessThanMatchID > 0 it
// returns matches older than that id (pagination). Results are newest-first.
func (c *Client) ProMatches(ctx context.Context, lessThanMatchID int64) ([]ProMatch, error) {
	path := "/proMatches"
	if lessThanMatchID > 0 {
		path += "?less_than_match_id=" + itoa(lessThanMatchID)
	}
	var out []ProMatch
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}
