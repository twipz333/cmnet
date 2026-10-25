// Package blend implements the §9 log-linear combination of the three opponent
// data layers for the enemy-prediction head:
//
//	score(hero) = log P1(hero) + w2·log aff2(hero) + w3_eff·log aff3(hero) → softmax
//	w3_eff = w3_max · n/(n+k)   (n = drafts observed in the current event)
//
// It is a pure, deterministic function of (layer-1 prior, snapshot vectors,
// weights). Missing layers (nil vectors) degrade gracefully: no layer-3 → 1+2,
// no snapshot → layer-1 only.
package blend

// Weights tunes the log-linear blend. w3_max > w2 so the team-this-event layer can
// dominate once enough drafts are observed, without erasing layers 1-2.
type Weights struct {
	W2        float64 // layer-2 (player affinity) weight
	W3Max     float64 // layer-3 (team this event) weight ceiling
	K         float64 // smoothing constant for w3_eff ramp
	Smoothing float64 // additive smoothing applied to affinity vectors (no log(0))
}

// DefaultWeights are the starting tuning (refined later with live data). w3>w2.
func DefaultWeights() Weights {
	return Weights{W2: 0.5, W3Max: 1.0, K: 2.0, Smoothing: 0.5}
}

// W3Eff returns the effective layer-3 weight for n observed event drafts:
// w3_max · n/(n+k). It ramps from 0 (no drafts) toward w3_max as the bracket
// progresses, so layer 3 never overfits to 1-3 matches.
func (w Weights) W3Eff(n int) float64 {
	if n <= 0 {
		return 0
	}
	return w.W3Max * float64(n) / (float64(n) + w.K)
}
