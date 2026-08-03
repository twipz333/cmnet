package stratz

import "context"

const playerProfileQuery = `query($id: Long!) {
  player(steamAccountId: $id) {
    steamAccount { name avatar profileUri }
  }
}`

type playerProfileResult struct {
	Player *struct {
		SteamAccount *struct {
			Name       string `json:"name"`
			Avatar     string `json:"avatar"`
			ProfileURI string `json:"profileUri"`
		} `json:"steamAccount"`
	} `json:"player"`
}

// Profile is a player's public identity (all fields may be empty).
type Profile struct {
	Name       string
	Avatar     string // full Steam avatar URL
	ProfileURI string
}

// PlayerProfile returns the persona name, avatar URL and profile URI for an
// account id. Missing fields come back empty rather than as an error.
func (c *Client) PlayerProfile(ctx context.Context, id int64) (Profile, error) {
	var res playerProfileResult
	if err := c.query(ctx, playerProfileQuery, map[string]any{"id": id}, &res); err != nil {
		return Profile{}, err
	}
	if res.Player == nil || res.Player.SteamAccount == nil {
		return Profile{}, nil
	}
	sa := res.Player.SteamAccount
	return Profile{Name: sa.Name, Avatar: sa.Avatar, ProfileURI: sa.ProfileURI}, nil
}

const playerPublicQuery = `query($id: Long!) {
  player(steamAccountId: $id) { matches(request: {take: 1}) { id } }
}`

type playerPublicResult struct {
	Player *struct {
		Matches []struct {
			ID int64 `json:"id"`
		} `json:"matches"`
	} `json:"player"`
}

// IsPublic reports whether the account's own match history is enumerable (i.e.
// "Expose Public Match Data" is on). A private profile returns an empty match
// list here even though the player's rows stay visible inside other players'
// matches, which is what makes the anchor trick necessary for closed profiles.
func (c *Client) IsPublic(ctx context.Context, id int64) (bool, error) {
	var res playerPublicResult
	if err := c.query(ctx, playerPublicQuery, map[string]any{"id": id}, &res); err != nil {
		return false, err
	}
	return res.Player != nil && len(res.Player.Matches) > 0, nil
}
