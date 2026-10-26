package web

import (
	"testing"

	"cmnet/internal/draft"
)

// A GSI board only knows each side's picks and bans in order; the ribbon needs
// them slotted into the schedule with the current step marked.
func TestSequenceOfSlotsAGSIBoardIntoTheSchedule(t *testing.T) {
	snap := &draft.Snapshot{
		RadiantBans: []int{1, 2, 5}, DireBans: []int{3, 4},
		CurrentTeam: draft.Dire, FirstPick: draft.Radiant,
	}
	seq := sequenceOf(snap, draft.DefaultOrder(), draft.Radiant)
	if len(seq) != draft.CMSteps {
		t.Fatalf("%d steps, want %d", len(seq), draft.CMSteps)
	}
	want := []int{1, 2, 3, 4, 5, 0}
	for i, h := range want {
		if seq[i].HeroID != h {
			t.Fatalf("step %d hero %d, want %d (seq %+v)", i, seq[i].HeroID, h, seq[:6])
		}
	}
	if !seq[5].Current || seq[5].Team != "Dire" || seq[5].Ours {
		t.Fatalf("step 5 = %+v, want the current Dire ban", seq[5])
	}
	for i, s := range seq {
		if s.Current && i != 5 {
			t.Fatalf("step %d also marked current", i)
		}
	}
}

func TestSequenceOfManualBoardUsesItsOwnSteps(t *testing.T) {
	d := draft.New(draft.Dire)
	_ = d.Apply(7)
	seq := sequenceOf(d, nil, draft.Radiant)
	if seq[0].HeroID != 7 || seq[0].Team != "Dire" || seq[0].Ours {
		t.Fatalf("step 0 = %+v", seq[0])
	}
	if !seq[1].Current {
		t.Fatalf("step 1 should be current: %+v", seq[1])
	}
}
