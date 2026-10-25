package draft

import (
	"reflect"
	"testing"

	"cmnet/internal/features"
)

// The built-in schedule is the 7.34-7.41 one. Pin it step by step: every other
// component (manual board, planner, phase features) reads its shape from here.
func TestDefaultOrderIs734Schedule(t *testing.T) {
	const F, S = false, true
	want := []Slot{
		{F, Ban}, {F, Ban}, {S, Ban}, {S, Ban}, {F, Ban}, {S, Ban}, {S, Ban},
		{F, Pick}, {S, Pick},
		{F, Ban}, {F, Ban}, {S, Ban},
		{S, Pick}, {F, Pick}, {F, Pick}, {S, Pick}, {S, Pick}, {F, Pick},
		{F, Ban}, {S, Ban}, {F, Ban}, {S, Ban},
		{F, Pick}, {S, Pick},
	}
	if got := DefaultOrder(); !reflect.DeepEqual([]Slot(got), want) {
		t.Fatalf("default order drifted:\n got %v\nwant %v", got, want)
	}
	if got := DefaultOrder().Phases(); !reflect.DeepEqual(got, []int{7, 2, 3, 6, 4, 2}) {
		t.Fatalf("phases = %v, want [7 2 3 6 4 2]", got)
	}
}

func TestOrderValidity(t *testing.T) {
	if !DefaultOrder().Valid() {
		t.Fatal("default order must be valid")
	}
	bad := DefaultOrder()
	bad[0].Second = true // S now has 8 bans
	if bad.Valid() {
		t.Fatal("unbalanced order accepted")
	}
	sFirst := DefaultOrder()
	sFirst[0], sFirst[2] = sFirst[2], sFirst[0]
	if sFirst.Valid() {
		t.Fatal("an order opening with the second-pick team breaks the seating convention")
	}
	if Order(nil).Valid() || bad.OrDefault().Equal(bad) {
		t.Fatal("OrDefault must replace an invalid order")
	}
}

// A bundle-supplied order must actually drive the board.
func TestNewWithOrderUsesGivenSchedule(t *testing.T) {
	o := DefaultOrder()
	o[3], o[4] = o[4], o[3] // F F S F S S S: still balanced
	d := NewWithOrder(Radiant, o)
	for i := 1; i <= 3; i++ {
		_ = d.Apply(i)
	}
	if s, _ := d.Current(); s != (Step{Radiant, Ban}) {
		t.Fatalf("step 3 = %+v, want Radiant ban under the given order", s)
	}
	if s, _ := New(Radiant).Current(); s != (Step{Radiant, Ban}) {
		t.Fatalf("default first step = %+v, want Radiant ban", s)
	}
}

func TestNextStepOf(t *testing.T) {
	seq := CMSequence(Radiant)
	// From step 2 (D ban), Radiant's next action is step 4 (ban).
	if i, s, ok := NextStepOf(seq, 2, Radiant); !ok || i != 4 || s.Action != Ban {
		t.Fatalf("NextStepOf = %d %+v %v", i, s, ok)
	}
	// After step 23 nobody but Dire acts; Radiant has nothing left.
	if _, _, ok := NextStepOf(seq, 23, Radiant); ok {
		t.Fatal("Radiant should have no step at index 23")
	}
}

// The predictor's phase one-hots are sized and split by features' own copy of the
// phase lengths (features stays dependency-free). Keep the two in lockstep.
func TestFeaturePhasesMatchDefaultOrder(t *testing.T) {
	if got, want := features.PhaseBounds(), DefaultOrder().Phases(); !reflect.DeepEqual(got, want) {
		t.Fatalf("features phase bounds %v != draft phases %v", got, want)
	}
}
