package pairstats

import (
	"encoding/json"
	"os"
)

// jsonPlayer is the flattened, stable JSON shape for one player.
type jsonPlayer struct {
	AccountID  int64        `json:"account_id"`
	Name       string       `json:"name,omitempty"`
	Matches    int          `json:"matches"`
	Wins       int          `json:"wins"`
	WinPct     float64      `json:"win_pct"`
	AvgIMP     *float64     `json:"avg_imp,omitempty"`
	AvgGPM     float64      `json:"avg_gpm"`
	AvgKDA     float64      `json:"avg_kda"`
	LaneWin    int          `json:"lane_win"`
	LaneTotal  int          `json:"lane_total"`
	LaneWinPct *float64     `json:"lane_win_pct,omitempty"`
	TopHeroes  []jsonBucket `json:"top_heroes"`
	Matches_   []MatchRow   `json:"per_match"`
}

type jsonBucket struct {
	HeroID int     `json:"hero_id"`
	Name   string  `json:"name"`
	Games  int     `json:"games"`
	Wins   int     `json:"wins"`
	WinPct float64 `json:"win_pct"`
}

type jsonReport struct {
	Generated     string       `json:"generated"`
	Anchor        int64        `json:"anchor"`
	Mode          string       `json:"mode"`
	TotalMatches  int          `json:"total_matches"`
	ParsedMatches int          `json:"parsed_matches"`
	Players       []jsonPlayer `json:"players"`
}

// WriteJSON writes the report as indented JSON to path.
func (r *Report) WriteJSON(path string) error {
	jr := jsonReport{
		Generated:     r.Generated.Format("2006-01-02T15:04:05Z"),
		Anchor:        r.Anchor,
		Mode:          r.Mode,
		TotalMatches:  r.TotalMatches,
		ParsedMatches: r.ParsedMatches,
	}
	for _, id := range r.Focus {
		a := r.Agg[id]
		if a == nil {
			continue
		}
		jp := jsonPlayer{
			AccountID: a.AccountID,
			Name:      a.Name,
			Matches:   a.Matches,
			Wins:      a.Wins,
			WinPct:    round1(a.WinPct()),
			AvgGPM:    round1(a.AvgGPM()),
			AvgKDA:    round2(a.AvgKDA()),
			LaneWin:   a.LaneWin,
			LaneTotal: a.LaneTot,
			Matches_:  r.PerMatch[id],
		}
		if v, ok := a.AvgIMP(); ok {
			rv := round1(v)
			jp.AvgIMP = &rv
		}
		if v, ok := a.LaneWinPct(); ok {
			rv := round1(v)
			jp.LaneWinPct = &rv
		}
		for _, h := range a.TopHeroes(0) {
			jp.TopHeroes = append(jp.TopHeroes, jsonBucket{
				HeroID: h.HeroID, Name: h.Key, Games: h.Games, Wins: h.Wins, WinPct: round1(h.WinPct()),
			})
		}
		jr.Players = append(jr.Players, jp)
	}
	data, err := json.MarshalIndent(jr, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
