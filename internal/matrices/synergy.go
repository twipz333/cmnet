// Package matrices computes hero synergy and counter matrices directly from cached
// matches (no ML). These feed the recommender's draft evaluation and the strength
// stacker.
//
// The values are NOT raw pair win rates. A raw pair win rate is dominated by the
// individual strength of the two heroes (a 55%-meta hero looks like "good synergy"
// with everyone), so each cell holds the LIFT: how much the pair over/under-performs
// what the two heroes' solo strengths already predict.
//
// Two things make this honest where a plain smoothed win rate is not:
//
//  1. The arithmetic happens in LOG-ODDS, not in probability space. Win-rate
//     contributions compose additively in log-odds — that is the model under which
//     "hero A is worth +0.2 and hero B is worth +0.2, so together +0.4" is even a
//     coherent sentence. Subtracting probabilities, as an earlier version did,
//     mis-states the expectation at the tails and drifts with the base rate.
//
//  2. Cells are shrunk toward zero by EMPIRICAL BAYES rather than by one fixed
//     pseudo-count. A pair seen 8 times and a pair seen 800 times carry wildly
//     different amounts of evidence, and a constant smoothing=4 treats them nearly
//     alike. Empirical Bayes estimates how much true pair-to-pair variation exists
//     (τ²) and shrinks each cell in proportion to its own sampling noise, so rare
//     pairs collapse to neutral while well-observed ones keep their signal. This
//     matters most for beam search, which actively hunts for the highest cell it
//     can find — precisely the cell most likely to be noise.
//
// Stored values are converted back to "win-rate points around 0.5" (the scale every
// consumer and the hero-grid overlay already speak), using the first-order mapping
// Δp ≈ Δlogit/4 that holds near a 50% base rate.
//
// Computation is deterministic: fixed iteration order, integer counts, closed-form
// estimators — no sampling anywhere.
package matrices

import (
	"fmt"
	"math"

	"cmnet/internal/cache"
)

// HeroIndexer maps a Valve hero id to a dense index. *features.HeroMap satisfies
// it; declared here to avoid importing features (which would be a heavier dep).
type HeroIndexer interface {
	Len() int
	Index(id int) (int, bool)
}

// DefaultPrior is the additive pseudo-count applied to a pair's own win rate
// before taking its logit. It exists only to keep logit(0) and logit(1) finite —
// the actual regularization is the empirical-Bayes shrinkage, not this.
const DefaultPrior = 2.0

// minGamesForTau is the per-pair sample floor for a cell to inform the τ²
// estimate. Cells below it are still shrunk, they just do not get a vote on how
// much true variation exists.
const minGamesForTau = 20

// logitToProbPoints converts a log-odds lift to win-rate points near a 50% base
// rate: the derivative of the logistic at p=0.5 is 1/4.
const logitToProbPoints = 0.25

// Options tunes matrix computation.
type Options struct {
	// Prior is the additive pseudo-count keeping logits finite (0 => DefaultPrior).
	Prior float64
	// NoShrink disables empirical-Bayes shrinkage (diagnostics only).
	NoShrink bool
}

func (o Options) withDefaults() Options {
	if o.Prior <= 0 {
		o.Prior = DefaultPrior
	}
	return o
}

// Matrices holds the synergy and counter lift matrices, indexed by dense hero
// index, expressed in win-rate points around 0.5 (0.5 == neutral). Synergy[i][j]
// is the teammate lift of heroes i and j. Counter[i][j] is the lift of hero i's
// team when i faces hero j on the enemy team.
type Matrices struct {
	Synergy [][]float64
	Counter [][]float64

	// Tau2Synergy / Tau2Counter are the estimated variances of the TRUE pair
	// effects, in log-odds. They are reported rather than hidden because zero is
	// a real and important answer: it means the observed pair-to-pair spread is
	// fully explained by sampling noise, every cell shrinks to neutral, and the
	// matrices carry nothing. Seeing that number is the difference between
	// "synergy is unmeasurable at this sample size" and "the code is broken".
	Tau2Synergy float64
	Tau2Counter float64
}

// SignalNote describes, in one line, whether the matrices carry measurable pair
// signal — for the training log, so a neutral matrix is never mistaken for a bug.
func (m *Matrices) SignalNote() string {
	if m == nil {
		return "matrices: none"
	}
	if m.Tau2Synergy <= 0 && m.Tau2Counter <= 0 {
		return "matrices: NO measurable pair signal (tau^2 = 0 for both synergy and counter) — " +
			"the pair-to-pair spread is fully explained by sampling noise, so every cell shrank to " +
			"neutral. This needs more matches per pair, not a code change"
	}
	return fmt.Sprintf("matrices: tau^2 synergy %.4f / counter %.4f (log-odds sd %.3f / %.3f)",
		m.Tau2Synergy, m.Tau2Counter, math.Sqrt(m.Tau2Synergy), math.Sqrt(m.Tau2Counter))
}

type counter struct {
	wins  int
	games int
}

// Compute builds the synergy/counter lift matrices from matches over the
// indexer's hero set.
func Compute(matches []cache.Match, hm HeroIndexer, opts Options) *Matrices {
	opts = opts.withDefaults()
	n := hm.Len()
	syn := newCounterGrid(n)
	cnt := newCounterGrid(n)
	solo := make([]counter, n) // per-hero solo win/games, the lift baseline

	for _, m := range matches {
		radiant, dire := teamPicks(m, hm)

		// Solo: every picked hero gets a win/game on its own side's outcome.
		for _, i := range radiant {
			bump(&solo[i], m.RadiantWin)
		}
		for _, i := range dire {
			bump(&solo[i], !m.RadiantWin)
		}

		// Synergy: every teammate pair, both orientations.
		accumulatePairs(syn, radiant, radiant, m.RadiantWin, true)
		accumulatePairs(syn, dire, dire, !m.RadiantWin, true)

		// Counter: radiant hero i vs dire hero j (i wins if radiant wins), and the
		// mirror dire-vs-radiant.
		accumulatePairs(cnt, radiant, dire, m.RadiantWin, false)
		accumulatePairs(cnt, dire, radiant, !m.RadiantWin, false)
	}

	// Solo strength in log-odds. Solo samples are orders of magnitude larger than
	// pair samples, so their own sampling error is negligible next to the pairs'.
	soloLogit := make([]float64, n)
	for i := range solo {
		soloLogit[i] = logit(rate(solo[i], opts.Prior))
	}

	synOut, synTau := finalize(syn, opts, func(i, j int) float64 { return soloLogit[i] + soloLogit[j] })
	cntOut, cntTau := finalize(cnt, opts, func(i, j int) float64 { return soloLogit[i] - soloLogit[j] })
	return &Matrices{
		Synergy:     synOut,
		Counter:     cntOut,
		Tau2Synergy: synTau,
		Tau2Counter: cntTau,
	}
}

// expectedFn returns the log-odds a pair's win rate is expected to hit from the
// two heroes' solo strengths alone.
type expectedFn func(i, j int) float64

// finalize turns raw pair counts into empirical-Bayes-shrunk lift, then converts
// to win-rate points around 0.5.
//
// Under an additive log-odds model of match outcome, two teammates are expected to
// win at logit(pᵢ)+logit(pⱼ) and a matchup at logit(pᵢ)−logit(pⱼ); anything beyond
// that is genuine pair interaction. The raw residual is noisy, so it is shrunk by
// τ²/(τ²+Vᵢⱼ), the posterior mean under a normal-normal hierarchy where Vᵢⱼ is the
// cell's own sampling variance (delta method) and τ² is the spread of true lifts
// estimated from the data itself.
func finalize(grid [][]counter, opts Options, expected expectedFn) ([][]float64, float64) {
	n := len(grid)
	raw := make([][]float64, n)
	variance := make([][]float64, n)
	for i := range grid {
		raw[i] = make([]float64, n)
		variance[i] = make([]float64, n)
		for j := range grid[i] {
			c := grid[i][j]
			if c.games == 0 {
				variance[i][j] = math.Inf(1) // no evidence: shrinks to exactly neutral
				continue
			}
			p := rate(c, opts.Prior)
			raw[i][j] = logit(p) - expected(i, j)
			variance[i][j] = 1 / (float64(c.games) * p * (1 - p)) // delta method
		}
	}

	// Center the raw lifts before shrinking.
	//
	// The additive log-odds expectation is only exact if a hero's solo rate is
	// measured across independent partners. It never is: heroes cluster into the
	// same lineups, so logit(pᵢ)+logit(pⱼ) systematically over- or under-shoots and
	// every cell inherits the same offset. Subtracting the mean removes that shared
	// bias and leaves what consumers actually want — how a pair compares to a
	// TYPICAL pair, with 0.5 meaning "nothing special here".
	mean := meanLift(grid, raw)
	for i := range raw {
		for j := range raw[i] {
			if grid[i][j].games > 0 {
				raw[i][j] -= mean
			}
		}
	}

	tau2 := estimateTau2(grid, raw, variance)

	out := make([][]float64, n)
	for i := range raw {
		out[i] = make([]float64, n)
		for j := range raw[i] {
			if grid[i][j].games == 0 {
				out[i][j] = 0.5 // never observed together: exactly neutral
				continue
			}
			lift := raw[i][j]
			if !opts.NoShrink {
				lift *= shrinkFactor(tau2, variance[i][j])
			}
			out[i][j] = clamp01(0.5 + lift*logitToProbPoints)
		}
	}
	return out, tau2
}

// meanLift is the games-weighted mean of the observed raw lifts. Weighting by
// sample size keeps a swarm of one-game pairs from dragging the center around.
func meanLift(grid [][]counter, raw [][]float64) float64 {
	var sum, weight float64
	for i := range grid {
		for j := range grid[i] {
			g := float64(grid[i][j].games)
			if g == 0 {
				continue
			}
			sum += raw[i][j] * g
			weight += g
		}
	}
	if weight == 0 {
		return 0
	}
	return sum / weight
}

// shrinkFactor is the empirical-Bayes weight τ²/(τ²+V): 1 when the cell is
// measured far more precisely than the true spread, ~0 when it is mostly noise.
func shrinkFactor(tau2, v float64) float64 {
	if tau2 <= 0 || math.IsInf(v, 1) {
		return 0
	}
	return tau2 / (tau2 + v)
}

// estimateTau2 estimates the variance of the TRUE pair lifts with the
// DerSimonian-Laird estimator.
//
// The naive version — observed spread minus MEAN sampling variance — collapses to
// zero on this data, and with it every cell collapses to neutral. The reason is
// that the mean is dominated by the thin pairs: a pair seen 20 times has sampling
// variance around 0.2 in log-odds, while the real spread of hero synergy is nearer
// 0.01, so the average noise term swamps the signal and the estimate goes
// negative no matter how many well-observed pairs exist.
//
// DerSimonian-Laird weights each cell by its own precision (1/V), so the pairs
// with hundreds of games — the ones that actually carry information about how much
// synergy varies — dominate the estimate, and the noisy ones contribute in
// proportion to how little they know.
func estimateTau2(grid [][]counter, raw, variance [][]float64) float64 {
	var sumW, sumW2, sumWL float64
	var k int
	for i := range grid {
		for j := range grid[i] {
			if grid[i][j].games < minGamesForTau {
				continue
			}
			v := variance[i][j]
			if v <= 0 || math.IsInf(v, 1) {
				continue
			}
			w := 1 / v
			sumW += w
			sumW2 += w * w
			sumWL += w * raw[i][j]
			k++
		}
	}
	if k < 2 || sumW == 0 {
		return 0
	}
	meanW := sumWL / sumW

	// Q is the weighted heterogeneity statistic; under "all cells share one true
	// lift" it has expectation k-1, so any excess is real between-pair variance.
	var q float64
	for i := range grid {
		for j := range grid[i] {
			if grid[i][j].games < minGamesForTau {
				continue
			}
			v := variance[i][j]
			if v <= 0 || math.IsInf(v, 1) {
				continue
			}
			d := raw[i][j] - meanW
			q += d * d / v
		}
	}
	denom := sumW - sumW2/sumW
	if denom <= 0 {
		return 0
	}
	if tau2 := (q - float64(k-1)) / denom; tau2 > 0 {
		return tau2
	}
	return 0
}

// MeanSynergy is the average teammate lift over all distinct unordered pairs among
// the dense indices, returned in LOG-ODDS (0 = neutral). Callers that combine it
// with a model logit need it on that scale.
func (m *Matrices) MeanSynergy(idx []int) float64 {
	if m == nil || len(idx) < 2 {
		return 0
	}
	var sum float64
	var n int
	for i := 0; i < len(idx); i++ {
		for j := i + 1; j < len(idx); j++ {
			sum += m.Synergy[idx[i]][idx[j]] - 0.5
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) / logitToProbPoints
}

// MeanCounter is the average matchup lift of team a facing team b, in log-odds.
func (m *Matrices) MeanCounter(a, b []int) float64 {
	if m == nil || len(a) == 0 || len(b) == 0 {
		return 0
	}
	var sum float64
	var n int
	for _, i := range a {
		for _, j := range b {
			sum += m.Counter[i][j] - 0.5
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) / logitToProbPoints
}

func newCounterGrid(n int) [][]counter {
	g := make([][]counter, n)
	for i := range g {
		g[i] = make([]counter, n)
	}
	return g
}

// teamPicks returns the dense indices of each team's picked heroes.
func teamPicks(m cache.Match, hm HeroIndexer) (radiant, dire []int) {
	for _, pb := range m.PickBans {
		if !pb.IsPick {
			continue
		}
		idx, ok := hm.Index(pb.HeroID)
		if !ok {
			continue
		}
		if pb.Team == 0 {
			radiant = append(radiant, idx)
		} else {
			dire = append(dire, idx)
		}
	}
	return radiant, dire
}

// accumulatePairs records a win/game for pairs across groups a and b. When
// sameTeam is true, a and b are the same group and only distinct unordered pairs
// are counted (each stored in both [i][j] and [j][i]); when false, every a×b pair
// is recorded directionally as a[i] facing b[j].
func accumulatePairs(grid [][]counter, a, b []int, win, sameTeam bool) {
	for ai, i := range a {
		for bj, j := range b {
			if sameTeam {
				if bj <= ai { // distinct unordered pairs only
					continue
				}
				bump(&grid[i][j], win)
				bump(&grid[j][i], win)
			} else {
				bump(&grid[i][j], win)
			}
		}
	}
}

func bump(c *counter, win bool) {
	c.games++
	if win {
		c.wins++
	}
}

// rate is the additively-smoothed win rate of a counter (0.5 with no games).
func rate(c counter, prior float64) float64 {
	return (float64(c.wins) + prior*0.5) / (float64(c.games) + prior)
}

// logit is the log-odds of p, clamped so the extremes stay finite.
func logit(p float64) float64 {
	const eps = 1e-6
	if p < eps {
		p = eps
	} else if p > 1-eps {
		p = 1 - eps
	}
	return math.Log(p / (1 - p))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
