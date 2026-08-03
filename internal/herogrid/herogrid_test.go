package herogrid

import (
	"sort"
	"testing"
)

// fakeIndexer maps the given ids to dense indices (sorted), like features.HeroMap.
type fakeIndexer struct {
	idx map[int]int
	n   int
}

func newFakeIndexer(ids ...int) *fakeIndexer {
	sort.Ints(ids)
	m := make(map[int]int, len(ids))
	for i, id := range ids {
		m[id] = i
	}
	return &fakeIndexer{idx: m, n: len(ids)}
}

func (f *fakeIndexer) Index(id int) (int, bool) { i, ok := f.idx[id]; return i, ok }
func (f *fakeIndexer) Len() int                 { return f.n }

// oneRoleGrid builds a minimal grid: one key hero (1) with one entry per list.
func oneRoleGrid() *File {
	return &File{Configs: []Config{{
		ConfigName: "Dota2ProTracker 7.41d - Carry",
		Categories: []Category{
			{CategoryName: "Top Heroes Pos 1", HeroIDs: []int{1}},
			{CategoryName: "Best with", HeroIDs: []int{2}},
			{CategoryName: "Worst with", HeroIDs: []int{3}},
			{CategoryName: "Best against", HeroIDs: []int{4}},
			{CategoryName: "Worst against", HeroIDs: []int{5}},
		},
	}}}
}

func TestOverlayBasics(t *testing.T) {
	hm := newFakeIndexer(1, 2, 3, 4, 5)
	o := oneRoleGrid().BuildOverlay(hm, DefaultParams())

	if o.Patch != "7.41d" {
		t.Errorf("patch = %q, want 7.41d", o.Patch)
	}
	if o.Heroes != 1 || o.Skipped != 0 {
		t.Errorf("heroes=%d skipped=%d, want 1/0", o.Heroes, o.Skipped)
	}

	i := func(id int) int { x, _ := hm.Index(id); return x }

	// Synergy is symmetric: best-with (1,2) > 0.5 both ways.
	if got := o.Synergy[[2]int{i(1), i(2)}]; got <= 0.5 {
		t.Errorf("best-with synergy = %v, want >0.5", got)
	}
	if o.Synergy[[2]int{i(1), i(2)}] != o.Synergy[[2]int{i(2), i(1)}] {
		t.Errorf("synergy not symmetric")
	}
	// Worst-with (1,3) < 0.5 both ways.
	if got := o.Synergy[[2]int{i(1), i(3)}]; got >= 0.5 {
		t.Errorf("worst-with synergy = %v, want <0.5", got)
	}

	// Best against: 1 strong vs 4 => counter[1][4] > 0.5; inversion counter[4][1] < 0.5.
	if got := o.Counter[[2]int{i(1), i(4)}]; got <= 0.5 {
		t.Errorf("counter[1][4] = %v, want >0.5", got)
	}
	if got := o.Counter[[2]int{i(4), i(1)}]; got >= 0.5 {
		t.Errorf("inverted counter[4][1] = %v, want <0.5", got)
	}
	// Worst against: 5 strong vs 1 => counter[5][1] > 0.5; inversion counter[1][5] < 0.5.
	if got := o.Counter[[2]int{i(5), i(1)}]; got <= 0.5 {
		t.Errorf("counter[5][1] = %v, want >0.5", got)
	}
	if got := o.Counter[[2]int{i(1), i(5)}]; got >= 0.5 {
		t.Errorf("inverted counter[1][5] = %v, want <0.5", got)
	}
}

func TestApplyToOnlyOverlaysCoveredCells(t *testing.T) {
	hm := newFakeIndexer(1, 2, 3, 4, 5)
	o := oneRoleGrid().BuildOverlay(hm, DefaultParams())

	n := hm.Len()
	syn := neutralGrid(n)
	cnt := neutralGrid(n)
	o.ApplyTo(syn, cnt)

	i := func(id int) int { x, _ := hm.Index(id); return x }
	// Covered cell changed…
	if syn[i(1)][i(2)] == 0.5 {
		t.Errorf("covered synergy cell was not overlaid")
	}
	// …uncovered cell (2 vs 3, never in any grid list together) stays neutral.
	if syn[i(2)][i(3)] != 0.5 {
		t.Errorf("uncovered synergy cell = %v, want untouched 0.5", syn[i(2)][i(3)])
	}
}

func TestRankLiftMonotonic(t *testing.T) {
	// Earlier ranks must yield >= lift than later ranks.
	prev := rankLift(0, 7, 0.10, 0.03)
	for r := 1; r < 7; r++ {
		cur := rankLift(r, 7, 0.10, 0.03)
		if cur > prev {
			t.Errorf("rankLift not monotonic: rank %d (%v) > rank %d (%v)", r, cur, r-1, prev)
		}
		prev = cur
	}
}

func TestSkippedCounted(t *testing.T) {
	hm := newFakeIndexer(1, 2, 3, 4) // 5 missing
	o := oneRoleGrid().BuildOverlay(hm, DefaultParams())
	if o.Skipped == 0 {
		t.Errorf("expected hero id 5 to be counted as skipped")
	}
}

func neutralGrid(n int) [][]float64 {
	g := make([][]float64, n)
	for i := range g {
		g[i] = make([]float64, n)
		for j := range g[i] {
			g[i][j] = 0.5
		}
	}
	return g
}
