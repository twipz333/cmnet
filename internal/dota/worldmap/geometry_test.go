package worldmap

import (
	"math"
	"testing"
)

func TestLoadCurrent(t *testing.T) {
	g, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if g.Version() != CurrentVersion {
		t.Fatalf("version %s", g.Version())
	}
}

func TestRegionAnchors(t *testing.T) {
	g, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		x, y int
		want RegionID
	}{
		{"radiant ancient", -5920, -5352, RegionRadiantBase},
		{"dire ancient", 5528, 5000, RegionDireBase},
		{"radiant t3 mid", -4640, -4144, RegionRadiantHighground},
		{"dire t3 mid", 4272, 3759, RegionDireHighground},
		{"radiant t1 mid", -1544, -1408, RegionMidLaneRadiant},
		{"dire t1 mid", 524, 652, RegionMidLaneDire},
		{"mid origin", 0, 0, RegionMidLaneCenter},
		{"radiant t1 top", -6336, 1856, RegionTopLaneRadiant},
		{"dire t1 top", -5274, 6036, RegionTopLaneDire},
		{"radiant t1 bot", 4859, -6379, RegionBotLaneRadiant},
		{"dire t1 bot", 6269, -2240, RegionBotLaneDire},
		{"bot river / rosh b", 2859, -2764, RegionBotRiver},
		{"outside map", 12000, 0, RegionOther},
		{"dire main jungle leftover", 8000, 4000, RegionDireMainJungle},
		{"radiant offlane jungle", -3000, 0, RegionRadiantOfflaneJungle},
		{"negative far", -12000, -12000, RegionOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := g.RegionAt(FromInt(tc.x, tc.y))
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestUnknownVsOther(t *testing.T) {
	g, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if Classify(g, nil) != RegionUnknown {
		t.Fatal("missing position must be UNKNOWN")
	}
	p := FromInt(12000, 0)
	if Classify(g, &p) != RegionOther {
		t.Fatal("known coord outside polygons must be OTHER")
	}
}

func TestOriginIsNotUnknown(t *testing.T) {
	g, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	got := g.RegionAt(FromInt(0, 0))
	if got == RegionUnknown {
		t.Fatal("(0,0) must be classified, not UNKNOWN")
	}
}

func TestBoundaryAndVertex(t *testing.T) {
	g, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	// MID_LANE_CENTER is rect(-600,-600,600,600). Corners and edges are inside.
	for _, p := range []WorldPosition{
		FromInt(-600, -600),
		FromInt(600, 600),
		FromInt(0, 600),
		FromInt(-600, 0),
	} {
		if got := g.RegionAt(p); got != RegionMidLaneCenter {
			t.Fatalf("%v got %s", p, got)
		}
	}
}

func TestNearRegionBorderDeterministic(t *testing.T) {
	g, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	a := g.RegionAt(FromInt(-601, 0))
	b := g.RegionAt(FromInt(-601, 0))
	if a != b {
		t.Fatal("classification must be deterministic")
	}
}

func TestLandmarks(t *testing.T) {
	g, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	pitB, ok := g.LandmarkPosition(LandmarkRoshanPitB)
	if !ok {
		t.Fatal("missing pit B")
	}
	if pitB.X != 2859 || pitB.Y != -2764 {
		t.Fatalf("pit B %v", pitB)
	}
	if !g.WithinRadius(pitB, LandmarkRoshanPitB, 1) {
		t.Fatal("pit center must be within radius 1")
	}
	d, ok := g.DistanceToLandmark(FromInt(2859, -2764), LandmarkRoshanPitB)
	if !ok || d != 0 {
		t.Fatalf("distance %v %v", d, ok)
	}
	if _, ok := g.LandmarkPosition("nope"); ok {
		t.Fatal("unknown landmark")
	}
	if g.WithinRadius(FromInt(0, 0), LandmarkRoshanPitB, 10) {
		t.Fatal("origin is not in the bot pit")
	}
}

func TestRejectsBrokenGeometry(t *testing.T) {
	_, err := parseGeometry([]byte(`{"version":"x","radii":{"roshan":1,"tormentor":1,"base":1,"grouped":1},"grouped_ratio_threshold":0.5,"region_priority":[],"regions":[],"landmarks":[]}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDuplicateRegionRejected(t *testing.T) {
	raw := []byte(`{
		"version":"x",
		"radii":{"roshan":1,"tormentor":1,"base":1,"grouped":1},
		"grouped_ratio_threshold":0.5,
		"region_priority":["RADIANT_BASE"],
		"regions":[
			{"id":"RADIANT_BASE","polygon":[[0,0],[1,0],[0,1]]},
			{"id":"RADIANT_BASE","polygon":[[0,0],[1,0],[0,1]]}
		],
		"landmarks":[]
	}`)
	if _, err := parseGeometry(raw); err == nil {
		t.Fatal("duplicate region must fail")
	}
}

func TestFiniteGuard(t *testing.T) {
	p := WorldPosition{X: math.NaN(), Y: 0}
	g, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if g.RegionAt(p) != RegionOther {
		t.Fatal("NaN coordinate is not a real region")
	}
}
