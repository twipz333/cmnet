package model

import "math"

// Platt is a Platt-scaling calibrator: p_cal = sigmoid(A·logit(p_raw) + B). The
// identity calibrator (A=1, B=0) leaves probabilities unchanged. Fitting is
// deterministic full-batch gradient descent so the calibrated bundle is
// reproducible (§6).
type Platt struct {
	A float64 `json:"a"`
	B float64 `json:"b"`
}

// Identity returns a no-op calibrator.
func Identity() Platt { return Platt{A: 1, B: 0} }

// Apply calibrates a raw probability.
func (p Platt) Apply(raw float64) float64 {
	return sigmoid(p.A*logit(raw) + p.B)
}

// logit is the inverse sigmoid with clamping to avoid ±Inf at the extremes.
func logit(p float64) float64 {
	const eps = 1e-6
	if p < eps {
		p = eps
	} else if p > 1-eps {
		p = 1 - eps
	}
	return math.Log(p / (1 - p))
}

// FitPlatt fits Platt scaling on raw probabilities against binary labels using
// deterministic full-batch gradient descent over the feature logit(raw). It
// returns the identity calibrator when there is no data.
func FitPlatt(rawProbs, labels []float64, epochs int, lr float64) Platt {
	return FitPlattWeighted(rawProbs, labels, nil, epochs, lr)
}

// FitPlattWeighted fits the same calibrator with a per-sample weight, and is what
// callers with weighted training data must use.
//
// The model is fitted on weighted rows — recency, provenance and the 1/n prefix
// split all scale a sample's influence — so a calibrator fitted on the unweighted
// rows is calibrating a different population than the one the model learned. The
// mismatch is not academic here: prefix rows outnumber matches ten to one, and the
// picks-only bulk pool outnumbers real drafts roughly eight to one, so unweighted
// fitting hands the calibration to whichever pool is most numerous rather than to
// whichever pool the weights say matters.
//
// A nil or mismatched weights slice means "all ones", which reproduces FitPlatt.
func FitPlattWeighted(rawProbs, labels, weights []float64, epochs int, lr float64) Platt {
	n := len(rawProbs)
	if n == 0 || n != len(labels) {
		return Identity()
	}
	if len(weights) != n {
		weights = nil
	}
	x := make([]float64, n)
	for i, r := range rawProbs {
		x[i] = logit(r)
	}
	sumW := float64(n)
	if weights != nil {
		sumW = 0
		for _, w := range weights {
			sumW += w
		}
		if sumW <= 0 {
			return Identity()
		}
	}
	a, b := 1.0, 0.0
	for e := 0; e < epochs; e++ {
		var ga, gb float64
		for i := 0; i < n; i++ {
			w := 1.0
			if weights != nil {
				w = weights[i]
			}
			pred := sigmoid(a*x[i] + b)
			err := w * (pred - labels[i])
			ga += err * x[i]
			gb += err
		}
		scale := lr / sumW
		a -= scale * ga
		b -= scale * gb
	}
	return Platt{A: a, B: b}
}

// BrierScore is the mean squared error of probabilities vs labels (lower is
// better); useful for checking calibration quality.
func BrierScore(probs, labels []float64) float64 {
	if len(probs) == 0 {
		return 0
	}
	var s float64
	for i := range probs {
		d := probs[i] - labels[i]
		s += d * d
	}
	return s / float64(len(probs))
}
