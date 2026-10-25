package lanestate

import (
	"math"
	"testing"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/track"
)

func TestBuildAssignsMidCreep(t *testing.T) {
	g := mustLanes(t)
	p := atProgress(t, g, worldmap.LaneMid, 0.5)
	st := tickState(10, "m", observe.TeamDire, true, melee(observe.TeamRadiant, p))
	obs := Build(st, g)
	if obs.Stats.LaneCreepsAssigned != 1 {
		t.Fatalf("assigned %d far=%d amb=%d", obs.Stats.LaneCreepsAssigned, obs.Stats.UnknownTooFar, obs.Stats.Ambiguous)
	}
	if len(obs.Mid.Radiant) != 1 {
		t.Fatalf("mid radiant groups=%d", len(obs.Mid.Radiant))
	}
	if math.Abs(float64(obs.Mid.Radiant[0].CenterAbsoluteProgress)-0.5) > 0.03 {
		t.Fatalf("progress %v", obs.Mid.Radiant[0].CenterAbsoluteProgress)
	}
}

func TestClusterTwoWaves(t *testing.T) {
	g := mustLanes(t)
	// Distances 100/180/240 vs 2100/2180 along MID, gap 1200.
	var creeps []observe.CreepObservation
	for _, d := range []float64{100, 180, 240, 2100, 2180} {
		creeps = append(creeps, melee(observe.TeamRadiant, atDistance(t, g, worldmap.LaneMid, d)))
	}
	obs := Build(tickState(1, "m", observe.TeamDire, true, creeps...), g)
	if len(obs.Mid.Radiant) != 2 {
		t.Fatalf("groups=%d want 2", len(obs.Mid.Radiant))
	}
	if obs.Mid.Radiant[0].CreepCount != 3 || obs.Mid.Radiant[1].CreepCount != 2 {
		t.Fatalf("counts %+v %+v", obs.Mid.Radiant[0], obs.Mid.Radiant[1])
	}
}

func TestSingletonGroup(t *testing.T) {
	g := mustLanes(t)
	p := atProgress(t, g, worldmap.LaneBot, 0.4)
	obs := Build(tickState(1, "m", observe.TeamDire, true, melee(observe.TeamDire, p)), g)
	if len(obs.Bot.Dire) != 1 || obs.Bot.Dire[0].CreepCount != 1 {
		t.Fatalf("%+v", obs.Bot.Dire)
	}
}

func TestNonLaneCreepSkipped(t *testing.T) {
	g := mustLanes(t)
	p := atProgress(t, g, worldmap.LaneMid, 0.5)
	c := observe.CreepObservation{
		UnitName: "npc_dota_neutral_kobold",
		Team:     observe.TeamNeutral,
		Class:    observe.CreepOther,
		Position: observe.Vec2{X: int(p.X), Y: int(p.Y)},
	}
	obs := Build(tickState(1, "m", observe.TeamDire, true, c), g)
	if obs.Stats.NonLaneSkipped != 1 || obs.Stats.LaneCreepsAssigned != 0 {
		t.Fatalf("%+v", obs.Stats)
	}
	if len(obs.Mid.Radiant)+len(obs.Mid.Dire) != 0 {
		t.Fatal("neutral leaked into waves")
	}
}

func TestFrontmostRadiantAndDire(t *testing.T) {
	g := mustLanes(t)
	a := melee(observe.TeamRadiant, atProgress(t, g, worldmap.LaneMid, 0.30))
	b := melee(observe.TeamRadiant, atProgress(t, g, worldmap.LaneMid, 0.60))
	obs := Build(tickState(1, "m", observe.TeamRadiant, true, a, b), g)
	// Far apart enough for 2 groups.
	if len(obs.Mid.Radiant) < 2 {
		t.Fatalf("need 2 groups, got %d", len(obs.Mid.Radiant))
	}
	fm := frontmostOf(obs.Mid.Radiant)
	if fm == nil || math.Abs(float64(fm.FrontAbsoluteProgress)-0.60) > 0.04 {
		t.Fatalf("radiant front %+v", fm)
	}

	c := melee(observe.TeamDire, atProgress(t, g, worldmap.LaneMid, 0.70))
	d := melee(observe.TeamDire, atProgress(t, g, worldmap.LaneMid, 0.40))
	obs = Build(tickState(1, "m", observe.TeamDire, true, c, d), g)
	if len(obs.Mid.Dire) < 2 {
		t.Fatalf("dire groups %d", len(obs.Mid.Dire))
	}
	fm = frontmostOf(obs.Mid.Dire)
	if fm == nil {
		t.Fatal("nil front")
	}
	if math.Abs(float64(fm.FrontAbsoluteProgress)-0.40) > 0.04 {
		t.Fatalf("dire front abs %v", fm.FrontAbsoluteProgress)
	}
	if math.Abs(float64(fm.FrontTeamProgress)-0.60) > 0.04 {
		t.Fatalf("dire front team %v", fm.FrontTeamProgress)
	}
}

func TestBuildIgnoresCreepsWhenMinimapAbsent(t *testing.T) {
	g := mustLanes(t)
	p := atProgress(t, g, worldmap.LaneMid, 0.5)
	st := tickState(1, "m", observe.TeamDire, false, melee(observe.TeamRadiant, p))
	obs := Build(st, g)
	if obs.MinimapPresent {
		t.Fatal("present")
	}
	if obs.Stats.LaneCreepsSeen != 0 || len(obs.Mid.Radiant) != 0 {
		t.Fatal("absent section must not produce waves from stale creeps")
	}
}

func mustLanes(t *testing.T) *worldmap.LaneGeometry {
	t.Helper()
	g, err := worldmap.LoadLanesCurrent()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func melee(side observe.Team, p worldmap.WorldPosition) observe.CreepObservation {
	return observe.CreepObservation{
		UnitName: "npc_dota_creep_goodguys_melee",
		Team:     side,
		Class:    observe.CreepMelee,
		Position: observe.Vec2{X: int(math.Round(p.X)), Y: int(math.Round(p.Y))},
	}
}

func tickState(gt int, match string, side observe.Team, minimap bool, creeps ...observe.CreepObservation) track.State {
	return track.State{
		Clock:          track.Clock{MatchID: match, GameTime: &gt, Seq: 1},
		Roster:         track.Roster{AllySide: side},
		Creeps:         creeps,
		MinimapPresent: minimap,
	}
}

func atProgress(t *testing.T, g *worldmap.LaneGeometry, lane worldmap.LaneID, prog float64) worldmap.WorldPosition {
	t.Helper()
	return atDistance(t, g, lane, g.PathLength(lane)*prog)
}

func atDistance(t *testing.T, g *worldmap.LaneGeometry, lane worldmap.LaneID, dist float64) worldmap.WorldPosition {
	t.Helper()
	pts := g.PathPoints(lane)
	if len(pts) < 2 {
		t.Fatal("path")
	}
	acc := 0.0
	for i := 0; i < len(pts)-1; i++ {
		seg := worldmap.Distance(pts[i], pts[i+1])
		if acc+seg >= dist || i == len(pts)-2 {
			tfrac := 0.0
			if seg > 0 {
				tfrac = (dist - acc) / seg
				if tfrac < 0 {
					tfrac = 0
				}
				if tfrac > 1 {
					tfrac = 1
				}
			}
			return worldmap.WorldPosition{
				X: pts[i].X + tfrac*(pts[i+1].X-pts[i].X),
				Y: pts[i].Y + tfrac*(pts[i+1].Y-pts[i].Y),
			}
		}
		acc += seg
	}
	return pts[len(pts)-1]
}
