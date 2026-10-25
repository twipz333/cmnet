package features

import (
	"math"
	"testing"
)

func TestEncodeWinProbAntisymmetric(t *testing.T) {
	h := NewHeroMap([]int{30, 10, 20})
	got := h.EncodeWinProb([]int{10, 30, 999}, []int{20})
	swapped := h.EncodeWinProb([]int{20}, []int{10, 30, 999})
	if len(got) != h.WinProbLen() {
		t.Fatalf("length = %d, want %d", len(got), h.WinProbLen())
	}
	for i := range got {
		if swapped[i] != -got[i] {
			t.Fatalf("index %d: swapped %v, want %v", i, swapped[i], -got[i])
		}
	}
	n := h.Len()
	// Main effects only: hero order in map is 30,10,20 → indices 0,1,2.
	wantMain := []float64{1, -1, 1} // sorted map 10,20,30
	for i := 0; i < n; i++ {
		if got[i] != wantMain[i] {
			t.Fatalf("main[%d]=%v, want %v (full=%v)", i, got[i], wantMain[i], got[:n])
		}
	}
	for i := n; i < len(got); i++ {
		if got[i] != 0 {
			t.Fatalf("position block[%d]=%v, want 0 without labels", i, got[i])
		}
	}
	empty := h.EncodeWinProb(nil, nil)
	if len(empty) != h.WinProbLen() {
		t.Fatalf("empty len = %d, want %d", len(empty), h.WinProbLen())
	}
	for i, v := range empty {
		if v != 0 {
			t.Fatalf("empty[%d]=%v, want 0", i, v)
		}
	}
}

func TestEncodeWinProbExpected(t *testing.T) {
	h := NewHeroMap([]int{10, 20, 30})
	base := h.EncodeWinProb([]int{10}, []int{30})
	if got := h.EncodeWinProbExpected([]int{10}, []int{30}, nil, nil); len(got) != len(base) {
		t.Fatalf("nil expectations len %d vs %d", len(got), len(base))
	} else {
		for i := range got {
			if got[i] != base[i] {
				t.Fatalf("nil expectations = %v, want %v", got, base)
			}
		}
	}
	got := h.EncodeWinProbExpected([]int{10}, []int{30}, []float64{0.1, 0.4, 0.2}, []float64{0.05, 0.1, 0.3})
	// Main: +1 on 10, -1 on 30, plus expectations on main only.
	wantMain := []float64{1.05, 0.3, -1.1}
	for i := 0; i < h.Len(); i++ {
		if math.Abs(got[i]-wantMain[i]) > 1e-12 {
			t.Fatalf("main expected encoding = %v, want %v", got[:h.Len()], wantMain)
		}
	}
}

func TestEncodeWinProbPositions(t *testing.T) {
	h := NewHeroMap([]int{10, 20, 30})
	x := h.EncodeWinProbPositions([]int{10}, []int{20}, map[int]int{10: 1}, map[int]int{20: 3})
	if len(x) != h.WinProbLen() {
		t.Fatalf("len=%d want %d", len(x), h.WinProbLen())
	}
	i10, _ := h.Index(10)
	i20, _ := h.Index(20)
	n := h.Len()
	if x[i10] != 1 || x[i20] != -1 {
		t.Fatalf("main effects: %v", x[:n])
	}
	if x[n+i10*WinProbPositions+0] != 1 {
		t.Fatalf("radiant pos1 slot = %v", x[n+i10*WinProbPositions+0])
	}
	if x[n+i20*WinProbPositions+2] != -1 {
		t.Fatalf("dire pos3 slot = %v", x[n+i20*WinProbPositions+2])
	}
}

func TestEncodeWinProbSoft(t *testing.T) {
	h := NewHeroMap([]int{10, 20})
	roc := map[int][WinProbPositions]float64{10: {0.7, 0.3, 0, 0, 0}}
	doc := map[int][WinProbPositions]float64{20: {0, 0, 1, 0, 0}}
	x := h.EncodeWinProbSoft([]int{10}, []int{20}, roc, doc)
	i10, _ := h.Index(10)
	n := h.Len()
	if abs := x[n+i10*WinProbPositions+0]; abs < 0.699 || abs > 0.701 {
		t.Fatalf("soft pos1 = %v", abs)
	}
}

func TestWinProbHeroes(t *testing.T) {
	if got := WinProbHeroes(18); got != 3 {
		t.Fatalf("v2: got %d", got)
	}
	if got := WinProbHeroes(3); got != 3 {
		t.Fatalf("v1: got %d", got)
	}
}

func TestEncodePredictorActorCentricAndLayout(t *testing.T) {
	h := NewHeroMap([]int{10, 20, 30, 40})
	dire := PredictorState{
		RadiantPicks: []int{10}, DirePicks: []int{20},
		RadiantBans: []int{30}, DireBans: []int{40},
		IsPick: true, Actor: teamDire, Ord: 5, FirstPick: teamRadiant,
	}
	got := h.EncodePredictor(dire)
	if len(got) != 4*h.Len()+predictorExtras {
		t.Fatalf("length = %d, want %d", len(got), 4*h.Len()+predictorExtras)
	}
	i20, _ := h.Index(20)
	if got[i20] != 1 {
		t.Fatalf("dire actor's pick missing from actor block: %v", got[:h.Len()])
	}

	mirror := PredictorState{
		RadiantPicks: []int{20}, DirePicks: []int{10},
		RadiantBans: []int{40}, DireBans: []int{30},
		IsPick: true, Actor: teamRadiant, Ord: 5, FirstPick: teamRadiant,
	}
	mirrored := h.EncodePredictor(mirror)
	firstFlag := 4*h.Len() + CMSteps + CMPhases + maxActionInPhase + 1
	for i := range got {
		if i != firstFlag && got[i] != mirrored[i] {
			t.Fatalf("mirrored actor differs at %d: %v vs %v", i, got[i], mirrored[i])
		}
	}
	if got[firstFlag] == mirrored[firstFlag] {
		t.Fatal("actor-drafts-first flag should be the only mirrored difference")
	}

	base := 4 * h.Len()
	assertOneHot := func(name string, values []float64, want int) {
		t.Helper()
		ones := 0
		for i, v := range values {
			if v != 0 {
				ones++
				if i != want || v != 1 {
					t.Fatalf("%s one-hot set at %d=%v, want %d=1", name, i, v, want)
				}
			}
		}
		if ones != 1 {
			t.Fatalf("%s has %d set positions, want 1", name, ones)
		}
	}
	assertOneHot("step", got[base:base+CMSteps], 5)
	base += CMSteps
	// Step 5 is the sixth ban of the opening seven-ban phase (7.34+ schedule).
	assertOneHot("phase", got[base:base+CMPhases], 0)
	base += CMPhases
	assertOneHot("action in phase", got[base:base+maxActionInPhase], 5)
}

func TestEncodePredictorDerivesUnknownOrd(t *testing.T) {
	h := NewHeroMap([]int{10, 20, 30})
	s := PredictorState{
		RadiantPicks: []int{10}, DirePicks: []int{20},
		RadiantBans: []int{30}, Ord: -1,
	}
	got := h.EncodePredictor(s)
	base := 4 * h.Len()
	if got[base+3] != 1 {
		t.Fatalf("derived step not set at board count 3: %v", got[base:base+CMSteps])
	}
}

func TestPhaseOfCaptainsModeBoundaries(t *testing.T) {
	for ord := -1; ord <= 24; ord++ {
		wantPhase, wantIn := -1, -1
		start := 0
		for phase, size := range []int{7, 2, 3, 6, 4, 2} {
			if ord >= start && ord < start+size {
				wantPhase, wantIn = phase, ord-start
				break
			}
			start += size
		}
		phase, in := PhaseOf(ord)
		if phase != wantPhase || in != wantIn {
			t.Errorf("PhaseOf(%d) = (%d,%d), want (%d,%d)", ord, phase, in, wantPhase, wantIn)
		}
	}
}