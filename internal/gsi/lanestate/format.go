package lanestate

import (
	"fmt"
	"strings"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
)

// Format is compact developer text. Last-seen is never printed as current.
func (s State) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lanes geometry %s (map %s) minimap=%v side=%s\n",
		s.GeometryVersion, s.MapGeometryVersion, s.MinimapPresent, sideName(s.OurSide))
	writeLane(&b, s.Top)
	writeLane(&b, s.Mid)
	writeLane(&b, s.Bot)
	return b.String()
}

func writeLane(b *strings.Builder, v LaneView) {
	fmt.Fprintf(b, "\n%s\n", v.Lane)
	fmt.Fprintf(b, "  our:\n")
	if len(v.Our.Waves) == 0 {
		b.WriteString("    waves=0\n")
	} else {
		fmt.Fprintf(b, "    waves=%d creeps=%d\n", len(v.Our.Waves), v.Our.CreepCount)
		if v.Our.HasFront {
			fmt.Fprintf(b, "    front_team=%.2f abs=%.2f depth=%s\n",
				v.Our.FrontTeam, v.Our.FrontAbs, v.Our.FrontDepth)
		}
		if len(v.Our.Waves) > 1 {
			for i, g := range v.Our.Waves {
				mark := ""
				if v.Our.Frontmost != nil && sameGroup(v.Our.Frontmost, g) {
					mark = " frontmost"
				}
				fmt.Fprintf(b, "    group%d team=%.2f creeps=%d%s\n", i+1, g.FrontTeamProgress, g.CreepCount, mark)
			}
		}
	}
	fmt.Fprintf(b, "  enemy:\n")
	if len(v.Enemy.Visible) == 0 {
		b.WriteString("    visible: none\n")
	} else {
		fmt.Fprintf(b, "    visible_waves=%d\n", len(v.Enemy.Visible))
		if v.Enemy.HasVisibleFront {
			fmt.Fprintf(b, "    visible_front_team=%.2f abs=%.2f depth=%s\n",
				v.Enemy.VisibleFrontTeam, v.Enemy.VisibleFrontAbs, v.Enemy.VisibleFrontDepth)
		}
	}
	if len(v.Enemy.LastSeen) == 0 {
		b.WriteString("    last seen: none\n")
	} else {
		age := "-"
		if v.Enemy.LastSeenAgeSeconds != nil {
			age = fmt.Sprintf("%.1fs", *v.Enemy.LastSeenAgeSeconds)
		}
		fmt.Fprintf(b, "    last seen: front_team=%.2f age=%s waves=%d\n",
			v.Enemy.LastSeenFrontTeam, age, len(v.Enemy.LastSeen))
	}
	if v.ObservedContact {
		gap := "-"
		if v.VisibleFrontPathGap != nil {
			gap = fmt.Sprintf("%.0f", *v.VisibleFrontPathGap)
		}
		fmt.Fprintf(b, "  observed_contact=true visible_front_gap=%s\n", gap)
	} else if v.VisibleFrontPathGap != nil {
		fmt.Fprintf(b, "  visible_front_gap=%.0f\n", *v.VisibleFrontPathGap)
	}
}

func sameGroup(a *WaveGroup, b WaveGroup) bool {
	if a == nil {
		return false
	}
	return a.CenterAbsoluteDistance == b.CenterAbsoluteDistance && a.CreepCount == b.CreepCount
}

func sideName(t observe.Team) string {
	switch t {
	case observe.TeamRadiant:
		return "radiant"
	case observe.TeamDire:
		return "dire"
	default:
		return "unknown"
	}
}

// FormatProjection is the CLI one-liner for a coordinate.
func FormatProjection(g *worldmap.LaneGeometry, p worldmap.WorldPosition, fromRadiant *bool) string {
	pr := g.Project(p)
	var b strings.Builder
	if pr.Assigned {
		fmt.Fprintf(&b, "lane=%s\n", pr.Lane)
	} else if pr.Ambiguous {
		b.WriteString("lane=UNKNOWN (ambiguous)\n")
	} else {
		b.WriteString("lane=UNKNOWN (too far)\n")
	}
	fmt.Fprintf(&b, "absolute_progress=%.3f\n", pr.AbsoluteProgress)
	fmt.Fprintf(&b, "distance_to_centerline=%.0f\n", pr.Distance)
	if fromRadiant != nil {
		tp := worldmap.TowardEnemy(pr.AbsoluteProgress, *fromRadiant)
		side := "Dire"
		if *fromRadiant {
			side = "Radiant"
		}
		fmt.Fprintf(&b, "team_progress(%s)=%.3f\n", side, tp)
		fmt.Fprintf(&b, "depth(%s)=%s\n", side, g.Depth(tp))
	} else {
		fmt.Fprintf(&b, "team_progress(Radiant)=%.3f\n", worldmap.TowardEnemy(pr.AbsoluteProgress, true))
		fmt.Fprintf(&b, "team_progress(Dire)=%.3f\n", worldmap.TowardEnemy(pr.AbsoluteProgress, false))
	}
	return b.String()
}
