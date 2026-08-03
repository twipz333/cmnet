package stratz

import "context"

// matchDetailQuery selects only the draft fields we need, keeping the payload
// small and the call fast/reliable (unlike OpenDota's full match document).
// The player block carries heroId, playerSlot and position on top of the account
// id: roles and players are what the draft models need beyond a bag of heroes, and
// they ride along on the request the sync already makes — no extra call, no extra
// quota. Field names verified against the live schema by introspection
// (MatchPlayerType), not assumed.
const matchDetailQuery = `query($id: Long!) {
  match(id: $id) {
    id
    didRadiantWin
    startDateTime
    gameMode
    rank
    pickBans { heroId isPick isRadiant order }
    players { playerSlot steamAccountId isRadiant heroId position }
  }
}`

type matchDetailResult struct {
	Match *struct {
		ID            int64  `json:"id"`
		DidRadiantWin bool   `json:"didRadiantWin"`
		StartDateTime int64  `json:"startDateTime"`
		GameMode      string `json:"gameMode"`
		PickBans      []struct {
			HeroID    int  `json:"heroId"`
			IsPick    bool `json:"isPick"`
			IsRadiant bool `json:"isRadiant"`
			Order     int  `json:"order"`
		} `json:"pickBans"`
		Rank    *int `json:"rank"`
		Players []struct {
			PlayerSlot     int     `json:"playerSlot"`
			SteamAccountID int64   `json:"steamAccountId"`
			IsRadiant      bool    `json:"isRadiant"`
			HeroID         int     `json:"heroId"`
			Position       *string `json:"position"`
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

// MatchPlayer identifies a participant, their side, hero and lane position.
type MatchPlayer struct {
	PlayerSlot     int   // Valve slot: 0-4 Radiant, 128-132 Dire
	SteamAccountID int64 // 0 when the profile is anonymous
	IsRadiant      bool
	HeroID         int
	// Position is the lane role 1-5, or 0 when STRATZ reports UNKNOWN / omits it.
	// STRATZ infers it, so it is a label rather than ground truth — which is why the
	// cache records where the position came from alongside the value.
	Position int
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

// MatchDetail is the focused match draft data from STRATZ.
type MatchDetail struct {
	ID            int64
	DidRadiantWin bool
	StartDateTime int64
	GameMode      string // e.g. "CAPTAINS_MODE"
	PickBans      []MatchPickBan
	Players       []MatchPlayer
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
		ID:            res.Match.ID,
		DidRadiantWin: res.Match.DidRadiantWin,
		StartDateTime: res.Match.StartDateTime,
		GameMode:      res.Match.GameMode,
	}
	for _, pb := range res.Match.PickBans {
		md.PickBans = append(md.PickBans, MatchPickBan{
			HeroID: pb.HeroID, IsPick: pb.IsPick, IsRadiant: pb.IsRadiant, Order: pb.Order,
		})
	}
	md.Rank = res.Match.Rank
	for _, p := range res.Match.Players {
		md.Players = append(md.Players, MatchPlayer{
			PlayerSlot:     p.PlayerSlot,
			SteamAccountID: p.SteamAccountID,
			IsRadiant:      p.IsRadiant,
			HeroID:         p.HeroID,
			Position:       parsePosition(p.Position),
		})
	}
	return md, nil
}
