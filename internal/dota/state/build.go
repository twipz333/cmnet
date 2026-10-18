package state

import (
	"fmt"
	"sort"
	"strings"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/belief"
	"cmnet/internal/gsi/lanestate"
	"cmnet/internal/gsi/mapstate"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/track"
)

func Build(ts track.State, ms *mapstate.State, ls *lanestate.State, bs *belief.State) (State, error) {
	out := State{}
	out.Meta = buildMeta(ts, ms, ls, bs)
	out.Match = buildMatch(ts)
	out.Self = buildSelf(ts, ms)
	out.Team = buildTeam(ts, ms, bs)
	out.Allies = buildAllies(ts, ms)
	out.Enemies = buildEnemies(ts, bs)
	out.Lanes = buildLanes(ls)
	out.Structures = buildStructures(ts)
	out.Objectives = buildObjectives(ts, ms)
	out.RecentEvents = buildRecentEvents(ts)
	out.Quality = buildQuality(ts, ms, ls, bs, out.Team)
	return out, nil
}

func buildMeta(ts track.State, ms *mapstate.State, ls *lanestate.State, bs *belief.State) Meta {
	meta := Meta{SchemaVersion: SchemaVersion, SourceSeq: ts.Clock.Seq}
	meta.MatchID = optStringPtr(ptrString(ts.Clock.MatchID), SourceHistory)
	meta.GameTime = optIntPtr(ts.Clock.GameTime, SourceObserved)
	meta.ClockTime = optIntPtr(ts.Clock.ClockTime, SourceObserved)
	meta.GeneratedAtGameTime = meta.GameTime
	if side := sideString(ts.Roster.AllySide); side != "" {
		meta.OurSide = optString(side, SourceHistory)
	}
	if ms != nil {
		meta.MapGeometryVersion = ms.GeometryVersion
	}
	if ls != nil {
		meta.LaneGeometryVersion = ls.GeometryVersion
	}
	if bs != nil {
		meta.RegionGraphVersion = bs.GraphVersion
		meta.BeliefModelVersion = bs.ModelVersion
		if meta.MapGeometryVersion == "" {
			meta.MapGeometryVersion = bs.GraphMapVersion
		}
	}
	return meta
}

func buildMatch(ts track.State) Match {
	m := Match{Phase: PhaseUnknown}
	m.GameState = optStringPtr(ptrString(ts.Clock.GameState), SourceObserved)
	m.RadiantScore = optInt(ts.Clock.RadiantScore, SourceObserved)
	m.DireScore = optInt(ts.Clock.DireScore, SourceObserved)
	if ts.Clock.GameTime != nil {
		m.Phase = phaseFor(*ts.Clock.GameTime)
	}
	m.Paused = optBool(ts.Clock.Paused, SourceObserved)
	return m
}

func buildSelf(ts track.State, ms *mapstate.State) Self {
	out := Self{}
	self := ts.Self.SelfObservation
	if self.HeroName != nil {
		out.Hero = optString(*self.HeroName, SourceObserved)
	}
	if ms != nil && ms.Self.PrimaryRegion != nil {
		out.Region = optRegion(*ms.Self.PrimaryRegion, SourceDerived)
	}
	out.Alive = optBoolPtr(self.Alive, SourceObserved)
	out.RespawnSeconds = optIntPtr(self.RespawnSeconds, SourceObserved)
	out.Level = optIntPtr(self.Level, SourceObserved)
	out.HPPct = optIntPtr(self.HPPercent, SourceObserved)
	out.ManaPct = optIntPtr(self.ManaPercent, SourceObserved)
	out.Gold = optIntPtr(self.Gold, SourceObserved)
	out.GPM = optIntPtr(self.GPM, SourceObserved)
	out.XPM = optIntPtr(self.XPM, SourceObserved)
	if self.BuybackCooldown != nil {
		avail := *self.BuybackCooldown == 0
		out.BuybackAvailable = optBool(avail, SourceObserved)
	}
	out.BuybackCooldown = optIntPtr(self.BuybackCooldown, SourceObserved)
	out.Smoked = optBoolPtr(self.Smoked, SourceObserved)
	out.Items = buildItems(self.Items)
	out.Abilities = buildAbilities(self.Abilities)
	return out
}

func buildItems(items observe.ItemSet) []ItemState {
	var out []ItemState
	appendItems := func(slotClass string, in []observe.ItemObservation) {
		for _, item := range in {
			out = append(out, ItemState{
				Name:      item.Name,
				SlotClass: slotClass,
				Ready:     readyFromCanCast(item.CanCast),
				Charges:   optIntPtr(item.Charges, SourceObserved),
				Cooldown:  cooldownFrom(item.Cooldown),
			})
		}
	}
	appendItems("inventory", items.Inventory)
	appendItems("backpack", items.Backpack)
	appendItems("stash", items.Stash)
	if items.TP != nil {
		appendItems("tp", []observe.ItemObservation{*items.TP})
	}
	appendItems("neutral", items.Neutral)
	return out
}

func buildAbilities(in []observe.AbilityObservation) []AbilityState {
	out := make([]AbilityState, 0, len(in))
	for _, ab := range in {
		out = append(out, AbilityState{
			Name:     ab.Name,
			Level:    optIntPtr(ab.Level, SourceObserved),
			Ready:    readyFromCanCast(ab.CanCast),
			Cooldown: cooldownFrom(ab.Cooldown),
			Ultimate: optBoolPtr(ab.Ultimate, SourceObserved),
		})
	}
	return out
}

func buildTeam(ts track.State, ms *mapstate.State, bs *belief.State) Team {
	out := Team{}
	if ms != nil {
		out.ObservedAllyCount = ms.AllyTeam.ObservedCount
		out.GroupedCount = ms.AllyTeam.GroupedCount
		out.GroupedRatio = optFloat(ms.AllyTeam.GroupedRatio, SourceDerived)
		out.AllyGrouped = optBool(ms.AllyTeam.AllyGrouped, SourceDerived)
		if ms.AllyTeam.Centroid != nil {
			out.MainGroupCentroid = OptionalPosition{X: ms.AllyTeam.Centroid.X, Y: ms.AllyTeam.Centroid.Y, Available: true, Source: SourceDerived}
		}
		if ms.AllyTeam.CentroidRegion != worldmap.RegionUnknown {
			out.MainGroupRegion = optRegion(ms.AllyTeam.CentroidRegion, SourceDerived)
		}
		out.Spread = optFloat(ms.AllyTeam.Spread, SourceDerived)
		out.VisibleEnemyCount = ms.VisibleEnemyCount
		out.MissingEnemyCount = ms.MissingEnemyCount
	}
	if bs != nil {
		var missAges, unmodeled []float64
		for _, enemy := range bs.Enemies {
			if enemy.Status == belief.StatusUnlocalized {
				out.UnlocalizedEnemies++
			}
			if enemy.Status == belief.StatusMissing {
				missAges = append(missAges, enemy.MissingAge)
				unmodeled = append(unmodeled, enemy.UnmodeledMass)
			}
		}
		out.MeanMissingAge = optFloatSliceMean(missAges, SourceBelief)
		out.MaxMissingAge = optFloatSliceMax(missAges, SourceBelief)
		out.MeanUnmodeledMass = optFloatSliceMean(unmodeled, SourceBelief)
		out.MaxUnmodeledMass = optFloatSliceMax(unmodeled, SourceBelief)
	}
	return out
}

func buildAllies(ts track.State, ms *mapstate.State) []AllyHero {
	if !ts.Roster.Valid() || ms == nil {
		return nil
	}
	byHero := map[string]mapstate.HeroSpatial{}
	for _, ally := range ms.Allies {
		byHero[ally.Hero] = ally
	}
	ordered := orderedAllies(ts)
	out := make([]AllyHero, 0, len(ordered))
	for _, hero := range ordered {
		spatial := byHero[hero]
		state := AllyHero{Hero: hero}
		if hero == ms.Self.Hero && ms.Self.Hero != "" {
			state.CurrentlyObserved = ms.Self.Visible
			state.Regions = append([]worldmap.RegionID(nil), ms.Self.Regions...)
			if ms.Self.PrimaryRegion != nil {
				state.PrimaryRegion = optRegion(*ms.Self.PrimaryRegion, SourceObserved)
			}
		} else {
			state.CurrentlyObserved = spatial.Visible
			state.Regions = append([]worldmap.RegionID(nil), spatial.Regions...)
			if spatial.PrimaryRegion != nil {
				state.PrimaryRegion = optRegion(*spatial.PrimaryRegion, SourceObserved)
			}
		}
		out = append(out, state)
	}
	return out
}

func orderedAllies(ts track.State) []string {
	var out []string
	selfHero := ""
	if ts.Self.HeroName != nil {
		selfHero = *ts.Self.HeroName
	}
	if selfHero != "" {
		out = append(out, selfHero)
	}
	for _, ref := range ts.Roster.Allies {
		if ref.UnitName == "" || ref.UnitName == selfHero {
			continue
		}
		out = append(out, ref.UnitName)
	}
	return out
}

func buildEnemies(ts track.State, bs *belief.State) []EnemyHero {
	if !ts.Roster.Valid() {
		return nil
	}
	beliefByHero := map[string]belief.EnemyBelief{}
	if bs != nil {
		for _, enemy := range bs.Enemies {
			beliefByHero[enemy.Hero] = enemy
		}
	}
	out := make([]EnemyHero, 0, len(ts.Roster.Enemies))
	for _, ref := range ts.Roster.Enemies {
		b := beliefByHero[ref.UnitName]
		e := EnemyHero{
			Hero:                ref.UnitName,
			Visible:             b.Visible,
			CurrentAmbiguous:    b.Visible && len(b.CurrentRegions) > 1,
			CurrentRegions:      append([]worldmap.RegionID(nil), b.CurrentRegions...),
			LastSeenRegions:     append([]worldmap.RegionID(nil), b.LastSeenRegions...),
			BeliefStatus:        mapBeliefStatus(b.Status),
			BeliefUnmodeledMass: b.UnmodeledMass,
		}
		if b.Status == belief.StatusMissing || b.Status == belief.StatusObserved || b.Status == belief.StatusObservedAmbiguous {
			e.BeliefEntropy = optFloat(b.Entropy, SourceBelief)
		}
		if len(b.LastSeenRegions) > 0 || b.MissingAge > 0 {
			e.LastSeenAge = optFloat(b.MissingAge, SourceHistory)
		}
		for _, rm := range b.TopRegionsN(len(b.TopRegions)) {
			e.BeliefRegionMass = append(e.BeliefRegionMass, BeliefMass{Region: rm.Region, Mass: rm.Mass})
		}
		for _, rr := range b.WalkingReachability {
			if rr.ReachableByWalkV0 {
				e.WalkingReachable = append(e.WalkingReachable, rr.Region)
			}
		}
		e.WalkingReachableCount = len(e.WalkingReachable)
		out = append(out, e)
	}
	return out
}

func buildLanes(ls *lanestate.State) Lanes {
	if ls == nil {
		return Lanes{}
	}
	return Lanes{Top: buildLane(ls.Top), Mid: buildLane(ls.Mid), Bot: buildLane(ls.Bot)}
}

func buildLane(v lanestate.LaneView) Lane {
	out := Lane{OurWaveCount: len(v.Our.Waves), VisibleEnemyWaveCount: len(v.Enemy.Visible)}
	out.OurFrontAvailable = v.Our.HasFront
	if v.Our.HasFront {
		out.OurFrontTeamProgress = optFloat(float64(v.Our.FrontTeam), SourceObserved)
		out.OurFrontDepth = optString(v.Our.FrontDepth.String(), SourceObserved)
		out.OurFrontCreepCount = optInt(v.Our.Frontmost.CreepCount, SourceObserved)
	}
	out.VisibleEnemyFrontAvailable = v.Enemy.HasVisibleFront
	if v.Enemy.HasVisibleFront {
		out.VisibleEnemyFrontProgress = optFloat(float64(v.Enemy.VisibleFrontTeam), SourceObserved)
		out.VisibleEnemyFrontDepth = optString(v.Enemy.VisibleFrontDepth.String(), SourceObserved)
		if v.Enemy.VisibleFrontmost != nil {
			out.VisibleEnemyFrontCreepCount = optInt(v.Enemy.VisibleFrontmost.CreepCount, SourceObserved)
		}
	}
	out.EnemyLastSeenFrontAvailable = v.Enemy.LastSeenFrontmost != nil
	if v.Enemy.LastSeenFrontmost != nil {
		out.EnemyLastSeenFrontProgress = optFloat(float64(v.Enemy.LastSeenFrontTeam), SourceHistory)
		out.EnemyLastSeenFrontAge = optFloatPtr(v.Enemy.LastSeenAgeSeconds, SourceHistory)
	}
	out.ObservedContact = optBool(v.ObservedContact, SourceDerived)
	if v.VisibleFrontPathGap != nil {
		out.FrontGap = optFloat(*v.VisibleFrontPathGap, SourceDerived)
	}
	return out
}

func buildStructures(ts track.State) Structures {
	out := Structures{}
	own := map[string]track.BuildingState{}
	for _, b := range ts.OwnBuildings {
		own[b.UnitName] = b
	}
	out.OurTop = ownLaneStructures(own, "top")
	out.OurMid = ownLaneStructures(own, "mid")
	out.OurBot = ownLaneStructures(own, "bot")
	enemy := map[string]track.StructureLife{}
	for _, s := range ts.EnemyStructures {
		enemy[s.UnitName] = s.Life
	}
	out.EnemyTop = enemyLaneStructures(enemy, "goodguys", "top")
	out.EnemyMid = enemyLaneStructures(enemy, "goodguys", "mid")
	out.EnemyBot = enemyLaneStructures(enemy, "goodguys", "bot")
	return out
}

func buildObjectives(ts track.State, ms *mapstate.State) Objectives {
	out := Objectives{}
	out.RoshanVisible = optBool(ts.Roshan.VisibleNow, SourceObserved)
	if age := ageSince(ts.Clock, ts.Roshan.LastSeenAt); age.Available {
		out.RoshanLastSeenAge = age
	}
	if ms != nil {
		out.PitA = RoshanPit{AlliesNear: ms.RoshanPitA.AlliesNear, VisibleEnemiesNear: ms.RoshanPitA.VisibleEnemiesNear}
		out.PitB = RoshanPit{AlliesNear: ms.RoshanPitB.AlliesNear, VisibleEnemiesNear: ms.RoshanPitB.VisibleEnemiesNear}
	}
	return out
}

func buildRecentEvents(ts track.State) RecentEvents {
	out := RecentEvents{}
	for i := len(ts.RecentEvents) - 1; i >= 0; i-- {
		ev := ts.RecentEvents[i]
		age := ageSince(ts.Clock, ev.GameTime)
		switch ev.Kind {
		case observe.EventHeroKill:
			if !out.RecentHeroKillAge.Available {
				out.RecentHeroKillAge = age
			}
		case observe.EventGlyphUsed:
			if !out.RecentGlyphAge.Available {
				out.RecentGlyphAge = age
			}
		}
	}
	if len(ts.RecentSmokes) > 0 {
		last := ts.RecentSmokes[len(ts.RecentSmokes)-1]
		out.RecentSmokeAge = ageSince(ts.Clock, last.GameTime)
	}
	return out
}

func buildQuality(ts track.State, ms *mapstate.State, ls *lanestate.State, bs *belief.State, team Team) Quality {
	return Quality{
		RosterReady:           ts.Roster.Valid(),
		MapStateAvailable:     ms != nil,
		LaneStateAvailable:    ls != nil,
		BeliefStateAvailable:  bs != nil,
		ObservedAllyCount:     team.ObservedAllyCount,
		VisibleEnemyCount:     team.VisibleEnemyCount,
		MissingEnemyCount:     team.MissingEnemyCount,
		UnlocalizedEnemyCount: team.UnlocalizedEnemies,
		GSIShardFresh:         ts.Clock.Seq > 0,
	}
}

func phaseFor(gameTime int) MatchPhase {
	switch {
	case gameTime < 0:
		return PhaseUnknown
	case gameTime < 12*60:
		return PhaseEarly
	case gameTime < 30*60:
		return PhaseMid
	default:
		return PhaseLate
	}
}

func mapBeliefStatus(in belief.Status) string {
	switch in {
	case belief.StatusObserved:
		return "OBSERVED"
	case belief.StatusObservedAmbiguous:
		return "OBSERVED_AMBIGUOUS"
	case belief.StatusMissing:
		return "MISSING_LOCALIZED"
	default:
		return "UNLOCALIZED"
	}
}

func ownLaneStructures(m map[string]track.BuildingState, lane string) OwnLaneStructures {
	prefix := "npc_dota_badguys_"
	return OwnLaneStructures{
		T1Alive:       ownAlive(m[prefix+"tower1_"+lane]),
		T2Alive:       ownAlive(m[prefix+"tower2_"+lane]),
		T3Alive:       ownAlive(m[prefix+"tower3_"+lane]),
		MeleeRaxAlive: ownAlive(m[prefix+"melee_rax_"+lane]),
		RangeRaxAlive: ownAlive(m[prefix+"range_rax_"+lane]),
	}
}

func ownAlive(b track.BuildingState) OptionalBool {
	if b.UnitName == "" || b.Alive == nil {
		return OptionalBool{}
	}
	return optBool(*b.Alive, SourceObserved)
}

func enemyLaneStructures(m map[string]track.StructureLife, teamPrefix, lane string) EnemyLaneStructures {
	prefix := "npc_dota_" + teamPrefix + "_"
	return EnemyLaneStructures{
		T1:       mapStructureKnowledge(m[prefix+"tower1_"+lane]),
		T2:       mapStructureKnowledge(m[prefix+"tower2_"+lane]),
		T3:       mapStructureKnowledge(m[prefix+"tower3_"+lane]),
		MeleeRax: mapStructureKnowledge(m[prefix+"melee_rax_"+lane]),
		RangeRax: mapStructureKnowledge(m[prefix+"range_rax_"+lane]),
	}
}

func mapStructureKnowledge(life track.StructureLife) StructureKnowledge {
	switch life {
	case track.StructureSeenAlive:
		return StructureKnowledgeSeenAlive
	case track.StructureLikelyDestroyed:
		return StructureKnowledgeLikelyDestroyed
	case track.StructureConfirmedDestroyed:
		return StructureKnowledgeConfirmedDestroyed
	default:
		return StructureKnowledgeUnknown
	}
}

func ageSince(clock track.Clock, seenAt *int) OptionalFloat {
	if seenAt == nil {
		return OptionalFloat{}
	}
	now, ok := clock.Instant()
	if !ok {
		return OptionalFloat{}
	}
	age := now - *seenAt
	if age < 0 {
		age = 0
	}
	return optFloat(float64(age), SourceHistory)
}

func optInt(v int, src SourceKind) OptionalInt {
	return OptionalInt{Value: v, Available: true, Source: src}
}
func optIntPtr(v *int, src SourceKind) OptionalInt {
	if v == nil {
		return OptionalInt{}
	}
	return optInt(*v, src)
}
func optFloat(v float64, src SourceKind) OptionalFloat {
	return OptionalFloat{Value: v, Available: true, Source: src}
}
func optFloatPtr(v *float64, src SourceKind) OptionalFloat {
	if v == nil {
		return OptionalFloat{}
	}
	return optFloat(*v, src)
}
func optBool(v bool, src SourceKind) OptionalBool {
	return OptionalBool{Value: v, Available: true, Source: src}
}
func optBoolPtr(v *bool, src SourceKind) OptionalBool {
	if v == nil {
		return OptionalBool{}
	}
	return optBool(*v, src)
}
func optString(v string, src SourceKind) OptionalString {
	return OptionalString{Value: v, Available: v != "", Source: src}
}
func optStringPtr(v *string, src SourceKind) OptionalString {
	if v == nil || *v == "" {
		return OptionalString{}
	}
	return optString(*v, src)
}
func optRegion(v worldmap.RegionID, src SourceKind) OptionalRegion {
	return OptionalRegion{Value: v, Available: v != "", Source: src}
}
func optFloatSliceMean(in []float64, src SourceKind) OptionalFloat {
	if len(in) == 0 {
		return OptionalFloat{}
	}
	sum := 0.0
	for _, v := range in {
		sum += v
	}
	return optFloat(sum/float64(len(in)), src)
}
func optFloatSliceMax(in []float64, src SourceKind) OptionalFloat {
	if len(in) == 0 {
		return OptionalFloat{}
	}
	mx := in[0]
	for _, v := range in[1:] {
		if v > mx {
			mx = v
		}
	}
	return optFloat(mx, src)
}

func sideString(team observe.Team) string {
	switch team {
	case observe.TeamRadiant:
		return "radiant"
	case observe.TeamDire:
		return "dire"
	default:
		return ""
	}
}

func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func cooldownFrom(v *float64) OptionalFloat {
	if v == nil {
		return OptionalFloat{}
	}
	return optFloat(*v, SourceObserved)
}

func readyFromCanCast(v *bool) OptionalBool {
	if v == nil {
		return OptionalBool{}
	}
	return optBool(*v, SourceObserved)
}

func regionMassesSorted(m map[worldmap.RegionID]float64) []BeliefMass {
	keys := make([]worldmap.RegionID, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]BeliefMass, 0, len(keys))
	for _, k := range keys {
		out = append(out, BeliefMass{Region: k, Mass: m[k]})
	}
	return out
}

func (e EnemyHero) TopBeliefString(n int) string {
	if n > len(e.BeliefRegionMass) {
		n = len(e.BeliefRegionMass)
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = fmt.Sprintf("%s %.2f", e.BeliefRegionMass[i].Region, e.BeliefRegionMass[i].Mass)
	}
	return strings.Join(parts, ", ")
}
