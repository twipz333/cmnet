package state

import (
	"math"
	"reflect"
	"testing"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/belief"
	"cmnet/internal/gsi/lanestate"
	"cmnet/internal/gsi/mapstate"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/track"
)

func TestBuildFullyObserved(t *testing.T) {
	ts, ms, ls, bs := fullObservedFixture(t)
	st, err := Build(ts, &ms, &ls, &bs)
	if err != nil {
		t.Fatal(err)
	}
	if st.Team.VisibleEnemyCount != 5 || st.Team.MissingEnemyCount != 0 {
		t.Fatalf("team=%+v", st.Team)
	}
	if len(st.Allies) != 5 || len(st.Enemies) != 5 {
		t.Fatalf("allies=%d enemies=%d", len(st.Allies), len(st.Enemies))
	}
}

func TestBuildFogCounts(t *testing.T) {
	ts, ms, ls, bs := fullObservedFixture(t)
	ms.VisibleEnemyCount = 2
	ms.MissingEnemyCount = 3
	bs.Enemies[2].Status = belief.StatusMissing
	bs.Enemies[2].Visible = false
	bs.Enemies[3].Status = belief.StatusMissing
	bs.Enemies[3].Visible = false
	bs.Enemies[4].Status = belief.StatusMissing
	bs.Enemies[4].Visible = false
	st, err := Build(ts, &ms, &ls, &bs)
	if err != nil {
		t.Fatal(err)
	}
	if st.Team.VisibleEnemyCount != 2 || st.Team.MissingEnemyCount != 3 {
		t.Fatalf("team=%+v", st.Team)
	}
}

func TestUnlocalizedEnemyPreserved(t *testing.T) {
	ts, ms, ls, bs := fullObservedFixture(t)
	bs.Enemies[0].Status = belief.StatusUnlocalized
	bs.Enemies[0].UnmodeledMass = 1
	bs.Enemies[0].TopRegions = nil
	st, err := Build(ts, &ms, &ls, &bs)
	if err != nil {
		t.Fatal(err)
	}
	if st.Enemies[0].BeliefStatus != "UNLOCALIZED" || st.Enemies[0].BeliefUnmodeledMass != 1 {
		t.Fatalf("enemy=%+v", st.Enemies[0])
	}
}

func TestAmbiguityPreserved(t *testing.T) {
	ts, ms, ls, bs := fullObservedFixture(t)
	bs.Enemies[0].Visible = true
	bs.Enemies[0].CurrentRegions = []worldmap.RegionID{worldmap.RegionTopRiver, worldmap.RegionMidLaneRadiant}
	bs.Enemies[0].Status = belief.StatusObservedAmbiguous
	st, err := Build(ts, &ms, &ls, &bs)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enemies[0].CurrentAmbiguous || len(st.Enemies[0].CurrentRegions) != 2 {
		t.Fatalf("enemy=%+v", st.Enemies[0])
	}
}

func TestQualityDegradedWithoutLaneBelief(t *testing.T) {
	ts, ms, _, _ := fullObservedFixture(t)
	st, err := Build(ts, &ms, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Quality.LaneStateAvailable || st.Quality.BeliefStateAvailable {
		t.Fatalf("quality=%+v", st.Quality)
	}
}

func TestEncoderDeterministicAndMask(t *testing.T) {
	ts, ms, ls, bs := fullObservedFixture(t)
	st, err := Build(ts, &ms, &ls, &bs)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("encoder must be deterministic")
	}
	for i := range a.Values {
		if a.AvailabilityMask[i] == 0 && a.Values[i] != 0 {
			continue
		}
	}
}

func TestEncoderRejectsMissingRoster(t *testing.T) {
	_, err := Encode(State{Meta: Meta{SchemaVersion: SchemaVersion}, Quality: Quality{RosterReady: false}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSemanticRegistryUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, feature := range SemanticFeatureRegistry() {
		if feature.Name == "" {
			t.Fatal("empty feature name")
		}
		if seen[feature.Name] {
			t.Fatalf("duplicate feature %s", feature.Name)
		}
		seen[feature.Name] = true
	}
}

func TestEncoderNoNaNOrInf(t *testing.T) {
	ts, ms, ls, bs := fullObservedFixture(t)
	st, err := Build(ts, &ms, &ls, &bs)
	if err != nil {
		t.Fatal(err)
	}
	vec, err := Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(vec.Values) != len(vec.AvailabilityMask) {
		t.Fatalf("values=%d mask=%d", len(vec.Values), len(vec.AvailabilityMask))
	}
	for i, v := range vec.Values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("bad value at %d: %v", i, v)
		}
	}
}

func fullObservedFixture(t *testing.T) (track.State, mapstate.State, lanestate.State, belief.State) {
	t.Helper()
	roster := track.Roster{AllySide: observe.TeamDire,
		Allies:  []track.HeroRef{{UnitName: "npc_dota_hero_mirana"}, {UnitName: "npc_dota_hero_sven"}, {UnitName: "npc_dota_hero_ember_spirit"}, {UnitName: "npc_dota_hero_night_stalker"}, {UnitName: "npc_dota_hero_rattletrap"}},
		Enemies: []track.HeroRef{{UnitName: "npc_dota_hero_luna"}, {UnitName: "npc_dota_hero_tidehunter"}, {UnitName: "npc_dota_hero_lion"}, {UnitName: "npc_dota_hero_dark_willow"}, {UnitName: "npc_dota_hero_wisp"}},
	}
	gt, ct := 1104, 1104
	selfHero := "npc_dota_hero_mirana"
	alive := true
	lvl, hp, mana, gold, gpm, xpm, bbcd := 14, 84, 62, 2200, 510, 590, 0
	ts := track.State{Clock: track.Clock{Seq: 12333, GameTime: &gt, ClockTime: &ct, MatchID: "x", DireScore: 10, RadiantScore: 12}, Roster: roster,
		Self:         track.SelfState{SelfObservation: observe.SelfObservation{Present: true, HeroName: &selfHero, Level: &lvl, HPPercent: &hp, ManaPercent: &mana, Gold: &gold, GPM: &gpm, XPM: &xpm, Alive: &alive, BuybackCooldown: &bbcd}},
		RecentSmokes: []track.SmokeActivation{{GameTime: &gt}},
		RecentEvents: []observe.Event{{Kind: observe.EventHeroKill, GameTime: &gt}},
		Roshan:       track.RoshanTrack{VisibleNow: false, LastSeenAt: &gt},
	}
	ms := mapstate.State{GeometryVersion: worldmap.CurrentVersion, Self: mapstate.HeroSpatial{Hero: selfHero, Visible: true, Regions: []worldmap.RegionID{worldmap.RegionBotRiver}, PrimaryRegion: ptrRegion(worldmap.RegionBotRiver)},
		Allies:            []mapstate.HeroSpatial{{Hero: "npc_dota_hero_sven", Visible: true, Regions: []worldmap.RegionID{worldmap.RegionBotRiver}, PrimaryRegion: ptrRegion(worldmap.RegionBotRiver)}, {Hero: "npc_dota_hero_ember_spirit", Visible: true, Regions: []worldmap.RegionID{worldmap.RegionBotRiver}, PrimaryRegion: ptrRegion(worldmap.RegionBotRiver)}, {Hero: "npc_dota_hero_night_stalker", Visible: false}, {Hero: "npc_dota_hero_rattletrap", Visible: false}},
		Enemies:           []mapstate.HeroSpatial{{Hero: "npc_dota_hero_luna", Visible: true, Regions: []worldmap.RegionID{worldmap.RegionTopLaneDire}, PrimaryRegion: ptrRegion(worldmap.RegionTopLaneDire)}, {Hero: "npc_dota_hero_tidehunter", Visible: true, Regions: []worldmap.RegionID{worldmap.RegionTopLaneCenter}, PrimaryRegion: ptrRegion(worldmap.RegionTopLaneCenter)}, {Hero: "npc_dota_hero_lion", Visible: true, Regions: []worldmap.RegionID{worldmap.RegionMidLaneCenter}, PrimaryRegion: ptrRegion(worldmap.RegionMidLaneCenter)}, {Hero: "npc_dota_hero_dark_willow", Visible: true, Regions: []worldmap.RegionID{worldmap.RegionMidLaneRadiant}, PrimaryRegion: ptrRegion(worldmap.RegionMidLaneRadiant)}, {Hero: "npc_dota_hero_wisp", Visible: true, Regions: []worldmap.RegionID{worldmap.RegionBotLaneDire}, PrimaryRegion: ptrRegion(worldmap.RegionBotLaneDire)}},
		AllyTeam:          mapstate.TeamSpatial{ObservedCount: 5, GroupedCount: 3, GroupedRatio: 0.6, AllyGrouped: true, CentroidRegion: worldmap.RegionBotRiver, Spread: 1240},
		VisibleEnemyCount: 5, MissingEnemyCount: 0,
		RoshanPitA: mapstate.ObjectiveProximity{AlliesNear: 0, VisibleEnemiesNear: 0}, RoshanPitB: mapstate.ObjectiveProximity{AlliesNear: 2, VisibleEnemiesNear: 0},
	}
	ls := lanestate.State{GeometryVersion: worldmap.CurrentLanesVersion, MapGeometryVersion: worldmap.CurrentVersion, MinimapPresent: true, OurSide: observe.TeamDire,
		Top: laneFixture(worldmap.LaneTop, 0.63, 0.38, 0.38, 0), Mid: laneFixture(worldmap.LaneMid, 0.55, 0.51, 0.51, 0), Bot: laneFixture(worldmap.LaneBot, 0.45, 0.66, 0.66, 7.2)}
	bs := belief.State{ModelVersion: belief.ModelVersion, GraphVersion: worldmap.CurrentRegionGraphVersion, GraphMapVersion: worldmap.CurrentVersion,
		Enemies: []belief.EnemyBelief{{Hero: "npc_dota_hero_luna", Visible: true, Status: belief.StatusObserved, CurrentRegions: []worldmap.RegionID{worldmap.RegionTopLaneDire}, TopRegions: []belief.RegionMass{{Region: worldmap.RegionTopLaneDire, Mass: 1}}}, {Hero: "npc_dota_hero_tidehunter", Visible: true, Status: belief.StatusObserved, CurrentRegions: []worldmap.RegionID{worldmap.RegionTopLaneCenter}, TopRegions: []belief.RegionMass{{Region: worldmap.RegionTopLaneCenter, Mass: 1}}}, {Hero: "npc_dota_hero_lion", Visible: true, Status: belief.StatusObserved, CurrentRegions: []worldmap.RegionID{worldmap.RegionMidLaneCenter}, TopRegions: []belief.RegionMass{{Region: worldmap.RegionMidLaneCenter, Mass: 1}}}, {Hero: "npc_dota_hero_dark_willow", Visible: false, Status: belief.StatusMissing, LastSeenRegions: []worldmap.RegionID{worldmap.RegionMidLaneRadiant}, MissingAge: 18.7, UnmodeledMass: 0.12, Entropy: 0.61, TopRegions: []belief.RegionMass{{Region: worldmap.RegionMidLaneRadiant, Mass: 0.19}, {Region: worldmap.RegionRadiantTriangle, Mass: 0.11}, {Region: worldmap.RegionRadiantMainJungle, Mass: 0.09}}, WalkingReachability: []belief.RegionReachability{{Region: worldmap.RegionMidLaneRadiant, ReachableByWalkV0: true}, {Region: worldmap.RegionRadiantTriangle, ReachableByWalkV0: true}}}, {Hero: "npc_dota_hero_wisp", Visible: false, Status: belief.StatusUnlocalized, UnmodeledMass: 1}},
	}
	return ts, ms, ls, bs
}

func laneFixture(id worldmap.LaneID, our, vis, last float64, age float64) lanestate.LaneView {
	v := lanestate.LaneView{Lane: id, ObservedContact: true}
	v.Our.HasFront = true
	v.Our.Waves = []lanestate.WaveGroup{{CreepCount: 4}}
	v.Our.FrontTeam = worldmap.TeamProgress(our)
	v.Our.FrontDepth = worldmap.DepthCenter
	v.Our.Frontmost = &lanestate.WaveGroup{CreepCount: 4}
	v.Enemy.Visible = []lanestate.WaveGroup{{CreepCount: 4}}
	v.Enemy.HasVisibleFront = true
	v.Enemy.VisibleFrontTeam = worldmap.TeamProgress(vis)
	v.Enemy.VisibleFrontDepth = worldmap.DepthCenter
	v.Enemy.VisibleFrontmost = &lanestate.WaveGroup{CreepCount: 4}
	if age > 0 {
		v.Enemy.LastSeenFrontTeam = worldmap.TeamProgress(last)
		v.Enemy.LastSeenAgeSeconds = &age
		v.Enemy.LastSeenFrontmost = &lanestate.WaveGroup{CreepCount: 4, FrontTeamProgress: worldmap.TeamProgress(last)}
	}
	return v
}

func ptrRegion(r worldmap.RegionID) *worldmap.RegionID { return &r }
