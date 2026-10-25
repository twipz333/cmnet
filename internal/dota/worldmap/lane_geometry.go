package worldmap

import (
	"encoding/json"
	"fmt"
	"math"
)

// CurrentLanesVersion is the lane dataset this binary ships. It must pair with
// CurrentVersion (map geometry). Independent of the Advisor binary version.
const CurrentLanesVersion = "7.39-lanes-v1"

// LaneGeometry is a versioned, immutable set of lane polylines and thresholds.
// Load once; query many times.
type LaneGeometry struct {
	version          string
	mapVersion       string
	assignmentRadius float64
	ambiguityEpsilon float64
	waveClusterGap   float64
	waveContactGap   float64
	ownBaseMax       float64
	ownSideMax       float64
	centerMax        float64
	enemySideMax     float64
	paths            []lanePath
}

type laneFileDoc struct {
	Version            string         `json:"version"`
	MapGeometryVersion string         `json:"map_geometry_version"`
	DotaMap            string         `json:"dota_map"`
	Direction          string         `json:"direction"`
	AssignmentRadius   float64        `json:"assignment_radius"`
	AmbiguityEpsilon   float64        `json:"ambiguity_epsilon"`
	WaveClusterGap     float64        `json:"wave_cluster_gap"`
	WaveContactGap     float64        `json:"wave_contact_gap"`
	Depth              laneFileDepth  `json:"depth"`
	Lanes              []laneFileLane `json:"lanes"`
}

type laneFileDepth struct {
	OwnBaseMax   float64 `json:"own_base_max"`
	OwnSideMax   float64 `json:"own_side_max"`
	CenterMax    float64 `json:"center_max"`
	EnemySideMax float64 `json:"enemy_side_max"`
}

type laneFileLane struct {
	ID     string      `json:"id"`
	Points [][]float64 `json:"points"`
}

// LoadLanesCurrent loads the lane dataset paired with current map geometry.
func LoadLanesCurrent() (*LaneGeometry, error) {
	return LoadLanes(CurrentLanesVersion)
}

// LoadLanes reads a named lane dataset and checks it against CurrentVersion.
func LoadLanes(version string) (*LaneGeometry, error) {
	if version == "" {
		return nil, fmt.Errorf("worldmap: empty lane version")
	}
	raw, err := dataFS.ReadFile("data/" + version + ".json")
	if err != nil {
		return nil, fmt.Errorf("worldmap: open lanes %s: %w", version, err)
	}
	return parseLaneGeometry(raw, CurrentVersion)
}

func parseLaneGeometry(raw []byte, expectMapVersion string) (*LaneGeometry, error) {
	var doc laneFileDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("worldmap: lanes json: %w", err)
	}
	if doc.Version == "" {
		return nil, fmt.Errorf("worldmap: lanes missing version")
	}
	if doc.MapGeometryVersion == "" {
		return nil, fmt.Errorf("worldmap: lanes missing map_geometry_version")
	}
	if expectMapVersion != "" && doc.MapGeometryVersion != expectMapVersion {
		return nil, fmt.Errorf("worldmap: lanes %s pair with map %s, current map is %s",
			doc.Version, doc.MapGeometryVersion, expectMapVersion)
	}
	if doc.Direction != "" && doc.Direction != "radiant_to_dire" {
		return nil, fmt.Errorf("worldmap: lanes direction must be radiant_to_dire, got %q", doc.Direction)
	}
	if doc.AssignmentRadius <= 0 || doc.AmbiguityEpsilon < 0 {
		return nil, fmt.Errorf("worldmap: assignment_radius must be > 0 and ambiguity_epsilon >= 0")
	}
	if doc.WaveClusterGap <= 0 || doc.WaveContactGap <= 0 {
		return nil, fmt.Errorf("worldmap: wave_cluster_gap and wave_contact_gap must be > 0")
	}
	d := doc.Depth
	if !(d.OwnBaseMax > 0 && d.OwnSideMax > d.OwnBaseMax && d.CenterMax > d.OwnSideMax &&
		d.EnemySideMax > d.CenterMax && d.EnemySideMax < 1) {
		return nil, fmt.Errorf("worldmap: depth thresholds must be strictly increasing in (0,1)")
	}

	g := &LaneGeometry{
		version:          doc.Version,
		mapVersion:       doc.MapGeometryVersion,
		assignmentRadius: doc.AssignmentRadius,
		ambiguityEpsilon: doc.AmbiguityEpsilon,
		waveClusterGap:   doc.WaveClusterGap,
		waveContactGap:   doc.WaveContactGap,
		ownBaseMax:       d.OwnBaseMax,
		ownSideMax:       d.OwnSideMax,
		centerMax:        d.CenterMax,
		enemySideMax:     d.EnemySideMax,
	}
	seen := map[LaneID]struct{}{}
	for _, ln := range doc.Lanes {
		id := LaneID(ln.ID)
		if id == "" || id == LaneUnknown {
			return nil, fmt.Errorf("worldmap: invalid lane id %q", ln.ID)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("worldmap: duplicate lane %s", id)
		}
		seen[id] = struct{}{}
		if len(ln.Points) < 2 {
			return nil, fmt.Errorf("worldmap: lane %s needs >= 2 points", id)
		}
		pts := make([]WorldPosition, len(ln.Points))
		for i, pt := range ln.Points {
			if len(pt) != 2 {
				return nil, fmt.Errorf("worldmap: lane %s point %d: want [x,y]", id, i)
			}
			p := WorldPosition{X: pt[0], Y: pt[1]}
			if !p.finite() {
				return nil, fmt.Errorf("worldmap: lane %s point %d: non-finite", id, i)
			}
			pts[i] = p
		}
		path := buildPath(id, pts)
		if path.length <= 0 || math.IsInf(path.length, 0) {
			return nil, fmt.Errorf("worldmap: lane %s path length must be > 0", id)
		}
		g.paths = append(g.paths, path)
	}
	for _, id := range requiredLanes() {
		if _, ok := seen[id]; !ok {
			return nil, fmt.Errorf("worldmap: missing required lane %s", id)
		}
	}
	// Stable query order: TOP, MID, BOT — not JSON object iteration.
	order := map[LaneID]int{LaneTop: 0, LaneMid: 1, LaneBot: 2}
	sorted := make([]lanePath, 3)
	for _, p := range g.paths {
		i, ok := order[p.id]
		if !ok {
			return nil, fmt.Errorf("worldmap: unsupported lane %s", p.id)
		}
		sorted[i] = p
	}
	g.paths = sorted
	return g, nil
}

func (g *LaneGeometry) Version() string {
	if g == nil {
		return ""
	}
	return g.version
}

func (g *LaneGeometry) MapGeometryVersion() string {
	if g == nil {
		return ""
	}
	return g.mapVersion
}

func (g *LaneGeometry) AssignmentRadius() float64 {
	if g == nil {
		return 0
	}
	return g.assignmentRadius
}

func (g *LaneGeometry) AmbiguityEpsilon() float64 {
	if g == nil {
		return 0
	}
	return g.ambiguityEpsilon
}

func (g *LaneGeometry) WaveClusterGap() float64 {
	if g == nil {
		return 0
	}
	return g.waveClusterGap
}

func (g *LaneGeometry) WaveContactGap() float64 {
	if g == nil {
		return 0
	}
	return g.waveContactGap
}

// Project assigns p to the nearest lane path. A missing/non-finite point is
// UNKNOWN. (0,0) is a real coordinate and is classified.
func (g *LaneGeometry) Project(p WorldPosition) LaneProjection {
	out := LaneProjection{Lane: LaneUnknown}
	if g == nil || !p.finite() {
		out.TooFar = true
		return out
	}
	type scored struct {
		path lanePath
		hit  polyHit
	}
	var best, second scored
	best.hit.dist = math.Inf(1)
	second.hit.dist = math.Inf(1)
	for _, path := range g.paths {
		hit := projectToPolyline(p, path)
		if hit.dist < best.hit.dist {
			second = best
			best = scored{path: path, hit: hit}
			continue
		}
		if hit.dist < second.hit.dist {
			second = scored{path: path, hit: hit}
		}
	}
	out.Nearest = best.hit.nearest
	out.Distance = best.hit.dist
	out.AbsoluteDistance = best.hit.absDist
	if best.path.length > 0 {
		out.AbsoluteProgress = AbsoluteProgress(best.hit.absDist / best.path.length)
	}
	if best.hit.dist > g.assignmentRadius {
		out.TooFar = true
		return out
	}
	if !math.IsInf(second.hit.dist, 1) && second.hit.dist-best.hit.dist <= g.ambiguityEpsilon {
		out.Ambiguous = true
		return out
	}
	out.Lane = best.path.id
	out.Assigned = true
	return out
}

// Depth buckets team-relative progress. Values outside [0,1] are UNKNOWN.
func (g *LaneGeometry) Depth(tp TeamProgress) LaneDepth {
	if g == nil {
		return DepthUnknown
	}
	x := float64(tp)
	if math.IsNaN(x) || math.IsInf(x, 0) || x < 0 || x > 1 {
		return DepthUnknown
	}
	switch {
	case x <= g.ownBaseMax:
		return DepthOwnBase
	case x <= g.ownSideMax:
		return DepthOwnSide
	case x <= g.centerMax:
		return DepthCenter
	case x <= g.enemySideMax:
		return DepthEnemySide
	default:
		return DepthEnemyBase
	}
}

// PathPoints returns a copy of the polyline for tests/debug.
func (g *LaneGeometry) PathPoints(id LaneID) []WorldPosition {
	if g == nil {
		return nil
	}
	for _, p := range g.paths {
		if p.id == id {
			return append([]WorldPosition(nil), p.pts...)
		}
	}
	return nil
}

func (g *LaneGeometry) PathLength(id LaneID) float64 {
	if g == nil {
		return 0
	}
	for _, p := range g.paths {
		if p.id == id {
			return p.length
		}
	}
	return 0
}
