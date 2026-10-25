package capability

import (
	"math"
	"testing"

	"cmnet/internal/scout"
)

func TestFromAlliesRequiresFive(t *testing.T) {
	uni := []int{1, 2, 3, 4, 5, 6}
	l2 := &scout.Layer2{Players: []scout.PlayerAffinity{
		{AccountID: 1, Nickname: "a", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 1, Games: 10}}},
		{AccountID: 2, Nickname: "b", Resolved: true, Heroes: []scout.HeroAffinity{{HeroID: 2, Games: 10}}},
	}}
	if FromAllies(l2, uni, 1) != nil {
		t.Fatal("expected nil with <5 players")
	}
}

func TestSoftPSumsNearOne(t *testing.T) {
	uni := []int{1, 2, 3, 4}
	l2 := fivePlayers(map[int64][]scout.HeroAffinity{
		1: {{HeroID: 1, Games: 8}, {HeroID: 2, Games: 2}},
		2: {{HeroID: 2, Games: 10}},
		3: {{HeroID: 3, Games: 10}},
		4: {{HeroID: 4, Games: 10}},
		5: {{HeroID: 1, Games: 5}},
	})
	r := FromAllies(l2, uni, 1)
	if r == nil {
		t.Fatal("nil roster")
	}
	for _, p := range r.Players {
		var sum float64
		for _, id := range uni {
			sum += p.SoftP[id]
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Fatalf("player %d softP sum=%v", p.AccountID, sum)
		}
	}
}

func TestAssignPrefersComfort(t *testing.T) {
	uni := []int{1, 2, 3, 4, 5}
	l2 := fivePlayers(map[int64][]scout.HeroAffinity{
		10: {{HeroID: 1, Games: 50}},
		20: {{HeroID: 2, Games: 50}},
		30: {{HeroID: 3, Games: 50}},
		40: {{HeroID: 4, Games: 50}},
		50: {{HeroID: 5, Games: 50}},
	})
	r := FromAllies(l2, uni, 0.1)
	a := Assign([]int{1, 2, 3}, r)
	if a.HeroToPlayer[1] < 0 {
		t.Fatal("missing assignment")
	}
	// Player account 10 should get hero 1
	pi := a.HeroToPlayer[1]
	if r.Players[pi].AccountID != 10 {
		t.Fatalf("hero 1 -> account %d, want 10", r.Players[pi].AccountID)
	}
	if a.Feasibility <= 0 || a.Feasibility > 1 {
		t.Fatalf("feas=%v", a.Feasibility)
	}
}

func TestBadLineupLowersFeasibility(t *testing.T) {
	uni := []int{1, 2, 3, 4, 5, 99}
	l2 := fivePlayers(map[int64][]scout.HeroAffinity{
		10: {{HeroID: 1, Games: 50}},
		20: {{HeroID: 2, Games: 50}},
		30: {{HeroID: 3, Games: 50}},
		40: {{HeroID: 4, Games: 50}},
		50: {{HeroID: 5, Games: 50}},
	})
	r := FromAllies(l2, uni, 0.1)
	good := Feasibility([]int{1, 2, 3, 4, 5}, r)
	bad := Feasibility([]int{99, 99, 99, 99, 99}, r) // uniqueSorted -> [99]
	bad = Feasibility([]int{99}, r)
	if !(bad < good) {
		t.Fatalf("bad=%v good=%v, want bad < good", bad, good)
	}
}

func fivePlayers(m map[int64][]scout.HeroAffinity) *scout.Layer2 {
	var ps []scout.PlayerAffinity
	for id, hs := range m {
		ps = append(ps, scout.PlayerAffinity{
			AccountID: id, Nickname: "p", Resolved: true, Heroes: hs,
		})
	}
	return &scout.Layer2{Players: ps}
}
