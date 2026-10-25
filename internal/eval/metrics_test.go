package eval

import "math"

import "testing"

func almost(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestAUCPerfectAndChance(t *testing.T) {
	// Perfectly separable: all positives score above all negatives → AUC 1.
	scores := []float64{0.1, 0.2, 0.8, 0.9}
	labels := []float64{0, 0, 1, 1}
	if got := AUC(scores, labels); !almost(got, 1.0, 1e-9) {
		t.Fatalf("perfect AUC = %v, want 1.0", got)
	}
	// Reversed → AUC 0.
	rev := []float64{0.9, 0.8, 0.2, 0.1}
	if got := AUC(rev, labels); !almost(got, 0.0, 1e-9) {
		t.Fatalf("reversed AUC = %v, want 0.0", got)
	}
	// All identical scores → every pair is a tie → AUC 0.5.
	flat := []float64{0.5, 0.5, 0.5, 0.5}
	if got := AUC(flat, labels); !almost(got, 0.5, 1e-9) {
		t.Fatalf("tie AUC = %v, want 0.5", got)
	}
}

func TestAUCEmptyClass(t *testing.T) {
	if got := AUC([]float64{0.3, 0.7}, []float64{1, 1}); !almost(got, 0.5, 1e-9) {
		t.Fatalf("single-class AUC = %v, want 0.5", got)
	}
}

func TestLogLossAndBrier(t *testing.T) {
	// A confident correct prediction has near-zero loss; a confident wrong one large.
	if ll := LogLoss([]float64{0.99}, []float64{1}); ll > 0.02 {
		t.Fatalf("confident-correct logloss = %v, want small", ll)
	}
	if ll := LogLoss([]float64{0.01}, []float64{1}); ll < 3 {
		t.Fatalf("confident-wrong logloss = %v, want large", ll)
	}
	// Brier of p=0.5 vs any label is 0.25.
	if br := Brier([]float64{0.5}, []float64{1}); !almost(br, 0.25, 1e-9) {
		t.Fatalf("brier = %v, want 0.25", br)
	}
}

func TestECEPerfectlyCalibrated(t *testing.T) {
	// Two bins, empirical rate matches predicted probability exactly → ECE 0.
	probs := []float64{0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1,
		0.9, 0.9, 0.9, 0.9, 0.9, 0.9, 0.9, 0.9, 0.9, 0.9}
	labels := []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 1,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 0}
	if e := ECE(probs, labels, 10); e > 1e-9 {
		t.Fatalf("ECE = %v, want ~0", e)
	}
}

func TestRankOf(t *testing.T) {
	dist := []float64{0.1, 0.5, 0.3, 0.05}
	if r := rankOf(dist, 1); r != 1 { // 0.5 is highest
		t.Fatalf("rank of top = %d, want 1", r)
	}
	if r := rankOf(dist, 3); r != 4 { // 0.05 is lowest
		t.Fatalf("rank of bottom = %d, want 4", r)
	}
}
