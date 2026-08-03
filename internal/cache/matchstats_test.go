package cache

import (
	"path/filepath"
	"testing"
)

func openTmp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "cmnet.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func ptrInt(v int) *int    { return &v }
func ptrBool(v bool) *bool { return &v }

// roster builds a valid 10-player roster on Valve's slot numbering, optionally
// carrying performance.
func statsRoster(withStats bool) []MatchPlayer {
	out := make([]MatchPlayer, 0, 10)
	for i := 0; i < 10; i++ {
		slot := i
		if i >= 5 {
			slot = 128 + (i - 5)
		}
		p := MatchPlayer{PlayerSlot: slot, HeroID: i + 1, IsRadiant: i < 5}
		if withStats {
			p.Lane = "SAFE_LANE"
			p.IsVictory = ptrBool(i < 5)
			p.Kills = ptrInt(i)
			p.GPM = ptrInt(400 + i)
			p.TowerDamage = ptrInt(0) // a real zero, not a missing field
			p.StatsSource = StatsSourceStratz
		}
		out = append(out, p)
	}
	return out
}

func mkMatchWith(id int64, players []MatchPlayer) Match {
	return Match{
		ID: id, StartTime: 1000 + id, RadiantWin: true,
		PickBans: []PickBan{{Ord: 0, HeroID: 1, IsPick: false, Team: 0}},
		Players:  players,
	}
}

// Performance round-trips, and a genuine zero survives as a zero rather than
// becoming "not collected".
func TestMatchStatsRoundTrip(t *testing.T) {
	s := openTmp(t)
	if err := s.UpsertMatch(mkMatchWith(1, statsRoster(true))); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := s.Matches(0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || len(got[0].Players) != 10 {
		t.Fatalf("got %d matches / %d players", len(got), len(got[0].Players))
	}
	p := got[0].Players[0]
	if !p.HasStats() {
		t.Fatal("stats_source did not survive the round trip")
	}
	if p.TowerDamage == nil || *p.TowerDamage != 0 {
		t.Errorf("tower damage = %v, want a stored zero: a support really can do none, "+
			"and folding that into NULL poisons every average over the column", p.TowerDamage)
	}
	if p.IsVictory == nil || !*p.IsVictory {
		t.Errorf("is_victory = %v, want true", p.IsVictory)
	}
	if p.GPM == nil {
		t.Error("gpm was lost")
	}
}

// Re-caching a match through a source that reports no performance must not erase
// performance a richer source already stored.
func TestPoorerSourceDoesNotEraseStats(t *testing.T) {
	s := openTmp(t)
	if err := s.UpsertMatch(mkMatchWith(1, statsRoster(true))); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := s.UpsertMatch(mkMatchWith(1, statsRoster(false))); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, err := s.Matches(0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, p := range got[0].Players {
		if !p.HasStats() {
			t.Fatalf("hero %d lost its stats to a picks-only re-cache", p.HeroID)
		}
	}
}

// A match cached before performance existed must be findable, so the backfill has
// a backlog to work through — every other completeness check calls it done.
func TestMatchesMissingStats(t *testing.T) {
	s := openTmp(t)
	if err := s.UpsertMatch(mkMatchWith(1, statsRoster(false))); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMatch(mkMatchWith(2, statsRoster(true))); err != nil {
		t.Fatal(err)
	}
	ids, err := s.MatchesMissingStats(0, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("missing-stats ids = %v, want [1]", ids)
	}

	withStats, withDraft, err := s.StatsCoverage(0)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if withStats != 1 || withDraft != 2 {
		t.Errorf("coverage = %d/%d, want 1/2", withStats, withDraft)
	}
}

// Match-level fields the richer fetch adds must not be blanked by a later cheaper
// write of the same match.
func TestMatchDurationSurvivesRecache(t *testing.T) {
	s := openTmp(t)
	m := mkMatchWith(1, statsRoster(true))
	m.Duration = ptrInt(2400)
	m.LobbyType = "RANKED"
	m.Patch = "7.39"
	if err := s.UpsertMatch(m); err != nil {
		t.Fatal(err)
	}

	poor := mkMatchWith(1, statsRoster(false)) // no duration, no lobby, no patch
	if err := s.UpsertMatch(poor); err != nil {
		t.Fatal(err)
	}
	got, err := s.Matches(0)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Duration == nil || *got[0].Duration != 2400 {
		t.Errorf("duration = %v, want it preserved", got[0].Duration)
	}
	if got[0].LobbyType != "RANKED" {
		t.Errorf("lobby_type = %q, want it preserved", got[0].LobbyType)
	}
	if got[0].Patch != "7.39" {
		t.Errorf("patch = %q, want it preserved", got[0].Patch)
	}
}
