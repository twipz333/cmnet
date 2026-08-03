package eval

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// CI is a bootstrap confidence interval around a point estimate.
type CI struct {
	Point float64
	Lo    float64 // 2.5th percentile
	Hi    float64 // 97.5th percentile
	N     int     // resamples used (0 => not computed)
}

// String renders the estimate with its interval, or just the estimate when no
// bootstrap was run.
func (c CI) String() string {
	if c.N == 0 || math.IsNaN(c.Lo) {
		return fmt.Sprintf("%.3f", c.Point)
	}
	return fmt.Sprintf("%.3f [%.3f–%.3f]", c.Point, c.Lo, c.Hi)
}

// Width is the interval width (NaN when not computed).
func (c CI) Width() float64 {
	if c.N == 0 {
		return math.NaN()
	}
	return c.Hi - c.Lo
}

// DefaultBootstrapN is the resample count. 1000 is enough for a stable 95%
// interval and cheap on these sample sizes.
const DefaultBootstrapN = 1000

// bootstrapSeed fixes the resampling stream. Determinism matters here for the same
// reason it matters everywhere else in this project: two people running the same
// eval on the same cache must see the same numbers, and a CI that jitters between
// runs is one more thing to argue about instead of a measurement.
const bootstrapSeed = 20260726

// Cluster groups predictions that must be resampled together. Draft actions within
// one match are NOT independent — the same captain, the same meta read, the same
// board — so resampling individual actions would understate the interval badly.
// The bootstrap unit is the match.
type Cluster struct {
	Scores []float64
	Labels []float64
}

// BootstrapCI resamples clusters with replacement and reports the percentile
// interval of stat over the resamples.
func BootstrapCI(clusters []Cluster, n int, stat func(scores, labels []float64) float64) CI {
	flatScores, flatLabels := flatten(clusters)
	point := stat(flatScores, flatLabels)
	if n <= 0 || len(clusters) < 2 {
		return CI{Point: point, Lo: math.NaN(), Hi: math.NaN()}
	}

	rng := rand.New(rand.NewSource(bootstrapSeed))
	vals := make([]float64, 0, n)
	scores := make([]float64, 0, len(flatScores))
	labels := make([]float64, 0, len(flatLabels))
	for b := 0; b < n; b++ {
		scores, labels = scores[:0], labels[:0]
		for i := 0; i < len(clusters); i++ {
			c := clusters[rng.Intn(len(clusters))]
			scores = append(scores, c.Scores...)
			labels = append(labels, c.Labels...)
		}
		v := stat(scores, labels)
		if !math.IsNaN(v) {
			vals = append(vals, v)
		}
	}
	if len(vals) < 2 {
		return CI{Point: point, Lo: math.NaN(), Hi: math.NaN()}
	}
	sort.Float64s(vals)
	return CI{
		Point: point,
		Lo:    percentile(vals, 0.025),
		Hi:    percentile(vals, 0.975),
		N:     len(vals),
	}
}

// PairedBootstrapDiff is the interval of (statA − statB) computed on the SAME
// resamples. Comparing two independently-computed intervals is the classic way to
// miss a real difference: the models are scored on identical matches, so most of
// the sampling noise is shared and cancels in the paired difference.
func PairedBootstrapDiff(a, b []Cluster, n int, stat func(scores, labels []float64) float64) CI {
	if len(a) != len(b) || len(a) < 2 {
		return CI{Point: math.NaN(), Lo: math.NaN(), Hi: math.NaN()}
	}
	as, al := flatten(a)
	bs, bl := flatten(b)
	point := stat(as, al) - stat(bs, bl)

	rng := rand.New(rand.NewSource(bootstrapSeed))
	vals := make([]float64, 0, n)
	for r := 0; r < n; r++ {
		var sa, la, sb, lb []float64
		for i := 0; i < len(a); i++ {
			k := rng.Intn(len(a))
			sa = append(sa, a[k].Scores...)
			la = append(la, a[k].Labels...)
			sb = append(sb, b[k].Scores...)
			lb = append(lb, b[k].Labels...)
		}
		v := stat(sa, la) - stat(sb, lb)
		if !math.IsNaN(v) {
			vals = append(vals, v)
		}
	}
	if len(vals) < 2 {
		return CI{Point: point, Lo: math.NaN(), Hi: math.NaN()}
	}
	sort.Float64s(vals)
	return CI{Point: point, Lo: percentile(vals, 0.025), Hi: percentile(vals, 0.975), N: len(vals)}
}

// RateCI is a bootstrap interval for a per-cluster hit rate (top-k accuracy):
// each cluster contributes its own hits and trials.
func RateCI(hits, trials []float64, n int) CI {
	clusters := make([]Cluster, len(hits))
	for i := range hits {
		clusters[i] = Cluster{Scores: []float64{hits[i]}, Labels: []float64{trials[i]}}
	}
	return BootstrapCI(clusters, n, func(h, t []float64) float64 {
		var sh, st float64
		for i := range h {
			sh += h[i]
			st += t[i]
		}
		if st == 0 {
			return math.NaN()
		}
		return sh / st
	})
}

func flatten(clusters []Cluster) (scores, labels []float64) {
	for _, c := range clusters {
		scores = append(scores, c.Scores...)
		labels = append(labels, c.Labels...)
	}
	return scores, labels
}

// percentile returns the p-quantile of a sorted slice (nearest-rank).
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	i := int(p * float64(len(sorted)))
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

// Significant reports whether an interval excludes zero — i.e. the difference is
// distinguishable from noise at the 95% level.
func (c CI) Significant() bool {
	if c.N == 0 || math.IsNaN(c.Lo) || math.IsNaN(c.Hi) {
		return false
	}
	return (c.Lo > 0 && c.Hi > 0) || (c.Lo < 0 && c.Hi < 0)
}
