package state

import (
	"fmt"
	"sort"

	"cmnet/internal/dota/worldmap"
)

func Encode(s State) (NumericFeatureVector, error) {
	if !s.Quality.RosterReady {
		return NumericFeatureVector{}, fmt.Errorf("dota-state: roster not ready for numeric encoding")
	}
	enc := encoder{}
	enc.meta(&s)
	enc.self(&s.Self)
	enc.team(&s.Team)
	for i := range s.Allies {
		enc.ally(i, &s.Allies[i])
	}
	for i := len(s.Allies); i < 5; i++ {
		enc.emptyAlly(i)
	}
	for i := range s.Enemies {
		enc.enemy(i, &s.Enemies[i])
	}
	for i := len(s.Enemies); i < 5; i++ {
		enc.emptyEnemy(i)
	}
	enc.lane("top", &s.Lanes.Top)
	enc.lane("mid", &s.Lanes.Mid)
	enc.lane("bot", &s.Lanes.Bot)
	enc.objectives(&s.Objectives)
	enc.events(&s.RecentEvents)
	enc.quality(&s.Quality)
	return NumericFeatureVector{SchemaVersion: s.Meta.SchemaVersion, FeatureNames: enc.names, Values: enc.values, AvailabilityMask: enc.mask}, nil
}

type encoder struct {
	names  []string
	values []float64
	mask   []uint8
}

func (e *encoder) add(name string, value float64, available bool) {
	e.names = append(e.names, name)
	e.values = append(e.values, value)
	if available {
		e.mask = append(e.mask, 1)
	} else {
		e.mask = append(e.mask, 0)
	}
}

func (e *encoder) addBool(name string, v OptionalBool) {
	val := 0.0
	if v.Value {
		val = 1
	}
	e.add(name, val, v.Available)
}
func (e *encoder) addInt(name string, v OptionalInt)     { e.add(name, float64(v.Value), v.Available) }
func (e *encoder) addFloat(name string, v OptionalFloat) { e.add(name, v.Value, v.Available) }
func (e *encoder) addRegionOneHot(prefix string, region OptionalRegion) {
	for _, vocab := range worldmap.MacroRegions() {
		available := region.Available
		value := 0.0
		if available && region.Value == vocab {
			value = 1
		}
		e.add(prefix+"."+string(vocab), value, available)
	}
}
func (e *encoder) addRegionMultiHot(prefix string, regions []worldmap.RegionID, available bool) {
	set := map[worldmap.RegionID]struct{}{}
	for _, region := range regions {
		set[region] = struct{}{}
	}
	for _, vocab := range worldmap.MacroRegions() {
		_, ok := set[vocab]
		value := 0.0
		if ok {
			value = 1
		}
		e.add(prefix+"."+string(vocab), value, available)
	}
}
func (e *encoder) addEnumOneHot(prefix string, value string, available bool, vocab []string) {
	for _, item := range vocab {
		v := 0.0
		if available && value == item {
			v = 1
		}
		e.add(prefix+"."+item, v, available)
	}
}

func (e *encoder) meta(s *State) {
	e.addInt("meta.game_time", s.Meta.GameTime)
	e.addInt("meta.clock_time", s.Meta.ClockTime)
	e.addEnumOneHot("meta.our_side", s.Meta.OurSide.Value, s.Meta.OurSide.Available, []string{"radiant", "dire"})
	e.addEnumOneHot("match.phase", string(s.Match.Phase), s.Match.Phase != PhaseUnknown, []string{string(PhaseEarly), string(PhaseMid), string(PhaseLate)})
}
func (e *encoder) self(s *Self) {
	e.addRegionOneHot("self.region", s.Region)
	e.addBool("self.alive", s.Alive)
	e.addInt("self.respawn_seconds", s.RespawnSeconds)
	e.addInt("self.level", s.Level)
	e.addInt("self.hp_pct", s.HPPct)
	e.addInt("self.mana_pct", s.ManaPct)
	e.addInt("self.gold", s.Gold)
	e.addInt("self.gpm", s.GPM)
	e.addInt("self.xpm", s.XPM)
	e.addBool("self.buyback_available", s.BuybackAvailable)
	e.addInt("self.buyback_cooldown", s.BuybackCooldown)
	e.addBool("self.smoked", s.Smoked)
}
func (e *encoder) team(t *Team) {
	e.add("team.observed_ally_count", float64(t.ObservedAllyCount), true)
	e.add("team.grouped_count", float64(t.GroupedCount), true)
	e.addFloat("team.grouped_ratio", t.GroupedRatio)
	e.addBool("team.ally_grouped", t.AllyGrouped)
	e.addRegionOneHot("team.main_group_region", t.MainGroupRegion)
	e.addFloat("team.spread", t.Spread)
	e.add("team.visible_enemy_count", float64(t.VisibleEnemyCount), true)
	e.add("team.missing_enemy_count", float64(t.MissingEnemyCount), true)
	e.add("team.unlocalized_enemy_count", float64(t.UnlocalizedEnemies), true)
	e.addFloat("team.mean_missing_age", t.MeanMissingAge)
	e.addFloat("team.max_missing_age", t.MaxMissingAge)
	e.addFloat("team.mean_unmodeled_mass", t.MeanUnmodeledMass)
	e.addFloat("team.max_unmodeled_mass", t.MaxUnmodeledMass)
}
func (e *encoder) ally(i int, a *AllyHero) {
	prefix := fmt.Sprintf("ally[%d]", i)
	e.add(prefix+".currently_observed", btof(a.CurrentlyObserved), true)
	e.addRegionMultiHot(prefix+".regions", a.Regions, len(a.Regions) > 0)
	e.addRegionOneHot(prefix+".primary_region", a.PrimaryRegion)
}
func (e *encoder) emptyAlly(i int) { e.ally(i, &AllyHero{}) }
func (e *encoder) enemy(i int, en *EnemyHero) {
	prefix := fmt.Sprintf("enemy[%d]", i)
	e.add(prefix+".visible", btof(en.Visible), true)
	e.add(prefix+".current_ambiguous", btof(en.CurrentAmbiguous), true)
	e.addRegionMultiHot(prefix+".current_regions", en.CurrentRegions, en.Visible)
	e.addRegionMultiHot(prefix+".last_seen_regions", en.LastSeenRegions, len(en.LastSeenRegions) > 0)
	e.addFloat(prefix+".last_seen_age", en.LastSeenAge)
	e.addEnumOneHot(prefix+".belief_status", en.BeliefStatus, en.BeliefStatus != "", []string{"OBSERVED", "OBSERVED_AMBIGUOUS", "MISSING_LOCALIZED", "UNLOCALIZED"})
	beliefMap := map[worldmap.RegionID]float64{}
	for _, rm := range en.BeliefRegionMass {
		beliefMap[rm.Region] = rm.Mass
	}
	for _, vocab := range worldmap.MacroRegions() {
		_, hasHistory := beliefMap[vocab]
		e.add(prefix+".belief."+string(vocab), beliefMap[vocab], hasHistory || en.BeliefStatus != "")
	}
	e.add(prefix+".belief.unmodeled", en.BeliefUnmodeledMass, en.BeliefStatus != "")
	e.addFloat(prefix+".belief.entropy", en.BeliefEntropy)
	e.add(prefix+".walking_reachable_count", float64(en.WalkingReachableCount), true)
}
func (e *encoder) emptyEnemy(i int) { e.enemy(i, &EnemyHero{}) }
func (e *encoder) lane(name string, lane *Lane) {
	prefix := "lane." + name
	e.add(prefix+".our_wave_count", float64(lane.OurWaveCount), true)
	e.add(prefix+".our_front_available", btof(lane.OurFrontAvailable), true)
	e.addFloat(prefix+".our_front_team_progress", lane.OurFrontTeamProgress)
	e.addInt(prefix+".our_front_creep_count", lane.OurFrontCreepCount)
	e.add(prefix+".visible_enemy_wave_count", float64(lane.VisibleEnemyWaveCount), true)
	e.add(prefix+".visible_enemy_front_available", btof(lane.VisibleEnemyFrontAvailable), true)
	e.addFloat(prefix+".visible_enemy_front_team_progress", lane.VisibleEnemyFrontProgress)
	e.addInt(prefix+".visible_enemy_front_creep_count", lane.VisibleEnemyFrontCreepCount)
	e.add(prefix+".enemy_last_seen_front_available", btof(lane.EnemyLastSeenFrontAvailable), true)
	e.addFloat(prefix+".enemy_last_seen_front_team_progress", lane.EnemyLastSeenFrontProgress)
	e.addFloat(prefix+".enemy_last_seen_front_age", lane.EnemyLastSeenFrontAge)
	e.addBool(prefix+".observed_contact", lane.ObservedContact)
	e.addFloat(prefix+".front_gap", lane.FrontGap)
}
func (e *encoder) objectives(o *Objectives) {
	e.addBool("objectives.roshan_visible", o.RoshanVisible)
	e.addFloat("objectives.roshan_last_seen_age", o.RoshanLastSeenAge)
	e.add("objectives.pit_a.allies_near", float64(o.PitA.AlliesNear), true)
	e.add("objectives.pit_a.visible_enemies_near", float64(o.PitA.VisibleEnemiesNear), true)
	e.add("objectives.pit_b.allies_near", float64(o.PitB.AlliesNear), true)
	e.add("objectives.pit_b.visible_enemies_near", float64(o.PitB.VisibleEnemiesNear), true)
}
func (e *encoder) events(r *RecentEvents) {
	e.addFloat("recent_events.recent_hero_kill_age", r.RecentHeroKillAge)
	e.addFloat("recent_events.recent_smoke_age", r.RecentSmokeAge)
	e.addFloat("recent_events.recent_glyph_age", r.RecentGlyphAge)
}
func (e *encoder) quality(q *Quality) {
	e.add("quality.roster_ready", btof(q.RosterReady), true)
	e.add("quality.map_state_available", btof(q.MapStateAvailable), true)
	e.add("quality.lane_state_available", btof(q.LaneStateAvailable), true)
	e.add("quality.belief_state_available", btof(q.BeliefStateAvailable), true)
	e.add("quality.gsi_payload_fresh", btof(q.GSIShardFresh), true)
}

func btof(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func NumericFeatureNames() []string {
	st := State{Meta: Meta{SchemaVersion: SchemaVersion, OurSide: optString("radiant", SourceObserved)}, Match: Match{Phase: PhaseEarly}, Quality: Quality{RosterReady: true}}
	vec, _ := Encode(st)
	out := append([]string(nil), vec.FeatureNames...)
	sort.Strings(out)
	return vec.FeatureNames
}
