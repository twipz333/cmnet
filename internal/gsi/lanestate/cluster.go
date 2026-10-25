package lanestate

import (
	"sort"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
)

func clusterWave(creeps []projectedCreep, gap float64, now *int, g *worldmap.LaneGeometry, side observe.Team) []WaveGroup {
	if len(creeps) == 0 {
		return nil
	}
	sort.Slice(creeps, func(i, j int) bool {
		if creeps[i].absDist != creeps[j].absDist {
			return creeps[i].absDist < creeps[j].absDist
		}
		if creeps[i].pos.X != creeps[j].pos.X {
			return creeps[i].pos.X < creeps[j].pos.X
		}
		return creeps[i].pos.Y < creeps[j].pos.Y
	})
	var groups []WaveGroup
	start := 0
	for i := 1; i <= len(creeps); i++ {
		split := i == len(creeps) || creeps[i].absDist-creeps[i-1].absDist > gap
		if !split {
			continue
		}
		groups = append(groups, makeGroup(creeps[start:i], now, g, side))
		start = i
	}
	sortGroups(groups)
	return groups
}

func makeGroup(creeps []projectedCreep, now *int, g *worldmap.LaneGeometry, side observe.Team) WaveGroup {
	wg := WaveGroup{
		Lane:                creeps[0].lane,
		Side:                side,
		CreepCount:          len(creeps),
		ObservedAtGameTime:  cloneInt(now),
		MinAbsoluteProgress: creeps[0].absProg,
		MaxAbsoluteProgress: creeps[0].absProg,
	}
	var sx, sy, sd float64
	for _, c := range creeps {
		switch c.class {
		case observe.CreepMelee:
			wg.MeleeCount++
		case observe.CreepRanged:
			wg.RangedCount++
		case observe.CreepFlagbearer:
			wg.FlagbearerCount++
		case observe.CreepSiege:
			wg.SiegeCount++
		default:
			wg.OtherCount++
		}
		wg.Positions = append(wg.Positions, c.pos)
		sx += c.pos.X
		sy += c.pos.Y
		sd += c.absDist
		if c.absProg < wg.MinAbsoluteProgress {
			wg.MinAbsoluteProgress = c.absProg
		}
		if c.absProg > wg.MaxAbsoluteProgress {
			wg.MaxAbsoluteProgress = c.absProg
		}
	}
	n := float64(len(creeps))
	wg.CenterPosition = worldmap.WorldPosition{X: sx / n, Y: sy / n}
	wg.CenterAbsoluteDistance = sd / n
	length := g.PathLength(wg.Lane)
	if length > 0 {
		wg.CenterAbsoluteProgress = worldmap.AbsoluteProgress(wg.CenterAbsoluteDistance / length)
	}
	radiant := fromRadiant(side)
	if radiant {
		wg.FrontAbsoluteProgress = wg.MaxAbsoluteProgress
		// Front distance: max absDist among members.
		maxD := creeps[0].absDist
		for _, c := range creeps[1:] {
			if c.absDist > maxD {
				maxD = c.absDist
			}
		}
		wg.FrontAbsoluteDistance = maxD
	} else {
		wg.FrontAbsoluteProgress = wg.MinAbsoluteProgress
		minD := creeps[0].absDist
		for _, c := range creeps[1:] {
			if c.absDist < minD {
				minD = c.absDist
			}
		}
		wg.FrontAbsoluteDistance = minD
	}
	wg.CenterTeamProgress = worldmap.TowardEnemy(wg.CenterAbsoluteProgress, radiant)
	wg.FrontTeamProgress = worldmap.TowardEnemy(wg.FrontAbsoluteProgress, radiant)
	return wg
}

func frontmostOf(groups []WaveGroup) *WaveGroup {
	if len(groups) == 0 {
		return nil
	}
	best := 0
	for i := 1; i < len(groups); i++ {
		if groups[i].FrontTeamProgress > groups[best].FrontTeamProgress {
			best = i
			continue
		}
		if groups[i].FrontTeamProgress == groups[best].FrontTeamProgress {
			if groups[i].CreepCount > groups[best].CreepCount {
				best = i
				continue
			}
			if groups[i].CreepCount == groups[best].CreepCount &&
				groups[i].CenterAbsoluteDistance < groups[best].CenterAbsoluteDistance {
				best = i
			}
		}
	}
	cp := groups[best]
	cp.Positions = append([]worldmap.WorldPosition(nil), cp.Positions...)
	return &cp
}

func copyGroups(in []WaveGroup) []WaveGroup {
	if in == nil {
		return nil
	}
	out := make([]WaveGroup, len(in))
	for i, g := range in {
		g.Positions = append([]worldmap.WorldPosition(nil), g.Positions...)
		out[i] = g
	}
	return out
}

func copyGroupPtr(g *WaveGroup) *WaveGroup {
	if g == nil {
		return nil
	}
	cp := *g
	cp.Positions = append([]worldmap.WorldPosition(nil), g.Positions...)
	return &cp
}

func groupsFor(tick LaneTick, side observe.Team) []WaveGroup {
	if side == observe.TeamRadiant {
		return tick.Radiant
	}
	if side == observe.TeamDire {
		return tick.Dire
	}
	return nil
}
