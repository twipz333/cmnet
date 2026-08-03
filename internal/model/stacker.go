package model

// Stacker combines the draft-strength signals into one calibrated probability:
//
//	logit(P(radiant win)) = bias + Σ βᵢ · featureᵢ
//
// It replaces the hand-tuned lambda that used to weight the synergy/counter
// matrices against the win-prob logit. Two reasons the hand-tuned version was
// wrong: the weight was swept on the very test set it was then reported against
// (so the number was partly selection noise), and adding a lift in PROBABILITY
// space distorts calibration at the extremes. The stacker is fitted on the
// validation block and works in log-odds, where the terms actually compose.
type Stacker struct {
	Weights []float64 `json:"weights"`
	Bias    float64   `json:"bias"`
}

// Stacker feature indices. Keep this order stable: the fitted weights are stored
// in the bundle positionally.
const (
	StackFeatWinLogit = iota // logit of the raw win-prob model output
	StackFeatSynergy         // mean teammate synergy lift, radiant minus dire
	StackFeatCounter         // mean cross-team counter lift, centered on 0
	StackFeatRole            // role/lane coverage differential, radiant minus dire
	StackFeatCount
)

// StackerFeatures assembles the feature row in the canonical order.
func StackerFeatures(rawWinProb, synergyDiff, counterLift, roleDiff float64) []float64 {
	x := make([]float64, StackFeatCount)
	x[StackFeatWinLogit] = logit(rawWinProb)
	x[StackFeatSynergy] = synergyDiff
	x[StackFeatCounter] = counterLift
	x[StackFeatRole] = roleDiff
	return x
}

// Apply returns the stacked probability for a feature row.
func (s *Stacker) Apply(x []float64) float64 {
	if s == nil || len(s.Weights) == 0 {
		// No stacker: fall back to the raw win-prob the first feature came from.
		if len(x) > StackFeatWinLogit {
			return sigmoid(x[StackFeatWinLogit])
		}
		return 0.5
	}
	n := len(x)
	if len(s.Weights) < n {
		n = len(s.Weights)
	}
	z := s.Bias
	for i := 0; i < n; i++ {
		z += s.Weights[i] * x[i]
	}
	return sigmoid(z)
}

// FitStacker fits the combiner with ridge-regularized logistic regression on the
// validation rows. Deterministic full-batch descent, same as every other fit here.
func FitStacker(rows [][]float64, labels, weights []float64, cfg FitConfig) *Stacker {
	if len(rows) == 0 || len(rows) != len(labels) {
		return nil
	}
	cfg = cfg.WithDefaults()
	m := trainBinaryLogReg(rows, labels, weights, StackFeatCount, fitOptions{
		Epochs: cfg.Epochs,
		LR:     cfg.LR,
		L2:     cfg.L2,
	})
	return &Stacker{Weights: m.Weights, Bias: m.Bias}
}
