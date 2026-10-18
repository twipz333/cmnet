package mapstate

import (
	"fmt"
	"strings"

	"cmnet/internal/dota/worldmap"
)

// Format is compact developer text. It never prints predicted locations.
func (s State) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Map geometry %s\n", s.GeometryVersion)
	b.WriteString("\nSelf:\n")
	writeHero(&b, s.Self)
	b.WriteString("\nAllies:\n")
	if len(s.Allies) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, a := range s.Allies {
		writeHero(&b, a)
	}
	fmt.Fprintf(&b, "\nTeam:\n  main_cluster=%d/%d\n  grouped_ratio=%.2f grouped=%v\n  centroid=%s\n  spread=%.0f\n",
		s.AllyTeam.GroupedCount, s.AllyTeam.ObservedCount, s.AllyTeam.GroupedRatio, s.AllyTeam.AllyGrouped,
		centroidLabel(s.AllyTeam), s.AllyTeam.Spread)

	b.WriteString("\nEnemies:\n")
	if len(s.Enemies) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, e := range s.Enemies {
		writeHero(&b, e)
	}
	fmt.Fprintf(&b, "\nVisible enemies: %d\nMissing enemies: %d\n", s.VisibleEnemyCount, s.MissingEnemyCount)
	if len(s.VisibleEnemiesByRegion) > 0 {
		b.WriteString("By region:\n")
		for _, rc := range s.VisibleEnemiesByRegion {
			fmt.Fprintf(&b, "  %s: %d\n", rc.Region, rc.Count)
		}
	}
	b.WriteString("\nRoshan:\n")
	writeObj(&b, "  Pit A", s.RoshanPitA)
	writeObj(&b, "  Pit B", s.RoshanPitB)
	b.WriteString("Bases:\n")
	writeObj(&b, "  ours", s.OurBase)
	writeObj(&b, "  enemy", s.EnemyBase)
	return b.String()
}

func writeHero(b *strings.Builder, h HeroSpatial) {
	name := h.Hero
	if name == "" {
		name = "(unknown)"
	}
	if h.Visible && len(h.Regions) > 0 {
		if h.PrimaryRegion != nil {
			fmt.Fprintf(b, "  %-16s %s\n", display(name), *h.PrimaryRegion)
			return
		}
		fmt.Fprintf(b, "  %-16s AMBIGUOUS %s\n", display(name), joinRegions(h.Regions))
		return
	}
	age := "?"
	if h.LastSeenAgeSeconds != nil {
		age = fmt.Sprintf("%.1fs", *h.LastSeenAgeSeconds)
	}
	last := "UNKNOWN"
	if len(h.LastSeenRegions) > 0 {
		last = string(h.LastSeenRegions[0])
		if len(uniqueRegions(h.LastSeenRegions)) > 1 {
			last = "AMBIGUOUS " + joinRegions(h.LastSeenRegions)
		}
	}
	fmt.Fprintf(b, "  %-16s missing %s, last=%s\n", display(name), age, last)
}

func writeObj(b *strings.Builder, label string, o ObjectiveProximity) {
	fmt.Fprintf(b, "%s: allies=%d visible_enemies=%d\n", label, o.AlliesNear, o.VisibleEnemiesNear)
}

func centroidLabel(t TeamSpatial) string {
	if t.Centroid == nil {
		return "UNKNOWN"
	}
	return string(t.CentroidRegion)
}

func joinRegions(rs []worldmap.RegionID) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = string(r)
	}
	return strings.Join(parts, ",")
}

func display(unit string) string {
	s := strings.TrimPrefix(unit, "npc_dota_hero_")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return unit
	}
	parts := strings.Fields(s)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
