package model

import "math"

// BinaryLogReg is a logistic regression model: P(y=1|x) = sigmoid(w·x + b).
type BinaryLogReg struct {
	Weights []float64 `json:"weights"`
	Bias    float64   `json:"bias"`
}

// Predict returns P(y=1|x).
func (m *BinaryLogReg) Predict(x []float64) float64 {
	return sigmoid(dot(m.Weights, x) + m.Bias)
}

// CurvePoint is one train/validation loss reading during gradient descent. It is
// what turns "300 epochs, lr 0.5" from a magic number into an observable: without
// the curve there is no way to tell under- from over-fitting.
type CurvePoint struct {
	Epoch     int
	TrainLoss float64
	ValLoss   float64 // NaN when no validation set was supplied
}

// fitOptions carries the gradient-descent hyperparameters plus the optional
// validation set used for early stopping and loss curves.
//
// Early stopping is deterministic: full-batch descent has no sampling, losses are
// evaluated on fixed epochs, and the best-so-far weights are snapshotted, so the
// same inputs always stop at the same epoch with the same weights (§6).
type fitOptions struct {
	Epochs int
	LR     float64
	L2     float64

	// Validation set. ValY is used by the binary trainer, ValLabels by the
	// softmax trainer. Early stopping needs both ValX and Patience > 0.
	ValX      [][]float64
	ValY      []float64
	ValLabels []int
	ValWeight []float64

	// EvalEvery is the epoch interval between loss evaluations (default 10).
	// Patience is how many consecutive evaluations may fail to improve the
	// validation loss before training stops; 0 disables early stopping.
	EvalEvery int
	Patience  int

	// Curve, if non-nil, receives every loss evaluation.
	Curve func(CurvePoint)
}

func (o fitOptions) withDefaults() fitOptions {
	if o.EvalEvery <= 0 {
		o.EvalEvery = 10
	}
	return o
}

// hasVal reports whether early stopping is active.
func (o fitOptions) hasVal() bool { return len(o.ValX) > 0 && o.Patience > 0 }

// trainBinaryLogReg fits a BinaryLogReg with deterministic full-batch gradient
// descent: zero init, fixed sample order, optional early stopping on a validation
// set. Full-batch (not SGD) keeps training reproducible regardless of platform.
// weights are optional per-sample weights (nil => all 1); the DATA gradient is
// normalized by their sum so uniform weights reproduce the unweighted result.
//
// The L2 term is deliberately NOT normalized by the sample-weight sum: the penalty
// belongs to the objective, not to the dataset size. Dividing it by sumW (as an
// earlier version did) made the effective lambda ~1/N of the requested one — i.e.
// no regularization at all on a 24k-match corpus.
func trainBinaryLogReg(X [][]float64, y, weights []float64, nFeatures int, opts fitOptions) *BinaryLogReg {
	opts = opts.withDefaults()
	w := make([]float64, nFeatures)
	var b float64
	n := len(X)
	if n == 0 {
		return &BinaryLogReg{Weights: w, Bias: b}
	}
	sumW := sampleWeightSum(weights, n)
	grad := make([]float64, nFeatures)

	best := &BinaryLogReg{Weights: append([]float64(nil), w...), Bias: b}
	bestLoss := math.Inf(1)
	stale := 0

	for e := 0; e < opts.Epochs; e++ {
		for i := range grad {
			grad[i] = 0
		}
		var gradB float64
		for i := 0; i < n; i++ {
			wi := sampleWeight(weights, i)
			pred := sigmoid(dot(w, X[i]) + b)
			err := wi * (pred - y[i])
			xi := X[i]
			for j := range xi {
				grad[j] += err * xi[j]
			}
			gradB += err
		}
		for j := range w {
			w[j] -= opts.LR * (grad[j]/sumW + opts.L2*w[j])
		}
		b -= opts.LR * gradB / sumW // bias is never penalized

		epoch := e + 1
		if epoch%opts.EvalEvery != 0 && epoch != opts.Epochs {
			continue
		}
		if opts.Curve == nil && !opts.hasVal() {
			continue // nobody is watching; skip the loss pass
		}
		cur := &BinaryLogReg{Weights: w, Bias: b}
		point := CurvePoint{
			Epoch:     epoch,
			TrainLoss: binaryLoss(cur, X, y, weights),
			ValLoss:   math.NaN(),
		}
		if len(opts.ValX) > 0 {
			point.ValLoss = binaryLoss(cur, opts.ValX, opts.ValY, opts.ValWeight)
		}
		if opts.Curve != nil {
			opts.Curve(point)
		}
		if !opts.hasVal() {
			continue
		}
		if point.ValLoss < bestLoss {
			bestLoss = point.ValLoss
			best.Weights = append(best.Weights[:0], w...)
			best.Bias = b
			stale = 0
			continue
		}
		stale++
		if stale >= opts.Patience {
			return best
		}
	}
	if opts.hasVal() && !math.IsInf(bestLoss, 1) {
		return best
	}
	return &BinaryLogReg{Weights: w, Bias: b}
}

// binaryLoss is the (sample-weighted) mean log-loss of a binary model.
func binaryLoss(m *BinaryLogReg, X [][]float64, y, weights []float64) float64 {
	if len(X) == 0 {
		return math.NaN()
	}
	var sum, sw float64
	for i := range X {
		wi := sampleWeight(weights, i)
		p := clampProb(m.Predict(X[i]))
		if y[i] > 0.5 {
			sum += -wi * math.Log(p)
		} else {
			sum += -wi * math.Log(1-p)
		}
		sw += wi
	}
	if sw == 0 {
		return math.NaN()
	}
	return sum / sw
}

// clampProb keeps probabilities away from 0/1 so log-loss stays finite.
func clampProb(p float64) float64 {
	const eps = 1e-12
	if p < eps {
		return eps
	}
	if p > 1-eps {
		return 1 - eps
	}
	return p
}

// sampleWeight returns weights[i] or 1 when weights is nil.
func sampleWeight(weights []float64, i int) float64 {
	if weights == nil || i >= len(weights) {
		return 1
	}
	return weights[i]
}

// sampleWeightSum returns the total sample weight (n when weights is nil).
func sampleWeightSum(weights []float64, n int) float64 {
	if weights == nil {
		return float64(n)
	}
	var s float64
	for _, w := range weights {
		s += w
	}
	if s == 0 {
		return 1 // avoid division by zero; degenerate all-zero weights
	}
	return s
}

// EffectiveSampleSize is Kish's ESS = (Σw)² / Σw². Under recency weighting the
// nominal match count badly overstates how much data the model actually sees (a
// 14-day half-life over a 78-day window keeps only ~37% of it), so the training
// pipeline reports this instead of the raw count.
func EffectiveSampleSize(weights []float64) float64 {
	if len(weights) == 0 {
		return 0
	}
	var s, sq float64
	for _, w := range weights {
		s += w
		sq += w * w
	}
	if sq == 0 {
		return 0
	}
	return s * s / sq
}

// SoftmaxReg is a multiclass logistic regression: P(class k|x) ∝ exp(W_k·x + b_k).
type SoftmaxReg struct {
	Weights [][]float64 `json:"weights"` // [nClasses][nFeatures]
	Bias    []float64   `json:"bias"`    // [nClasses]
}

// Predict returns the probability distribution over classes for x.
func (m *SoftmaxReg) Predict(x []float64) []float64 {
	return softmax(m.Logits(x))
}

// Logits returns the pre-softmax scores for x. Callers that mask unavailable
// classes need the logits so they can renormalize over the legal set only.
func (m *SoftmaxReg) Logits(x []float64) []float64 {
	logits := make([]float64, len(m.Weights))
	for k := range m.Weights {
		logits[k] = dot(m.Weights[k], x) + m.Bias[k]
	}
	return logits
}

// nzFeature is one nonzero entry of a feature vector (sparse representation).
type nzFeature struct {
	idx int
	val float64
}

// sparsify drops the exact zeros from each feature vector. The predictor inputs
// are multi-hot (only ~10-20 of several hundred features are set), so iterating
// only the nonzeros is ~25x less work. This is bit-for-bit identical to the dense
// loop: skipped terms are exactly 0.0, which never changes a running sum, and the
// surviving terms keep ascending index order — so determinism is preserved.
func sparsify(X [][]float64) [][]nzFeature {
	out := make([][]nzFeature, len(X))
	for i, xi := range X {
		var s []nzFeature
		for j, v := range xi {
			if v != 0 {
				s = append(s, nzFeature{j, v})
			}
		}
		out[i] = s
	}
	return out
}

// trainSoftmaxReg fits a SoftmaxReg with deterministic full-batch gradient
// descent (zero init, fixed sample order), with the same L2 and early-stopping
// semantics as trainBinaryLogReg.
func trainSoftmaxReg(X [][]float64, labels []int, weights []float64, nClasses, nFeatures int, opts fitOptions, report func(epoch int)) *SoftmaxReg {
	opts = opts.withDefaults()
	w := make([][]float64, nClasses)
	for k := range w {
		w[k] = make([]float64, nFeatures)
	}
	b := make([]float64, nClasses)
	n := len(X)
	if n == 0 {
		return &SoftmaxReg{Weights: w, Bias: b}
	}

	sparse := sparsify(X)
	valSparse := sparsify(opts.ValX)
	gradW := make([][]float64, nClasses)
	for k := range gradW {
		gradW[k] = make([]float64, nFeatures)
	}
	gradB := make([]float64, nClasses)
	logits := make([]float64, nClasses)
	sumW := sampleWeightSum(weights, n)

	best := newSoftmaxSnapshot(nClasses, nFeatures)
	bestLoss := math.Inf(1)
	stale := 0

	for e := 0; e < opts.Epochs; e++ {
		for k := range gradW {
			for j := range gradW[k] {
				gradW[k][j] = 0
			}
			gradB[k] = 0
		}
		for i := 0; i < n; i++ {
			wi := sampleWeight(weights, i)
			xi := sparse[i]
			for k := range w {
				wk := w[k]
				var s float64
				for _, p := range xi {
					s += wk[p.idx] * p.val
				}
				logits[k] = s + b[k]
			}
			probs := softmax(logits)
			for k := 0; k < nClasses; k++ {
				err := probs[k]
				if k == labels[i] {
					err -= 1
				}
				err *= wi
				gk := gradW[k]
				for _, p := range xi {
					gk[p.idx] += err * p.val
				}
				gradB[k] += err
			}
		}
		for k := 0; k < nClasses; k++ {
			for j := range w[k] {
				w[k][j] -= opts.LR * (gradW[k][j]/sumW + opts.L2*w[k][j])
			}
			b[k] -= opts.LR * gradB[k] / sumW // bias unpenalized
		}
		if report != nil {
			report(e + 1)
		}

		epoch := e + 1
		if epoch%opts.EvalEvery != 0 && epoch != opts.Epochs {
			continue
		}
		if opts.Curve == nil && !opts.hasVal() {
			continue
		}
		cur := &SoftmaxReg{Weights: w, Bias: b}
		point := CurvePoint{
			Epoch:     epoch,
			TrainLoss: softmaxLossSparse(cur, sparse, labels, weights),
			ValLoss:   math.NaN(),
		}
		if len(valSparse) > 0 {
			point.ValLoss = softmaxLossSparse(cur, valSparse, opts.ValLabels, opts.ValWeight)
		}
		if opts.Curve != nil {
			opts.Curve(point)
		}
		if !opts.hasVal() {
			continue
		}
		if point.ValLoss < bestLoss {
			bestLoss = point.ValLoss
			copySoftmax(best, w, b)
			stale = 0
			continue
		}
		stale++
		if stale >= opts.Patience {
			return best
		}
	}
	if opts.hasVal() && !math.IsInf(bestLoss, 1) {
		return best
	}
	return &SoftmaxReg{Weights: w, Bias: b}
}

func newSoftmaxSnapshot(nClasses, nFeatures int) *SoftmaxReg {
	w := make([][]float64, nClasses)
	for k := range w {
		w[k] = make([]float64, nFeatures)
	}
	return &SoftmaxReg{Weights: w, Bias: make([]float64, nClasses)}
}

func copySoftmax(dst *SoftmaxReg, w [][]float64, b []float64) {
	for k := range w {
		copy(dst.Weights[k], w[k])
	}
	copy(dst.Bias, b)
}

// softmaxLossSparse is the (sample-weighted) mean cross-entropy over sparse rows.
func softmaxLossSparse(m *SoftmaxReg, rows [][]nzFeature, labels []int, weights []float64) float64 {
	if len(rows) == 0 || len(labels) != len(rows) {
		return math.NaN()
	}
	nClasses := len(m.Weights)
	logits := make([]float64, nClasses)
	var sum, sw float64
	for i, xi := range rows {
		for k := 0; k < nClasses; k++ {
			wk := m.Weights[k]
			var s float64
			for _, p := range xi {
				s += wk[p.idx] * p.val
			}
			logits[k] = s + m.Bias[k]
		}
		probs := softmax(logits)
		wi := sampleWeight(weights, i)
		sum += -wi * math.Log(clampProb(probs[labels[i]]))
		sw += wi
	}
	if sw == 0 {
		return math.NaN()
	}
	return sum / sw
}
