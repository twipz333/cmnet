package state

import (
	"fmt"
	"strings"

	"cmnet/internal/dota/worldmap"
)

func (s State) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "DotaState %s\n", s.Meta.SchemaVersion)
	if s.Meta.GameTime.Available || s.Meta.ClockTime.Available || s.Meta.OurSide.Available {
		fmt.Fprintf(&b, "time=%s side=%s\n", formatOptionalInt(s.Meta.GameTime, s.Meta.ClockTime), s.Meta.OurSide.Value)
	}
	b.WriteString("\nSelf:\n")
	fmt.Fprintf(&b, "  %s\n", valueOrUnknown(s.Self.Hero))
	fmt.Fprintf(&b, "  region=%s\n", regionOrUnknown(s.Self.Region))
	fmt.Fprintf(&b, "  lvl=%s hp=%s mana=%s\n", intOrQ(s.Self.Level), intOrQ(s.Self.HPPct), intOrQ(s.Self.ManaPct))
	fmt.Fprintf(&b, "  buyback=%s smoked=%s\n", boolOrQ(s.Self.BuybackAvailable), boolOrQ(s.Self.Smoked))
	b.WriteString("\nTeam:\n")
	fmt.Fprintf(&b, "  grouped=%d/%d (%.2f)\n", s.Team.GroupedCount, s.Team.ObservedAllyCount, s.Team.GroupedRatio.Value)
	fmt.Fprintf(&b, "  main_region=%s\n", regionOrUnknown(s.Team.MainGroupRegion))
	fmt.Fprintf(&b, "  spread=%s\n", floatOrQ(s.Team.Spread))
	b.WriteString("\nEnemies:\n")
	for _, enemy := range s.Enemies {
		fmt.Fprintf(&b, "  %s\n", displayHero(enemy.Hero))
		if enemy.Visible {
			fmt.Fprintf(&b, "    visible %s\n", strings.Join(regionStrings(enemy.CurrentRegions), ","))
			continue
		}
		fmt.Fprintf(&b, "    missing %s\n", floatOrQ(enemy.LastSeenAge))
		fmt.Fprintf(&b, "    last=%s\n", strings.Join(regionStrings(enemy.LastSeenRegions), ","))
		if len(enemy.BeliefRegionMass) > 0 {
			fmt.Fprintf(&b, "    belief: %s\n", enemy.TopBeliefString(min(3, len(enemy.BeliefRegionMass))))
		}
		fmt.Fprintf(&b, "    unmodeled=%.2f\n", enemy.BeliefUnmodeledMass)
	}
	b.WriteString("\nLanes:\n")
	fmt.Fprintf(&b, "  TOP our=%s enemy_visible=%s enemy_last=%s age=%s\n",
		floatOrQ(s.Lanes.Top.OurFrontTeamProgress), floatOrQ(s.Lanes.Top.VisibleEnemyFrontProgress), floatOrQ(s.Lanes.Top.EnemyLastSeenFrontProgress), floatOrQ(s.Lanes.Top.EnemyLastSeenFrontAge))
	fmt.Fprintf(&b, "  MID our=%s enemy_visible=%s enemy_last=%s age=%s\n",
		floatOrQ(s.Lanes.Mid.OurFrontTeamProgress), floatOrQ(s.Lanes.Mid.VisibleEnemyFrontProgress), floatOrQ(s.Lanes.Mid.EnemyLastSeenFrontProgress), floatOrQ(s.Lanes.Mid.EnemyLastSeenFrontAge))
	fmt.Fprintf(&b, "  BOT our=%s enemy_visible=%s enemy_last=%s age=%s\n",
		floatOrQ(s.Lanes.Bot.OurFrontTeamProgress), floatOrQ(s.Lanes.Bot.VisibleEnemyFrontProgress), floatOrQ(s.Lanes.Bot.EnemyLastSeenFrontProgress), floatOrQ(s.Lanes.Bot.EnemyLastSeenFrontAge))
	b.WriteString("\nRoshan Pit B:\n")
	fmt.Fprintf(&b, "  allies=%d visible_enemies=%d\n", s.Objectives.PitB.AlliesNear, s.Objectives.PitB.VisibleEnemiesNear)
	b.WriteString("\nQuality:\n")
	fmt.Fprintf(&b, "  roster=%v allies=%d missing_enemies=%d\n", s.Quality.RosterReady, s.Quality.ObservedAllyCount, s.Quality.MissingEnemyCount)
	return b.String()
}

func displayHero(unit string) string {
	unit = strings.TrimPrefix(unit, "npc_dota_hero_")
	unit = strings.ReplaceAll(unit, "_", " ")
	parts := strings.Fields(unit)
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
func valueOrUnknown(v OptionalString) string {
	if !v.Available {
		return "UNKNOWN"
	}
	return v.Value
}
func regionOrUnknown(v OptionalRegion) string {
	if !v.Available {
		return "UNKNOWN"
	}
	return string(v.Value)
}
func intOrQ(v OptionalInt) string {
	if !v.Available {
		return "?"
	}
	return fmt.Sprintf("%d", v.Value)
}
func floatOrQ(v OptionalFloat) string {
	if !v.Available {
		return "?"
	}
	return fmt.Sprintf("%.2f", v.Value)
}
func boolOrQ(v OptionalBool) string {
	if !v.Available {
		return "?"
	}
	if v.Value {
		return "yes"
	}
	return "no"
}
func regionStrings(in []worldmap.RegionID) []string {
	out := make([]string, len(in))
	for i, r := range in {
		out[i] = string(r)
	}
	return out
}
func formatOptionalInt(game OptionalInt, clock OptionalInt) string {
	if game.Available {
		return fmt.Sprintf("%d", game.Value)
	}
	if clock.Available {
		return fmt.Sprintf("%d", clock.Value)
	}
	return "?"
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
