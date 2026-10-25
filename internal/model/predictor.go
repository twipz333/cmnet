package model

import (
	"math"

	"cmnet/internal/features"
)

// Predictor binds the trained next-action models to a hero map for inference.
//
// Picks and bans are separate heads. They answer different questions from the
// same board — a ban targets what the opponent wants, a pick fills your own
// composition — and in Captains Mode they occur at different phases, so a single
// head has to spend capacity untangling the two. Two heads also let each one carry
// its own phase-conditioned hero prior.
type Predictor struct {
	Pick    *SoftmaxReg
	Ban     *SoftmaxReg
	HeroMap *features.HeroMap
}

// HeroProb is a hero id with its predicted probability.
type HeroProb struct {
	HeroID int
	Prob   float64
}

// head returns the model for the upcoming action, falling back to whichever head
// exists when only one was trained (tiny corpora may have no bans at all).
func (p *Predictor) head(isPick bool) *SoftmaxReg {
	if isPick {
		if p.Pick != nil {
			return p.Pick
		}
		return p.Ban
	}
	if p.Ban != nil {
		return p.Ban
	}
	return p.Pick
}

// Distribution returns P(next hero | state) as rounded probabilities by dense
// index order, with every hero already on the board masked out and the remainder
// renormalized.
//
// Masking is not cosmetic: an unmasked distribution spends probability mass on
// heroes that cannot legally be taken, which both misranks the legal ones and
// makes offline top-k disagree with what the live UI shows.
func (p *Predictor) Distribution(s features.PredictorState) []float64 {
	return p.DistributionMasked(s, takenFrom(s))
}

// DistributionMasked is Distribution with an explicit unavailable-hero list (ids).
func (p *Predictor) DistributionMasked(s features.PredictorState, taken []int) []float64 {
	blocked := make([]bool, p.HeroMap.Len())
	for _, id := range taken {
		if i, ok := p.HeroMap.Index(id); ok && i < len(blocked) {
			blocked[i] = true
		}
	}
	return p.DistributionMaskedIdx(s, blocked)
}

// DistributionMaskedIdx is DistributionMasked with the mask already expressed as
// a per-dense-index flag slice (what a replay loop naturally maintains).
func (p *Predictor) DistributionMaskedIdx(s features.PredictorState, blocked []bool) []float64 {
	m := p.head(s.IsPick)
	n := p.HeroMap.Len()
	if m == nil {
		return make([]float64, n)
	}
	x := p.HeroMap.EncodePredictor(s)
	logits := m.Logits(x)
	if len(blocked) < len(logits) {
		grown := make([]bool, len(logits))
		copy(grown, blocked)
		blocked = grown
	}
	legal := 0
	for i := range logits {
		if !blocked[i] {
			legal++
		}
	}
	if legal == 0 {
		return make([]float64, len(logits))
	}
	for i := range logits {
		if blocked[i] {
			logits[i] = math.Inf(-1) // exp(-Inf) = 0: exact, no epsilon fudge
		}
	}
	probs := softmax(logits)
	for i := range probs {
		probs[i] = RoundScore(probs[i])
	}
	return probs
}

// takenFrom collects every hero id already on the board in a predictor state.
func takenFrom(s features.PredictorState) []int {
	out := make([]int, 0, len(s.RadiantPicks)+len(s.DirePicks)+len(s.RadiantBans)+len(s.DireBans))
	out = append(out, s.RadiantPicks...)
	out = append(out, s.DirePicks...)
	out = append(out, s.RadiantBans...)
	out = append(out, s.DireBans...)
	return out
}
