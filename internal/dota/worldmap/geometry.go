package worldmap

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Geometry is a versioned, immutable map dataset. Load once; query many times.
type Geometry struct {
	version    string
	priority   []RegionID
	regions    map[RegionID][]WorldPosition
	landmarks  map[LandmarkID]landmark
	radii      Radii
	groupRatio float64
}

type landmark struct {
	id   LandmarkID
	kind string
	pos  WorldPosition
}

type fileDoc struct {
	Version               string         `json:"version"`
	DotaMap               string         `json:"dota_map"`
	Radii                 Radii          `json:"radii"`
	GroupedRatioThreshold float64        `json:"grouped_ratio_threshold"`
	RegionPriority        []string       `json:"region_priority"`
	Regions               []fileRegion   `json:"regions"`
	Landmarks             []fileLandmark `json:"landmarks"`
}

type fileRegion struct {
	ID      string      `json:"id"`
	Polygon [][]float64 `json:"polygon"`
}

type fileLandmark struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Position []float64 `json:"position"`
}

// LoadCurrent loads the geometry this binary treats as current.
func LoadCurrent() (*Geometry, error) {
	return Load(CurrentVersion)
}

// Load reads a named dataset from the embedded files (version == filename stem).
func Load(version string) (*Geometry, error) {
	if version == "" {
		return nil, fmt.Errorf("worldmap: empty version")
	}
	raw, err := dataFS.ReadFile("data/" + version + ".json")
	if err != nil {
		return nil, fmt.Errorf("worldmap: open %s: %w", version, err)
	}
	return parseGeometry(raw)
}

func parseGeometry(raw []byte) (*Geometry, error) {
	var doc fileDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("worldmap: json: %w", err)
	}
	if doc.Version == "" {
		return nil, fmt.Errorf("worldmap: missing version")
	}
	g := &Geometry{
		version:    doc.Version,
		regions:    map[RegionID][]WorldPosition{},
		landmarks:  map[LandmarkID]landmark{},
		radii:      doc.Radii,
		groupRatio: doc.GroupedRatioThreshold,
	}
	if g.groupRatio <= 0 || g.groupRatio > 1 {
		return nil, fmt.Errorf("worldmap: grouped_ratio_threshold must be in (0,1]")
	}
	if g.radii.Roshan <= 0 || g.radii.Grouped <= 0 || g.radii.Base <= 0 {
		return nil, fmt.Errorf("worldmap: radii must be positive")
	}
	seen := map[RegionID]struct{}{}
	for _, r := range doc.Regions {
		id := RegionID(r.ID)
		if id == "" || id == RegionUnknown || id == RegionOther {
			return nil, fmt.Errorf("worldmap: invalid region id %q", r.ID)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("worldmap: duplicate region %s", id)
		}
		seen[id] = struct{}{}
		poly, err := parsePoly(r.Polygon)
		if err != nil {
			return nil, fmt.Errorf("worldmap: region %s: %w", id, err)
		}
		g.regions[id] = poly
	}
	if len(doc.RegionPriority) == 0 {
		return nil, fmt.Errorf("worldmap: empty region_priority")
	}
	usedPri := map[RegionID]struct{}{}
	for _, s := range doc.RegionPriority {
		id := RegionID(s)
		if _, ok := g.regions[id]; !ok {
			return nil, fmt.Errorf("worldmap: priority references unknown region %s", id)
		}
		if _, dup := usedPri[id]; dup {
			return nil, fmt.Errorf("worldmap: duplicate priority %s", id)
		}
		usedPri[id] = struct{}{}
		g.priority = append(g.priority, id)
	}
	for _, id := range requiredRegions() {
		if _, ok := g.regions[id]; !ok {
			return nil, fmt.Errorf("worldmap: missing required region %s", id)
		}
	}
	lmSeen := map[LandmarkID]struct{}{}
	for _, lm := range doc.Landmarks {
		id := LandmarkID(lm.ID)
		if id == "" {
			return nil, fmt.Errorf("worldmap: empty landmark id")
		}
		if _, ok := lmSeen[id]; ok {
			return nil, fmt.Errorf("worldmap: duplicate landmark %s", id)
		}
		lmSeen[id] = struct{}{}
		if len(lm.Position) != 2 {
			return nil, fmt.Errorf("worldmap: landmark %s: position must be [x,y]", id)
		}
		pos := WorldPosition{X: lm.Position[0], Y: lm.Position[1]}
		if !pos.finite() {
			return nil, fmt.Errorf("worldmap: landmark %s: non-finite position", id)
		}
		g.landmarks[id] = landmark{id: id, kind: lm.Kind, pos: pos}
	}
	for _, id := range requiredLandmarks() {
		if _, ok := g.landmarks[id]; !ok {
			return nil, fmt.Errorf("worldmap: missing required landmark %s", id)
		}
	}
	return g, nil
}

func parsePoly(in [][]float64) ([]WorldPosition, error) {
	if len(in) < 3 {
		return nil, fmt.Errorf("polygon needs >= 3 points")
	}
	out := make([]WorldPosition, len(in))
	for i, pt := range in {
		if len(pt) != 2 {
			return nil, fmt.Errorf("point %d: want [x,y]", i)
		}
		p := WorldPosition{X: pt[0], Y: pt[1]}
		if !p.finite() {
			return nil, fmt.Errorf("point %d: non-finite", i)
		}
		out[i] = p
	}
	return out, nil
}

func (g *Geometry) Version() string {
	if g == nil {
		return ""
	}
	return g.version
}

func (g *Geometry) Radii() Radii {
	if g == nil {
		return Radii{}
	}
	return g.radii
}

func (g *Geometry) GroupedRatioThreshold() float64 {
	if g == nil {
		return 0
	}
	return g.groupRatio
}

func (g *Geometry) HasRegion(id RegionID) bool {
	if g == nil {
		return false
	}
	_, ok := g.regions[id]
	return ok
}

func (g *Geometry) RegionIDs() []RegionID {
	if g == nil {
		return nil
	}
	out := make([]RegionID, 0, len(g.regions))
	for id := range g.regions {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// RegionAt returns the first matching macro region in priority order.
// A known coordinate that matches nothing is OTHER. Callers pass UNKNOWN themselves
// when there is no position.
func (g *Geometry) RegionAt(p WorldPosition) RegionID {
	if g == nil || !p.finite() {
		return RegionOther
	}
	for _, id := range g.priority {
		if pointInPolygon(g.regions[id], p) {
			return id
		}
	}
	return RegionOther
}

func (g *Geometry) LandmarkPosition(id LandmarkID) (WorldPosition, bool) {
	if g == nil {
		return WorldPosition{}, false
	}
	lm, ok := g.landmarks[id]
	return lm.pos, ok
}

func (g *Geometry) DistanceToLandmark(p WorldPosition, id LandmarkID) (float64, bool) {
	lm, ok := g.LandmarkPosition(id)
	if !ok {
		return 0, false
	}
	return Distance(p, lm), true
}

func (g *Geometry) WithinRadius(p WorldPosition, id LandmarkID, radius float64) bool {
	d, ok := g.DistanceToLandmark(p, id)
	return ok && d <= radius
}

func (g *Geometry) RoshanPits() []LandmarkID {
	return []LandmarkID{LandmarkRoshanPitA, LandmarkRoshanPitB}
}

func (g *Geometry) LandmarkIDs() []LandmarkID {
	if g == nil {
		return nil
	}
	out := make([]LandmarkID, 0, len(g.landmarks))
	for id := range g.landmarks {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Classify is RegionAt for a present coordinate. Missing positions are UNKNOWN.
func Classify(g *Geometry, p *WorldPosition) RegionID {
	if p == nil {
		return RegionUnknown
	}
	if g == nil {
		return RegionOther
	}
	return g.RegionAt(*p)
}

// RegionPolygon returns a copy of the region polygon vertices, if known.
func (g *Geometry) RegionPolygon(id RegionID) ([]WorldPosition, bool) {
	if g == nil {
		return nil, false
	}
	poly, ok := g.regions[id]
	if !ok || len(poly) == 0 {
		return nil, false
	}
	out := make([]WorldPosition, len(poly))
	copy(out, poly)
	return out, true
}
