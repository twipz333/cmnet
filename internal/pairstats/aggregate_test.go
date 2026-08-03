package pairstats

import (
	"testing"

	"cmnet/internal/sources/stratz"
)

func ip(v int) *int { return &v }

// twoMatch builds a fixed two-match set: ids 1 (carry) and 2 (mid) as teammates
// on radiant, with two enemies sharing their positions for lane-lead math.
func twoMatch() []stratz.JointMatch {
	mk := func(id, t int64, p1win bool, p1imp, p2imp int, p1nw, e1nw int) stratz.JointMatch {
		return stratz.JointMatch{
			ID: id, StartDateTime: t, DidRadiantWin: p1win,
			Players: []stratz.JointPlayer{
				{SteamAccountID: 1, HeroID: 10, IsRadiant: true, IsVictory: p1win, Position: "POSITION_1",
					Kills: 4, Deaths: 2, Assists: 6, GoldPerMinute: 600, IMP: ip(p1imp),
					NetworthPerMinute: ramp(p1nw)},
				{SteamAccountID: 2, HeroID: 20, IsRadiant: true, IsVictory: p1win, Position: "POSITION_2",
					Kills: 8, Deaths: 1, Assists: 5, GoldPerMinute: 700, IMP: ip(p2imp),
					NetworthPerMinute: ramp(5000)},
				{SteamAccountID: 99, HeroID: 30, IsRadiant: false, IsVictory: !p1win, Position: "POSITION_1",
					NetworthPerMinute: ramp(e1nw)},
				{SteamAccountID: 98, HeroID: 40, IsRadiant: false, IsVictory: !p1win, Position: "POSITION_2",
					NetworthPerMinute: ramp(4000)},
			},
		}
	}
	// Deliberately out of chronological order to exercise the sort.
	return []stratz.JointMatch{
		mk(200, 2000, false, -5, 3, 3000, 4000), // id1 loses lane (3000<4000)
		mk(100, 1000, true, 10, 6, 5000, 2000),  // id1 wins lane (5000>2000)
	}
}

// ramp returns a 20-long per-minute array whose minute-10 value is exactly v.
func ramp(v int) []int {
	out := make([]int, 20)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestBuildAggregates(t *testing.T) {
	r := Build(twoMatch(), []int64{1, 2}, 2, "test", nil, map[int]string{10: "Carry", 20: "Mid"})

	if r.TotalMatches != 2 || r.ParsedMatches != 2 {
		t.Fatalf("matches: total=%d parsed=%d, want 2/2", r.TotalMatches, r.ParsedMatches)
	}
	a1 := r.Agg[1]
	if a1.Matches != 2 || a1.Wins != 1 {
		t.Fatalf("p1 games/wins = %d/%d, want 2/1", a1.Matches, a1.Wins)
	}
	if got, _ := a1.AvgIMP(); got != 2.5 { // (10 + -5)/2
		t.Fatalf("p1 avgIMP=%v, want 2.5", got)
	}
	if a1.LaneWin != 1 || a1.LaneTot != 2 { // wins minute-10 lead once
		t.Fatalf("p1 lane=%d/%d, want 1/2", a1.LaneWin, a1.LaneTot)
	}

	// PerMatch must be chronological (id 100 before id 200) regardless of input order.
	rows := r.PerMatch[1]
	if len(rows) != 2 || rows[0].MatchID != 100 || rows[1].MatchID != 200 {
		t.Fatalf("per-match order = %v, want [100 200]", []int64{rows[0].MatchID, rows[1].MatchID})
	}
	if rows[0].LaneLead10 == nil || *rows[0].LaneLead10 != 3000 { // 5000-2000
		t.Fatalf("lane lead row0 = %v, want 3000", rows[0].LaneLead10)
	}
}

// TestDeterministic confirms identical input yields identical text output.
func TestDeterministic(t *testing.T) {
	var prev string
	for i := range 5 {
		r := Build(twoMatch(), []int64{1, 2}, 2, "test", nil, nil)
		got := r.Text()
		if i > 0 && got != prev {
			t.Fatal("non-deterministic Text() output across runs")
		}
		prev = got
	}
}
