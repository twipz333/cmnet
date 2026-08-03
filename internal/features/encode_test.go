package features

import (
	"math"
	"reflect"
	"testing"
)

func TestEncodeWinProbAntisymmetric(t *testing.T) {
	h := NewHeroMap([]int{30, 10, 20})
	got := h.EncodeWinProb([]int{10, 30, 999}, []int{20})
	swapped := h.EncodeWinProb([]int{20}, []int{10, 30, 999})
	if len(got) != h.Len() {
		t.Fatalf("length = %d, want %d", len(got), h.Len())
	}
	for i := range got {
		if swapped[i] != -got[i] {
			t.Fatalf("index %d: swapped %v, want %v", i, swapped[i], -got[i])
		}
	}
	if want := []float64{1, -1, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown hero affected encoding: got %v, want %v", got, want)
	}
	if empty := h.EncodeWinProb(nil, nil); !reflect.DeepEqual(empty, []float64{0, 0, 0}) {
		t.Fatalf("empty draft = %v, want zeros", empty)
	}
}

func TestEncodeWinProbExpected(t *testing.T) {
	h := NewHeroMap([]int{10, 20, 30})
	base := h.EncodeWinProb([]int{10}, []int{30})
	if got := h.EncodeWinProbExpected([]int{10}, []int{30}, nil, nil); !reflect.DeepEqual(got, base) {
		t.Fatalf("nil expectations = %v, want %v", got, base)
	}
	got := h.EncodeWinProbExpected([]int{10}, []int{30}, []float64{0.1, 0.4, 0.2}, []float64{0.05, 0.1, 0.3})
	want := []float64{1.05, 0.3, -1.1}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Fatalf("expected encoding = %v, want %v", got, want)
		}
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
	assertOneHot("phase", got[base:base+CMPhases], 1)
	base += CMPhases
	assertOneHot("action in phase", got[base:base+maxActionInPhase], 1)
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
		for phase, size := range []int{4, 4, 6, 4, 4, 2} {
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
