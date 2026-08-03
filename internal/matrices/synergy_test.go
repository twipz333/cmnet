package matrices

import (
	"math"
	"testing"

	"cmnet/internal/cache"
)

// stubIndexer maps hero ids 1..n to dense indices 0..n-1.
type stubIndexer struct{ n int }

func (s stubIndexer) Len() int { return s.n }
func (s stubIndexer) Index(id int) (int, bool) {
	if id >= 1 && id <= s.n {
		return id - 1, true
	}
	return 0, false
}

func match(id int64, radiantWin bool, rPicks, dPicks []int) cache.Match {
	m := cache.Match{ID: id, RadiantWin: radiantWin}
	ord := 0
	for _, h := range rPicks {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 0})
		ord++
	}
	for _, h := range dPicks {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 1})
		ord++
	}
	return m
}

// repeat emits n copies of a match with distinct ids, so a pair can clear the
// sample floor that the empirical-Bayes estimator needs.
func repeat(n int, id int64, radiantWin bool, rPicks, dPicks []int) []cache.Match {
	out := make([]cache.Match, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, match(id+int64(i), radiantWin, rPicks, dPicks))
	}
	return out
}

// rawOpts disables shrinkage so a test can check the DIRECTION of a lift without
// needing the sample sizes that shrinkage rightly demands.
var rawOpts = Options{NoShrink: true}

func TestEmptyMatricesAreNeutral(t *testing.T) {
	mx := Compute(nil, stubIndexer{n: 3}, Options{})
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if mx.Synergy[i][j] != 0.5 {
				t.Errorf("synergy[%d][%d] = %v, want 0.5 (no data)", i, j, mx.Synergy[i][j])
			}
			if mx.Counter[i][j] != 0.5 {
				t.Errorf("counter[%d][%d] = %v, want 0.5 (no data)", i, j, mx.Counter[i][j])
			}
		}
	}
}

func TestSynergyLiftPositiveAndSymmetric(t *testing.T) {
	// Heroes 1&2 OVERPERFORM their solo rates when together: they win as a pair but
	// lose when split apart. That is genuine positive synergy lift (> 0.5), unlike a
	// raw pair win rate which would also be inflated by strong solo heroes.
	matches := []cache.Match{
		match(1, true, []int{1, 2}, []int{5, 6}),  // together -> win
		match(2, true, []int{1, 2}, []int{5, 6}),  // together -> win
		match(3, false, []int{1, 3}, []int{5, 6}), // 1 without 2 -> loss
		match(4, false, []int{2, 4}, []int{5, 6}), // 2 without 1 -> loss
	}
	mx := Compute(matches, stubIndexer{n: 6}, rawOpts)

	if mx.Synergy[0][1] != mx.Synergy[1][0] {
		t.Errorf("synergy not symmetric: %v vs %v", mx.Synergy[0][1], mx.Synergy[1][0])
	}
	if mx.Synergy[0][1] <= 0.5 {
		t.Errorf("synergy lift[0][1] = %v, want > 0.5 (pair beats solo expectation)", mx.Synergy[0][1])
	}
}

func TestCounterLiftIsExpectationRelative(t *testing.T) {
	// Hero1 always beats hero2, but they have identical solo strength elsewhere, so
	// beating hero2 is genuinely "better than expected" -> counter lift > 0.5, and
	// the mirror < 0.5.
	matches := []cache.Match{
		match(1, true, []int{1}, []int{2}),  // 1 beats 2
		match(2, true, []int{1}, []int{2}),  // 1 beats 2
		match(3, true, []int{2}, []int{7}),  // 2 wins elsewhere (raise 2 solo)
		match(4, false, []int{1}, []int{7}), // 1 loses elsewhere (lower 1 solo)
	}
	mx := Compute(matches, stubIndexer{n: 7}, rawOpts)

	if mx.Counter[0][1] <= 0.5 {
		t.Errorf("counter lift[0][1] = %v, want > 0.5 (1 beats 2 above expectation)", mx.Counter[0][1])
	}
	if mx.Counter[1][0] >= 0.5 {
		t.Errorf("counter lift[1][0] = %v, want < 0.5 (mirror)", mx.Counter[1][0])
	}
}

func TestSparsePairStaysNeutral(t *testing.T) {
	// A single game is no evidence of anything: the cell must stay ~neutral instead
	// of reporting a spurious extreme.
	matches := []cache.Match{match(1, true, []int{1, 2}, []int{3})}
	mx := Compute(matches, stubIndexer{n: 3}, Options{})
	if mx.Synergy[0][1] < 0.45 || mx.Synergy[0][1] > 0.55 {
		t.Errorf("sparse synergy lift = %v, want ~0.5 (no spurious synergy)", mx.Synergy[0][1])
	}
}

// The whole point of empirical Bayes over a fixed pseudo-count: how much a cell is
// pulled toward neutral must depend on that cell's own evidence.
func TestShrinkFactorScalesWithEvidence(t *testing.T) {
	const tau2 = 0.04
	precise := shrinkFactor(tau2, 0.004) // 10x more precise than the true spread
	noisy := shrinkFactor(tau2, 4.0)     // mostly sampling noise
	if precise <= noisy {
		t.Errorf("precise cell shrunk at least as hard as a noisy one: %v vs %v", precise, noisy)
	}
	if precise <= 0.5 || precise >= 1 {
		t.Errorf("precise cell factor = %v, want in (0.5, 1)", precise)
	}
	if noisy >= 0.05 {
		t.Errorf("noisy cell factor = %v, want near 0", noisy)
	}
	// No estimated spread means no signal anywhere: everything collapses.
	if got := shrinkFactor(0, 0.001); got != 0 {
		t.Errorf("tau2=0 factor = %v, want 0", got)
	}
	// An unobserved cell (infinite variance) must not leak any lift.
	if got := shrinkFactor(tau2, math.Inf(1)); got != 0 {
		t.Errorf("unobserved cell factor = %v, want 0", got)
	}
}

// Degenerate inputs (every match a Radiant win, heroes that only ever appear
// together) must still produce in-range, finite cells rather than NaNs or values
// outside [0,1].
func TestDegenerateInputStaysInRange(t *testing.T) {
	var matches []cache.Match
	matches = append(matches, repeat(40, 100, true, []int{1, 2}, []int{5, 6})...)
	matches = append(matches, repeat(2, 300, true, []int{3, 4}, []int{5, 6})...)
	mx := Compute(matches, stubIndexer{n: 7}, Options{})
	for i := range mx.Synergy {
		for j := range mx.Synergy[i] {
			for _, v := range []float64{mx.Synergy[i][j], mx.Counter[i][j]} {
				if math.IsNaN(v) || v < 0 || v > 1 {
					t.Fatalf("cell [%d][%d] = %v out of range", i, j, v)
				}
			}
		}
	}
}

// Centering must remove the shared offset: with balanced data the games-weighted
// mean lift is ~0, so a typical pair reads as neutral instead of inheriting the
// bias of the solo-rate expectation.
func TestLiftsAreCentered(t *testing.T) {
	var matches []cache.Match
	// A balanced round-robin: each pair wins some and loses some.
	matches = append(matches, repeat(10, 100, true, []int{1, 2}, []int{3, 4})...)
	matches = append(matches, repeat(10, 200, false, []int{1, 2}, []int{3, 4})...)
	matches = append(matches, repeat(10, 300, true, []int{1, 3}, []int{2, 4})...)
	matches = append(matches, repeat(10, 400, false, []int{1, 3}, []int{2, 4})...)
	matches = append(matches, repeat(10, 500, true, []int{1, 4}, []int{2, 3})...)
	matches = append(matches, repeat(10, 600, false, []int{1, 4}, []int{2, 3})...)

	mx := Compute(matches, stubIndexer{n: 4}, rawOpts)
	var sum, n float64
	for i := range mx.Synergy {
		for j := range mx.Synergy[i] {
			if i == j {
				continue
			}
			sum += mx.Synergy[i][j] - 0.5
			n++
		}
	}
	if avg := sum / n; math.Abs(avg) > 0.02 {
		t.Errorf("mean synergy lift = %v, want ~0 after centering", avg)
	}
}

func TestMeanHelpersNeutralWhenEmpty(t *testing.T) {
	mx := Compute(nil, stubIndexer{n: 3}, Options{})
	if got := mx.MeanSynergy([]int{0}); got != 0 {
		t.Errorf("MeanSynergy with one hero = %v, want 0", got)
	}
	if got := mx.MeanCounter(nil, []int{0}); got != 0 {
		t.Errorf("MeanCounter with empty side = %v, want 0", got)
	}
	if got := mx.MeanSynergy([]int{0, 1}); got != 0 {
		t.Errorf("MeanSynergy on unobserved pair = %v, want 0 (neutral)", got)
	}
}

func TestComputeDeterministic(t *testing.T) {
	matches := []cache.Match{
		match(1, true, []int{1, 2}, []int{3}),
		match(2, false, []int{2, 3}, []int{1}),
	}
	a := Compute(matches, stubIndexer{n: 3}, Options{})
	b := Compute(matches, stubIndexer{n: 3}, Options{})
	for i := range a.Synergy {
		for j := range a.Synergy[i] {
			if a.Synergy[i][j] != b.Synergy[i][j] || a.Counter[i][j] != b.Counter[i][j] {
				t.Fatalf("non-deterministic at [%d][%d]", i, j)
			}
		}
	}
}
