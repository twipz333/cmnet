package lineup

import (
	"testing"

	"cmnet/internal/capability"
	"cmnet/internal/roles"
	"cmnet/internal/scout"
)

func TestRoleName(t *testing.T) {
	if RoleName(2) != "мид" {
		t.Fatalf("RoleName(2)=%q", RoleName(2))
	}
}

func TestSyncRespectsLocked(t *testing.T) {
	prev := Empty()
	prev[1] = Slot{Position: 2, AccountID: 42, HeroID: 10, Locked: true}
	prior := &roles.Prior{Prob: map[int][roles.NumPositions]float64{
		10: {0.1, 0.6, 0.1, 0.1, 0.1},
		11: {0.6, 0.1, 0.1, 0.1, 0.1},
	}}
	out := Sync(prev, []int{10, 11}, prior, nil, nil)
	if !out[1].Locked || out[1].AccountID != 42 || out[1].HeroID != 10 {
		t.Fatalf("locked seat lost: %+v", out[1])
	}
}

func TestSyncClearsMissingLockedHero(t *testing.T) {
	prev := Empty()
	prev[0] = Slot{Position: 1, AccountID: 7, HeroID: 99, Locked: true}
	out := Sync(prev, []int{10}, nil, nil, nil)
	if out[0].HeroID != 0 {
		t.Fatalf("expected hero cleared, got %+v", out[0])
	}
	if out[0].AccountID != 7 || !out[0].Locked {
		t.Fatalf("lock/account should remain: %+v", out[0])
	}
}

func TestAssignFixedPins(t *testing.T) {
	uni := []int{1, 2, 3, 4, 5}
	l2 := &scout.Layer2{Players: []scout.PlayerAffinity{
		{AccountID: 10, Nickname: "a", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 1, Games: 40}}},
		{AccountID: 20, Nickname: "b", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 2, Games: 40}}},
		{AccountID: 30, Nickname: "c", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 3, Games: 40}}},
		{AccountID: 40, Nickname: "d", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 4, Games: 40}}},
		{AccountID: 50, Nickname: "e", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 5, Games: 40}}},
	}}
	r := capability.FromAllies(l2, uni, 0.1)
	if r == nil {
		t.Fatal("nil roster")
	}
	a := capability.AssignFixed([]int{1, 2}, r, map[int]int64{1: 50})
	pi := a.HeroToPlayer[1]
	if r.Players[pi].AccountID != 50 {
		t.Fatalf("hero 1 -> %d, want account 50", r.Players[pi].AccountID)
	}
}

func TestSyncEmptyBoardKeepsLockedAccount(t *testing.T) {
	prev := Empty()
	prev[0] = Slot{Position: 1, AccountID: 99, Locked: true}
	out := Sync(prev, nil, nil, nil, nil)
	if !out[0].Locked || out[0].AccountID != 99 {
		t.Fatalf("empty-board sync wiped pin: %+v", out[0])
	}
}

func TestAssignHeroSwap(t *testing.T) {
	prev := Empty()
	prev[1] = Slot{Position: 2, HeroID: 51, Locked: false} // clockwerk auto-mid
	prev[4] = Slot{Position: 5, HeroID: 0}
	out := prev.AssignHero(5, 51)
	if out[4].HeroID != 51 || !out[4].Locked {
		t.Fatalf("pos5: %+v", out[4])
	}
	if out[1].HeroID != 0 {
		t.Fatalf("mid should be empty after move: %+v", out[1])
	}
	// swap with occupied seat
	out[1].HeroID = 19
	out = out.AssignHero(2, 51) // clockwerk back to mid, 19 -> pos5
	if out[1].HeroID != 51 || out[4].HeroID != 19 {
		t.Fatalf("swap failed: mid=%+v pos5=%+v", out[1], out[4])
	}
	if !out[1].Locked || !out[4].Locked {
		t.Fatal("both seats should stay locked after swap")
	}
}

func TestAssignHeroSurvivesSync(t *testing.T) {
	prev := Empty()
	prev[1] = Slot{Position: 2, HeroID: 51}
	prev = prev.AssignHero(5, 51)
	prior := &roles.Prior{Prob: map[int][roles.NumPositions]float64{
		51: {0.05, 0.7, 0.1, 0.1, 0.05}, // strongly mid
	}}
	out := Sync(prev, []int{51}, prior, nil, nil)
	if out[4].HeroID != 51 || !out[4].Locked {
		t.Fatalf("manual pos5 lost to MAP: %+v", out)
	}
}

