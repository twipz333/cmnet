package worldmap

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// CurrentRegionGraphVersion is the macro walking graph paired with CurrentVersion.
const CurrentRegionGraphVersion = "7.39-regiongraph-v1"

// RegionEdge is one declarative macro-region walking edge.
type RegionEdge struct {
	From          RegionID `json:"from"`
	To            RegionID `json:"to"`
	WalkDistance  float64  `json:"walk_distance"`
	Bidirectional bool     `json:"bidirectional"`
}

type regionGraphDoc struct {
	Version            string       `json:"version"`
	MapGeometryVersion string       `json:"map_geometry_version"`
	Nodes              []RegionID   `json:"nodes"`
	Edges              []RegionEdge `json:"edges"`
}

type graphArc struct {
	to   RegionID
	dist float64
}

// RegionGraph is a versioned, immutable macro walking graph over RegionID.
type RegionGraph struct {
	version    string
	mapVersion string
	nodes      []RegionID
	edges      []RegionEdge
	adj        map[RegionID][]graphArc
	apsp       map[RegionID]map[RegionID]float64
	next       map[RegionID]map[RegionID]RegionID
}

// LoadRegionGraphCurrent loads the graph paired with the current map geometry.
func LoadRegionGraphCurrent() (*RegionGraph, error) {
	return LoadRegionGraph(CurrentRegionGraphVersion)
}

// LoadRegionGraph reads a named graph dataset and checks it against CurrentVersion.
func LoadRegionGraph(version string) (*RegionGraph, error) {
	if version == "" {
		return nil, fmt.Errorf("worldmap: empty region graph version")
	}
	raw, err := dataFS.ReadFile("data/" + version + ".json")
	if err != nil {
		return nil, fmt.Errorf("worldmap: open region graph %s: %w", version, err)
	}
	geo, err := LoadCurrent()
	if err != nil {
		return nil, err
	}
	return parseRegionGraph(raw, geo, CurrentVersion)
}

func parseRegionGraph(raw []byte, geo *Geometry, expectMapVersion string) (*RegionGraph, error) {
	var doc regionGraphDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("worldmap: region graph json: %w", err)
	}
	if doc.Version == "" {
		return nil, fmt.Errorf("worldmap: region graph missing version")
	}
	if doc.MapGeometryVersion == "" {
		return nil, fmt.Errorf("worldmap: region graph missing map_geometry_version")
	}
	if expectMapVersion != "" && doc.MapGeometryVersion != expectMapVersion {
		return nil, fmt.Errorf("worldmap: region graph %s pairs with map %s, current map is %s",
			doc.Version, doc.MapGeometryVersion, expectMapVersion)
	}
	if geo == nil {
		return nil, fmt.Errorf("worldmap: region graph requires geometry")
	}
	if len(doc.Nodes) == 0 {
		return nil, fmt.Errorf("worldmap: region graph has no nodes")
	}
	g := &RegionGraph{
		version:    doc.Version,
		mapVersion: doc.MapGeometryVersion,
		edges:      append([]RegionEdge(nil), doc.Edges...),
		adj:        map[RegionID][]graphArc{},
		apsp:       map[RegionID]map[RegionID]float64{},
		next:       map[RegionID]map[RegionID]RegionID{},
	}
	seenNode := map[RegionID]struct{}{}
	for _, id := range doc.Nodes {
		if id == "" || id == RegionUnknown || id == RegionOther {
			return nil, fmt.Errorf("worldmap: invalid region graph node %q", id)
		}
		if !geo.HasRegion(id) {
			return nil, fmt.Errorf("worldmap: region graph node %s missing in geometry", id)
		}
		if _, dup := seenNode[id]; dup {
			return nil, fmt.Errorf("worldmap: duplicate region graph node %s", id)
		}
		seenNode[id] = struct{}{}
		g.nodes = append(g.nodes, id)
		g.adj[id] = nil
	}
	for _, id := range requiredRegions() {
		if _, ok := seenNode[id]; !ok {
			return nil, fmt.Errorf("worldmap: region graph missing required node %s", id)
		}
	}
	seenEdge := map[string]struct{}{}
	addArc := func(from, to RegionID, dist float64) error {
		key := string(from) + "\x00" + string(to)
		if _, dup := seenEdge[key]; dup {
			return fmt.Errorf("worldmap: duplicate region graph edge %s -> %s", from, to)
		}
		seenEdge[key] = struct{}{}
		g.adj[from] = append(g.adj[from], graphArc{to: to, dist: dist})
		return nil
	}
	for _, e := range doc.Edges {
		if _, ok := seenNode[e.From]; !ok {
			return nil, fmt.Errorf("worldmap: region graph edge from unknown node %s", e.From)
		}
		if _, ok := seenNode[e.To]; !ok {
			return nil, fmt.Errorf("worldmap: region graph edge to unknown node %s", e.To)
		}
		if e.From == e.To {
			return nil, fmt.Errorf("worldmap: self edge %s", e.From)
		}
		if math.IsNaN(e.WalkDistance) || math.IsInf(e.WalkDistance, 0) || e.WalkDistance <= 0 {
			return nil, fmt.Errorf("worldmap: edge %s -> %s has invalid walk_distance", e.From, e.To)
		}
		if err := addArc(e.From, e.To, e.WalkDistance); err != nil {
			return nil, err
		}
		if e.Bidirectional {
			if err := addArc(e.To, e.From, e.WalkDistance); err != nil {
				return nil, err
			}
		}
	}
	for _, id := range g.nodes {
		if len(g.adj[id]) == 0 {
			return nil, fmt.Errorf("worldmap: isolated region graph node %s", id)
		}
		sort.Slice(g.adj[id], func(i, j int) bool {
			if g.adj[id][i].dist == g.adj[id][j].dist {
				return g.adj[id][i].to < g.adj[id][j].to
			}
			return g.adj[id][i].dist < g.adj[id][j].dist
		})
	}
	comps := g.ConnectedComponents()
	if len(comps) != 1 {
		return nil, fmt.Errorf("worldmap: region graph must be connected, got %d components", len(comps))
	}
	g.precompute()
	return g, nil
}

func (g *RegionGraph) precompute() {
	for _, src := range g.nodes {
		dist, prev := g.shortestFrom(src)
		g.apsp[src] = dist
		nextRow := map[RegionID]RegionID{}
		for _, dst := range g.nodes {
			if src == dst {
				nextRow[dst] = src
				continue
			}
			if _, ok := dist[dst]; !ok {
				continue
			}
			path := reconstructPath(src, dst, prev)
			if len(path) >= 2 {
				nextRow[dst] = path[1]
			}
		}
		g.next[src] = nextRow
	}
}

func (g *RegionGraph) Version() string {
	if g == nil {
		return ""
	}
	return g.version
}

func (g *RegionGraph) MapGeometryVersion() string {
	if g == nil {
		return ""
	}
	return g.mapVersion
}

func (g *RegionGraph) Nodes() []RegionID {
	if g == nil {
		return nil
	}
	return append([]RegionID(nil), g.nodes...)
}

func (g *RegionGraph) Edges() []RegionEdge {
	if g == nil {
		return nil
	}
	return append([]RegionEdge(nil), g.edges...)
}

func (g *RegionGraph) ConnectedComponents() [][]RegionID {
	if g == nil {
		return nil
	}
	undir := map[RegionID][]RegionID{}
	for _, id := range g.nodes {
		undir[id] = nil
	}
	for from, arcs := range g.adj {
		for _, arc := range arcs {
			undir[from] = append(undir[from], arc.to)
			undir[arc.to] = append(undir[arc.to], from)
		}
	}
	for id := range undir {
		sort.Slice(undir[id], func(i, j int) bool { return undir[id][i] < undir[id][j] })
	}
	seen := map[RegionID]bool{}
	var comps [][]RegionID
	for _, start := range g.nodes {
		if seen[start] {
			continue
		}
		stack := []RegionID{start}
		seen[start] = true
		var comp []RegionID
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			comp = append(comp, v)
			for i := len(undir[v]) - 1; i >= 0; i-- {
				w := undir[v][i]
				if seen[w] {
					continue
				}
				seen[w] = true
				stack = append(stack, w)
			}
		}
		sort.Slice(comp, func(i, j int) bool { return comp[i] < comp[j] })
		comps = append(comps, comp)
	}
	sort.Slice(comps, func(i, j int) bool { return comps[i][0] < comps[j][0] })
	return comps
}

// ShortestWalkDistance returns the shortest macro walking distance.
func (g *RegionGraph) ShortestWalkDistance(from, to RegionID) (float64, bool) {
	if g == nil {
		return 0, false
	}
	row, ok := g.apsp[from]
	if !ok {
		return 0, false
	}
	d, ok := row[to]
	return d, ok
}

// ShortestWalkDistances returns all shortest distances from one source.
func (g *RegionGraph) ShortestWalkDistances(from RegionID) map[RegionID]float64 {
	if g == nil {
		return nil
	}
	row, ok := g.apsp[from]
	if !ok {
		return nil
	}
	out := make(map[RegionID]float64, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out
}

// ShortestWalkPath returns the deterministic shortest path.
func (g *RegionGraph) ShortestWalkPath(from, to RegionID) ([]RegionID, float64, bool) {
	if g == nil {
		return nil, 0, false
	}
	d, ok := g.ShortestWalkDistance(from, to)
	if !ok {
		return nil, 0, false
	}
	if from == to {
		return []RegionID{from}, 0, true
	}
	cur := from
	path := []RegionID{from}
	seen := map[RegionID]struct{}{from: {}}
	for cur != to {
		nxt, ok := g.next[cur][to]
		if !ok {
			return nil, 0, false
		}
		if _, dup := seen[nxt]; dup {
			return nil, 0, false
		}
		seen[nxt] = struct{}{}
		path = append(path, nxt)
		cur = nxt
	}
	return path, d, true
}

func reconstructPath(src, dst RegionID, prev map[RegionID]RegionID) []RegionID {
	if src == dst {
		return []RegionID{src}
	}
	var rev []RegionID
	cur := dst
	for {
		rev = append(rev, cur)
		if cur == src {
			break
		}
		p, ok := prev[cur]
		if !ok {
			return nil
		}
		cur = p
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func (g *RegionGraph) shortestFrom(src RegionID) (map[RegionID]float64, map[RegionID]RegionID) {
	dist := map[RegionID]float64{src: 0}
	prev := map[RegionID]RegionID{}
	bestPath := map[RegionID]string{src: string(src)}
	pq := &regionPQ{{node: src, dist: 0, pathKey: string(src)}}
	heap.Init(pq)
	for pq.Len() > 0 {
		it := heap.Pop(pq).(regionPQItem)
		if cur, ok := dist[it.node]; ok && (it.dist > cur+1e-9 || (math.Abs(it.dist-cur) <= 1e-9 && it.pathKey != bestPath[it.node])) {
			continue
		}
		for _, arc := range g.adj[it.node] {
			alt := it.dist + arc.dist
			key := it.pathKey + "\x00" + string(arc.to)
			cur, seen := dist[arc.to]
			if !seen || alt < cur-1e-9 || (math.Abs(alt-cur) <= 1e-9 && key < bestPath[arc.to]) {
				dist[arc.to] = alt
				prev[arc.to] = it.node
				bestPath[arc.to] = key
				heap.Push(pq, regionPQItem{node: arc.to, dist: alt, pathKey: key})
			}
		}
	}
	return dist, prev
}

type regionPQItem struct {
	node    RegionID
	dist    float64
	pathKey string
}

type regionPQ []regionPQItem

func (p regionPQ) Len() int { return len(p) }
func (p regionPQ) Less(i, j int) bool {
	if math.Abs(p[i].dist-p[j].dist) > 1e-9 {
		return p[i].dist < p[j].dist
	}
	return p[i].pathKey < p[j].pathKey
}
func (p regionPQ) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p *regionPQ) Push(x any)   { *p = append(*p, x.(regionPQItem)) }
func (p *regionPQ) Pop() any {
	old := *p
	n := len(old)
	it := old[n-1]
	*p = old[:n-1]
	return it
}
