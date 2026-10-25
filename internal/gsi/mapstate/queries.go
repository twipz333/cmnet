package mapstate

import "cmnet/internal/dota/worldmap"

// CountAlliesWithin counts unique allies with at least one current position inside radius.
func (s State) CountAlliesWithin(p worldmap.WorldPosition, radius float64) int {
	n := 0
	for _, a := range s.Allies {
		if anyWithin(a.Positions, p, radius) {
			n++
		}
	}
	return n
}

// CountVisibleEnemiesWithin counts unique visible enemies with a current position inside radius.
func (s State) CountVisibleEnemiesWithin(p worldmap.WorldPosition, radius float64) int {
	n := 0
	for _, e := range s.Enemies {
		if !e.Visible {
			continue
		}
		if anyWithin(e.Positions, p, radius) {
			n++
		}
	}
	return n
}

// IsHeroFarFromLandmark reports whether every current position of hero is farther than dist.
// Missing heroes (no current positions) return false: we do not know they are far.
func (s State) IsHeroFarFromLandmark(hero string, g *worldmap.Geometry, id worldmap.LandmarkID, dist float64) bool {
	if g == nil {
		return false
	}
	lm, ok := g.LandmarkPosition(id)
	if !ok {
		return false
	}
	for _, h := range append(append([]HeroSpatial{}, s.Allies...), s.Enemies...) {
		if h.Hero != hero {
			continue
		}
		if len(h.Positions) == 0 {
			return false
		}
		for _, p := range h.Positions {
			if worldmap.Distance(p, lm) <= dist {
				return false
			}
		}
		return true
	}
	return false
}

func anyWithin(pts []worldmap.WorldPosition, c worldmap.WorldPosition, r float64) bool {
	for _, p := range pts {
		if worldmap.Distance(p, c) <= r {
			return true
		}
	}
	return false
}
