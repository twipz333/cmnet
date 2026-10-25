package stratz

import "context"

// matchDetailQuery selects only the draft fields we need, keeping the payload
// small and the call fast/reliable (unlike OpenDota's full match document).
// The player block carries heroId, playerSlot and position on top of the account
// id: roles and players are what the draft models need beyond a bag of heroes, and
// they ride along on the request the sync already makes — no extra call, no extra
// quota. Field names verified against the live schema by introspection
// (MatchPlayerType), not assumed.
// The per-player block carries the whole match performance, not just the draft.
// Every one of these fields rides along on the request the sync already makes:
// they are the same fields pairmatches.go has been retrieving in one call for the
// pair report, so this costs no extra call and no extra quota. Storing a match as
// a game rather than a bag of heroes is what the lane-strength and player-pool
// aggregates are built from. Field names verified against the live schema by
// introspection (MatchPlayerType), not assumed.
const matchDetailQuery = `query($id: Long!) {
  match(id: $id) {
    id
    didRadiantWin
    startDateTime
    durationSeconds
    gameMode
    lobbyType
    rank
    pickBans { heroId isPick isRadiant order }
    players {
      playerSlot
      steamAccountId
      isRadiant
      heroId
      position
      lane
      isVictory
      kills
      deaths
      assists
      goldPerMinute
      experiencePerMinute
      numLastHits
      numDenies
      networth
      heroDamage
      towerDamage
      imp
    }
  }
}`

type matchDetailResult struct {
	Match *struct {
		ID              int64  `json:"id"`
		DidRadiantWin   bool   `json:"didRadiantWin"`
		StartDateTime   int64  `json:"startDateTime"`
		DurationSeconds int    `json:"durationSeconds"`
		GameMode        string `json:"gameMode"`
		LobbyType       string `json:"lobbyType"`
		PickBans        []struct {
			HeroID    int  `json:"heroId"`
			IsPick    bool `json:"isPick"`
			IsRadiant bool `json:"isRadiant"`
			Order     int  `json:"order"`
		} `json:"pickBans"`
		Rank    *int `json:"rank"`
		Players []struct {
			PlayerSlot          int     `json:"playerSlot"`
			SteamAccountID      int64   `json:"steamAccountId"`
			IsRadiant           bool    `json:"isRadiant"`
			HeroID              int     `json:"heroId"`
			Position            *string `json:"position"`
			Lane                *string `json:"lane"`
			IsVictory           bool    `json:"isVictory"`
			Kills               *int    `json:"kills"`
			Deaths              *int    `json:"deaths"`
			Assists             *int    `json:"assists"`
			GoldPerMinute       *int    `json:"goldPerMinute"`
			ExperiencePerMinute *int    `json:"experiencePerMinute"`
			NumLastHits         *int    `json:"numLastHits"`
			NumDenies           *int    `json:"numDenies"`
			Networth            *int    `json:"networth"`
			HeroDamage          *int    `json:"heroDamage"`
			TowerDamage         *int    `json:"towerDamage"`
			Imp                 *int    `json:"imp"`
		} `json:"players"`
	} `json:"match"`
}

// MatchPickBan is one draft action from a STRATZ match.
type MatchPickBan struct {
	HeroID    int
	IsPick    bool
	IsRadiant bool
	Order     int
}

// MatchPlayer identifies a participant, their side, hero, lane position and how
// the game went for them.
//
// The performance fields are pointers because "not reported" and "zero" are
// different facts: a support really can finish on zero tower damage, and folding
// that together with a missing field would poison every average computed over it.
type MatchPlayer struct {
	PlayerSlot     int   // Valve slot: 0-4 Radiant, 128-132 Dire
	SteamAccountID int64 // 0 when the profile is anonymous
	IsRadiant      bool
	HeroID         int
	// Position is the lane role 1-5, or 0 when STRATZ reports UNKNOWN / omits it.
	// STRATZ infers it, so it is a label rather than ground truth — which is why the
	// cache records where the position came from alongside the value.
	Position int
	// Lane is where the player actually stood (SAFE_LANE / MID_LANE / OFF_LANE /
	// ROAMING / JUNGLE), which is not the same claim as Position: position is the
	// role STRATZ inferred, lane is the observed lane. Lane strength is about the
	// second.
	Lane      string
	IsVictory bool

	Kills, Deaths, Assists           *int
	GoldPerMinute, ExperiencePerMin  *int
	NumLastHits, NumDenies           *int
	Networth, HeroDamage, TowerDamage *int
	// Imp is STRATZ's own "individual match performance" score, roughly -100..100.
	Imp *int
}

// HasStats reports whether the source filled in the performance fields. Used to
// tell "this match predates the richer collection" from "this player did nothing".
func (p MatchPlayer) HasStats() bool {
	return p.Kills != nil || p.GoldPerMinute != nil || p.Networth != nil
}

// parsePosition maps the STRATZ MatchPlayerPositionType enum to 1-5, returning 0
// for UNKNOWN / FILTERED / ALL / absent. The enum names are fixed by the API
// (POSITION_1 … POSITION_5); anything else is deliberately not guessed.
func parsePosition(s *string) int {
	if s == nil {
		return 0
	}
	switch *s {
	case "POSITION_1":
		return 1
	case "POSITION_2":
		return 2
	case "POSITION_3":
		return 3
	case "POSITION_4":
		return 4
	case "POSITION_5":
		return 5
	default:
		return 0
	}
}

// deref returns the pointed-to string, or "" for a nil pointer (the API omits
// enum fields it has no value for rather than sending a zero).
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// MatchDetail is the focused match data from STRATZ: the draft plus how the game
// actually went.
type MatchDetail struct {
	ID              int64
	DidRadiantWin   bool
	StartDateTime   int64
	DurationSeconds int
	GameMode        string // e.g. "CAPTAINS_MODE"
	LobbyType       string // e.g. "RANKED", "UNRANKED", "BATTLE_CUP"
	PickBans        []MatchPickBan
	Players         []MatchPlayer
	// Rank is STRATZ's average rank of the match (10-85 scale, medal*10+stars),
	// nil when the match is unranked or the rank is hidden.
	Rank *int
}

// IsCaptainsMode reports whether the match is Captains Mode.
func (m *MatchDetail) IsCaptainsMode() bool { return m.GameMode == "CAPTAINS_MODE" }

// NOTE: STRATZ exposes a batch matches(ids:[Long!]!) field, but it is admin-only
// ("User is not an admin"), so detail is fetched one id at a time via Match below.

// Match fetches a single match's draft detail by id.
func (c *Client) Match(ctx context.Context, id int64) (*MatchDetail, error) {
	var res matchDetailResult
	if err := c.query(ctx, matchDetailQuery, map[string]any{"id": id}, &res); err != nil {
		return nil, err
	}
	if res.Match == nil {
		return nil, nil
	}
	md := &MatchDetail{
		ID:              res.Match.ID,
		DidRadiantWin:   res.Match.DidRadiantWin,
		StartDateTime:   res.Match.StartDateTime,
		DurationSeconds: res.Match.DurationSeconds,
		GameMode:        res.Match.GameMode,
		LobbyType:       res.Match.LobbyType,
	}
	for _, pb := range res.Match.PickBans {
		md.PickBans = append(md.PickBans, MatchPickBan{
			HeroID: pb.HeroID, IsPick: pb.IsPick, IsRadiant: pb.IsRadiant, Order: pb.Order,
		})
	}
	md.Rank = res.Match.Rank
	for _, p := range res.Match.Players {
		md.Players = append(md.Players, MatchPlayer{
			PlayerSlot:          p.PlayerSlot,
			SteamAccountID:      p.SteamAccountID,
			IsRadiant:           p.IsRadiant,
			HeroID:              p.HeroID,
			Position:            parsePosition(p.Position),
			Lane:                deref(p.Lane),
			IsVictory:           p.IsVictory,
			Kills:               p.Kills,
			Deaths:              p.Deaths,
			Assists:             p.Assists,
			GoldPerMinute:       p.GoldPerMinute,
			ExperiencePerMin:    p.ExperiencePerMinute,
			NumLastHits:         p.NumLastHits,
			NumDenies:           p.NumDenies,
			Networth:            p.Networth,
			HeroDamage:          p.HeroDamage,
			TowerDamage:         p.TowerDamage,
			Imp:                 p.Imp,
		})
	}
	return md, nil
}
