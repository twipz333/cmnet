package worldmap

import (
	"fmt"
	"math"
)

// WorldPosition is a Dota world XY, compatible with GSI integer coords.
type WorldPosition struct {
	X float64
	Y float64
}

func FromInt(x, y int) WorldPosition {
	return WorldPosition{X: float64(x), Y: float64(y)}
}

func (p WorldPosition) Equal(q WorldPosition) bool {
	return p.X == q.X && p.Y == q.Y
}

func (p WorldPosition) String() string {
	return fmt.Sprintf("(%.0f, %.0f)", p.X, p.Y)
}

func Distance(a, b WorldPosition) float64 {
	return math.Hypot(a.X-b.X, a.Y-b.Y)
}

func (p WorldPosition) finite() bool {
	return !math.IsNaN(p.X) && !math.IsNaN(p.Y) && !math.IsInf(p.X, 0) && !math.IsInf(p.Y, 0)
}
