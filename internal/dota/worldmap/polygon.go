package worldmap

import "math"

const boundaryEps = 1.0

func pointInPolygon(poly []WorldPosition, p WorldPosition) bool {
	if len(poly) < 3 {
		return false
	}
	if onBoundary(poly, p) {
		return true
	}
	inside := false
	j := len(poly) - 1
	for i := 0; i < len(poly); i++ {
		yi, yj := poly[i].Y, poly[j].Y
		if (yi > p.Y) != (yj > p.Y) {
			denom := yj - yi
			if denom != 0 {
				xint := (poly[j].X-poly[i].X)*(p.Y-yi)/denom + poly[i].X
				if p.X < xint {
					inside = !inside
				}
			}
		}
		j = i
	}
	return inside
}

func onBoundary(poly []WorldPosition, p WorldPosition) bool {
	n := len(poly)
	for i := 0; i < n; i++ {
		if onSegment(poly[i], poly[(i+1)%n], p) {
			return true
		}
	}
	return false
}

func onSegment(a, b, p WorldPosition) bool {
	dx := b.X - a.X
	dy := b.Y - a.Y
	lab2 := dx*dx + dy*dy
	if lab2 == 0 {
		return Distance(a, p) <= boundaryEps
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / lab2
	if t < 0 || t > 1 {
		return false
	}
	qx := a.X + t*dx
	qy := a.Y + t*dy
	return math.Hypot(p.X-qx, p.Y-qy) <= boundaryEps
}
