package recommender

import (
	"cmnet/internal/blend"
	"cmnet/internal/draft"
)

// blendEnemy folds the scout layers (§9) into a layer-1 distribution for an enemy
// action of the given kind.
func (r *Recommender) blendEnemy(dist []float64, action draft.Action) []float64 {
	if !r.scoutEnabled {
		return dist
	}
	_ = action
	return blend.Blend(dist, r.enemyAff2, r.enemyAff3, r.eventN, r.blendW)
}
