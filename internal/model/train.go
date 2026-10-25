package model

// Default training hyperparameters. Full-batch gradient descent with these is
// deterministic and reproducible.
//
// DefaultL2 was raised from 1e-4 once the regularization bug was fixed: the old
// update divided the penalty by the sample-weight sum, so the effective lambda on
// a 24k-match corpus was ~4e-9 and both heads were in practice trained
// unregularized. With the fix the requested lambda is the applied one.
// DefaultEpochs was raised from 300 once the loss curves became visible: at 300
// epochs BOTH next-action heads and the win-prob head still had a falling
// validation loss — the models were UNDER-trained, not over-trained, and 300 was
// never anything but a round number. Measured on the real cache, going to 1500
// moved next-action top-5 from 0.209 to 0.226 and CM win-prob AUC from 0.562 to
// 0.570. Early stopping on the validation block is what guards the other side, so
// a generous ceiling costs training time rather than accuracy.
const (
	DefaultEpochs = 1500
	DefaultLR     = 0.5
	DefaultL2     = 1e-3

	// DefaultEvalEvery / DefaultPatience drive early stopping when a validation
	// set is supplied. Patience is counted in evaluations, not epochs.
	DefaultEvalEvery = 10
	DefaultPatience  = 5
)

// FitConfig is the public knob set for training either head.
type FitConfig struct {
	Epochs    int
	LR        float64
	L2        float64
	EvalEvery int
	Patience  int              // 0 => no early stopping even with a validation set
	Curve     func(CurvePoint) // optional loss-curve sink
}

// WithDefaults fills unset fields with the package defaults.
func (c FitConfig) WithDefaults() FitConfig {
	if c.Epochs == 0 {
		c.Epochs = DefaultEpochs
	}
	if c.LR == 0 {
		c.LR = DefaultLR
	}
	if c.L2 == 0 {
		c.L2 = DefaultL2
	}
	if c.EvalEvery == 0 {
		c.EvalEvery = DefaultEvalEvery
	}
	return c
}

func (c FitConfig) toFitOptions() fitOptions {
	c = c.WithDefaults()
	return fitOptions{
		Epochs:    c.Epochs,
		LR:        c.LR,
		L2:        c.L2,
		EvalEvery: c.EvalEvery,
		Patience:  c.Patience,
		Curve:     c.Curve,
	}
}

// WinProbSample is one labeled win-prob training example.
type WinProbSample struct {
	X      []float64
	Y      float64 // 1 if radiant won
	Weight float64 // recency weight; 0 is treated as 1 for back-compat
}

// PredictorSample is one labeled next-pick training example.
type PredictorSample struct {
	X      []float64
	Label  int // dense hero index chosen
	Weight float64
}

// TrainWinProb fits the win-prob model from samples (recency-weighted). val, when
// non-empty and cfg.Patience > 0, drives early stopping on held-out log-loss.
func TrainWinProb(samples, val []WinProbSample, nFeatures int, cfg FitConfig) *BinaryLogReg {
	X, y, weights := splitWinProb(samples)
	opts := cfg.toFitOptions()
	if len(val) > 0 {
		opts.ValX, opts.ValY, opts.ValWeight = splitWinProb(val)
	}
	return trainBinaryLogReg(X, y, weights, nFeatures, opts)
}

func splitWinProb(samples []WinProbSample) (X [][]float64, y, weights []float64) {
	X = make([][]float64, len(samples))
	y = make([]float64, len(samples))
	weights = make([]float64, len(samples))
	for i, s := range samples {
		X[i] = s.X
		y[i] = s.Y
		weights[i] = normWeight(s.Weight)
	}
	return X, y, weights
}

// TrainPredictor fits the next-pick predictor from samples (recency-weighted).
// report, if non-nil, receives the just-completed epoch for progress reporting.
func TrainPredictor(samples, val []PredictorSample, nClasses, nFeatures int, cfg FitConfig, report func(epoch int)) *SoftmaxReg {
	X, labels, weights := splitPredictor(samples)
	opts := cfg.toFitOptions()
	if len(val) > 0 {
		opts.ValX, opts.ValLabels, opts.ValWeight = splitPredictor(val)
	}
	return trainSoftmaxReg(X, labels, weights, nClasses, nFeatures, opts, report)
}

func splitPredictor(samples []PredictorSample) (X [][]float64, labels []int, weights []float64) {
	X = make([][]float64, len(samples))
	labels = make([]int, len(samples))
	weights = make([]float64, len(samples))
	for i, s := range samples {
		X[i] = s.X
		labels[i] = s.Label
		weights[i] = normWeight(s.Weight)
	}
	return X, labels, weights
}

// SampleWeights returns the normalized recency weights of win-prob samples, for
// effective-sample-size reporting.
func SampleWeights(samples []WinProbSample) []float64 {
	out := make([]float64, len(samples))
	for i, s := range samples {
		out[i] = normWeight(s.Weight)
	}
	return out
}

// PredictorSampleWeights is SampleWeights for predictor samples.
func PredictorSampleWeights(samples []PredictorSample) []float64 {
	out := make([]float64, len(samples))
	for i, s := range samples {
		out[i] = normWeight(s.Weight)
	}
	return out
}

// normWeight maps a zero (unset) weight to 1 so callers that don't set weights
// get the unweighted behavior.
func normWeight(w float64) float64 {
	if w == 0 {
		return 1
	}
	return w
}
