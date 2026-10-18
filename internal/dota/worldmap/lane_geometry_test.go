package worldmap

import (
	"math"
	"testing"
)

func TestLoadLanesCurrentPairsWithMap(t *testing.T) {
	g, err := LoadLanesCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if g.Version() != CurrentLanesVersion {
		t.Fatalf("version %s", g.Version())
	}
	if g.MapGeometryVersion() != CurrentVersion {
		t.Fatalf("map version %s want %s", g.MapGeometryVersion(), CurrentVersion)
	}
	if g.AssignmentRadius() <= 0 || g.WaveClusterGap() <= 0 {
		t.Fatal("thresholds")
	}
}

func TestLaneEndpointsAndMid(t *testing.T) {
	g := mustLanes(t)
	top := g.PathPoints(LaneTop)
	mid := g.PathPoints(LaneMid)
	bot := g.PathPoints(LaneBot)
	cases := []struct {
		name   string
		p      WorldPosition
		want   LaneID
		absLo  float64
		absHi  float64
		onPath bool
	}{
		{"top radiant end", top[0], LaneTop, 0, 0.001, true},
		{"top dire end", top[len(top)-1], LaneTop, 0.999, 1.001, true},
		{"mid radiant end", mid[0], LaneMid, 0, 0.001, true},
		{"mid dire end", mid[len(mid)-1], LaneMid, 0.999, 1.001, true},
		{"bot radiant end", bot[0], LaneBot, 0, 0.001, true},
		{"bot dire end", bot[len(bot)-1], LaneBot, 0.999, 1.001, true},
		{"between mid vertices", WorldPosition{
			X: (mid[3].X + mid[4].X) / 2,
			Y: (mid[3].Y + mid[4].Y) / 2,
		}, LaneMid, 0.2, 0.7, true},
		{"origin is mid", FromInt(0, 0), LaneMid, 0.3, 0.6, false},
		{"near top t1 radiant", FromInt(-6336, 1856), LaneTop, 0.15, 0.55, true},
		{"negative coords top rax", FromInt(-6336, -3758), LaneTop, 0, 0.02, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pr := g.Project(tc.p)
			if !pr.Assigned || pr.Lane != tc.want {
				t.Fatalf("got lane=%s assigned=%v dist=%.1f", pr.Lane, pr.Assigned, pr.Distance)
			}
			if tc.onPath && pr.Distance > 1 {
				t.Fatalf("expected on-path, dist=%.3f", pr.Distance)
			}
			a := float64(pr.AbsoluteProgress)
			if a < tc.absLo || a > tc.absHi {
				t.Fatalf("abs progress %.4f not in [%.3f, %.3f]", a, tc.absLo, tc.absHi)
			}
			if math.Abs(float64(TowardEnemy(pr.AbsoluteProgress, false))-(1-a)) > 1e-9 {
				t.Fatal("dire team progress must be 1-abs")
			}
			if TowardEnemy(pr.AbsoluteProgress, true) != TeamProgress(pr.AbsoluteProgress) {
				t.Fatal("radiant team progress must equal abs")
			}
		})
	}
}

func TestLaneMidpointProgress(t *testing.T) {
	g := mustLanes(t)
	mid := g.PathPoints(LaneMid)
	// Walk to half path length.
	need := g.PathLength(LaneMid) / 2
	var acc float64
	var p WorldPosition
	for i := 0; i < len(mid)-1; i++ {
		seg := Distance(mid[i], mid[i+1])
		if acc+seg >= need {
			t := (need - acc) / seg
			p = WorldPosition{X: mid[i].X + t*(mid[i+1].X-mid[i].X), Y: mid[i].Y + t*(mid[i+1].Y-mid[i].Y)}
			break
		}
		acc += seg
	}
	pr := g.Project(p)
	if !pr.Assigned || pr.Lane != LaneMid {
		t.Fatalf("%+v", pr)
	}
	if math.Abs(float64(pr.AbsoluteProgress)-0.5) > 0.01 {
		t.Fatalf("midpoint progress %v", pr.AbsoluteProgress)
	}
}

func TestLaneOutsideRadius(t *testing.T) {
	g := mustLanes(t)
	pr := g.Project(FromInt(12000, 0))
	if pr.Assigned || !pr.TooFar || pr.Lane != LaneUnknown {
		t.Fatalf("%+v", pr)
	}
}

func TestLaneAmbiguousNearFountain(t *testing.T) {
	g := mustLanes(t)
	// Capture point that sat between TOP and MID near Radiant HG.
	pr := g.Project(FromInt(-5877, -4674))
	if pr.Assigned {
		t.Fatalf("expected ambiguous/unknown, got %s dist=%.0f", pr.Lane, pr.Distance)
	}
	if !pr.Ambiguous && !pr.TooFar {
		t.Fatalf("want ambiguous or too far: %+v", pr)
	}
}

func TestLaneNearPathStillAssigned(t *testing.T) {
	g := mustLanes(t)
	mid := g.PathPoints(LaneMid)
	// Offset perpendicular from a mid vertex by a few hundred units.
	p := WorldPosition{X: mid[3].X + 80, Y: mid[3].Y - 80}
	pr := g.Project(p)
	if !pr.Assigned || pr.Lane != LaneMid {
		t.Fatalf("%+v", pr)
	}
	if pr.Distance < 1 {
		t.Fatal("offset point should not be distance 0")
	}
}

func TestLaneOriginNotUnknown(t *testing.T) {
	g := mustLanes(t)
	pr := g.Project(FromInt(0, 0))
	if pr.Lane == LaneUnknown && !pr.Assigned {
		t.Fatal("(0,0) must be classified, not auto-UNKNOWN")
	}
}

func TestLaneRejectsBroken(t *testing.T) {
	_, err := parseLaneGeometry([]byte(`{"version":"x"}`), CurrentVersion)
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = parseLaneGeometry([]byte(`{
		"version":"x","map_geometry_version":"nope","direction":"radiant_to_dire",
		"assignment_radius":1,"ambiguity_epsilon":0,"wave_cluster_gap":1,"wave_contact_gap":1,
		"depth":{"own_base_max":0.1,"own_side_max":0.2,"center_max":0.3,"enemy_side_max":0.4},
		"lanes":[{"id":"TOP","points":[[0,0],[1,1]]},{"id":"MID","points":[[0,0],[1,1]]},{"id":"BOT","points":[[0,0],[1,1]]}]
	}`), CurrentVersion)
	if err == nil {
		t.Fatal("expected map version mismatch")
	}
}

func TestLaneDepthBuckets(t *testing.T) {
	g := mustLanes(t)
	if g.Depth(-0.1) != DepthUnknown {
		t.Fatal("negative")
	}
	if g.Depth(0) != DepthOwnBase {
		t.Fatal("0")
	}
	if g.Depth(1) != DepthEnemyBase {
		t.Fatal("1")
	}
}

func mustLanes(t *testing.T) *LaneGeometry {
	t.Helper()
	g, err := LoadLanesCurrent()
	if err != nil {
		t.Fatal(err)
	}
	return g
}
