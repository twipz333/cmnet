package model

import (
	"math"
	"testing"
)

func TestRoundScore(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0.123456789, 0.12346},
		{0.000004, 0.0},
		{0.999995, 1.0},
	}
	for _, c := range cases {
		if got := RoundScore(c.in); got != c.want {
			t.Errorf("RoundScore(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBinaryLogRegLearnsSeparable(t *testing.T) {
	// Feature 0 perfectly predicts the label.
	X := [][]float64{{1, 0}, {1, 0}, {0, 1}, {0, 1}}
	y := []float64{1, 1, 0, 0}
	m := trainBinaryLogReg(X, y, nil, 2, fitOptions{Epochs: 500, LR: 1.0})
	if m.Predict([]float64{1, 0}) < 0.5 {
		t.Error("expected P>0.5 for positive class")
	}
	if m.Predict([]float64{0, 1}) > 0.5 {
		t.Error("expected P<0.5 for negative class")
	}
}

func TestSoftmaxRegLearnsClasses(t *testing.T) {
	X := [][]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	labels := []int{0, 1, 2}
	m := trainSoftmaxReg(X, labels, nil, 3, 3, fitOptions{Epochs: 800, LR: 1.0}, nil)
	for i, x := range X {
		probs := m.Predict(x)
		best := 0
		for k, p := range probs {
			if p > probs[best] {
				best = k
			}
		}
		if best != labels[i] {
			t.Errorf("sample %d: argmax=%d, want %d (probs=%v)", i, best, labels[i], probs)
		}
	}
}

// Training must be deterministic: identical inputs -> bit-identical weights.
func TestTrainingDeterministic(t *testing.T) {
	X := [][]float64{{1, 0}, {0, 1}, {1, 1}, {0, 0}}
	y := []float64{1, 0, 1, 0}
	opts := fitOptions{Epochs: 200, LR: 0.5, L2: 1e-4}
	a := trainBinaryLogReg(X, y, nil, 2, opts)
	b := trainBinaryLogReg(X, y, nil, 2, opts)
	if a.Bias != b.Bias {
		t.Errorf("bias differs: %v vs %v", a.Bias, b.Bias)
	}
	for i := range a.Weights {
		if a.Weights[i] != b.Weights[i] {
			t.Errorf("weight %d differs: %v vs %v", i, a.Weights[i], b.Weights[i])
		}
	}
}

// L2 must actually shrink the weights. The earlier update divided the penalty by
// the sample-weight sum, which made lambda effectively 1/N of the requested value
// — on real corpora that is no regularization at all.
func TestL2ShrinksWeights(t *testing.T) {
	X := [][]float64{{1, 0}, {1, 0}, {0, 1}, {0, 1}}
	y := []float64{1, 1, 0, 0}
	norm := func(m *BinaryLogReg) float64 {
		var s float64
		for _, w := range m.Weights {
			s += w * w
		}
		return s
	}
	free := trainBinaryLogReg(X, y, nil, 2, fitOptions{Epochs: 300, LR: 0.5, L2: 0})
	reg := trainBinaryLogReg(X, y, nil, 2, fitOptions{Epochs: 300, LR: 0.5, L2: 0.1})
	if norm(reg) >= norm(free) {
		t.Errorf("L2 did not shrink weights: |w|²=%v regularized vs %v free", norm(reg), norm(free))
	}
	// And the strength must matter: a heavier penalty shrinks further. (Stay under
	// lr*l2 < 2, above which the plain-GD weight update oscillates and diverges.)
	heavy := trainBinaryLogReg(X, y, nil, 2, fitOptions{Epochs: 300, LR: 0.5, L2: 1.0})
	if norm(heavy) >= norm(reg) {
		t.Errorf("stronger L2 did not shrink further: %v vs %v", norm(heavy), norm(reg))
	}
}

// L2 must not scale with the dataset size: duplicating every sample leaves the
// objective unchanged, so the fitted weights must be (near) identical.
func TestL2IndependentOfSampleCount(t *testing.T) {
	X := [][]float64{{1, 0}, {0, 1}}
	y := []float64{1, 0}
	dupX := [][]float64{{1, 0}, {0, 1}, {1, 0}, {0, 1}}
	dupY := []float64{1, 0, 1, 0}
	opts := fitOptions{Epochs: 200, LR: 0.5, L2: 0.5}
	small := trainBinaryLogReg(X, y, nil, 2, opts)
	big := trainBinaryLogReg(dupX, dupY, nil, 2, opts)
	for i := range small.Weights {
		if math.Abs(small.Weights[i]-big.Weights[i]) > 1e-9 {
			t.Errorf("weight %d differs after duplicating the data: %v vs %v (L2 is scaling with N)",
				i, small.Weights[i], big.Weights[i])
		}
	}
}

// Early stopping must return the best validation snapshot, deterministically.
func TestEarlyStoppingDeterministicAndBest(t *testing.T) {
	// Train data the model can overfit; validation disagrees on one feature.
	X := [][]float64{{1, 0}, {1, 0}, {0, 1}, {0, 1}}
	y := []float64{1, 1, 0, 0}
	valX := [][]float64{{1, 0}, {0, 1}, {1, 0}}
	valY := []float64{1, 0, 0}

	var curve []CurvePoint
	opts := fitOptions{
		Epochs: 2000, LR: 1.0, L2: 0,
		ValX: valX, ValY: valY,
		EvalEvery: 10, Patience: 3,
		Curve: func(p CurvePoint) { curve = append(curve, p) },
	}
	a := trainBinaryLogReg(X, y, nil, 2, opts)
	b := trainBinaryLogReg(X, y, nil, 2, fitOptions{
		Epochs: 2000, LR: 1.0, L2: 0,
		ValX: valX, ValY: valY,
		EvalEvery: 10, Patience: 3,
	})
	if len(curve) == 0 {
		t.Fatal("no curve points reported")
	}
	if curve[0].ValLoss != curve[0].ValLoss { // NaN check
		t.Error("validation loss not computed despite a validation set")
	}
	if len(curve)*10 >= 2000 {
		t.Errorf("early stopping never fired: ran %d evaluations of 200", len(curve))
	}
	if a.Bias != b.Bias {
		t.Errorf("early stopping not deterministic: bias %v vs %v", a.Bias, b.Bias)
	}
	for i := range a.Weights {
		if a.Weights[i] != b.Weights[i] {
			t.Errorf("early stopping not deterministic: weight %d %v vs %v", i, a.Weights[i], b.Weights[i])
		}
	}
}

func TestEffectiveSampleSize(t *testing.T) {
	if got := EffectiveSampleSize([]float64{1, 1, 1, 1}); math.Abs(got-4) > 1e-9 {
		t.Errorf("uniform weights: ESS = %v, want 4", got)
	}
	// One dominant weight collapses the effective size toward 1.
	got := EffectiveSampleSize([]float64{1, 0.001, 0.001, 0.001})
	if got > 1.1 {
		t.Errorf("skewed weights: ESS = %v, want ~1", got)
	}
	if got := EffectiveSampleSize(nil); got != 0 {
		t.Errorf("empty weights: ESS = %v, want 0", got)
	}
}

func TestSoftmaxSumsToOne(t *testing.T) {
	out := softmax([]float64{2, 1, 0.1})
	var sum float64
	for _, p := range out {
		sum += p
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Errorf("softmax sum = %v, want 1", sum)
	}
}
