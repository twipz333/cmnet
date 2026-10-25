package model

import "math"

// FM is a factorization-machine draft evaluator: hero main effects plus low-rank
// interactions, in log-odds.
//
//	z = bias
//	  + Σ_{i∈R} main_i − Σ_{i∈D} main_i                        // individual strength
//	  + Σ_{i<j∈R} ⟨e_i,e_j⟩ − Σ_{i<j∈D} ⟨e_i,e_j⟩              // teammate synergy
//	  + ⟨Σ_R a, Σ_D b⟩ − ⟨Σ_D a, Σ_R b⟩                        // cross-team matchup
//
// Why low-rank instead of a weight per hero pair: 127 heroes make ~8k teammate
// pairs and ~16k matchups, and a 30k-match corpus cannot estimate that many
// independent numbers — most pairs would be fitted on a handful of games. Rank-k
// embeddings share statistical strength between pairs (heroes that behave alike
// end up close in the latent space), so a pair seen twice still gets a sensible
// value inferred from everything its two heroes did separately.
//
// The whole form is ANTISYMMETRIC under swapping the sides: every term flips sign,
// except the bias, which is exactly the Radiant map advantage. That symmetry is a
// property of the parameterization, not something the fit has to discover.
type FM struct {
	Bias   float64     `json:"bias"`
	Main   []float64   `json:"main"`    // [N]
	Emb    [][]float64 `json:"emb"`     // [N][K] teammate-interaction factors
	CrossA [][]float64 `json:"cross_a"` // [N][K] matchup factors, attacking side
	CrossB [][]float64 `json:"cross_b"` // [N][K] matchup factors, defending side
}

// FMSample is one labeled composition.
type FMSample struct {
	Radiant []int // dense hero indices
	Dire    []int
	Y       float64 // 1 if radiant won
	Weight  float64
}

// FMConfig tunes the factorization machine.
type FMConfig struct {
	K         int     // latent rank; 0 => DefaultFMRank
	Epochs    int     // 0 => model default
	LR        float64 // 0 => model default
	L2Main    float64 // penalty on main effects
	L2Emb     float64 // penalty on the factors; should be stronger than L2Main
	Patience  int     // early-stopping patience in evaluations; 0 => model default
	EvalEvery int
	Curve     func(CurvePoint)
}

// Defaults for the factorization machine. Rank 4 is the starting point: with only
// a few thousand Captains Mode drafts, anything above 8 has more capacity than the
// data can identify, and the extra factors just memorize.
const (
	DefaultFMRank   = 4
	DefaultFML2Main = 1e-3
	DefaultFML2Emb  = 1e-2 // factors are penalized harder than main effects
	fmInitScale     = 0.01
)

func (c FMConfig) withDefaults() FMConfig {
	if c.K <= 0 {
		c.K = DefaultFMRank
	}
	if c.Epochs == 0 {
		c.Epochs = DefaultEpochs
	}
	if c.LR == 0 {
		c.LR = DefaultLR
	}
	if c.L2Main == 0 {
		c.L2Main = DefaultFML2Main
	}
	if c.L2Emb == 0 {
		c.L2Emb = DefaultFML2Emb
	}
	if c.EvalEvery <= 0 {
		c.EvalEvery = DefaultEvalEvery
	}
	return c
}

// NewFM allocates a factorization machine with deterministic small initial
// factors. Zero init would be a saddle point — every factor gradient vanishes by
// symmetry — so the values come from a fixed integer LCG rather than a random
// source: same weights on every platform, every run (§6).
func NewFM(n, k int) *FM {
	f := &FM{
		Main:   make([]float64, n),
		Emb:    make([][]float64, n),
		CrossA: make([][]float64, n),
		CrossB: make([][]float64, n),
	}
	seed := uint32(2463534242)
	next := func() float64 {
		// xorshift32: deterministic, integer-only, identical everywhere.
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		return (float64(seed)/float64(math.MaxUint32) - 0.5) * 2 * fmInitScale
	}
	for i := 0; i < n; i++ {
		f.Emb[i] = make([]float64, k)
		f.CrossA[i] = make([]float64, k)
		f.CrossB[i] = make([]float64, k)
		for d := 0; d < k; d++ {
			f.Emb[i][d] = next()
			f.CrossA[i][d] = next()
			f.CrossB[i][d] = next()
		}
	}
	return f
}

// Rank returns the latent dimension.
func (f *FM) Rank() int {
	if len(f.Emb) == 0 {
		return 0
	}
	return len(f.Emb[0])
}

// Logit scores a composition.
func (f *FM) Logit(radiant, dire []int) float64 {
	z := f.Bias
	for _, i := range radiant {
		z += f.Main[i]
	}
	for _, i := range dire {
		z -= f.Main[i]
	}
	z += f.teamSynergy(radiant) - f.teamSynergy(dire)

	k := f.Rank()
	aR, bR := sumRows(f.CrossA, radiant, k), sumRows(f.CrossB, radiant, k)
	aD, bD := sumRows(f.CrossA, dire, k), sumRows(f.CrossB, dire, k)
	z += dot(aR, bD) - dot(aD, bR)
	return z
}

// Predict returns P(radiant win).
func (f *FM) Predict(radiant, dire []int) float64 { return sigmoid(f.Logit(radiant, dire)) }

// teamSynergy is Σ_{i<j} ⟨e_i,e_j⟩ via the standard FM identity, which turns an
// O(n²) pair sum into O(n·k).
func (f *FM) teamSynergy(team []int) float64 {
	k := f.Rank()
	if k == 0 || len(team) < 2 {
		return 0
	}
	s := sumRows(f.Emb, team, k)
	var sq float64
	for _, i := range team {
		for d := 0; d < k; d++ {
			sq += f.Emb[i][d] * f.Emb[i][d]
		}
	}
	return 0.5 * (dot(s, s) - sq)
}

func sumRows(rows [][]float64, idx []int, k int) []float64 {
	out := make([]float64, k)
	for _, i := range idx {
		for d := 0; d < k; d++ {
			out[d] += rows[i][d]
		}
	}
	return out
}

// TrainFM fits the factorization machine with deterministic full-batch gradient
// descent and optional early stopping on val.
func TrainFM(samples, val []FMSample, n int, cfg FMConfig) *FM {
	cfg = cfg.withDefaults()
	f := NewFM(n, cfg.K)
	if len(samples) == 0 {
		return f
	}
	k := cfg.K

	var sumW float64
	for _, s := range samples {
		sumW += normWeight(s.Weight)
	}
	if sumW == 0 {
		sumW = 1
	}

	gMain := make([]float64, n)
	gEmb := newGrid(n, k)
	gA := newGrid(n, k)
	gB := newGrid(n, k)

	best := NewFM(n, k)
	bestLoss := math.Inf(1)
	stale := 0

	for e := 0; e < cfg.Epochs; e++ {
		zeroSlice(gMain)
		zeroGrid(gEmb)
		zeroGrid(gA)
		zeroGrid(gB)
		var gBias float64

		for _, s := range samples {
			w := normWeight(s.Weight)
			err := w * (sigmoid(f.Logit(s.Radiant, s.Dire)) - s.Y)
			gBias += err

			// Main effects.
			for _, i := range s.Radiant {
				gMain[i] += err
			}
			for _, i := range s.Dire {
				gMain[i] -= err
			}

			// Teammate factors: d/de_i of Σ_{i<j}⟨e_i,e_j⟩ is (Σ_j e_j) − e_i.
			accumulateTeamGrad(gEmb, f.Emb, s.Radiant, err, k)
			accumulateTeamGrad(gEmb, f.Emb, s.Dire, -err, k)

			// Cross-team factors.
			aR, bR := sumRows(f.CrossA, s.Radiant, k), sumRows(f.CrossB, s.Radiant, k)
			aD, bD := sumRows(f.CrossA, s.Dire, k), sumRows(f.CrossB, s.Dire, k)
			for _, i := range s.Radiant {
				for d := 0; d < k; d++ {
					gA[i][d] += err * bD[d]
					gB[i][d] -= err * aD[d]
				}
			}
			for _, j := range s.Dire {
				for d := 0; d < k; d++ {
					gA[j][d] -= err * bR[d]
					gB[j][d] += err * aR[d]
				}
			}
		}

		f.Bias -= cfg.LR * gBias / sumW
		for i := 0; i < n; i++ {
			f.Main[i] -= cfg.LR * (gMain[i]/sumW + cfg.L2Main*f.Main[i])
			for d := 0; d < k; d++ {
				f.Emb[i][d] -= cfg.LR * (gEmb[i][d]/sumW + cfg.L2Emb*f.Emb[i][d])
				f.CrossA[i][d] -= cfg.LR * (gA[i][d]/sumW + cfg.L2Emb*f.CrossA[i][d])
				f.CrossB[i][d] -= cfg.LR * (gB[i][d]/sumW + cfg.L2Emb*f.CrossB[i][d])
			}
		}

		epoch := e + 1
		if epoch%cfg.EvalEvery != 0 && epoch != cfg.Epochs {
			continue
		}
		if cfg.Curve == nil && (len(val) == 0 || cfg.Patience <= 0) {
			continue
		}
		point := CurvePoint{Epoch: epoch, TrainLoss: fmLoss(f, samples), ValLoss: math.NaN()}
		if len(val) > 0 {
			point.ValLoss = fmLoss(f, val)
		}
		if cfg.Curve != nil {
			cfg.Curve(point)
		}
		if len(val) == 0 || cfg.Patience <= 0 {
			continue
		}
		if point.ValLoss < bestLoss {
			bestLoss = point.ValLoss
			copyFM(best, f)
			stale = 0
			continue
		}
		stale++
		if stale >= cfg.Patience {
			return best
		}
	}
	if len(val) > 0 && cfg.Patience > 0 && !math.IsInf(bestLoss, 1) {
		return best
	}
	return f
}

// accumulateTeamGrad adds the teammate-factor gradient for one side.
func accumulateTeamGrad(g, emb [][]float64, team []int, err float64, k int) {
	if len(team) < 2 {
		return
	}
	s := sumRows(emb, team, k)
	for _, i := range team {
		for d := 0; d < k; d++ {
			g[i][d] += err * (s[d] - emb[i][d])
		}
	}
}

func fmLoss(f *FM, samples []FMSample) float64 {
	var sum, sw float64
	for _, s := range samples {
		w := normWeight(s.Weight)
		p := clampProb(f.Predict(s.Radiant, s.Dire))
		if s.Y > 0.5 {
			sum += -w * math.Log(p)
		} else {
			sum += -w * math.Log(1-p)
		}
		sw += w
	}
	if sw == 0 {
		return math.NaN()
	}
	return sum / sw
}

func newGrid(n, k int) [][]float64 {
	g := make([][]float64, n)
	for i := range g {
		g[i] = make([]float64, k)
	}
	return g
}

func zeroSlice(s []float64) {
	for i := range s {
		s[i] = 0
	}
}

func zeroGrid(g [][]float64) {
	for i := range g {
		zeroSlice(g[i])
	}
}

func copyFM(dst, src *FM) {
	dst.Bias = src.Bias
	copy(dst.Main, src.Main)
	for i := range src.Emb {
		copy(dst.Emb[i], src.Emb[i])
		copy(dst.CrossA[i], src.CrossA[i])
		copy(dst.CrossB[i], src.CrossB[i])
	}
}
