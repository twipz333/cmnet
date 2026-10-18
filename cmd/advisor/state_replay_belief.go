package main

import (
	"fmt"
	"sort"
	"strings"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/belief"
	"cmnet/internal/gsi/mapstate"
	"cmnet/internal/gsi/track"
)

type beliefReplayCollector struct {
	geo       *worldmap.Geometry
	graph     *worldmap.RegionGraph
	cfg       belief.Config
	traceHero string

	lastTrace   map[string]traceCheckpoint
	traceRows   []beliefTraceRow
	prevBeliefs map[string]belief.EnemyBelief
	lastVisible map[string]visibleCheckpoint

	transitionCounts      map[string]int
	observedTransitions   int
	transitionsWithPath   int
	travelTimeAnomalies   []transitionAnomaly
	reappearanceEvents    int
	walkingEnvelopeHits   int
	walkingEnvelopeMisses []reappearanceMiss
	unlocalizedIntervals  int
	heuristic             endpointMetrics
	lastSeenOnly          endpointMetrics
	uniformRegions        endpointMetrics
}

type traceCheckpoint struct {
	Status belief.Status
	Bucket int
	TopKey string
}

type beliefTraceRow struct {
	Seq      uint64
	GameTime string
	Body     string
}

type visibleCheckpoint struct {
	Region   worldmap.RegionID
	GameTime int
}

type transitionAnomaly struct {
	Hero        string
	From        worldmap.RegionID
	To          worldmap.RegionID
	ObservedDT  float64
	MinWalkTime float64
	Seq         uint64
}

type endpointMetrics struct {
	Name   string
	Top1   int
	Top3   int
	Top5   int
	Masses []float64
}

type reappearanceMiss struct {
	Hero       string
	LastSeen   []worldmap.RegionID
	Age        float64
	Reappear   []worldmap.RegionID
	TopRegions []belief.RegionMass
}

func newBeliefReplayCollector(geo *worldmap.Geometry, graph *worldmap.RegionGraph, cfg belief.Config, traceHero string) *beliefReplayCollector {
	return &beliefReplayCollector{
		geo:              geo,
		graph:            graph,
		cfg:              cfg,
		traceHero:        traceHero,
		lastTrace:        map[string]traceCheckpoint{},
		prevBeliefs:      map[string]belief.EnemyBelief{},
		lastVisible:      map[string]visibleCheckpoint{},
		transitionCounts: map[string]int{},
		heuristic:        endpointMetrics{Name: "HeuristicBeliefV0"},
		lastSeenOnly:     endpointMetrics{Name: "LastSeenOnly"},
		uniformRegions:   endpointMetrics{Name: "UniformRegions"},
	}
}

func (c *beliefReplayCollector) OnTick(seq uint64, st track.State) {
	if c == nil || c.geo == nil || c.graph == nil {
		return
	}
	ms := mapstate.Build(st, c.geo)
	bs, err := belief.Build(ms, c.graph, c.cfg)
	if err != nil {
		return
	}
	instant, _ := st.Clock.Instant()
	byHeroSpatial := map[string]mapstate.HeroSpatial{}
	for _, enemy := range ms.Enemies {
		byHeroSpatial[enemy.Hero] = enemy
	}
	for _, enemy := range bs.Enemies {
		spatial := byHeroSpatial[enemy.Hero]
		c.noteTrace(seq, st.Clock.GameTime, enemy)
		c.noteTransition(seq, instant, enemy)
		c.noteReappearance(enemy)
		c.prevBeliefs[enemy.Hero] = enemy
		_ = spatial
	}
}

func (c *beliefReplayCollector) noteTrace(seq uint64, gameTime *int, enemy belief.EnemyBelief) {
	if c.traceHero == "" || enemy.Hero != c.traceHero {
		return
	}
	bucket := -1
	if enemy.Status == belief.StatusMissing {
		bucket = ageBucket(enemy.MissingAge)
	}
	topKey := topKey(enemy.TopRegionsN(3))
	prev, ok := c.lastTrace[enemy.Hero]
	if ok && prev.Status == enemy.Status && prev.Bucket == bucket && prev.TopKey == topKey {
		return
	}
	gt := "-"
	if gameTime != nil {
		gt = fmt.Sprintf("%d", *gameTime)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "%s %s\n", displayHero(enemy.Hero), enemy.Status)
	switch enemy.Status {
	case belief.StatusObserved:
		region := "UNKNOWN"
		if len(enemy.TopRegions) > 0 {
			region = string(enemy.TopRegions[0].Region)
		}
		fmt.Fprintf(&body, "region=%s", region)
	case belief.StatusObservedAmbiguous:
		fmt.Fprintf(&body, "regions=%s", strings.Join(regionIDs(enemy.CurrentRegions), ","))
	case belief.StatusUnlocalized:
		fmt.Fprintf(&body, "reason=%s\nunmodeled=%.2f", enemy.Reason, enemy.UnmodeledMass)
	default:
		last := "UNKNOWN"
		if len(enemy.LastSeenRegions) > 0 {
			last = strings.Join(regionIDs(enemy.LastSeenRegions), ",")
		}
		fmt.Fprintf(&body, "last=%s\nage=%.1f\nunmodeled=%.2f\nentropy=%.2f", last, enemy.MissingAge, enemy.UnmodeledMass, enemy.Entropy)
		if top := enemy.TopRegionsN(3); len(top) > 0 {
			body.WriteString("\ntop:")
			for _, rm := range top {
				fmt.Fprintf(&body, "\n  %s %.2f", rm.Region, rm.Mass)
			}
		}
	}
	c.traceRows = append(c.traceRows, beliefTraceRow{Seq: seq, GameTime: gt, Body: body.String()})
	c.lastTrace[enemy.Hero] = traceCheckpoint{Status: enemy.Status, Bucket: bucket, TopKey: topKey}
}

func (c *beliefReplayCollector) noteTransition(seq uint64, instant int, enemy belief.EnemyBelief) {
	if enemy.Status != belief.StatusObserved || len(enemy.CurrentRegions) != 1 {
		return
	}
	region := enemy.CurrentRegions[0]
	prev, ok := c.lastVisible[enemy.Hero]
	c.lastVisible[enemy.Hero] = visibleCheckpoint{Region: region, GameTime: instant}
	if !ok || prev.Region == region {
		return
	}
	c.observedTransitions++
	key := string(prev.Region) + " -> " + string(region)
	c.transitionCounts[key]++
	dist, pathOK := c.graph.ShortestWalkDistance(prev.Region, region)
	if pathOK {
		c.transitionsWithPath++
		minWalk := dist / c.cfg.ReferenceWalkSpeed
		observed := float64(instant - prev.GameTime)
		if observed < 0 {
			observed = 0
		}
		if minWalk > observed+c.cfg.ReachabilitySlack+3 {
			c.travelTimeAnomalies = append(c.travelTimeAnomalies, transitionAnomaly{
				Hero: enemy.Hero, From: prev.Region, To: region, ObservedDT: observed, MinWalkTime: minWalk, Seq: seq,
			})
		}
	}
}

func (c *beliefReplayCollector) noteReappearance(current belief.EnemyBelief) {
	prev, ok := c.prevBeliefs[current.Hero]
	if !ok {
		return
	}
	if prev.Status != belief.StatusMissing && prev.Status != belief.StatusUnlocalized {
		return
	}
	if current.Status != belief.StatusObserved && current.Status != belief.StatusObservedAmbiguous {
		return
	}
	c.reappearanceEvents++
	reapp := usableRegionsForEval(current.CurrentRegions, c.graph)
	if len(reapp) == 0 {
		return
	}
	if prev.Status == belief.StatusUnlocalized {
		c.unlocalizedIntervals++
	}
	mass := 0.0
	for _, region := range reapp {
		if prev.RegionMass[region] > mass {
			mass = prev.RegionMass[region]
		}
	}
	c.heuristic.Masses = append(c.heuristic.Masses, mass)
	if containsTop(prev.TopRegionsN(1), reapp) {
		c.heuristic.Top1++
	}
	if containsTop(prev.TopRegionsN(3), reapp) {
		c.heuristic.Top3++
	}
	if containsTop(prev.TopRegionsN(5), reapp) {
		c.heuristic.Top5++
	}
	if withinReachability(prev.WalkingReachability, reapp) {
		c.walkingEnvelopeHits++
	} else if len(c.walkingEnvelopeMisses) < 5 {
		c.walkingEnvelopeMisses = append(c.walkingEnvelopeMisses, reappearanceMiss{
			Hero:       current.Hero,
			LastSeen:   append([]worldmap.RegionID(nil), prev.LastSeenRegions...),
			Age:        prev.MissingAge,
			Reappear:   append([]worldmap.RegionID(nil), reapp...),
			TopRegions: append([]belief.RegionMass(nil), prev.TopRegionsN(3)...),
		})
	}

	lastSeenMass := lastSeenOnlyMass(prev.LastSeenRegions, reapp, c.graph)
	c.lastSeenOnly.Masses = append(c.lastSeenOnly.Masses, lastSeenMass)
	if lastSeenTopHit(prev.LastSeenRegions, reapp, c.graph, 1) {
		c.lastSeenOnly.Top1++
	}
	if lastSeenTopHit(prev.LastSeenRegions, reapp, c.graph, 3) {
		c.lastSeenOnly.Top3++
	}
	if lastSeenTopHit(prev.LastSeenRegions, reapp, c.graph, 5) {
		c.lastSeenOnly.Top5++
	}

	uniformMass := 1 / float64(len(c.graph.Nodes()))
	c.uniformRegions.Masses = append(c.uniformRegions.Masses, uniformMass)
	if uniformTopHit(c.graph.Nodes(), reapp, 1) {
		c.uniformRegions.Top1++
	}
	if uniformTopHit(c.graph.Nodes(), reapp, 3) {
		c.uniformRegions.Top3++
	}
	if uniformTopHit(c.graph.Nodes(), reapp, 5) {
		c.uniformRegions.Top5++
	}
}

func (c *beliefReplayCollector) Summary() string {
	if c == nil || c.graph == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nRegionGraph:\n")
	fmt.Fprintf(&b, "  version=%s\n", c.graph.Version())
	fmt.Fprintf(&b, "  map=%s\n", c.graph.MapGeometryVersion())
	fmt.Fprintf(&b, "  nodes=%d\n", len(c.graph.Nodes()))
	fmt.Fprintf(&b, "  edges=%d\n", len(c.graph.Edges()))
	fmt.Fprintf(&b, "  connected_components=%d\n", len(c.graph.ConnectedComponents()))
	fmt.Fprintf(&b, "  observed_region_transitions=%d\n", c.observedTransitions)
	fmt.Fprintf(&b, "  transitions_with_valid_graph_path=%d\n", c.transitionsWithPath)
	if len(c.transitionCounts) > 0 {
		fmt.Fprintf(&b, "  top_observed_transitions:\n")
		for _, row := range sortedTransitionCounts(c.transitionCounts, 5) {
			fmt.Fprintf(&b, "    %s (%d)\n", row.Key, row.Count)
		}
	}
	fmt.Fprintf(&b, "  travel_time_anomalies=%d\n", len(c.travelTimeAnomalies))
	for _, an := range topAnomalies(c.travelTimeAnomalies, 5) {
		fmt.Fprintf(&b, "    %s %s -> %s observed=%.1fs min_walk=%.1fs seq=%d\n",
			displayHero(an.Hero), an.From, an.To, an.ObservedDT, an.MinWalkTime, an.Seq)
	}

	fmt.Fprintf(&b, "\nReappearance evaluation:\n")
	fmt.Fprintf(&b, "  Evaluated missing->reappear intervals: %d\n", c.reappearanceEvents)
	writeMetrics(&b, c.heuristic, c.reappearanceEvents)
	writeMetrics(&b, c.lastSeenOnly, c.reappearanceEvents)
	writeMetrics(&b, c.uniformRegions, c.reappearanceEvents)
	fmt.Fprintf(&b, "  Walking envelope hit: %d/%d (%.1f%%)\n", c.walkingEnvelopeHits, c.reappearanceEvents, pct(c.walkingEnvelopeHits, c.reappearanceEvents))
	fmt.Fprintf(&b, "  Unlocalized: %d\n", c.unlocalizedIntervals)
	return b.String()
}

func (c *beliefReplayCollector) Trace() string {
	if len(c.traceRows) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nBelief trace %s (%d rows):\n", c.traceHero, len(c.traceRows))
	for _, row := range c.traceRows {
		fmt.Fprintf(&b, "seq=%d gt=%s\n%s\n\n", row.Seq, row.GameTime, row.Body)
	}
	return b.String()
}

type countRow struct {
	Key   string
	Count int
}

func sortedTransitionCounts(m map[string]int, topN int) []countRow {
	rows := make([]countRow, 0, len(m))
	for k, v := range m {
		rows = append(rows, countRow{Key: k, Count: v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Key < rows[j].Key
	})
	if topN > 0 && len(rows) > topN {
		rows = rows[:topN]
	}
	return rows
}

func topAnomalies(in []transitionAnomaly, topN int) []transitionAnomaly {
	out := append([]transitionAnomaly(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		di := out[i].MinWalkTime - out[i].ObservedDT
		dj := out[j].MinWalkTime - out[j].ObservedDT
		if di != dj {
			return di > dj
		}
		if out[i].Hero != out[j].Hero {
			return out[i].Hero < out[j].Hero
		}
		return out[i].Seq < out[j].Seq
	})
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out
}

func writeMetrics(b *strings.Builder, m endpointMetrics, n int) {
	fmt.Fprintf(b, "  %s\n", m.Name)
	fmt.Fprintf(b, "    Top-1: %d/%d (%.1f%%)\n", m.Top1, n, pct(m.Top1, n))
	fmt.Fprintf(b, "    Top-3: %d/%d (%.1f%%)\n", m.Top3, n, pct(m.Top3, n))
	fmt.Fprintf(b, "    Top-5: %d/%d (%.1f%%)\n", m.Top5, n, pct(m.Top5, n))
	fmt.Fprintf(b, "    Mean mass on reappearance region: %.3f\n", mean(m.Masses))
	fmt.Fprintf(b, "    Median mass on reappearance region: %.3f\n", median(m.Masses))
}

func pct(x, n int) float64 {
	if n == 0 {
		return 0
	}
	return 100 * float64(x) / float64(n)
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func containsTop(top []belief.RegionMass, regions []worldmap.RegionID) bool {
	for _, rm := range top {
		for _, region := range regions {
			if rm.Region == region {
				return true
			}
		}
	}
	return false
}

func usableRegionsForEval(in []worldmap.RegionID, graph *worldmap.RegionGraph) []worldmap.RegionID {
	out := make([]worldmap.RegionID, 0, len(in))
	for _, region := range in {
		if _, ok := graph.ShortestWalkDistance(region, region); ok {
			out = append(out, region)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func withinReachability(in []belief.RegionReachability, regions []worldmap.RegionID) bool {
	for _, rr := range in {
		if !rr.ReachableByWalkV0 {
			continue
		}
		for _, region := range regions {
			if rr.Region == region {
				return true
			}
		}
	}
	return false
}

func lastSeenOnlyMass(lastSeen, reapp []worldmap.RegionID, graph *worldmap.RegionGraph) float64 {
	valid := usableRegionsForEval(lastSeen, graph)
	if len(valid) == 0 {
		return 0
	}
	share := 1 / float64(len(valid))
	for _, region := range reapp {
		for _, seed := range valid {
			if region == seed {
				return share
			}
		}
	}
	return 0
}

func lastSeenTopHit(lastSeen, reapp []worldmap.RegionID, graph *worldmap.RegionGraph, topN int) bool {
	valid := usableRegionsForEval(lastSeen, graph)
	if topN > 0 && len(valid) > topN {
		valid = valid[:topN]
	}
	for _, region := range valid {
		for _, target := range reapp {
			if region == target {
				return true
			}
		}
	}
	return false
}

func uniformTopHit(nodes, reapp []worldmap.RegionID, topN int) bool {
	if topN > 0 && len(nodes) > topN {
		nodes = nodes[:topN]
	}
	for _, region := range nodes {
		for _, target := range reapp {
			if region == target {
				return true
			}
		}
	}
	return false
}

func ageBucket(age float64) int {
	thresholds := []float64{0, 2, 5, 10, 20, 30, 60, 90, 120}
	bucket := len(thresholds)
	for i, th := range thresholds {
		if age <= th {
			bucket = i
			break
		}
	}
	return bucket
}

func topKey(top []belief.RegionMass) string {
	parts := make([]string, len(top))
	for i, rm := range top {
		parts[i] = fmt.Sprintf("%s=%.3f", rm.Region, rm.Mass)
	}
	return strings.Join(parts, "|")
}

func regionIDs(in []worldmap.RegionID) []string {
	out := make([]string, len(in))
	for i, region := range in {
		out[i] = string(region)
	}
	return out
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

func topRegionsString(in []belief.RegionMass) string {
	parts := make([]string, len(in))
	for i, rm := range in {
		parts[i] = fmt.Sprintf("%s=%.2f", rm.Region, rm.Mass)
	}
	return strings.Join(parts, ",")
}
