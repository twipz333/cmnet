package replay

import (
	"fmt"
	"strings"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/lanestate"
	"cmnet/internal/gsi/observe"
)

// LaneCoverage aggregates assignment and grouping over a replay.
type LaneCoverage struct {
	Version       string
	Eligible      int
	Assigned      int
	TooFar        int
	Ambiguous     int
	NonLane       int
	ByLane        map[worldmap.LaneID]int
	RadiantAbsSum float64
	RadiantAbsN   int
	DireAbsSum    float64
	DireAbsN      int

	// GroupHist[lane][side][bucket] where bucket 0,1,2,3=3+
	GroupHist map[worldmap.LaneID]map[observe.Team][4]int

	DepthHits map[worldmap.LaneDepth]int
	Jumps     []LaneJump
}

type LaneJump struct {
	Seq      uint64
	Lane     worldmap.LaneID
	From, To worldmap.TeamProgress
	Dt       int
}

type LaneSamples struct {
	Snapshot  *LaneTickSample
	MultiWave *LaneTickSample
	Contact   *LaneTickSample
	Fog       *FogSample
}

type LaneTickSample struct {
	Seq   uint64
	Clock int
	Text  string
}

type FogSample struct {
	VisibleSeq    uint64
	VisibleFront  worldmap.TeamProgress
	GoneSeq       uint64
	GoneAge       float64
	StillSeq      uint64
	StillAge      float64
	ReappearSeq   uint64
	ReappearFront worldmap.TeamProgress
	Lane          worldmap.LaneID
	phase         int
}

type lanePrev struct {
	front worldmap.TeamProgress
	has   bool
	gt    int
	seq   uint64
}

func accumulateLaneObs(cov *LaneCoverage, obs lanestate.Observation, g *worldmap.LaneGeometry) {
	if cov.ByLane == nil {
		cov.ByLane = map[worldmap.LaneID]int{}
		cov.GroupHist = map[worldmap.LaneID]map[observe.Team][4]int{}
		cov.DepthHits = map[worldmap.LaneDepth]int{}
		if g != nil {
			cov.Version = g.Version()
		}
	}
	cov.Eligible += obs.Stats.LaneCreepsSeen
	cov.Assigned += obs.Stats.LaneCreepsAssigned
	cov.TooFar += obs.Stats.UnknownTooFar
	cov.Ambiguous += obs.Stats.Ambiguous
	cov.NonLane += obs.Stats.NonLaneSkipped
	if !obs.MinimapPresent {
		return
	}
	noteHist(cov, worldmap.LaneTop, observe.TeamRadiant, len(obs.Top.Radiant))
	noteHist(cov, worldmap.LaneTop, observe.TeamDire, len(obs.Top.Dire))
	noteHist(cov, worldmap.LaneMid, observe.TeamRadiant, len(obs.Mid.Radiant))
	noteHist(cov, worldmap.LaneMid, observe.TeamDire, len(obs.Mid.Dire))
	noteHist(cov, worldmap.LaneBot, observe.TeamRadiant, len(obs.Bot.Radiant))
	noteHist(cov, worldmap.LaneBot, observe.TeamDire, len(obs.Bot.Dire))
	cov.ByLane[worldmap.LaneTop] += assignedIn(obs.Top)
	cov.ByLane[worldmap.LaneMid] += assignedIn(obs.Mid)
	cov.ByLane[worldmap.LaneBot] += assignedIn(obs.Bot)
	sumAbs(cov, obs.Top)
	sumAbs(cov, obs.Mid)
	sumAbs(cov, obs.Bot)
}

func sumAbs(cov *LaneCoverage, tick lanestate.LaneTick) {
	for _, g := range tick.Radiant {
		cov.RadiantAbsSum += float64(g.CenterAbsoluteProgress) * float64(g.CreepCount)
		cov.RadiantAbsN += g.CreepCount
	}
	for _, g := range tick.Dire {
		cov.DireAbsSum += float64(g.CenterAbsoluteProgress) * float64(g.CreepCount)
		cov.DireAbsN += g.CreepCount
	}
}

func assignedIn(tick lanestate.LaneTick) int {
	n := 0
	for _, g := range tick.Radiant {
		n += g.CreepCount
	}
	for _, g := range tick.Dire {
		n += g.CreepCount
	}
	return n
}

func noteHist(cov *LaneCoverage, lane worldmap.LaneID, side observe.Team, n int) {
	m := cov.GroupHist[lane]
	if m == nil {
		m = map[observe.Team][4]int{}
		cov.GroupHist[lane] = m
	}
	b := n
	if b > 3 {
		b = 3
	}
	arr := m[side]
	arr[b]++
	m[side] = arr
}

func noteLaneSamples(s *LaneSamples, cov *LaneCoverage, seq uint64, clock int, ls lanestate.State, prev map[worldmap.LaneID]*lanePrev) {
	if cov.DepthHits == nil {
		cov.DepthHits = map[worldmap.LaneDepth]int{}
	}
	for _, v := range []lanestate.LaneView{ls.Top, ls.Mid, ls.Bot} {
		if v.Our.HasFront {
			cov.DepthHits[v.Our.FrontDepth]++
			p := prev[v.Lane]
			if p == nil {
				p = &lanePrev{}
				prev[v.Lane] = p
			}
			if p.has && clock-p.gt <= 1 && clock >= p.gt {
				delta := v.Our.FrontTeam - p.front
				if delta < 0 {
					delta = -delta
				}
				if delta >= 0.25 && len(cov.Jumps) < 8 {
					cov.Jumps = append(cov.Jumps, LaneJump{
						Seq: seq, Lane: v.Lane, From: p.front, To: v.Our.FrontTeam, Dt: clock - p.gt,
					})
				}
			}
			p.has, p.front, p.gt, p.seq = true, v.Our.FrontTeam, clock, seq
		}
		if s.MultiWave == nil && len(v.Our.Waves) >= 2 {
			cp := LaneTickSample{Seq: seq, Clock: clock, Text: describeLane(v)}
			s.MultiWave = &cp
		}
	}
	if s.Snapshot == nil && clock >= 120 && ls.Top.Our.HasFront && ls.Mid.Our.HasFront && ls.Bot.Our.HasFront {
		cp := LaneTickSample{Seq: seq, Clock: clock, Text: ls.Format()}
		s.Snapshot = &cp
	}
	if s.Contact == nil {
		for _, v := range []lanestate.LaneView{ls.Top, ls.Mid, ls.Bot} {
			if v.ObservedContact {
				cp := LaneTickSample{Seq: seq, Clock: clock, Text: describeLane(v)}
				s.Contact = &cp
				break
			}
		}
	}
	noteFog(s, seq, ls)
}

func noteFog(s *LaneSamples, seq uint64, ls lanestate.State) {
	if s.Fog != nil && s.Fog.phase >= 4 {
		return
	}
	for _, v := range []lanestate.LaneView{ls.Bot, ls.Mid, ls.Top} {
		vis := v.Enemy.HasVisibleFront
		if s.Fog == nil {
			if vis {
				s.Fog = &FogSample{Lane: v.Lane, VisibleSeq: seq, VisibleFront: v.Enemy.VisibleFrontTeam, phase: 1}
			}
			continue
		}
		if s.Fog.Lane != v.Lane {
			continue
		}
		age := 0.0
		if v.Enemy.LastSeenAgeSeconds != nil {
			age = *v.Enemy.LastSeenAgeSeconds
		}
		switch s.Fog.phase {
		case 1:
			if !vis {
				s.Fog.GoneSeq = seq
				s.Fog.GoneAge = age
				s.Fog.phase = 2
			}
		case 2:
			if !vis && age >= 2 {
				s.Fog.StillSeq = seq
				s.Fog.StillAge = age
				s.Fog.phase = 3
			} else if vis {
				s.Fog = nil // too brief
			}
		case 3:
			if vis {
				s.Fog.ReappearSeq = seq
				s.Fog.ReappearFront = v.Enemy.VisibleFrontTeam
				s.Fog.phase = 4
			}
		}
	}
}

func describeLane(v lanestate.LaneView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s our_waves=%d", v.Lane, len(v.Our.Waves))
	if v.Our.HasFront {
		fmt.Fprintf(&b, " front_team=%.2f depth=%s creeps=%d", v.Our.FrontTeam, v.Our.FrontDepth, v.Our.CreepCount)
	}
	if len(v.Our.Waves) >= 2 {
		for i, g := range v.Our.Waves {
			mark := ""
			if v.Our.Frontmost != nil && g.CenterAbsoluteDistance == v.Our.Frontmost.CenterAbsoluteDistance {
				mark = " frontmost"
			}
			fmt.Fprintf(&b, "\n    group%d team=%.2f abs=%.2f creeps=%d%s", i+1, g.FrontTeamProgress, g.FrontAbsoluteProgress, g.CreepCount, mark)
		}
	}
	if v.Enemy.HasVisibleFront {
		fmt.Fprintf(&b, " enemy_visible_front=%.2f", v.Enemy.VisibleFrontTeam)
	} else {
		b.WriteString(" enemy_visible=none")
	}
	if v.ObservedContact {
		b.WriteString(" contact=true")
	}
	return b.String()
}

func (c LaneCoverage) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lane geometry version: %s\n", c.Version)
	fmt.Fprintf(&b, "Eligible lane creep observations: %d\n", c.Eligible)
	fmt.Fprintf(&b, "Assigned: %d\n", c.Assigned)
	fmt.Fprintf(&b, "  TOP: %d\n  MID: %d\n  BOT: %d\n", c.ByLane[worldmap.LaneTop], c.ByLane[worldmap.LaneMid], c.ByLane[worldmap.LaneBot])
	fmt.Fprintf(&b, "UNKNOWN too far: %d\nAMBIGUOUS: %d\nNon-lane skipped: %d\n", c.TooFar, c.Ambiguous, c.NonLane)
	cov := 0.0
	if c.Eligible > 0 {
		cov = 100 * float64(c.Assigned) / float64(c.Eligible)
	}
	fmt.Fprintf(&b, "Coverage: %.2f%%\n", cov)
	if c.RadiantAbsN > 0 && c.DireAbsN > 0 {
		fmt.Fprintf(&b, "Mean absolute progress: Radiant=%.3f Dire=%.3f\n", c.RadiantAbsSum/float64(c.RadiantAbsN), c.DireAbsSum/float64(c.DireAbsN))
	}
	b.WriteString("\nWave groups (minimap-present ticks):\n")
	for _, lane := range []worldmap.LaneID{worldmap.LaneTop, worldmap.LaneMid, worldmap.LaneBot} {
		sides := c.GroupHist[lane]
		for _, side := range []observe.Team{observe.TeamRadiant, observe.TeamDire} {
			h := sides[side]
			name := "Radiant"
			if side == observe.TeamDire {
				name = "Dire"
			}
			fmt.Fprintf(&b, "  %s %s: 0=%d 1=%d 2=%d 3+=%d\n", lane, name, h[0], h[1], h[2], h[3])
		}
	}
	if len(c.DepthHits) > 0 {
		b.WriteString("\nOur front depth hits:\n")
		for _, d := range []worldmap.LaneDepth{
			worldmap.DepthOwnBase, worldmap.DepthOwnSide, worldmap.DepthCenter,
			worldmap.DepthEnemySide, worldmap.DepthEnemyBase, worldmap.DepthUnknown,
		} {
			if c.DepthHits[d] > 0 {
				fmt.Fprintf(&b, "  %s: %d\n", d, c.DepthHits[d])
			}
		}
	}
	if len(c.Jumps) > 0 {
		b.WriteString("\nOur front jumps (>=0.25 team progress in <=1s game clock):\n")
		for _, j := range c.Jumps {
			fmt.Fprintf(&b, "  seq=%d %s %.2f -> %.2f dt=%d\n", j.Seq, j.Lane, j.From, j.To, j.Dt)
		}
	}
	return b.String()
}
