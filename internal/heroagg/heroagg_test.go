package heroagg

import (
	"testing"

	"cmnet/internal/cache"
)

// mkMatch builds a match where radiant heroes are 1..5 on positions 1..5 and dire
// heroes are 11..15 on positions 1..5.
func mkMatch(id int64, radiantWin bool, bans ...int) cache.Match {
	m := cache.Match{ID: id, StartTime: 1000 + id, RadiantWin: radiantWin}
	for i, hero := range bans {
		m.PickBans = append(m.PickBans, cache.PickBan{Ord: i, HeroID: hero, IsPick: false, Team: i % 2})
	}
	pos := func(p int) *int { return &p }
	for i := 0; i < 5; i++ {
		m.Players = append(m.Players, cache.MatchPlayer{
			PlayerSlot: i, HeroID: i + 1, IsRadiant: true,
			Position: pos(i + 1), PosSource: cache.PosSourceStratz,
		})
		m.Players = append(m.Players, cache.MatchPlayer{
			PlayerSlot: 128 + i, HeroID: 11 + i, IsRadiant: false,
			Position: pos(i + 1), PosSource: cache.PosSourceStratz,
		})
	}
	return m
}

func corpus(n int) []cache.Match {
	var out []cache.Match
	for i := 0; i < n; i++ {
		out = append(out, mkMatch(int64(i+1), i%2 == 0, 90, 91))
	}
	return out
}

func TestPositionAndBanRates(t *testing.T) {
	st := Compute(corpus(40), Options{MinMatches: 5})

	if st.Drafts != 40 {
		t.Fatalf("drafts = %d, want 40", st.Drafts)
	}
	var found bool
	for _, r := range st.Positions {
		if r.HeroID == 1 && r.Position == 1 {
			found = true
			if r.Matches != 40 {
				t.Errorf("hero 1 pos 1 matches = %d, want 40", r.Matches)
			}
			if r.PickRate < 0.99 {
				t.Errorf("pick rate = %v, want ~1", r.PickRate)
			}
		}
		if r.HeroID == 90 {
			t.Error("a banned hero appeared in the per-position rows, but a ban has no position")
		}
	}
	if !found {
		t.Fatal("hero 1 has no row for position 1")
	}

	if len(st.Bans) != 2 {
		t.Fatalf("bans = %d rows, want 2", len(st.Bans))
	}
	if st.Bans[0].Bans != 40 || st.Bans[0].BanRate < 0.99 {
		t.Errorf("ban row = %+v, want 40 bans at ~100%%", st.Bans[0])
	}
}

// Lane pairings are keyed by sorted hero ids, so the same duo reported from either
// side lands in one cell rather than two half-populated ones.
func TestLanePairingsAreOrderIndependent(t *testing.T) {
	st := Compute(corpus(40), Options{MinMatches: 5})

	var safe, mid, off int
	for _, r := range st.Lanes {
		switch r.Lane {
		case LaneSafe:
			safe++
			if r.Heroes[0] > r.Heroes[1] {
				t.Errorf("safe lane pairing %v is not sorted", r.Heroes)
			}
		case LaneMid:
			mid++
			if r.Heroes[1] != 0 {
				t.Errorf("mid is solo but carries a second hero: %v", r.Heroes)
			}
		case LaneOff:
			off++
		}
	}
	if safe != 2 || off != 2 || mid != 2 {
		t.Fatalf("lane rows: safe=%d mid=%d off=%d, want 2 of each (one per side)", safe, mid, off)
	}

	lift, ok := st.LaneLift(LaneSafe, 5, 1) // reversed order
	if !ok {
		t.Fatal("LaneLift did not find the pairing when asked in the other order")
	}
	if lift > 1 || lift < -1 {
		t.Errorf("lift = %v, out of range", lift)
	}
}

// A rate computed from three games must not outrank a real one. Smoothing is what
// keeps a per-position panel from recommending noise.
func TestSmallSamplesAreSmoothedAndFloored(t *testing.T) {
	matches := corpus(30)
	// One extra hero appears twice, winning both.
	for i := 0; i < 2; i++ {
		m := mkMatch(int64(100+i), true)
		m.Players[0].HeroID = 77 // radiant position 1
		matches = append(matches, m)
	}
	st := Compute(matches, Options{MinMatches: 5})
	for _, r := range st.Positions {
		if r.HeroID == 77 {
			t.Fatalf("a hero with 2 games passed the MinMatches floor: %+v", r)
		}
	}

	st2 := Compute(matches, Options{MinMatches: 1, Prior: 2})
	for _, r := range st2.Positions {
		if r.HeroID == 77 && r.Position == 1 {
			if r.WinRate >= 1.0 {
				t.Errorf("win rate = %v for a 2-0 hero; smoothing did not apply", r.WinRate)
			}
		}
	}
}

// The result is baked into a bundle, so two runs over the same corpus must agree
// on the exact ordering — Go randomises map iteration, which is precisely the trap.
func TestOrderingIsDeterministic(t *testing.T) {
	c := corpus(40)
	for i := 0; i < 5; i++ {
		a := Compute(c, Options{MinMatches: 5})
		b := Compute(c, Options{MinMatches: 5})
		if len(a.Positions) != len(b.Positions) {
			t.Fatalf("row counts differ: %d vs %d", len(a.Positions), len(b.Positions))
		}
		for i := range a.Positions {
			if a.Positions[i] != b.Positions[i] {
				t.Fatalf("position row %d differs between runs:\n%+v\n%+v", i, a.Positions[i], b.Positions[i])
			}
		}
		for i := range a.Lanes {
			if a.Lanes[i] != b.Lanes[i] {
				t.Fatalf("lane row %d differs between runs:\n%+v\n%+v", i, a.Lanes[i], b.Lanes[i])
			}
		}
	}
}

// Matches with no participants still contribute their bans: the draft rows are
// there, only the roster is missing.
func TestMatchesWithoutPlayersStillCountBans(t *testing.T) {
	m := cache.Match{
		ID: 1, RadiantWin: true,
		PickBans: []cache.PickBan{{Ord: 0, HeroID: 42, IsPick: false}},
	}
	st := Compute([]cache.Match{m, m, m}, Options{MinMatches: 1})
	if len(st.Bans) != 1 || st.Bans[0].HeroID != 42 {
		t.Fatalf("bans = %+v, want hero 42", st.Bans)
	}
	if len(st.Positions) != 0 {
		t.Errorf("positions were invented for a match with no roster: %+v", st.Positions)
	}
}

// Per-player aggregates use a much lower floor: a scouted opponent has a handful
// of games, and the corpus-wide floor would report nothing about them at all.
func TestPlayerAggregates(t *testing.T) {
	var matches []cache.Match
	acct := int64(7)
	for i := 0; i < 4; i++ {
		m := mkMatch(int64(i+1), true)
		m.Players[0].AccountID = &acct
		matches = append(matches, m)
	}
	st := Compute(matches, Options{MinMatches: 20})

	var hero, pos bool
	for _, r := range st.PlayerHeroes {
		if r.AccountID == acct && r.HeroID == 1 {
			hero = true
			if r.Matches != 4 {
				t.Errorf("player hero matches = %d, want 4", r.Matches)
			}
		}
	}
	for _, r := range st.PlayerPositions {
		if r.AccountID == acct && r.Position == 1 {
			pos = true
			if r.Share < 0.99 {
				t.Errorf("position share = %v, want ~1", r.Share)
			}
		}
	}
	if !hero || !pos {
		t.Error("per-player aggregates were dropped by the corpus-wide floor")
	}
}
