package scout

import (
	"context"
	"strings"
	"testing"
)

func wins(n int, won bool) []MatchRef {
	out := make([]MatchRef, n)
	for i := range out {
		out[i] = MatchRef{MatchID: int64(1000 - i), StartTime: int64(10_000 - i), Won: won, PartySize: 1}
	}
	return out
}

func TestAssessSmurfNilEmpty(t *testing.T) {
	if AssessSmurf(0, nil) != nil {
		t.Fatal("want nil")
	}
}

func TestAssessSmurfLowBaseline(t *testing.T) {
	ms := wins(20, false)
	for i := 0; i < 10; i++ {
		ms[i].Won = true
	}
	r := AssessSmurf(2000, ms)
	if r == nil || r.Level != "low" || r.Score != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestAssessSmurfThinAccountHighWR(t *testing.T) {
	ms := wins(20, true)
	r := AssessSmurf(60, ms)
	if r == nil || r.Level != "high" {
		t.Fatalf("got %+v", r)
	}
	if r.Score < 50 {
		t.Fatalf("score %d want >=50", r.Score)
	}
	if len(r.Reasons) < 2 {
		t.Fatalf("reasons = %v", r.Reasons)
	}
	for i := 1; i < len(r.Reasons); i++ {
		if r.Reasons[i-1] > r.Reasons[i] {
			t.Fatalf("reasons not sorted: %v", r.Reasons)
		}
	}
}

func TestAssessSmurfWRSpike(t *testing.T) {
	ms := make([]MatchRef, 30)
	for i := range ms {
		ms[i] = MatchRef{MatchID: int64(2000 - i), StartTime: int64(20_000 - i), PartySize: 1}
		ms[i].Won = i < 15
	}
	r := AssessSmurf(2000, ms)
	if r == nil {
		t.Fatal("nil")
	}
	found := false
	for _, s := range r.Reasons {
		if strings.Contains(s, "скачок WR") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want spike reason, got %v", r.Reasons)
	}
}

func TestAssessSmurfPartyGap(t *testing.T) {
	ms := make([]MatchRef, 0, 20)
	for i := 0; i < 10; i++ {
		ms = append(ms, MatchRef{MatchID: int64(i + 1), StartTime: int64(100 + i), Won: true, PartySize: 3})
	}
	for i := 0; i < 10; i++ {
		ms = append(ms, MatchRef{MatchID: int64(i + 100), StartTime: int64(50 + i), Won: false, PartySize: 1})
	}
	r := AssessSmurf(2000, ms)
	if r == nil {
		t.Fatal("nil")
	}
	found := false
	for _, s := range r.Reasons {
		if strings.Contains(s, "пати WR") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want party reason, got %v score=%d", r.Reasons, r.Score)
	}
}

func TestBuildLayer2AttachesSmurf(t *testing.T) {
	ms := wins(20, true)
	src := &fakeSource{
		heroes:  map[int64][]PlayerHero{7: {{HeroID: 1, Games: 40, Wins: 30}}},
		recents: map[int64][]MatchRef{7: ms},
	}
	l2 := BuildLayer2WithSmurf(context.Background(), src, []ResolvedPlayer{{AccountID: 7, Nickname: "a", Resolved: true}}, 30, nil)
	if l2.Players[0].Smurf == nil || l2.Players[0].Smurf.Level != "high" {
		t.Fatalf("smurf = %+v", l2.Players[0].Smurf)
	}
}
