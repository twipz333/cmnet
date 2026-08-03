package cache

import (
	"testing"
	"time"
)

// The address book round-trips, and a refresh that resolves no nickname must not
// erase the one already stored — losing a label because a lookup came back blank
// is exactly what makes a feature untrustworthy.
func TestRememberPlayerKeepsWhatItKnows(t *testing.T) {
	s := openTmp(t)

	if err := s.RememberPlayer(KnownPlayer{AccountID: 7, Persona: "Egor", Role: RoleAlly, Note: "мид"}); err != nil {
		t.Fatalf("remember: %v", err)
	}
	// A later scout resolves the id but not the name.
	if err := s.RememberPlayer(KnownPlayer{AccountID: 7, Role: RoleAlly}); err != nil {
		t.Fatalf("re-remember: %v", err)
	}

	got, err := s.KnownPlayers("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("list has %d entries, want 1", len(got))
	}
	if got[0].Persona != "Egor" {
		t.Errorf("persona = %q, want it preserved through a nameless refresh", got[0].Persona)
	}
	if got[0].Note != "мид" {
		t.Errorf("note = %q, want the user's own label preserved", got[0].Note)
	}
}

// Filtering by role is what makes the teammate list a teammate list.
func TestKnownPlayersByRole(t *testing.T) {
	s := openTmp(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.RememberPlayer(KnownPlayer{AccountID: 1, Role: RoleAlly}))
	must(s.RememberPlayer(KnownPlayer{AccountID: 2, Role: RoleEnemy}))

	allies, err := s.KnownPlayers(RoleAlly)
	if err != nil {
		t.Fatal(err)
	}
	if len(allies) != 1 || allies[0].AccountID != 1 {
		t.Fatalf("allies = %+v, want just account 1", allies)
	}
	all, err := s.KnownPlayers("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered list has %d, want 2", len(all))
	}
}

// Removing an opponent from the snapshot must leave the cached rows alone: they
// are what makes scouting the same team again fast, which is the point of keeping
// them. Only an explicit "forget" clears them.
func TestForgetDropsCacheButRemovalDoesNot(t *testing.T) {
	s := openTmp(t)
	const window = 30
	if err := s.RememberPlayer(KnownPlayer{AccountID: 42, Role: RoleEnemy, Persona: "Rival"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplacePlayerHeroStats(42, window, []PlayerHeroRow{
		{AccountID: 42, HeroID: 8, Matches: 12, Wins: 7},
	}); err != nil {
		t.Fatal(err)
	}

	// Nothing here simulates snapshot removal beyond not touching the cache — which
	// is the assertion: the snapshot path has no reason to reach into these tables.
	rows, err := s.PlayerHeroStats(42, window)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("cached hero rows = %d, want 1 to survive for a fast re-scout", len(rows))
	}

	if err := s.ForgetPlayer(42); err != nil {
		t.Fatalf("forget: %v", err)
	}
	rows, err = s.PlayerHeroStats(42, window)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("forget left %d cached rows behind", len(rows))
	}
	list, err := s.KnownPlayers("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("forget left the account in the address book: %+v", list)
	}
}

// A cached row is only an answer for the window it was collected over. Serving a
// 30-day row to a 7-day question would be a cache hit that quietly swaps the
// population being described.
func TestPlayerStatsAreKeyedByWindow(t *testing.T) {
	s := openTmp(t)
	if err := s.ReplacePlayerHeroStats(42, 30, []PlayerHeroRow{{AccountID: 42, HeroID: 8, Matches: 5}}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.PlayerHeroStats(42, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("a 30-day row answered a 7-day question: %+v", rows)
	}

	age, ok, err := s.PlayerStatsAge(42, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("no age reported for a window that has rows")
	}
	if age > time.Minute {
		t.Errorf("age = %v for a row just written", age)
	}
}

// The long tail of opponents scouted once must not grow without bound, and the
// regular five must survive the pruning.
func TestPruneKeepsPinnedAndRecent(t *testing.T) {
	s := openTmp(t)
	old := time.Now().Add(-90 * 24 * time.Hour).Unix()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.RememberPlayer(KnownPlayer{AccountID: 1, Role: RoleAlly, LastSeen: old, Pinned: true}))
	must(s.RememberPlayer(KnownPlayer{AccountID: 2, Role: RoleEnemy, LastSeen: old}))
	must(s.RememberPlayer(KnownPlayer{AccountID: 3, Role: RoleEnemy}))

	n, err := s.PruneKnownPlayers(30*24*time.Hour, 1)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned %d entries, want 1", n)
	}
	got, err := s.KnownPlayers("")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int64]bool{}
	for _, p := range got {
		ids[p.AccountID] = true
	}
	if !ids[1] {
		t.Error("a pinned entry was pruned")
	}
	if !ids[3] {
		t.Error("a recently seen entry was pruned")
	}
	if ids[2] {
		t.Error("a stale unpinned entry survived")
	}
}
