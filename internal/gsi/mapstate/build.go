package mapstate

import (
	"sort"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/track"
)

// Build is a stateless conversion of a tracker snapshot into MapState.
func Build(st track.State, g *worldmap.Geometry) State {
	out := State{GeometryVersion: ""}
	if g != nil {
		out.GeometryVersion = g.Version()
	}

	selfName := ""
	if st.Self.Present && st.Self.HeroName != nil {
		selfName = *st.Self.HeroName
	}

	out.Allies = make([]HeroSpatial, 0, len(st.Allies))
	var clusterPts []namedPos
	for _, a := range st.Allies {
		hs := allySpatial(a, st.Self, selfName, g)
		out.Allies = append(out.Allies, hs)
		if len(hs.Positions) == 1 {
			clusterPts = append(clusterPts, namedPos{name: hs.Hero, pos: hs.Positions[0]})
		}
	}
	if selfName != "" && st.Self.Position != nil && !allyContains(out.Allies, selfName) {
		hs := selfOnlySpatial(st.Self, g)
		out.Self = hs
		if len(hs.Positions) == 1 {
			clusterPts = append(clusterPts, namedPos{name: hs.Hero, pos: hs.Positions[0]})
		}
	} else {
		out.Self = selfSpatial(st.Self, out.Allies, g)
	}

	out.Enemies = make([]HeroSpatial, 0, len(st.Enemies))
	byRegion := map[worldmap.RegionID]map[string]struct{}{}
	for _, e := range st.Enemies {
		hs := enemySpatial(e, g)
		out.Enemies = append(out.Enemies, hs)
		if hs.Visible {
			out.VisibleEnemyCount++
			for _, r := range uniqueRegions(hs.Regions) {
				if byRegion[r] == nil {
					byRegion[r] = map[string]struct{}{}
				}
				byRegion[r][hs.Hero] = struct{}{}
			}
		} else {
			out.MissingEnemyCount++
		}
	}
	out.VisibleEnemiesByRegion = flattenRegionCounts(byRegion)

	out.AllyTeam = buildTeam(clusterPts, g)

	out.RoshanPitA = objectiveNear(g, worldmap.LandmarkRoshanPitA, g.Radii().Roshan, out.Allies, out.Enemies)
	out.RoshanPitB = objectiveNear(g, worldmap.LandmarkRoshanPitB, g.Radii().Roshan, out.Allies, out.Enemies)

	ourAnc, theirAnc, ourTor, theirTor := sides(st.Roster.AllySide)
	out.OurBase = objectiveNear(g, ourAnc, g.Radii().Base, out.Allies, out.Enemies)
	out.EnemyBase = objectiveNear(g, theirAnc, g.Radii().Base, out.Allies, out.Enemies)
	out.OurTormentor = objectiveNear(g, ourTor, g.Radii().Tormentor, out.Allies, out.Enemies)
	out.EnemyTormentor = objectiveNear(g, theirTor, g.Radii().Tormentor, out.Allies, out.Enemies)
	return out
}

func allyContains(allies []HeroSpatial, name string) bool {
	for _, a := range allies {
		if a.Hero == name {
			return true
		}
	}
	return false
}

func selfSpatial(self track.SelfState, allies []HeroSpatial, g *worldmap.Geometry) HeroSpatial {
	if self.Present && self.HeroName != nil {
		for _, a := range allies {
			if a.Hero == *self.HeroName {
				hs := a
				if self.Position != nil {
					hs.Positions = []worldmap.WorldPosition{fromVec(*self.Position)}
					hs.Regions = regionsOf(g, hs.Positions)
					hs.PrimaryRegion = primaryOf(hs.Regions)
					hs.Visible = true
				}
				return hs
			}
		}
	}
	return selfOnlySpatial(self, g)
}

func selfOnlySpatial(self track.SelfState, g *worldmap.Geometry) HeroSpatial {
	hs := HeroSpatial{Visible: self.Present && self.Position != nil}
	if self.HeroName != nil {
		hs.Hero = *self.HeroName
	}
	if self.Position != nil {
		hs.Positions = []worldmap.WorldPosition{fromVec(*self.Position)}
		hs.Regions = regionsOf(g, hs.Positions)
		hs.PrimaryRegion = primaryOf(hs.Regions)
	}
	return hs
}

func allySpatial(a track.AllyHeroTrack, self track.SelfState, selfName string, g *worldmap.Geometry) HeroSpatial {
	hs := HeroSpatial{
		Hero:    a.Hero,
		Visible: a.CurrentlyObserved,
	}
	if a.Hero == selfName && self.Position != nil {
		// Local player observation is authoritative; do not expand centroid with illusions.
		hs.Visible = true
		hs.Positions = []worldmap.WorldPosition{fromVec(*self.Position)}
	} else {
		hs.Positions = fromVecs(a.PositionCandidates)
	}
	hs.Regions = regionsOf(g, hs.Positions)
	hs.PrimaryRegion = primaryOf(hs.Regions)
	hs.LastSeenPositions = fromVecs(a.LastObservedPositions)
	hs.LastSeenRegions = regionsOf(g, hs.LastSeenPositions)
	return hs
}

func enemySpatial(e track.EnemyHeroTrack, g *worldmap.Geometry) HeroSpatial {
	hs := HeroSpatial{
		Hero:               e.Hero,
		Visible:            e.VisibleNow,
		LastSeenAgeSeconds: e.LastSeenAgeSeconds,
	}
	if e.VisibleNow {
		hs.Positions = fromVecs(e.VisiblePositions)
		hs.Regions = regionsOf(g, hs.Positions)
		hs.PrimaryRegion = primaryOf(hs.Regions)
	}
	hs.LastSeenPositions = fromVecs(e.LastSeenPositions)
	hs.LastSeenRegions = regionsOf(g, hs.LastSeenPositions)
	return hs
}

func fromVec(v observe.Vec2) worldmap.WorldPosition {
	return worldmap.FromInt(v.X, v.Y)
}

func fromVecs(in []observe.Vec2) []worldmap.WorldPosition {
	if len(in) == 0 {
		return nil
	}
	out := make([]worldmap.WorldPosition, len(in))
	for i, v := range in {
		out[i] = fromVec(v)
	}
	return out
}

func regionsOf(g *worldmap.Geometry, pts []worldmap.WorldPosition) []worldmap.RegionID {
	if len(pts) == 0 {
		return nil
	}
	out := make([]worldmap.RegionID, len(pts))
	for i, p := range pts {
		if g == nil {
			out[i] = worldmap.RegionOther
			continue
		}
		out[i] = g.RegionAt(p)
	}
	return out
}

func uniqueRegions(in []worldmap.RegionID) []worldmap.RegionID {
	seen := map[worldmap.RegionID]struct{}{}
	var out []worldmap.RegionID
	for _, r := range in {
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

func primaryOf(regions []worldmap.RegionID) *worldmap.RegionID {
	if len(regions) == 0 {
		return nil
	}
	first := regions[0]
	for _, r := range regions[1:] {
		if r != first {
			return nil
		}
	}
	cp := first
	return &cp
}

func flattenRegionCounts(m map[worldmap.RegionID]map[string]struct{}) []RegionCount {
	out := make([]RegionCount, 0, len(m))
	for r, heroes := range m {
		out = append(out, RegionCount{Region: r, Count: len(heroes)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Region == out[j].Region {
			return out[i].Count > out[j].Count
		}
		return out[i].Region < out[j].Region
	})
	return out
}

func sides(ally observe.Team) (ourAnc, theirAnc, ourTor, theirTor worldmap.LandmarkID) {
	if ally == observe.TeamDire {
		return worldmap.LandmarkDireAncient, worldmap.LandmarkRadiantAncient,
			worldmap.LandmarkDireTormentor, worldmap.LandmarkRadiantTormentor
	}
	return worldmap.LandmarkRadiantAncient, worldmap.LandmarkDireAncient,
		worldmap.LandmarkRadiantTormentor, worldmap.LandmarkDireTormentor
}

func objectiveNear(g *worldmap.Geometry, id worldmap.LandmarkID, radius float64, allies, enemies []HeroSpatial) ObjectiveProximity {
	out := ObjectiveProximity{Landmark: id}
	if g == nil || radius <= 0 {
		return out
	}
	for _, a := range allies {
		if heroNear(g, a.Positions, id, radius) {
			out.AlliesNear++
			out.AllyHeroes = append(out.AllyHeroes, a.Hero)
		}
	}
	for _, e := range enemies {
		if !e.Visible {
			continue
		}
		if heroNear(g, e.Positions, id, radius) {
			out.VisibleEnemiesNear++
			out.VisibleEnemyHeroes = append(out.VisibleEnemyHeroes, e.Hero)
		}
	}
	sort.Strings(out.AllyHeroes)
	sort.Strings(out.VisibleEnemyHeroes)
	out.VisibleEnemyDelta = out.AlliesNear - out.VisibleEnemiesNear
	return out
}

func heroNear(g *worldmap.Geometry, pts []worldmap.WorldPosition, id worldmap.LandmarkID, radius float64) bool {
	for _, p := range pts {
		if g.WithinRadius(p, id, radius) {
			return true
		}
	}
	return false
}

type namedPos struct {
	name string
	pos  worldmap.WorldPosition
}

func buildTeam(pts []namedPos, g *worldmap.Geometry) TeamSpatial {
	ts := TeamSpatial{ObservedCount: len(pts), CentroidRegion: worldmap.RegionUnknown}
	if len(pts) == 0 {
		return ts
	}
	cluster := largestCluster(pts, g.Radii().Grouped)
	ts.GroupedCount = len(cluster)
	if ts.ObservedCount > 0 {
		ts.GroupedRatio = float64(ts.GroupedCount) / float64(ts.ObservedCount)
	}
	if g != nil {
		ts.AllyGrouped = ts.GroupedRatio >= g.GroupedRatioThreshold()
	}
	c := centroid(cluster)
	ts.Centroid = &c
	if g != nil {
		ts.CentroidRegion = g.RegionAt(c)
	} else {
		ts.CentroidRegion = worldmap.RegionOther
	}
	if len(cluster) >= 2 {
		var sum float64
		for _, p := range cluster {
			sum += worldmap.Distance(p.pos, c)
		}
		ts.Spread = sum / float64(len(cluster))
	}
	return ts
}

func centroid(pts []namedPos) worldmap.WorldPosition {
	var s worldmap.WorldPosition
	for _, p := range pts {
		s.X += p.pos.X
		s.Y += p.pos.Y
	}
	n := float64(len(pts))
	return worldmap.WorldPosition{X: s.X / n, Y: s.Y / n}
}

func largestCluster(pts []namedPos, r float64) []namedPos {
	n := len(pts)
	if n <= 1 {
		return append([]namedPos(nil), pts...)
	}
	adj := make([][]int, n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if worldmap.Distance(pts[i].pos, pts[j].pos) <= r {
				adj[i] = append(adj[i], j)
				adj[j] = append(adj[j], i)
			}
		}
	}
	seen := make([]bool, n)
	best := []int{}
	bestKey := ""
	for i := 0; i < n; i++ {
		if seen[i] {
			continue
		}
		comp := dfsComp(i, adj, seen)
		key := componentKey(pts, comp)
		if len(comp) > len(best) || (len(comp) == len(best) && key < bestKey) {
			best = comp
			bestKey = key
		}
	}
	out := make([]namedPos, len(best))
	for i, idx := range best {
		out[i] = pts[idx]
	}
	return out
}

func dfsComp(start int, adj [][]int, seen []bool) []int {
	stack := []int{start}
	seen[start] = true
	var comp []int
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		comp = append(comp, v)
		for _, w := range adj[v] {
			if !seen[w] {
				seen[w] = true
				stack = append(stack, w)
			}
		}
	}
	sort.Ints(comp)
	return comp
}

func componentKey(pts []namedPos, comp []int) string {
	names := make([]string, len(comp))
	for i, idx := range comp {
		names[i] = pts[idx].name
	}
	sort.Strings(names)
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "\x00"
		}
		out += n
	}
	return out
}
