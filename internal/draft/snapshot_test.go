package draft

import "testing"

func TestSnapshotView(t *testing.T) {
	s := &Snapshot{
		RadiantPicks:  []int{8, 14},
		RadiantBans:   []int{5},
		DirePicks:     []int{11},
		DireBans:      []int{2, 3},
		CurrentTeam:   Dire,
		CurrentIsPick: true,
	}
	if got := s.Picks(Radiant); len(got) != 2 || got[0] != 8 {
		t.Errorf("radiant picks = %v", got)
	}
	if got := s.Bans(Dire); len(got) != 2 || got[1] != 3 {
		t.Errorf("dire bans = %v", got)
	}
	step, ok := s.Current()
	if !ok || step.Team != Dire || step.Action != Pick {
		t.Errorf("current = %+v ok=%v, want Dire Pick", step, ok)
	}
	for _, id := range []int{8, 14, 5, 11, 2, 3} {
		if !s.IsTaken(id) {
			t.Errorf("hero %d should be taken", id)
		}
	}
	if s.IsTaken(99) || s.IsTaken(NoHero) {
		t.Error("untaken / NoHero reported as taken")
	}
}

func TestSnapshotDone(t *testing.T) {
	s := &Snapshot{Done: true, RadiantPicks: []int{1}}
	if _, ok := s.Current(); ok {
		t.Error("done snapshot should report Current ok=false")
	}
}

// Snapshot must satisfy View (also asserted at compile time in view.go).
func TestSnapshotIsView(t *testing.T) {
	var _ View = &Snapshot{}
}
