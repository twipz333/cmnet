package worldmap

import (
	"strings"
	"testing"
)

func TestLoadRegionGraphCurrentPairsWithMap(t *testing.T) {
	g, err := LoadRegionGraphCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if g.Version() != CurrentRegionGraphVersion {
		t.Fatalf("version %s", g.Version())
	}
	if g.MapGeometryVersion() != CurrentVersion {
		t.Fatalf("map version %s want %s", g.MapGeometryVersion(), CurrentVersion)
	}
	if len(g.Nodes()) != len(requiredRegions()) {
		t.Fatalf("nodes=%d want=%d", len(g.Nodes()), len(requiredRegions()))
	}
	if len(g.ConnectedComponents()) != 1 {
		t.Fatalf("components=%d", len(g.ConnectedComponents()))
	}
}

func TestRegionGraphRejectsVersionMismatch(t *testing.T) {
	geo, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseRegionGraph([]byte(`{"version":"x","map_geometry_version":"nope","nodes":["RADIANT_BASE"],"edges":[]}`), geo, CurrentVersion)
	if err == nil || !strings.Contains(err.Error(), "pairs with map") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegionGraphRejectsUnknownNode(t *testing.T) {
	geo, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":"x","map_geometry_version":"7.39-macro-v1","nodes":["NOPE"],"edges":[]}`)
	_, err = parseRegionGraph(raw, geo, CurrentVersion)
	if err == nil || !strings.Contains(err.Error(), "missing in geometry") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegionGraphRejectsDuplicateEdge(t *testing.T) {
	geo, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{
      "version":"x",
      "map_geometry_version":"7.39-macro-v1",
      "nodes":[
        "RADIANT_BASE","DIRE_BASE","RADIANT_HIGHGROUND","DIRE_HIGHGROUND","RADIANT_TRIANGLE","DIRE_TRIANGLE",
        "TOP_LANE_RADIANT","TOP_LANE_CENTER","TOP_LANE_DIRE","MID_LANE_RADIANT","MID_LANE_CENTER","MID_LANE_DIRE",
        "BOT_LANE_RADIANT","BOT_LANE_CENTER","BOT_LANE_DIRE","RADIANT_MAIN_JUNGLE","DIRE_MAIN_JUNGLE",
        "RADIANT_OFFLANE_JUNGLE","DIRE_OFFLANE_JUNGLE","TOP_RIVER","BOT_RIVER"
      ],
      "edges":[
        {"from":"RADIANT_BASE","to":"RADIANT_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"RADIANT_BASE","walk_distance":1,"bidirectional":false},
        {"from":"DIRE_BASE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"TOP_LANE_RADIANT","walk_distance":1,"bidirectional":true},
        {"from":"TOP_LANE_RADIANT","to":"TOP_LANE_CENTER","walk_distance":1,"bidirectional":true},
        {"from":"TOP_LANE_CENTER","to":"TOP_LANE_DIRE","walk_distance":1,"bidirectional":true},
        {"from":"TOP_LANE_DIRE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"MID_LANE_RADIANT","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_RADIANT","to":"MID_LANE_CENTER","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_CENTER","to":"MID_LANE_DIRE","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_DIRE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"BOT_LANE_RADIANT","walk_distance":1,"bidirectional":true},
        {"from":"BOT_LANE_RADIANT","to":"BOT_LANE_CENTER","walk_distance":1,"bidirectional":true},
        {"from":"BOT_LANE_CENTER","to":"BOT_LANE_DIRE","walk_distance":1,"bidirectional":true},
        {"from":"BOT_LANE_DIRE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"RADIANT_MAIN_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_MAIN_JUNGLE","to":"RADIANT_TRIANGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"RADIANT_OFFLANE_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_OFFLANE_JUNGLE","to":"TOP_RIVER","walk_distance":1,"bidirectional":true},
        {"from":"TOP_RIVER","to":"DIRE_MAIN_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"DIRE_MAIN_JUNGLE","to":"DIRE_TRIANGLE","walk_distance":1,"bidirectional":true},
        {"from":"BOT_LANE_CENTER","to":"DIRE_OFFLANE_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_TRIANGLE","to":"BOT_RIVER","walk_distance":1,"bidirectional":true}
      ]
    }`)
	_, err = parseRegionGraph(raw, geo, CurrentVersion)
	if err == nil || !strings.Contains(err.Error(), "duplicate region graph edge") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegionGraphRejectsInvalidDistance(t *testing.T) {
	geo, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{
      "version":"x",
      "map_geometry_version":"7.39-macro-v1",
      "nodes":[
        "RADIANT_BASE","DIRE_BASE","RADIANT_HIGHGROUND","DIRE_HIGHGROUND","RADIANT_TRIANGLE","DIRE_TRIANGLE",
        "TOP_LANE_RADIANT","TOP_LANE_CENTER","TOP_LANE_DIRE","MID_LANE_RADIANT","MID_LANE_CENTER","MID_LANE_DIRE",
        "BOT_LANE_RADIANT","BOT_LANE_CENTER","BOT_LANE_DIRE","RADIANT_MAIN_JUNGLE","DIRE_MAIN_JUNGLE",
        "RADIANT_OFFLANE_JUNGLE","DIRE_OFFLANE_JUNGLE","TOP_RIVER","BOT_RIVER"
      ],
      "edges":[{"from":"RADIANT_BASE","to":"RADIANT_HIGHGROUND","walk_distance":0,"bidirectional":true}]
    }`)
	_, err = parseRegionGraph(raw, geo, CurrentVersion)
	if err == nil || !strings.Contains(err.Error(), "invalid walk_distance") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegionGraphRejectsIsolatedNode(t *testing.T) {
	geo, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{
      "version":"x",
      "map_geometry_version":"7.39-macro-v1",
      "nodes":[
        "RADIANT_BASE","DIRE_BASE","RADIANT_HIGHGROUND","DIRE_HIGHGROUND","RADIANT_TRIANGLE","DIRE_TRIANGLE",
        "TOP_LANE_RADIANT","TOP_LANE_CENTER","TOP_LANE_DIRE","MID_LANE_RADIANT","MID_LANE_CENTER","MID_LANE_DIRE",
        "BOT_LANE_RADIANT","BOT_LANE_CENTER","BOT_LANE_DIRE","RADIANT_MAIN_JUNGLE","DIRE_MAIN_JUNGLE",
        "RADIANT_OFFLANE_JUNGLE","DIRE_OFFLANE_JUNGLE","TOP_RIVER","BOT_RIVER"
      ],
      "edges":[
        {"from":"RADIANT_BASE","to":"RADIANT_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"TOP_LANE_RADIANT","walk_distance":1,"bidirectional":true},
        {"from":"TOP_LANE_RADIANT","to":"TOP_LANE_CENTER","walk_distance":1,"bidirectional":true},
        {"from":"TOP_LANE_CENTER","to":"TOP_LANE_DIRE","walk_distance":1,"bidirectional":true},
        {"from":"TOP_LANE_DIRE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"DIRE_HIGHGROUND","to":"DIRE_BASE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"MID_LANE_RADIANT","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_RADIANT","to":"MID_LANE_CENTER","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_CENTER","to":"MID_LANE_DIRE","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_DIRE","to":"DIRE_TRIANGLE","walk_distance":1,"bidirectional":true},
        {"from":"DIRE_TRIANGLE","to":"DIRE_MAIN_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"RADIANT_MAIN_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_MAIN_JUNGLE","to":"RADIANT_TRIANGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_TRIANGLE","to":"BOT_RIVER","walk_distance":1,"bidirectional":true},
        {"from":"BOT_RIVER","to":"BOT_LANE_CENTER","walk_distance":1,"bidirectional":true},
        {"from":"BOT_LANE_CENTER","to":"BOT_LANE_RADIANT","walk_distance":1,"bidirectional":true},
        {"from":"BOT_LANE_CENTER","to":"DIRE_OFFLANE_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"RADIANT_OFFLANE_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_OFFLANE_JUNGLE","to":"TOP_RIVER","walk_distance":1,"bidirectional":true}
      ]
    }`)
	_, err = parseRegionGraph(raw, geo, CurrentVersion)
	if err == nil || !strings.Contains(err.Error(), "isolated region graph node") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegionGraphShortestPathAndBidirectional(t *testing.T) {
	g, err := LoadRegionGraphCurrent()
	if err != nil {
		t.Fatal(err)
	}
	d1, ok := g.ShortestWalkDistance(RegionMidLaneRadiant, RegionTopRiver)
	if !ok || d1 <= 0 {
		t.Fatalf("d1=%v ok=%v", d1, ok)
	}
	d2, ok := g.ShortestWalkDistance(RegionTopRiver, RegionMidLaneRadiant)
	if !ok || d2 != d1 {
		t.Fatalf("d2=%v d1=%v ok=%v", d2, d1, ok)
	}
	path, dist, ok := g.ShortestWalkPath(RegionMidLaneRadiant, RegionTopRiver)
	if !ok || dist != d1 || len(path) < 2 {
		t.Fatalf("path=%v dist=%v ok=%v", path, dist, ok)
	}
	if path[0] != RegionMidLaneRadiant || path[len(path)-1] != RegionTopRiver {
		t.Fatalf("path=%v", path)
	}
}

func TestRegionGraphDeterministicTiePath(t *testing.T) {
	geo, err := LoadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{
      "version":"tie",
      "map_geometry_version":"7.39-macro-v1",
      "nodes":[
        "RADIANT_BASE","DIRE_BASE","RADIANT_HIGHGROUND","DIRE_HIGHGROUND","RADIANT_TRIANGLE","DIRE_TRIANGLE",
        "TOP_LANE_RADIANT","TOP_LANE_CENTER","TOP_LANE_DIRE","MID_LANE_RADIANT","MID_LANE_CENTER","MID_LANE_DIRE",
        "BOT_LANE_RADIANT","BOT_LANE_CENTER","BOT_LANE_DIRE","RADIANT_MAIN_JUNGLE","DIRE_MAIN_JUNGLE",
        "RADIANT_OFFLANE_JUNGLE","DIRE_OFFLANE_JUNGLE","TOP_RIVER","BOT_RIVER"
      ],
      "edges":[
        {"from":"RADIANT_BASE","to":"RADIANT_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"DIRE_BASE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"TOP_LANE_RADIANT","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"MID_LANE_RADIANT","walk_distance":1,"bidirectional":true},
        {"from":"TOP_LANE_RADIANT","to":"TOP_RIVER","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_RADIANT","to":"TOP_RIVER","walk_distance":1,"bidirectional":true},
        {"from":"TOP_RIVER","to":"TOP_LANE_CENTER","walk_distance":1,"bidirectional":true},
        {"from":"TOP_LANE_CENTER","to":"TOP_LANE_DIRE","walk_distance":1,"bidirectional":true},
        {"from":"TOP_LANE_DIRE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_RADIANT","to":"MID_LANE_CENTER","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_CENTER","to":"MID_LANE_DIRE","walk_distance":1,"bidirectional":true},
        {"from":"MID_LANE_DIRE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"BOT_LANE_RADIANT","walk_distance":1,"bidirectional":true},
        {"from":"BOT_LANE_RADIANT","to":"BOT_LANE_CENTER","walk_distance":1,"bidirectional":true},
        {"from":"BOT_LANE_CENTER","to":"BOT_LANE_DIRE","walk_distance":1,"bidirectional":true},
        {"from":"BOT_LANE_DIRE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"RADIANT_MAIN_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_MAIN_JUNGLE","to":"RADIANT_TRIANGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_TRIANGLE","to":"BOT_RIVER","walk_distance":1,"bidirectional":true},
        {"from":"BOT_RIVER","to":"DIRE_OFFLANE_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"DIRE_OFFLANE_JUNGLE","to":"DIRE_HIGHGROUND","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_HIGHGROUND","to":"RADIANT_OFFLANE_JUNGLE","walk_distance":1,"bidirectional":true},
        {"from":"RADIANT_OFFLANE_JUNGLE","to":"TOP_RIVER","walk_distance":2,"bidirectional":true},
        {"from":"TOP_RIVER","to":"DIRE_TRIANGLE","walk_distance":2,"bidirectional":true},
        {"from":"DIRE_TRIANGLE","to":"DIRE_MAIN_JUNGLE","walk_distance":1,"bidirectional":true}
      ]
    }`)
	g, err := parseRegionGraph(raw, geo, CurrentVersion)
	if err != nil {
		t.Fatal(err)
	}
	path, _, ok := g.ShortestWalkPath(RegionRadiantHighground, RegionTopRiver)
	if !ok {
		t.Fatal("missing path")
	}
	want := []RegionID{RegionRadiantHighground, RegionMidLaneRadiant, RegionTopRiver}
	if len(path) != len(want) {
		t.Fatalf("path=%v want=%v", path, want)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Fatalf("path=%v want=%v", path, want)
		}
	}
}
