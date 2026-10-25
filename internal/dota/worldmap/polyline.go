package worldmap

import "math"

type lanePath struct {
	id     LaneID
	pts    []WorldPosition
	length float64
	prefix []float64
}

func buildPath(id LaneID, pts []WorldPosition) lanePath {
	pref := make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		pref[i] = pref[i-1] + Distance(pts[i-1], pts[i])
	}
	return lanePath{id: id, pts: pts, length: pref[len(pref)-1], prefix: pref}
}

type polyHit struct {
	nearest WorldPosition
	dist    float64
	absDist float64
}

func projectToPolyline(p WorldPosition, path lanePath) polyHit {
	best := polyHit{dist: math.Inf(1)}
	for i := 0; i < len(path.pts)-1; i++ {
		a, b := path.pts[i], path.pts[i+1]
		hit, t := projectToSegment(p, a, b)
		d := Distance(p, hit)
		ad := path.prefix[i] + t*Distance(a, b)
		if d < best.dist {
			best = polyHit{nearest: hit, dist: d, absDist: ad}
		}
	}
	return best
}

func projectToSegment(p, a, b WorldPosition) (WorldPosition, float64) {
	abx := b.X - a.X
	aby := b.Y - a.Y
	ab2 := abx*abx + aby*aby
	if ab2 == 0 {
		return a, 0
	}
	t := ((p.X-a.X)*abx + (p.Y-a.Y)*aby) / ab2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return WorldPosition{X: a.X + t*abx, Y: a.Y + t*aby}, t
}
