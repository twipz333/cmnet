package opendota

import (
	"context"
	"fmt"
	"net/url"
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

// playerRaw mirrors one entry of the match's players array.
//
// The performance fields are pointers because OpenDota omits them for unparsed
// matches, and "absent" is a different fact from zero — a support finishing on no
// tower damage is a real row, a match nobody parsed is not.
type playerRaw struct {
	AccountID   int64 `json:"account_id"`
	PlayerSlot  int   `json:"player_slot"` // <128 => radiant
	HeroID      int   `json:"hero_id"`
	Win         *int  `json:"win"`
	Kills       *int  `json:"kills"`
	Deaths      *int  `json:"deaths"`
	Assists     *int  `json:"assists"`
	GoldPerMin  *int  `json:"gold_per_min"`
	XPPerMin    *int  `json:"xp_per_min"`
	LastHits    *int  `json:"last_hits"`
	Denies      *int  `json:"denies"`
	NetWorth    *int  `json:"net_worth"`
	HeroDamage  *int  `json:"hero_damage"`
	TowerDamage *int  `json:"tower_damage"`
	// LaneRole is OpenDota's own 1-4 role guess (1 safe, 2 mid, 3 off, 4 jungle),
	// present only on parsed matches. It is not the same scale as STRATZ positions,
	// so it is mapped rather than copied.
	LaneRole *int `json:"lane_role"`
}

// matchRaw mirrors the subset of GET /matches/{id} we cache.
type matchRaw struct {
	MatchID    int64        `json:"match_id"`
	StartTime  int64        `json:"start_time"`
	Duration   int          `json:"duration"`
	RadiantWin bool         `json:"radiant_win"`
	GameMode   int          `json:"game_mode"` // 2 == Captains Mode
	LobbyType  int          `json:"lobby_type"`
	Patch      int          `json:"patch"`
	AvgRank    *int         `json:"avg_rank_tier"`
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

// MatchPlayer identifies a participant, which side they were on, and how the game
// went for them when the match was parsed.
type MatchPlayer struct {
	AccountID  int64
	PlayerSlot int
	HeroID     int
	Radiant    bool
	// Position is 1-5 when derivable from OpenDota's lane_role, 0 otherwise.
	Position    int
	Win         *bool
	Kills       *int
	Deaths      *int
	Assists     *int
	GoldPerMin  *int
	XPPerMin    *int
	LastHits    *int
	Denies      *int
	NetWorth    *int
	HeroDamage  *int
	TowerDamage *int
}

// HasStats reports whether the match was parsed well enough to carry performance.
func (p MatchPlayer) HasStats() bool { return p.Kills != nil || p.GoldPerMin != nil }

// Match is the detailed match data we extract from OpenDota.
type Match struct {
	MatchID    int64
	StartTime  int64
	Duration   int
	RadiantWin bool
	GameMode   int
	LobbyType  int
	// Patch is OpenDota's numeric patch id, or 0 when absent.
	Patch int
	// AvgRankTier is the match's average rank on the 10-85 medal scale, nil when
	// OpenDota does not report one (unranked, or too few public profiles).
	AvgRankTier *int
	PickBans    []PickBan
	Players     []MatchPlayer
}

// Match fetches detailed match data including picks_bans (GET /matches/{id}).
func (c *Client) Match(ctx context.Context, id int64) (*Match, error) {
	var raw matchRaw
	if err := c.getJSON(ctx, "/matches/"+itoa(id), &raw); err != nil {
		return nil, err
	}
	m := &Match{
		MatchID:     raw.MatchID,
		StartTime:   raw.StartTime,
		Duration:    raw.Duration,
		RadiantWin:  raw.RadiantWin,
		GameMode:    raw.GameMode,
		LobbyType:   raw.LobbyType,
		Patch:       raw.Patch,
		AvgRankTier: raw.AvgRank,
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
		mp := MatchPlayer{
			AccountID:   p.AccountID,
			PlayerSlot:  p.PlayerSlot,
			HeroID:      p.HeroID,
			Radiant:     p.PlayerSlot < 128,
			Position:    positionFromLaneRole(p.LaneRole),
			Kills:       p.Kills,
			Deaths:      p.Deaths,
			Assists:     p.Assists,
			GoldPerMin:  p.GoldPerMin,
			XPPerMin:    p.XPPerMin,
			LastHits:    p.LastHits,
			Denies:      p.Denies,
			NetWorth:    p.NetWorth,
			HeroDamage:  p.HeroDamage,
			TowerDamage: p.TowerDamage,
		}
		if p.Win != nil {
			w := *p.Win != 0
			mp.Win = &w
		}
		m.Players = append(m.Players, mp)
	}
	return m, nil
}

// positionFromLaneRole maps OpenDota's lane_role onto the 1-5 position scale.
//
// The two do not line up: lane_role says which LANE a player was in (1 safe,
// 2 mid, 3 off, 4 jungle), while a position also encodes whether they were the
// core or the support of that lane — information OpenDota simply does not carry
// here. Only mid is unambiguous. Rather than guess a carry from a support and
// poison every per-position aggregate with it, the ambiguous lanes report 0 and
// the row is stored with no position at all, which the aggregates already skip.
func positionFromLaneRole(role *int) int {
	if role == nil {
		return 0
	}
	if *role == 2 {
		return 2
	}
	return 0
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
		PersonaName  string `json:"personaname"`
		Avatar       string `json:"avatar"`
		AvatarMedium string `json:"avatarmedium"`
		AvatarFull   string `json:"avatarfull"`
	} `json:"profile"`
}

// PlayerProfile is a player's public Steam face as OpenDota mirrors it.
type PlayerProfile struct {
	Persona string
	Avatar  string // URL of the largest avatar, "" when none
}

// Profile fetches a player's display name and avatar (GET /players/{id}). Both
// are empty, with no error, when the account has no public profile.
func (c *Client) Profile(ctx context.Context, accountID int64) (PlayerProfile, error) {
	var out profileRaw
	if err := c.getJSON(ctx, "/players/"+itoa(accountID), &out); err != nil {
		return PlayerProfile{}, err
	}
	if out.Profile == nil {
		return PlayerProfile{}, nil
	}
	av := out.Profile.AvatarFull
	if av == "" {
		av = out.Profile.AvatarMedium
	}
	if av == "" {
		av = out.Profile.Avatar
	}
	return PlayerProfile{Persona: sanitizeDisplayName(out.Profile.PersonaName), Avatar: av}, nil
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

// SearchHit is one row of GET /search?q=.
type SearchHit struct {
	AccountID   int64   `json:"account_id"`
	PersonaName string  `json:"personaname"`
	Similarity  float64 `json:"similarity"` // present on some payloads
	SML         float64 `json:"sml"`        // OpenDota's current similarity field
}

// Search looks up players by persona / vanity name (GET /search?q=).
// Used to resolve steamcommunity.com/id/<vanity> without scraping Dotabuff.
func (c *Client) Search(ctx context.Context, query string) ([]SearchHit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("empty search query")
	}
	var out []SearchHit
	path := "/search?q=" + url.QueryEscape(q)
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
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
	MatchID    int64 `json:"match_id"`
	StartTime  int64 `json:"start_time"`
	HeroID     int   `json:"hero_id"`
	PlayerSlot int   `json:"player_slot"` // <128 => radiant
	Duration   int   `json:"duration"`
	GameMode   int   `json:"game_mode"`
	LobbyType  int   `json:"lobby_type"`
	PartySize  int   `json:"party_size"`
	Kills      int   `json:"kills"`
	Deaths     int   `json:"deaths"`
	Assists    int   `json:"assists"`
	// RadiantWin is set once the match has an outcome; absent/zero while the
	// game is still in progress on the API side.
	RadiantWin bool `json:"radiant_win"`
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
