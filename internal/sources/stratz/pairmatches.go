package stratz

import "context"

// jointMatchesQuery pulls an anchor player's matches filtered server-side to only
// those where every friend account id was a teammate (withFriendSteamAccountIds).
// Each match carries full inline per-player stats, so the whole pair dataset is
// fetched without any per-match follow-up calls (the batch matches(ids:) field is
// admin-only — see match.go). Private profiles only hide a player's own match
// LIST; their per-match rows stay visible inside the anchor's matches.
const jointMatchesQuery = `query($id: Long!, $friends: [Long]!, $take: Int!, $skip: Int!) {
  player(steamAccountId: $id) {
    matches(request: {withFriendSteamAccountIds: $friends, take: $take, skip: $skip}) {
      id
      didRadiantWin
      startDateTime
      durationSeconds
      gameMode
      lobbyType
      players {
        steamAccountId
        heroId
        isRadiant
        isVictory
        lane
        position
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
        award
        stats { networthPerMinute }
      }
    }
  }
}`

type jointMatchesResult struct {
	Player *struct {
		Matches []struct {
			ID              int64  `json:"id"`
			DidRadiantWin   bool   `json:"didRadiantWin"`
			StartDateTime   int64  `json:"startDateTime"`
			DurationSeconds int    `json:"durationSeconds"`
			GameMode        string `json:"gameMode"`
			LobbyType       string `json:"lobbyType"`
			Players         []struct {
				SteamAccountID      int64    `json:"steamAccountId"`
				HeroID              int      `json:"heroId"`
				IsRadiant           bool     `json:"isRadiant"`
				IsVictory           bool     `json:"isVictory"`
				Lane                string   `json:"lane"`
				Position            string   `json:"position"`
				Kills               int      `json:"kills"`
				Deaths              int      `json:"deaths"`
				Assists             int      `json:"assists"`
				GoldPerMinute       int      `json:"goldPerMinute"`
				ExperiencePerMinute int      `json:"experiencePerMinute"`
				NumLastHits         int      `json:"numLastHits"`
				NumDenies           int      `json:"numDenies"`
				Networth            int      `json:"networth"`
				HeroDamage          int      `json:"heroDamage"`
				TowerDamage         int      `json:"towerDamage"`
				IMP                 *int     `json:"imp"`
				Award               string   `json:"award"`
				Stats               *struct {
					NetworthPerMinute []int `json:"networthPerMinute"`
				} `json:"stats"`
			} `json:"players"`
		} `json:"matches"`
	} `json:"player"`
}

// JointPlayer is one participant's row inside a joint match. IMP is a pointer
// because STRATZ leaves it (and the per-minute arrays) null for unparsed matches.
type JointPlayer struct {
	SteamAccountID      int64
	HeroID              int
	IsRadiant           bool
	IsVictory           bool
	Lane                string
	Position            string
	Kills               int
	Deaths              int
	Assists             int
	GoldPerMinute       int
	ExperiencePerMinute int
	NumLastHits         int
	NumDenies           int
	Networth            int
	HeroDamage          int
	TowerDamage         int
	IMP                 *int
	Award               string
	NetworthPerMinute   []int
}

// JointMatch is one match with all ten inline player rows.
type JointMatch struct {
	ID              int64
	DidRadiantWin   bool
	StartDateTime   int64
	DurationSeconds int
	GameMode        string // e.g. "ALL_PICK_RANKED", "CAPTAINS_MODE", "TURBO"
	LobbyType       string // e.g. "RANKED", "BATTLE_CUP", "UNRANKED"
	Players         []JointPlayer
}

// JointFilter controls the joint-match pull.
type JointFilter struct {
	PerPage  int // page size, capped to 100 (STRATZ max); 0 → 100
	MaxPages int // safety cap on pagination; 0 → 50 (≈5000 matches)
}

// JointMatches returns the anchor player's matches in which every account id in
// friends was a teammate, with full inline stats. The anchor must be a public
// profile; friends may be private (their rows are still returned). Results are
// paginated until a short page is seen or MaxPages is hit. Order follows STRATZ
// (most-recent first); callers that need determinism should sort by ID.
func (c *Client) JointMatches(ctx context.Context, anchor int64, friends []int64, f JointFilter) ([]JointMatch, error) {
	per := f.PerPage
	if per <= 0 || per > 100 {
		per = 100
	}
	maxPages := f.MaxPages
	if maxPages <= 0 {
		maxPages = 50
	}

	var out []JointMatch
	for page := 0; page < maxPages; page++ {
		vars := map[string]any{
			"id":      anchor,
			"friends": friends,
			"take":    per,
			"skip":    page * per,
		}
		var res jointMatchesResult
		if err := c.query(ctx, jointMatchesQuery, vars, &res); err != nil {
			return nil, err
		}
		if res.Player == nil {
			break // private anchor / unknown id → no enumerable matches
		}
		ms := res.Player.Matches
		for _, m := range ms {
			jm := JointMatch{
				ID:              m.ID,
				DidRadiantWin:   m.DidRadiantWin,
				StartDateTime:   m.StartDateTime,
				DurationSeconds: m.DurationSeconds,
				GameMode:        m.GameMode,
				LobbyType:       m.LobbyType,
			}
			for _, p := range m.Players {
				jp := JointPlayer{
					SteamAccountID:      p.SteamAccountID,
					HeroID:              p.HeroID,
					IsRadiant:           p.IsRadiant,
					IsVictory:           p.IsVictory,
					Lane:                p.Lane,
					Position:            p.Position,
					Kills:               p.Kills,
					Deaths:              p.Deaths,
					Assists:             p.Assists,
					GoldPerMinute:       p.GoldPerMinute,
					ExperiencePerMinute: p.ExperiencePerMinute,
					NumLastHits:         p.NumLastHits,
					NumDenies:           p.NumDenies,
					Networth:            p.Networth,
					HeroDamage:          p.HeroDamage,
					TowerDamage:         p.TowerDamage,
					IMP:                 p.IMP,
					Award:               p.Award,
				}
				if p.Stats != nil {
					jp.NetworthPerMinute = p.Stats.NetworthPerMinute
				}
				jm.Players = append(jm.Players, jp)
			}
			out = append(out, jm)
		}
		if len(ms) < per {
			break // last page
		}
	}
	return out, nil
}
