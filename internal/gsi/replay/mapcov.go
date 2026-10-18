package replay

import (
	"fmt"
	"sort"
	"strings"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/mapstate"
	"cmnet/internal/gsi/observe"
)

// MapCoverage aggregates RegionAt over every current hero position seen in replay.
type MapCoverage struct {
	Version    string
	Positions  int
	Classified int
	Other      int
	ByRegion   map[worldmap.RegionID]int
}

type MapSamples struct {
	AllGrouped  *MapTickSample
	FourPlusOne *MapTickSample
	Split       *MapTickSample
	RoshanNear  *MapTickSample
}

type MapTickSample struct {
	Seq            uint64
	Clock          int
	Grouped        int
	Observed       int
	Ratio          float64
	CentroidRegion worldmap.RegionID
	PitAAllies     int
	PitAVisEnemies int
	PitBAllies     int
	PitBVisEnemies int
}

func noteSamples(s *MapSamples, seq uint64, clock int, ms mapstate.State) {
	sample := MapTickSample{
		Seq: seq, Clock: clock,
		Grouped: ms.AllyTeam.GroupedCount, Observed: ms.AllyTeam.ObservedCount,
		Ratio: ms.AllyTeam.GroupedRatio, CentroidRegion: ms.AllyTeam.CentroidRegion,
		PitAAllies: ms.RoshanPitA.AlliesNear, PitAVisEnemies: ms.RoshanPitA.VisibleEnemiesNear,
		PitBAllies: ms.RoshanPitB.AlliesNear, PitBVisEnemies: ms.RoshanPitB.VisibleEnemiesNear,
	}
	if s.AllGrouped == nil && sample.Observed >= 5 && sample.Grouped == sample.Observed {
		cp := sample
		s.AllGrouped = &cp
	}
	if s.FourPlusOne == nil && sample.Observed >= 5 && sample.Grouped == 4 {
		cp := sample
		s.FourPlusOne = &cp
	}
	if s.Split == nil && sample.Observed >= 4 && sample.Grouped > 0 && sample.Grouped <= sample.Observed/2 {
		cp := sample
		s.Split = &cp
	}
	if s.RoshanNear == nil && (sample.PitAAllies >= 2 || sample.PitBAllies >= 2) {
		cp := sample
		s.RoshanNear = &cp
	}
}

func accumulateCoverage(cov *MapCoverage, ms mapstate.State, g *worldmap.Geometry) {
	if cov.ByRegion == nil {
		cov.ByRegion = map[worldmap.RegionID]int{}
		if g != nil {
			cov.Version = g.Version()
		}
	}
	add := func(pts []worldmap.WorldPosition, regions []worldmap.RegionID) {
		for i := range pts {
			cov.Positions++
			var id worldmap.RegionID
			if i < len(regions) {
				id = regions[i]
			} else if g != nil {
				id = g.RegionAt(pts[i])
			} else {
				id = worldmap.RegionOther
			}
			if id == worldmap.RegionOther {
				cov.Other++
			} else {
				cov.Classified++
			}
			cov.ByRegion[id]++
		}
	}
	add(ms.Self.Positions, ms.Self.Regions)
	for _, a := range ms.Allies {
		if a.Hero == ms.Self.Hero {
			continue // self already counted
		}
		add(a.Positions, a.Regions)
	}
	for _, e := range ms.Enemies {
		add(e.Positions, e.Regions)
	}
}

func classifyTrace(g *worldmap.Geometry, visible bool, cur, last []observe.Vec2) (regions, lastSeen []worldmap.RegionID) {
	if g == nil {
		return nil, nil
	}
	if visible {
		for _, p := range cur {
			regions = append(regions, g.RegionAt(worldmap.FromInt(p.X, p.Y)))
		}
	}
	for _, p := range last {
		lastSeen = append(lastSeen, g.RegionAt(worldmap.FromInt(p.X, p.Y)))
	}
	return regions, lastSeen
}

func (c MapCoverage) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Map geometry version: %s\n", c.Version)
	fmt.Fprintf(&b, "Hero positions classified: %d\n", c.Positions)
	fmt.Fprintf(&b, "Classified: %d\n", c.Classified)
	fmt.Fprintf(&b, "OTHER: %d\n", c.Other)
	if c.Positions > 0 {
		fmt.Fprintf(&b, "Coverage: %.2f%%\n", 100*float64(c.Classified)/float64(c.Positions))
	}
	if len(c.ByRegion) == 0 {
		return b.String()
	}
	b.WriteString("Region distribution:\n")
	keys := make([]worldmap.RegionID, 0, len(c.ByRegion))
	for k := range c.ByRegion {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return c.ByRegion[keys[i]] > c.ByRegion[keys[j]] })
	for _, k := range keys {
		fmt.Fprintf(&b, "  %-28s %d\n", k, c.ByRegion[k])
	}
	return b.String()
}
