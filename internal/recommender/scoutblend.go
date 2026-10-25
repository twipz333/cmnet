package recommender

import (
	"cmnet/internal/blend"
	"cmnet/internal/draft"
)

// blendEnemy folds the scout layers (§9) into a layer-1 distribution for an
// enemy action of the given kind.
//
// Picks and bans need different evidence. The scout vectors aff2 (what the enemy
// players are comfortable on) and aff3 (what this team picked earlier in the
// event) are PICK evidence: they say what the enemy wants to play. Blending them
// into a ban step — as the code used to — predicts the enemy banning their own
// comfort heroes. What a team bans is what it fears from US, so a ban step blends
//
//   - layer 3: the heroes this team BANNED in its earlier event drafts, and
//   - layer 2: our own players' comfort pool (the allies part of the snapshot),
//     which is what a scouting captain on the other side bans out.
//
// Same weights and the same n/(n+k) ramp for layer 3 as the pick side.
func (r *Recommender) blendEnemy(dist []float64, action draft.Action) []float64 {
	l2, l3 := r.enemyAff2, r.enemyAff3
	if action == draft.Ban {
		l2, l3 = r.allyRaw, r.enemyAff3Bans
	}
	if l2 == nil && l3 == nil {
		return dist
	}
	return blend.Blend(dist, l2, l3, r.eventN, r.scoutWeights())
}

// scoutWeights are the blend weights from WithScout, or the defaults when only the
// allies part of a snapshot was loaded.
func (r *Recommender) scoutWeights() blend.Weights {
	if r.blendW == (blend.Weights{}) {
		return blend.DefaultWeights()
	}
	return r.blendW
}

// WithScoutBans adds layer-3 ban evidence: per dense hero index, how often the
// enemy team banned it in its earlier event drafts (scout.Aff3BanVector). It uses
// the draft count given to WithScout for its weight ramp. Nil is a no-op.
func (r *Recommender) WithScoutBans(aff3Bans []float64) *Recommender {
	if aff3Bans != nil {
		r.enemyAff3Bans = aff3Bans
	}
	return r
}

// provenanceFor tags which layer contributed signal for a hero, for an enemy
// action of the given kind (display only).
func (r *Recommender) provenanceFor(idx int, action draft.Action) string {
	l2, l3 := r.enemyAff2, r.enemyAff3
	if action == draft.Ban {
		l2, l3 = r.allyRaw, r.enemyAff3Bans
	}
	if l3 != nil && r.eventN > 0 && idx < len(l3) && l3[idx] > 0 {
		return "event"
	}
	if l2 != nil && idx < len(l2) && l2[idx] > 0 {
		return "player"
	}
	return "meta"
}
