package mapstate

import (
	"testing"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/track"
)

func geo(t *testing.T) *worldmap.Geometry {
	t.Helper()
	g, err := worldmap.LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func vec(x, y int) observe.Vec2 { return observe.Vec2{X: x, Y: y} }

func TestVisibleHeroRegion(t *testing.T) {
	g := geo(t)
	st := track.State{
		Roster: track.Roster{AllySide: observe.TeamDire},
		Allies: []track.AllyHeroTrack{{
			Hero: "npc_dota_hero_mirana", CurrentlyObserved: true,
			PositionCandidates: []observe.Vec2{vec(2859, -2764)},
		}},
	}
	ms := Build(st, g)
	if len(ms.Allies) != 1 || ms.Allies[0].PrimaryRegion == nil || *ms.Allies[0].PrimaryRegion != worldmap.RegionBotRiver {
		t.Fatalf("%+v", ms.Allies)
	}
}

func TestMissingEnemyKeepsLastSeenOnly(t *testing.T) {
	g := geo(t)
	age := 13.4
	st := track.State{
		Roster: track.Roster{AllySide: observe.TeamDire},
		Enemies: []track.EnemyHeroTrack{{
			Hero: "npc_dota_hero_dark_willow", VisibleNow: false,
			LastSeenPositions:  []observe.Vec2{vec(-2561, -1131)},
			LastSeenAgeSeconds: &age,
		}},
	}
	ms := Build(st, g)
	e := ms.Enemies[0]
	if e.Visible || len(e.Positions) != 0 || len(e.Regions) != 0 {
		t.Fatalf("current must be empty: %+v", e)
	}
	if e.PrimaryRegion != nil {
		t.Fatal("missing enemy must not have a current primary region")
	}
	if len(e.LastSeenRegions) != 1 {
		t.Fatalf("last seen regions %v", e.LastSeenRegions)
	}
	if ms.MissingEnemyCount != 1 || ms.VisibleEnemyCount != 0 {
		t.Fatalf("counts vis=%d miss=%d", ms.VisibleEnemyCount, ms.MissingEnemyCount)
	}
}

func TestIllusionRegionsNotCollapsed(t *testing.T) {
	g := geo(t)
	st := track.State{
		Roster: track.Roster{AllySide: observe.TeamDire},
		Enemies: []track.EnemyHeroTrack{{
			Hero: "npc_dota_hero_luna", VisibleNow: true,
			VisiblePositions: []observe.Vec2{
				vec(-5274, 6036), // TOP_LANE_DIRE
				vec(2496, 2112),  // MID_LANE_DIRE
				vec(2859, -2764), // BOT_RIVER
			},
		}},
	}
	ms := Build(st, g)
	e := ms.Enemies[0]
	if !e.Visible || e.PrimaryRegion != nil {
		t.Fatalf("want visible + ambiguous primary, got %+v", e)
	}
	if len(e.Positions) != 3 || len(e.Regions) != 3 {
		t.Fatalf("positions/regions %+v %+v", e.Positions, e.Regions)
	}
	if ms.VisibleEnemyCount != 1 {
		t.Fatalf("illusions are one hero, count=%d", ms.VisibleEnemyCount)
	}
}

func TestSelfOverridesAllyIllusionsInCluster(t *testing.T) {
	g := geo(t)
	name := "npc_dota_hero_mirana"
	pos := vec(0, 0)
	st := track.State{
		Roster: track.Roster{AllySide: observe.TeamDire},
		Self:   track.SelfState{SelfObservation: observe.SelfObservation{Present: true, HeroName: &name, Position: &pos}},
		Allies: []track.AllyHeroTrack{{
			Hero: name, CurrentlyObserved: true,
			PositionCandidates: []observe.Vec2{vec(0, 0), vec(7000, 7000)},
		}},
	}
	ms := Build(st, g)
	if ms.AllyTeam.ObservedCount != 1 {
		t.Fatalf("ambiguous minimap self must not enter cluster twice, observed=%d", ms.AllyTeam.ObservedCount)
	}
	if len(ms.Allies[0].Positions) != 1 {
		t.Fatalf("self override positions %v", ms.Allies[0].Positions)
	}
}

func TestTeamAllGrouped(t *testing.T) {
	g := geo(t)
	st := track.State{Roster: track.Roster{AllySide: observe.TeamDire}}
	base := vec(0, 0)
	for i, h := range []string{"a", "b", "c", "d", "e"} {
		st.Allies = append(st.Allies, track.AllyHeroTrack{
			Hero: "npc_dota_hero_" + h, CurrentlyObserved: true,
			PositionCandidates: []observe.Vec2{vec(base.X+i*50, base.Y)},
		})
	}
	ms := Build(st, g)
	if ms.AllyTeam.GroupedCount != 5 || ms.AllyTeam.ObservedCount != 5 || ms.AllyTeam.GroupedRatio != 1 {
		t.Fatalf("%+v", ms.AllyTeam)
	}
	if !ms.AllyTeam.AllyGrouped {
		t.Fatal("want grouped")
	}
}

func TestTeamFourPlusOne(t *testing.T) {
	g := geo(t)
	st := track.State{Roster: track.Roster{AllySide: observe.TeamDire}}
	hs := []string{"a", "b", "c", "d", "e"}
	for i, h := range hs {
		p := vec(i*40, 0)
		if i == 4 {
			p = vec(8000, 8000)
		}
		st.Allies = append(st.Allies, track.AllyHeroTrack{
			Hero: "npc_dota_hero_" + h, CurrentlyObserved: true,
			PositionCandidates: []observe.Vec2{p},
		})
	}
	ms := Build(st, g)
	if ms.AllyTeam.GroupedCount != 4 || ms.AllyTeam.ObservedCount != 5 {
		t.Fatalf("%+v", ms.AllyTeam)
	}
	if ms.AllyTeam.GroupedRatio != 0.8 {
		t.Fatalf("ratio %v", ms.AllyTeam.GroupedRatio)
	}
}

func TestTeamSplitTieBreakDeterministic(t *testing.T) {
	g := geo(t)
	st := track.State{Roster: track.Roster{AllySide: observe.TeamDire}}
	// 2 + 2 + 1, clusters of 2 are equal size; names decide.
	pts := []observe.Vec2{
		vec(0, 0), vec(10, 0),
		vec(8000, 8000), vec(8010, 8000),
		vec(-8000, 8000),
	}
	names := []string{"zeta", "alpha", "miro", "naga", "lone"}
	for i, h := range names {
		st.Allies = append(st.Allies, track.AllyHeroTrack{
			Hero: "npc_dota_hero_" + h, CurrentlyObserved: true,
			PositionCandidates: []observe.Vec2{pts[i]},
		})
	}
	a := Build(st, g)
	b := Build(st, g)
	if a.AllyTeam.GroupedCount != 2 {
		t.Fatalf("want 2, got %+v", a.AllyTeam)
	}
	if a.AllyTeam.Centroid == nil || b.AllyTeam.Centroid == nil {
		t.Fatal("centroid")
	}
	if *a.AllyTeam.Centroid != *b.AllyTeam.Centroid {
		t.Fatal("tie-break must be deterministic")
	}
}

func TestMissingAllyObservationRatio(t *testing.T) {
	g := geo(t)
	st := track.State{Roster: track.Roster{AllySide: observe.TeamDire}}
	for i, h := range []string{"a", "b", "c", "d", "e"} {
		a := track.AllyHeroTrack{Hero: "npc_dota_hero_" + h}
		if i < 4 {
			a.CurrentlyObserved = true
			a.PositionCandidates = []observe.Vec2{vec(i*30, 0)}
		}
		st.Allies = append(st.Allies, a)
	}
	ms := Build(st, g)
	if ms.AllyTeam.ObservedCount != 4 || ms.AllyTeam.GroupedCount != 4 {
		t.Fatalf("%+v", ms.AllyTeam)
	}
	if ms.AllyTeam.GroupedRatio != 1 {
		t.Fatalf("ratio uses observed, got %v", ms.AllyTeam.GroupedRatio)
	}
}

func TestRoshanVisibleOnly(t *testing.T) {
	g := geo(t)
	pit, _ := g.LandmarkPosition(worldmap.LandmarkRoshanPitB)
	near := observe.Vec2{X: int(pit.X), Y: int(pit.Y)}
	far := vec(8000, 8000)
	st := track.State{
		Roster: track.Roster{AllySide: observe.TeamDire},
		Allies: []track.AllyHeroTrack{
			{Hero: "a", CurrentlyObserved: true, PositionCandidates: []observe.Vec2{near}},
			{Hero: "b", CurrentlyObserved: true, PositionCandidates: []observe.Vec2{near}},
			{Hero: "c", CurrentlyObserved: true, PositionCandidates: []observe.Vec2{near}},
			{Hero: "d", CurrentlyObserved: true, PositionCandidates: []observe.Vec2{far}},
			{Hero: "e", CurrentlyObserved: false},
		},
		Enemies: []track.EnemyHeroTrack{
			{Hero: "n1", VisibleNow: true, VisiblePositions: []observe.Vec2{near}},
			{Hero: "n2", VisibleNow: true, VisiblePositions: []observe.Vec2{far}},
			{Hero: "n3", VisibleNow: true, VisiblePositions: []observe.Vec2{far}},
			{Hero: "m1", VisibleNow: false, LastSeenPositions: []observe.Vec2{near}},
			{Hero: "m2", VisibleNow: false},
		},
	}
	ms := Build(st, g)
	if ms.RoshanPitB.AlliesNear != 3 || ms.RoshanPitB.VisibleEnemiesNear != 1 {
		t.Fatalf("pit B %+v", ms.RoshanPitB)
	}
	if ms.MissingEnemyCount != 2 {
		t.Fatalf("missing %d", ms.MissingEnemyCount)
	}
	if ms.RoshanPitB.VisibleEnemiesNear == 0 {
		t.Fatal("sanity")
	}
}

func TestNoSafetyFromZeroVisible(t *testing.T) {
	g := geo(t)
	st := track.State{
		Roster: track.Roster{AllySide: observe.TeamDire},
		Enemies: []track.EnemyHeroTrack{
			{Hero: "a", VisibleNow: false},
			{Hero: "b", VisibleNow: false},
		},
	}
	ms := Build(st, g)
	if ms.RoshanPitB.VisibleEnemiesNear != 0 || ms.MissingEnemyCount != 2 {
		t.Fatalf("%+v miss=%d", ms.RoshanPitB, ms.MissingEnemyCount)
	}
}

func TestBuildCopiesSlices(t *testing.T) {
	g := geo(t)
	st := track.State{
		Allies: []track.AllyHeroTrack{{
			Hero: "h", CurrentlyObserved: true,
			PositionCandidates: []observe.Vec2{vec(0, 0)},
		}},
	}
	ms := Build(st, g)
	ms.Allies[0].Positions[0].X = 99
	ms2 := Build(st, g)
	if ms2.Allies[0].Positions[0].X == 99 {
		t.Fatal("aliased")
	}
}
