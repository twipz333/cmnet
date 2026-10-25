package pairstats

import (
	"regexp"
	"strconv"
	"testing"
)

var rectWidth = regexp.MustCompile(`<rect [^>]*width="([0-9.]+)"`)

func barWidths(t *testing.T, html string) []float64 {
	t.Helper()
	ms := rectWidth.FindAllStringSubmatch(html, -1)
	out := make([]float64, len(ms))
	for i, m := range ms {
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			t.Fatalf("bad width %q: %v", m[1], err)
		}
		out[i] = v
	}
	return out
}

// TestSvgBarsMonotonicNegative guards the fix: with two negative values the
// larger (less-negative) value must render the longer bar.
func TestSvgBarsMonotonicNegative(t *testing.T) {
	vals := []float64{-7.4, -1.3}
	lo, hi := impRange(vals)
	html := string(svgBars("imp", []string{"a", "b"}, vals, "", lo, hi))
	w := barWidths(t, html)
	if len(w) != 2 {
		t.Fatalf("got %d bars, want 2", len(w))
	}
	if !(w[1] > w[0]) {
		t.Fatalf("-1.3 bar (%.1f) must be longer than -7.4 bar (%.1f)", w[1], w[0])
	}
}

// TestSvgBarsMonotonicMixed checks mixed and all-positive ordering too.
func TestSvgBarsMonotonicMixed(t *testing.T) {
	vals := []float64{-1.2, 6.1}
	lo, hi := impRange(vals)
	w := barWidths(t, string(svgBars("imp", []string{"a", "b"}, vals, "", lo, hi)))
	if !(w[1] > w[0]) {
		t.Fatalf("+6.1 (%.1f) must be longer than -1.2 (%.1f)", w[1], w[0])
	}

	pct := []float64{58, 64}
	wp := barWidths(t, string(svgBars("win", []string{"a", "b"}, pct, "%", 0, 100)))
	if !(wp[1] > wp[0]) {
		t.Fatalf("64%% (%.1f) must be longer than 58%% (%.1f)", wp[1], wp[0])
	}
}
