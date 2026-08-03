package stratz

import (
	"context"
	"fmt"
	"strings"
)

// PositionStat is a hero's aggregated play in one lane position over the queried
// weeks: total matches and wins. WinRate = Wins/Matches.
type PositionStat struct {
	HeroID  int
	Matches int
	Wins    int
}

// rankBracketNames are the STRATZ winWeek bracketIds enum values, indexed by the
// OpenDota rank tier tens digit (tier/10): 1=Herald … 8=Immortal. This mirrors
// the advisor's --min/max-rank-tier scale so the meta is fetched at our bracket.
var rankBracketNames = map[int]string{
	1: "HERALD",
	2: "GUARDIAN",
	3: "CRUSADER",
	4: "ARCHON",
	5: "LEGEND",
	6: "ANCIENT",
	7: "DIVINE",
	8: "IMMORTAL",
}

// BracketsForTierRange maps an OpenDota avg_rank_tier range (e.g. 40..85) to the
// STRATZ bracket enum names it covers (ARCHON..IMMORTAL). Out-of-range tiers are
// clamped to [1,8]. Returns at least IMMORTAL when the range is empty.
func BracketsForTierRange(minTier, maxTier int) []string {
	lo, hi := minTier/10, maxTier/10
	if lo < 1 {
		lo = 1
	}
	if hi > 8 {
		hi = 8
	}
	var out []string
	for t := lo; t <= hi; t++ {
		if name, ok := rankBracketNames[t]; ok {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		out = []string{"IMMORTAL"}
	}
	return out
}

// buildHeroStatsQuery returns a winWeek query for one lane position with the enum
// arguments inlined. We inline the enum lists (rather than using typed GraphQL
// variables) to avoid guessing STRATZ's enum type names — the working community
// query does the same. Values are enum identifiers from our own code (positions
// 1-5, fixed bracket/mode names), never user free-text, so inlining is safe.
func buildHeroStatsQuery(take, pos int, brackets, modes []string) string {
	return fmt.Sprintf(`{
  heroStats {
    winWeek(take: %d, bracketIds: [%s], positionIds: [POSITION_%d], gameModeIds: [%s]) {
      heroId
      matchCount
      winCount
    }
  }
}`, take, strings.Join(brackets, ", "), pos, strings.Join(modes, ", "))
}

type heroStatsResult struct {
	HeroStats struct {
		WinWeek []struct {
			HeroID     int `json:"heroId"`
			MatchCount int `json:"matchCount"`
			WinCount   int `json:"winCount"`
		} `json:"winWeek"`
	} `json:"heroStats"`
}

// HeroPositionStats returns, for each lane position 1-5, the per-hero aggregated
// play (matches/wins) over the last `weeks` weeks at the given brackets and game
// modes. gameModes are STRATZ GameModeEnumType names (e.g. "ALL_PICK_RANKED").
// Positions are queried one at a time because winWeek aggregates across whatever
// positionIds it is given, and we want them kept separate.
func (c *Client) HeroPositionStats(ctx context.Context, brackets, gameModes []string, weeks int) (map[int][]PositionStat, error) {
	if weeks <= 0 {
		weeks = 1
	}
	out := make(map[int][]PositionStat, 5)
	for pos := 1; pos <= 5; pos++ {
		var res heroStatsResult
		q := buildHeroStatsQuery(weeks, pos, brackets, gameModes)
		if err := c.query(ctx, q, nil, &res); err != nil {
			return nil, fmt.Errorf("stratz heroStats POSITION_%d: %w", pos, err)
		}
		// Aggregate the per-week rows into one entry per hero.
		agg := map[int]*PositionStat{}
		for _, row := range res.HeroStats.WinWeek {
			s := agg[row.HeroID]
			if s == nil {
				s = &PositionStat{HeroID: row.HeroID}
				agg[row.HeroID] = s
			}
			s.Matches += row.MatchCount
			s.Wins += row.WinCount
		}
		list := make([]PositionStat, 0, len(agg))
		for _, s := range agg {
			list = append(list, *s)
		}
		out[pos] = list
	}
	return out, nil
}

// DefaultMetaGameModes is the game-mode filter for the position meta. Ranked
// all-pick gives the densest, most reliably position-labeled signal (STRATZ infers
// roles), which is what the "patch meta by position" panel wants — even though the
// advisor itself drafts Captains Mode.
func DefaultMetaGameModes() []string { return []string{"ALL_PICK_RANKED"} }
