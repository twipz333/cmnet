package belief

import (
	"fmt"
	"strings"

	"cmnet/internal/dota/worldmap"
)

func (s State) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Belief %s (graph %s / map %s)\n", s.ModelVersion, s.GraphVersion, s.GraphMapVersion)
	for _, enemy := range s.Enemies {
		fmt.Fprintf(&b, "\n%s\n", displayHero(enemy.Hero))
		switch enemy.Status {
		case StatusObserved:
			region := "UNKNOWN"
			if len(enemy.TopRegions) > 0 {
				region = string(enemy.TopRegions[0].Region)
			}
			fmt.Fprintf(&b, "  %s\n  %s\n", enemy.Status, region)
		case StatusObservedAmbiguous:
			fmt.Fprintf(&b, "  %s\n", enemy.Status)
			for _, rm := range enemy.TopRegionsN(3) {
				fmt.Fprintf(&b, "  %-22s %.2f\n", rm.Region, rm.Mass)
			}
			if enemy.UnmodeledMass > 0 {
				fmt.Fprintf(&b, "  unmodeled=%.2f\n", enemy.UnmodeledMass)
			}
		case StatusUnlocalized:
			fmt.Fprintf(&b, "  %s\n", enemy.Status)
			if enemy.Reason != "" {
				fmt.Fprintf(&b, "  %s\n", enemy.Reason)
			}
			fmt.Fprintf(&b, "  unmodeled=%.2f\n", enemy.UnmodeledMass)
		default:
			reachable := 0
			for _, rr := range enemy.WalkingReachability {
				if rr.ReachableByWalkV0 {
					reachable++
				}
			}
			last := "UNKNOWN"
			if len(enemy.LastSeenRegions) > 0 {
				last = string(enemy.LastSeenRegions[0])
				if len(enemy.LastSeenRegions) > 1 {
					last = strings.Join(regionIDs(enemy.LastSeenRegions), ",")
				}
			}
			fmt.Fprintf(&b, "  missing=%.1fs\n", enemy.MissingAge)
			fmt.Fprintf(&b, "  last=%s\n", last)
			fmt.Fprintf(&b, "  walk_reachable=%d regions\n", reachable)
			fmt.Fprintf(&b, "  unmodeled=%.2f\n", enemy.UnmodeledMass)
			fmt.Fprintf(&b, "  entropy=%.2f\n", enemy.Entropy)
			for i, rm := range enemy.TopRegionsN(3) {
				fmt.Fprintf(&b, "\n  %d. %-22s %.2f\n", i+1, rm.Region, rm.Mass)
			}
		}
	}
	return b.String()
}

func displayHero(unit string) string {
	unit = strings.TrimPrefix(unit, "npc_dota_hero_")
	unit = strings.ReplaceAll(unit, "_", " ")
	if unit == "" {
		return "(unknown)"
	}
	parts := strings.Fields(unit)
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func regionIDs(in []worldmap.RegionID) []string {
	out := make([]string, len(in))
	for i, region := range in {
		out[i] = string(region)
	}
	return out
}
