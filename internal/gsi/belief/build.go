package belief

import (
	"fmt"
	"math"
	"sort"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/mapstate"
)

func Build(ms mapstate.State, graph *worldmap.RegionGraph, cfg Config) (State, error) {
	if err := cfg.Validate(); err != nil {
		return State{Stats: BuildStats{BeliefErrors: 1}}, err
	}
	if graph == nil {
		return State{Stats: BuildStats{BeliefErrors: 1}}, fmt.Errorf("belief: region graph is nil")
	}
	if ms.GeometryVersion != "" && graph.MapGeometryVersion() != "" && ms.GeometryVersion != graph.MapGeometryVersion() {
		return State{Stats: BuildStats{BeliefErrors: 1}}, fmt.Errorf("belief: map geometry version %s does not match region graph map version %s", ms.GeometryVersion, graph.MapGeometryVersion())
	}
	out := State{
		ModelVersion:    ModelVersion,
		GraphVersion:    graph.Version(),
		GraphMapVersion: graph.MapGeometryVersion(),
		Enemies:         make([]EnemyBelief, 0, len(ms.Enemies)),
		Stats:           BuildStats{BeliefBuilds: 1},
	}
	for _, hero := range ms.Enemies {
		eb := buildEnemy(hero, graph, cfg)
		out.Enemies = append(out.Enemies, eb)
		switch eb.Status {
		case StatusObserved, StatusObservedAmbiguous:
			out.Stats.ObservedEnemies++
		case StatusMissing:
			out.Stats.MissingEnemies++
			if len(usableGraphRegions(eb.LastSeenRegions, graph)) > 1 {
				out.Stats.MultiSeedBeliefs++
			}
		case StatusUnlocalized:
			out.Stats.UnlocalizedEnemies++
		}
	}
	return out, nil
}

func buildEnemy(hero mapstate.HeroSpatial, graph *worldmap.RegionGraph, cfg Config) EnemyBelief {
	eb := EnemyBelief{
		Hero:            hero.Hero,
		Visible:         hero.Visible,
		CurrentRegions:  uniqueSortedRegions(hero.Regions),
		LastSeenRegions: uniqueSortedRegions(hero.LastSeenRegions),
		ModelKind:       ModelKindHeuristicV0,
		RegionMass:      map[worldmap.RegionID]float64{},
	}
	if hero.LastSeenAgeSeconds != nil {
		eb.MissingAge = *hero.LastSeenAgeSeconds
	}

	if hero.Visible {
		currentValid, currentInvalid := splitRegions(eb.CurrentRegions, graph)
		if len(currentValid) == 0 {
			eb.Status = StatusUnlocalized
			eb.UnmodeledMass = 1
			eb.Reason = "no usable current region"
			eb.Entropy = 0
			return eb
		}
		eb.WalkingReachability = buildReachability(currentValid, 0, graph, cfg)
		if len(currentValid) == 1 && currentInvalid == 0 {
			eb.Status = StatusObserved
		} else {
			eb.Status = StatusObservedAmbiguous
		}
		total := len(currentValid) + currentInvalid
		invalidShare := 0.0
		if total > 0 {
			invalidShare = float64(currentInvalid) / float64(total)
		}
		eb.UnmodeledMass = invalidShare
		remaining := 1 - eb.UnmodeledMass
		share := remaining / float64(len(currentValid))
		for _, region := range currentValid {
			eb.RegionMass[region] = share
		}
		eb.TopRegions = sortMasses(eb.RegionMass, cfg.TopRegions)
		eb.Entropy = normalizedEntropy(eb.RegionMass, eb.UnmodeledMass)
		return eb
	}

	seeds, invalid := splitRegions(eb.LastSeenRegions, graph)
	if len(seeds) == 0 {
		eb.Status = StatusUnlocalized
		eb.UnmodeledMass = 1
		eb.Reason = "no usable last-seen region"
		eb.Entropy = 0
		return eb
	}
	eb.Status = StatusMissing
	eb.WalkingReachability = buildReachability(seeds, eb.MissingAge, graph, cfg)
	invalidShare := float64(invalid) / float64(len(seeds)+invalid)
	eb.UnmodeledMass = cfg.unmodeledMass(eb.MissingAge, invalidShare)
	remaining := 1 - eb.UnmodeledMass
	if remaining < 1e-12 {
		eb.UnmodeledMass = 1
		eb.Entropy = 0
		return eb
	}
	scale := math.Max(eb.MissingAge, cfg.TimeFloorSeconds)
	seedWeight := 1 / float64(len(seeds))
	raw := map[worldmap.RegionID]float64{}
	for _, target := range graph.Nodes() {
		score := 0.0
		for _, seed := range seeds {
			dist, ok := graph.ShortestWalkDistance(seed, target)
			if !ok {
				continue
			}
			travel := cfg.referenceTravelSeconds(dist)
			term := seedWeight * math.Exp(-travel/scale)
			score += term
		}
		if score > 0 && !math.IsNaN(score) && !math.IsInf(score, 0) {
			raw[target] = score
		}
	}
	normalizeMasses(eb.RegionMass, raw, remaining, seeds)
	eb.TopRegions = sortMasses(eb.RegionMass, cfg.TopRegions)
	eb.Entropy = normalizedEntropy(eb.RegionMass, eb.UnmodeledMass)
	return eb
}

func splitRegions(regions []worldmap.RegionID, graph *worldmap.RegionGraph) ([]worldmap.RegionID, int) {
	valid := make([]worldmap.RegionID, 0, len(regions))
	invalid := 0
	for _, region := range regions {
		if _, ok := graph.ShortestWalkDistance(region, region); ok {
			valid = append(valid, region)
			continue
		}
		invalid++
	}
	return valid, invalid
}

func usableGraphRegions(regions []worldmap.RegionID, graph *worldmap.RegionGraph) []worldmap.RegionID {
	valid, _ := splitRegions(regions, graph)
	return valid
}

func buildReachability(seeds []worldmap.RegionID, age float64, graph *worldmap.RegionGraph, cfg Config) []RegionReachability {
	out := make([]RegionReachability, 0, len(graph.Nodes()))
	for _, region := range graph.Nodes() {
		bestDist := math.Inf(1)
		for _, seed := range seeds {
			dist, ok := graph.ShortestWalkDistance(seed, region)
			if ok && dist < bestDist {
				bestDist = dist
			}
		}
		if math.IsInf(bestDist, 1) {
			continue
		}
		travel := cfg.reachabilityTravelSeconds(bestDist)
		out = append(out, RegionReachability{
			Region:            region,
			MinWalkDistance:   bestDist,
			MinWalkTime:       travel,
			ReachableByWalkV0: travel <= age+cfg.ReachabilitySlack,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if math.Abs(out[i].MinWalkTime-out[j].MinWalkTime) > 1e-9 {
			return out[i].MinWalkTime < out[j].MinWalkTime
		}
		return out[i].Region < out[j].Region
	})
	return out
}

func normalizeMasses(dst, raw map[worldmap.RegionID]float64, totalMass float64, seeds []worldmap.RegionID) {
	sum := 0.0
	for _, region := range sortedMassKeys(raw) {
		sum += raw[region]
	}
	if sum <= 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
		share := totalMass / float64(len(seeds))
		for _, seed := range seeds {
			dst[seed] = share
		}
		return
	}
	for _, region := range sortedMassKeys(raw) {
		score := raw[region]
		mass := totalMass * score / sum
		if mass <= 0 || math.IsNaN(mass) || math.IsInf(mass, 0) {
			continue
		}
		dst[region] = mass
	}
}

func sortedMassKeys(m map[worldmap.RegionID]float64) []worldmap.RegionID {
	keys := make([]worldmap.RegionID, 0, len(m))
	for region := range m {
		keys = append(keys, region)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func sortMasses(masses map[worldmap.RegionID]float64, topN int) []RegionMass {
	out := make([]RegionMass, 0, len(masses))
	for _, region := range sortedMassKeys(masses) {
		mass := masses[region]
		if mass <= 0 {
			continue
		}
		out = append(out, RegionMass{Region: region, Mass: mass})
	}
	sort.Slice(out, func(i, j int) bool {
		if math.Abs(out[i].Mass-out[j].Mass) > 1e-12 {
			return out[i].Mass > out[j].Mass
		}
		return out[i].Region < out[j].Region
	})
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out
}

func uniqueSortedRegions(in []worldmap.RegionID) []worldmap.RegionID {
	seen := map[worldmap.RegionID]struct{}{}
	out := make([]worldmap.RegionID, 0, len(in))
	for _, region := range in {
		if _, ok := seen[region]; ok {
			continue
		}
		seen[region] = struct{}{}
		out = append(out, region)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizedEntropy(masses map[worldmap.RegionID]float64, unmodeled float64) float64 {
	var buckets []float64
	for _, region := range sortedMassKeys(masses) {
		mass := masses[region]
		if mass > 0 {
			buckets = append(buckets, mass)
		}
	}
	if unmodeled > 0 {
		buckets = append(buckets, unmodeled)
	}
	if len(buckets) <= 1 {
		return 0
	}
	h := 0.0
	for _, mass := range buckets {
		h -= mass * math.Log(mass)
	}
	denom := math.Log(float64(len(buckets)))
	if denom <= 0 || math.IsNaN(h) || math.IsInf(h, 0) {
		return 0
	}
	return clamp01(h / denom)
}

func clamp01(v float64) float64 {
	switch {
	case math.IsNaN(v), math.IsInf(v, 0), v <= 0:
		return 0
	case v >= 1:
		return 1
	default:
		return v
	}
}
