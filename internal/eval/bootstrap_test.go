package eval

import (
	"math"
	"testing"
)

func mean(scores, _ []float64) float64 {
	var sum float64
	for _, x := range scores {
		sum += x
	}
	return sum / float64(len(scores))
}

func TestBootstrapCIDeterministicAndCoversPoint(t *testing.T) {
	clusters := []Cluster{
		{Scores: []float64{0, 1}}, {Scores: []float64{2, 3}},
		{Scores: []float64{4, 5}}, {Scores: []float64{6, 7}},
	}
	a, b := BootstrapCI(clusters, 500, mean), BootstrapCI(clusters, 500, mean)
	if a.Lo != b.Lo || a.Hi != b.Hi {
		t.Fatalf("fixed-seed intervals differ: %+v vs %+v", a, b)
	}
	if a.Lo > a.Point || a.Point > a.Hi {
		t.Fatalf("interval [%v,%v] does not cover point %v", a.Lo, a.Hi, a.Point)
	}
}

func TestBootstrapCIZeroResamples(t *testing.T) {
	ci := BootstrapCI([]Cluster{{Scores: []float64{1}}, {Scores: []float64{3}}}, 0, mean)
	if ci.N != 0 || !math.IsNaN(ci.Lo) || !math.IsNaN(ci.Hi) || ci.Point != 2 {
		t.Fatalf("zero-resample CI = %+v", ci)
	}
}

func TestPairedBootstrapIdenticalClusters(t *testing.T) {
	clusters := []Cluster{{Scores: []float64{1, 2}}, {Scores: []float64{3, 4}}}
	ci := PairedBootstrapDiff(clusters, clusters, 200, mean)
	if ci.Point != 0 || ci.Significant() {
		t.Fatalf("identical paired result = %+v, want zero and insignificant", ci)
	}
}

func TestCISignificant(t *testing.T) {
	cases := []struct {
		ci   CI
		want bool
	}{
		{CI{Lo: 0.1, Hi: 1, N: 10}, true},
		{CI{Lo: -1, Hi: -0.1, N: 10}, true},
		{CI{Lo: -0.1, Hi: 1, N: 10}, false},
		{CI{Lo: 0, Hi: 1, N: 10}, false},
	}
	for _, tc := range cases {
		if got := tc.ci.Significant(); got != tc.want {
			t.Errorf("Significant(%+v) = %v, want %v", tc.ci, got, tc.want)
		}
	}
}

func TestRateCIPoint(t *testing.T) {
	if got := RateCI([]float64{1, 1, 1}, []float64{2, 2, 2}, 100).Point; got != 0.5 {
		t.Fatalf("rate point = %v, want 0.5", got)
	}
}

func TestBootstrapResamplesWholeClusters(t *testing.T) {
	var correlated []Cluster
	for clusterValue := 0.0; clusterValue <= 1; clusterValue++ {
		values := make([]float64, 20)
		for i := range values {
			values[i] = clusterValue
		}
		correlated = append(correlated, Cluster{Scores: values})
	}
	var independent []Cluster
	for _, c := range correlated {
		for _, value := range c.Scores {
			independent = append(independent, Cluster{Scores: []float64{value}})
		}
	}
	clusteredCI := BootstrapCI(correlated, 1000, mean)
	independentCI := BootstrapCI(independent, 1000, mean)
	if clusteredCI.Width() <= independentCI.Width() {
		t.Fatalf("clustered width %v, want greater than pointwise width %v", clusteredCI.Width(), independentCI.Width())
	}
}
