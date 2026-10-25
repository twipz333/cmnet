package cache

import (
	"path/filepath"
	"testing"
)

// roster builds a valid 10-player participant list: heroes 1..10, Radiant on
// slots 0-4 with positions 1-5, Dire on slots 128-132 likewise.
func roster() []MatchPlayer {
	out := make([]MatchPlayer, 0, 10)
	for i := 0; i < 10; i++ {
		slot := i
		radiant := true
		if i >= 5 {
			slot = 128 + (i - 5)
			radiant = false
		}
		pos := i%5 + 1
		acc := int64(1000 + i)
		out = append(out, MatchPlayer{
			PlayerSlot: slot,
			AccountID:  &acc,
			HeroID:     i + 1,
			IsRadiant:  radiant,
			Position:   &pos,
			PosSource:  PosSourceStratz,
		})
	}
	return out
}

func matchWithRoster(ps []MatchPlayer) Match {
	return Match{
		ID: 42, StartTime: 1000, RadiantWin: true, GameMode: 2, Source: SourcePro,
		PickBans: []PickBan{{Ord: 0, HeroID: 1, IsPick: true, Team: 0}},
		Players:  ps,
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertMatchStoresParticipants(t *testing.T) {
	s := openTestStore(t)
	if err := s.UpsertMatch(matchWithRoster(roster())); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := s.Matches(0)
	if err != nil {
		t.Fatalf("matches: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("matches = %d, want 1", len(got))
	}
	ps := got[0].Players
	if len(ps) != 10 {
		t.Fatalf("players = %d, want 10", len(ps))
	}
	// Ordered by player_slot, so Radiant (0-4) comes before Dire (128-132).
	if ps[0].PlayerSlot != 0 || !ps[0].IsRadiant {
		t.Errorf("first row = %+v, want slot 0 Radiant", ps[0])
	}
	if ps[9].PlayerSlot != 132 || ps[9].IsRadiant {
		t.Errorf("last row = %+v, want slot 132 Dire", ps[9])
	}
	if ps[0].Position == nil || *ps[0].Position != 1 {
		t.Errorf("position = %v, want 1", ps[0].Position)
	}
	if ps[0].PosSource != PosSourceStratz {
		t.Errorf("pos_source = %q, want %q", ps[0].PosSource, PosSourceStratz)
	}
}

// An anonymous account must round-trip as NULL, not as a zero that later JOINs
// would treat as a real player.
func TestAnonymousAccountStaysNull(t *testing.T) {
	s := openTestStore(t)
	ps := roster()
	ps[3].AccountID = nil
	if err := s.UpsertMatch(matchWithRoster(ps)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _ := s.Matches(0)
	for _, p := range got[0].Players {
		if p.PlayerSlot == 3 && p.AccountID != nil {
			t.Errorf("anonymous account came back as %v, want nil", *p.AccountID)
		}
	}
}

// A truncated or malformed roster must leave the match stored WITHOUT
// participants rather than with a biased partial set.
func TestInvalidRosterIsSkippedNotStored(t *testing.T) {
	cases := map[string][]MatchPlayer{
		"nine players":   roster()[:9],
		"duplicate hero": func() []MatchPlayer { r := roster(); r[1].HeroID = r[0].HeroID; return r }(),
		"duplicate slot": func() []MatchPlayer { r := roster(); r[1].PlayerSlot = r[0].PlayerSlot; return r }(),
		"lopsided sides": func() []MatchPlayer { r := roster(); r[9].IsRadiant = true; return r }(),
		"bad position":   func() []MatchPlayer { r := roster(); bad := 7; r[2].Position = &bad; return r }(),
		"slot out of range": func() []MatchPlayer { r := roster(); r[0].PlayerSlot = 42; return r }(),
		"side contradicts slot": func() []MatchPlayer {
			r := roster()
			r[0].IsRadiant = false // slot 0 is a Radiant slot
			return r
		}(),
	}
	for name, ps := range cases {
		t.Run(name, func(t *testing.T) {
			s := openTestStore(t)
			if err := s.UpsertMatch(matchWithRoster(ps)); err != nil {
				t.Fatalf("upsert: %v", err)
			}
			got, err := s.Matches(0)
			if err != nil {
				t.Fatalf("matches: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("match itself must still be stored, got %d", len(got))
			}
			if len(got[0].Players) != 0 {
				t.Errorf("participants = %d, want 0 for an invalid roster", len(got[0].Players))
			}
		})
	}
}

// Re-caching a match from a source that reports no participants must not delete
// the rows a richer source already wrote.
func TestReCacheWithoutPlayersKeepsExistingRows(t *testing.T) {
	s := openTestStore(t)
	if err := s.UpsertMatch(matchWithRoster(roster())); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	bare := matchWithRoster(nil)
	if err := s.UpsertMatch(bare); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, _ := s.Matches(0)
	if len(got[0].Players) != 10 {
		t.Errorf("players after bare re-cache = %d, want 10 kept", len(got[0].Players))
	}
}

// A match cached before participants were recorded must NOT count as complete,
// otherwise sync skips it forever and its roles never arrive.
func TestMatchIsCompleteRequiresParticipants(t *testing.T) {
	s := openTestStore(t)
	m := matchWithRoster(nil)
	m.PickBans = append(m.PickBans, PickBan{Ord: 1, HeroID: 2, IsPick: false, Team: 1}) // a ban
	if err := s.UpsertMatch(m); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	complete, err := s.MatchIsComplete(m.ID, true)
	if err != nil {
		t.Fatalf("MatchIsComplete: %v", err)
	}
	if complete {
		t.Error("match with bans but no participants reported complete, want incomplete")
	}

	m.Players = roster()
	if err := s.UpsertMatch(m); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	complete, err = s.MatchIsComplete(m.ID, true)
	if err != nil {
		t.Fatalf("MatchIsComplete: %v", err)
	}
	if !complete {
		t.Error("match with bans and participants reported incomplete")
	}
}

// Without a STRATZ client there is no source of participants, so a run must not
// demand them — otherwise every cached draft looks incomplete and the run spends
// itself re-downloading what it already has.
func TestMatchIsCompleteWithoutRequiringPlayers(t *testing.T) {
	s := openTestStore(t)
	m := matchWithRoster(nil)
	m.PickBans = append(m.PickBans, PickBan{Ord: 1, HeroID: 2, IsPick: false, Team: 1})
	if err := s.UpsertMatch(m); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	complete, err := s.MatchIsComplete(m.ID, false)
	if err != nil {
		t.Fatalf("MatchIsComplete: %v", err)
	}
	if !complete {
		t.Error("bans-only match reported incomplete when players are not required")
	}
}

// A partial participant list must not read as a complete roster.
func TestMatchIsCompleteRejectsPartialRoster(t *testing.T) {
	s := openTestStore(t)
	m := matchWithRoster(roster())
	m.PickBans = append(m.PickBans, PickBan{Ord: 1, HeroID: 99, IsPick: false, Team: 1})
	if err := s.UpsertMatch(m); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Simulate a half-written roster reaching the table by any route.
	if _, err := s.db.Exec("DELETE FROM match_players WHERE match_id = ? AND player_slot > 2", m.ID); err != nil {
		t.Fatalf("trim roster: %v", err)
	}
	complete, err := s.MatchIsComplete(m.ID, true)
	if err != nil {
		t.Fatalf("MatchIsComplete: %v", err)
	}
	if complete {
		t.Error("partial roster reported complete")
	}
}

// A picks-only bulk match (no bans) is never complete, participants or not.
func TestMatchIsCompleteRequiresBans(t *testing.T) {
	s := openTestStore(t)
	m := matchWithRoster(roster()) // PickBans holds a single pick, no bans
	if err := s.UpsertMatch(m); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	complete, err := s.MatchIsComplete(m.ID, true)
	if err != nil {
		t.Fatalf("MatchIsComplete: %v", err)
	}
	if complete {
		t.Error("picks-only match reported complete")
	}
}

// Matches with no participant rows must simply come back empty — the whole
// pipeline has to keep working on a cache filled before this table existed.
func TestMatchesWithoutParticipants(t *testing.T) {
	s := openTestStore(t)
	if err := s.UpsertMatch(matchWithRoster(nil)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := s.Matches(0)
	if err != nil {
		t.Fatalf("matches: %v", err)
	}
	if len(got[0].Players) != 0 {
		t.Errorf("players = %d, want 0", len(got[0].Players))
	}
}
