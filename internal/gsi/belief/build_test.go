package belief

import (
	"math"
	"reflect"
	"testing"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/mapstate"
)

func TestVisibleHeroSingleRegion(t *testing.T) {
	graph := mustGraph(t)
	cfg := DefaultConfig()
	st := mapstate.State{GeometryVersion: worldmap.CurrentVersion, Enemies: []mapstate.HeroSpatial{{
		Hero:    "npc_dota_hero_luna",
		Visible: true,
		Regions: []worldmap.RegionID{worldmap.RegionTopLaneDire},
	}}}
	bs, err := Build(st, graph, cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := bs.Enemies[0]
	if got.Status != StatusObserved || got.UnmodeledMass != 0 {
		t.Fatalf("status=%s unmodeled=%v", got.Status, got.UnmodeledMass)
	}
	if got.RegionMass[worldmap.RegionTopLaneDire] != 1 {
		t.Fatalf("mass=%v", got.RegionMass)
	}
}

func TestVisibleHeroAmbiguousIllusions(t *testing.T) {
	graph := mustGraph(t)
	st := mapstate.State{GeometryVersion: worldmap.CurrentVersion, Enemies: []mapstate.HeroSpatial{{
		Hero:    "npc_dota_hero_luna",
		Visible: true,
		Regions: []worldmap.RegionID{worldmap.RegionTopLaneDire, worldmap.RegionBotRiver, worldmap.RegionMidLaneDire},
	}}}
	bs, err := Build(st, graph, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	got := bs.Enemies[0]
	if got.Status != StatusObservedAmbiguous {
		t.Fatalf("status=%s", got.Status)
	}
	for _, region := range []worldmap.RegionID{worldmap.RegionTopLaneDire, worldmap.RegionBotRiver, worldmap.RegionMidLaneDire} {
		if math.Abs(got.RegionMass[region]-1.0/3.0) > 1e-9 {
			t.Fatalf("mass[%s]=%v", region, got.RegionMass[region])
		}
	}
}

func TestMissingAgeExpansion(t *testing.T) {
	graph := mustGraph(t)
	origin := worldmap.RegionMidLaneRadiant
	b1 := mustMissing(t, graph, origin, 1)
	b10 := mustMissing(t, graph, origin, 10)
	b40 := mustMissing(t, graph, origin, 40)
	if !(b1.RegionMass[origin] > b10.RegionMass[origin] && b10.RegionMass[origin] > b40.RegionMass[origin]) {
		t.Fatalf("origin masses 1=%v 10=%v 40=%v", b1.RegionMass[origin], b10.RegionMass[origin], b40.RegionMass[origin])
	}
	if !(b1.Entropy < b10.Entropy && b40.Entropy >= b10.Entropy-0.02) {
		t.Fatalf("entropy 1=%v 10=%v 40=%v", b1.Entropy, b10.Entropy, b40.Entropy)
	}
	if !(b1.UnmodeledMass < b10.UnmodeledMass && b10.UnmodeledMass < b40.UnmodeledMass) {
		t.Fatalf("unmodeled 1=%v 10=%v 40=%v", b1.UnmodeledMass, b10.UnmodeledMass, b40.UnmodeledMass)
	}
}

func TestMultipleSeedRegions(t *testing.T) {
	graph := mustGraph(t)
	st := missingState([]worldmap.RegionID{worldmap.RegionTopRiver, worldmap.RegionBotRiver}, 12)
	bs, err := Build(st, graph, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	got := bs.Enemies[0]
	if got.RegionMass[worldmap.RegionTopRiver] <= 0 || got.RegionMass[worldmap.RegionBotRiver] <= 0 {
		t.Fatalf("mass=%v", got.RegionMass)
	}
}

func TestUnknownAndOtherBecomeUnlocalized(t *testing.T) {
	graph := mustGraph(t)
	st := missingState([]worldmap.RegionID{worldmap.RegionUnknown, worldmap.RegionOther}, 5)
	bs, err := Build(st, graph, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	got := bs.Enemies[0]
	if got.Status != StatusUnlocalized || got.UnmodeledMass != 1 || len(got.RegionMass) != 0 {
		t.Fatalf("got=%+v", got)
	}
}

func TestReachabilityDoesNotHardMask(t *testing.T) {
	graph := mustGraph(t)
	cfg := DefaultConfig()
	cfg.ReachabilityWalkSpeed = 400
	cfg.ReachabilitySlack = 0
	got := mustMissingWithConfig(t, graph, worldmap.RegionTopLaneRadiant, 6, cfg)
	seenUnreachableWithMass := false
	for _, rr := range got.WalkingReachability {
		if rr.Region == worldmap.RegionTopLaneDire && !rr.ReachableByWalkV0 && got.RegionMass[rr.Region] > 0 {
			seenUnreachableWithMass = true
		}
	}
	if !seenUnreachableWithMass {
		t.Fatalf("expected non-zero mass outside walking envelope: reach=%v mass=%v", got.WalkingReachability, got.RegionMass[worldmap.RegionTopLaneDire])
	}
}

func TestNormalizationAndNoNaN(t *testing.T) {
	graph := mustGraph(t)
	for _, age := range []float64{0, 1, 10, 1000} {
		got := mustMissing(t, graph, worldmap.RegionMidLaneRadiant, age)
		sum := got.UnmodeledMass
		for _, mass := range got.RegionMass {
			if math.IsNaN(mass) || math.IsInf(mass, 0) || mass < 0 {
				t.Fatalf("bad mass %v", mass)
			}
			sum += mass
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Fatalf("sum=%v", sum)
		}
	}
}

func TestPauseDeterminism(t *testing.T) {
	graph := mustGraph(t)
	st := missingState([]worldmap.RegionID{worldmap.RegionMidLaneRadiant}, 9)
	a, err := Build(st, graph, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(st, graph, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("belief must be deterministic")
	}
}

func TestReappearanceResetsBelief(t *testing.T) {
	graph := mustGraph(t)
	missing, err := Build(missingState([]worldmap.RegionID{worldmap.RegionMidLaneRadiant}, 15), graph, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if missing.Enemies[0].Status != StatusMissing {
		t.Fatalf("status=%s", missing.Enemies[0].Status)
	}
	observedState := mapstate.State{GeometryVersion: worldmap.CurrentVersion, Enemies: []mapstate.HeroSpatial{{
		Hero:    "npc_dota_hero_dark_willow",
		Visible: true,
		Regions: []worldmap.RegionID{worldmap.RegionTopRiver},
	}}}
	observed, err := Build(observedState, graph, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if observed.Enemies[0].Status != StatusObserved || observed.Enemies[0].RegionMass[worldmap.RegionTopRiver] != 1 {
		t.Fatalf("observed=%+v", observed.Enemies[0])
	}
}

func mustGraph(t *testing.T) *worldmap.RegionGraph {
	t.Helper()
	g, err := worldmap.LoadRegionGraphCurrent()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func missingState(regions []worldmap.RegionID, age float64) mapstate.State {
	ageCopy := age
	return mapstate.State{GeometryVersion: worldmap.CurrentVersion, Enemies: []mapstate.HeroSpatial{{
		Hero:               "npc_dota_hero_dark_willow",
		Visible:            false,
		LastSeenRegions:    regions,
		LastSeenAgeSeconds: &ageCopy,
	}}}
}

func mustMissing(t *testing.T, graph *worldmap.RegionGraph, origin worldmap.RegionID, age float64) EnemyBelief {
	t.Helper()
	return mustMissingWithConfig(t, graph, origin, age, DefaultConfig())
}

func mustMissingWithConfig(t *testing.T, graph *worldmap.RegionGraph, origin worldmap.RegionID, age float64, cfg Config) EnemyBelief {
	t.Helper()
	st := missingState([]worldmap.RegionID{origin}, age)
	bs, err := Build(st, graph, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return bs.Enemies[0]
}
