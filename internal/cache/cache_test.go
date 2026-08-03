package cache

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMetaRoundTrip(t *testing.T) {
	s := openTemp(t)
	if _, ok, _ := s.GetMeta("missing"); ok {
		t.Error("missing key reported present")
	}
	if err := s.SetMeta("k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("k", "v2"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.GetMeta("k")
	if err != nil || !ok || v != "v2" {
		t.Fatalf("got (%q,%v,%v), want (v2,true,nil)", v, ok, err)
	}
}

func TestUpsertAndReadMatch(t *testing.T) {
	s := openTemp(t)
	mmr := 6000
	m := Match{
		ID: 123, StartTime: 1000, RadiantWin: true, AvgMMR: &mmr, Patch: "7.36",
		PickBans: []PickBan{
			{Ord: 0, HeroID: 5, IsPick: false, Team: 0},
			{Ord: 1, HeroID: 8, IsPick: true, Team: 1},
		},
	}
	if err := s.UpsertMatch(m); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	has, err := s.HasMatch(123)
	if err != nil || !has {
		t.Fatalf("HasMatch=%v err=%v", has, err)
	}

	got, err := s.Matches(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 123 || len(got[0].PickBans) != 2 {
		t.Fatalf("unexpected matches: %+v", got)
	}
	if got[0].PickBans[1].HeroID != 8 || !got[0].PickBans[1].IsPick {
		t.Errorf("pick_ban order/content wrong: %+v", got[0].PickBans)
	}
}

func TestUpsertReplacesPickBans(t *testing.T) {
	s := openTemp(t)
	_ = s.UpsertMatch(Match{ID: 1, StartTime: 10, PickBans: []PickBan{{Ord: 0, HeroID: 1, Team: 0}}})
	_ = s.UpsertMatch(Match{ID: 1, StartTime: 10, PickBans: []PickBan{{Ord: 0, HeroID: 2, Team: 0}, {Ord: 1, HeroID: 3, Team: 1}}})
	got, _ := s.Matches(0)
	if len(got) != 1 || len(got[0].PickBans) != 2 || got[0].PickBans[0].HeroID != 2 {
		t.Fatalf("replace failed: %+v", got)
	}
}

func TestMaxStartTimeWatermark(t *testing.T) {
	s := openTemp(t)
	if wm, _ := s.MaxStartTime(); wm != 0 {
		t.Errorf("empty watermark = %d, want 0", wm)
	}
	_ = s.UpsertMatch(Match{ID: 1, StartTime: 100})
	_ = s.UpsertMatch(Match{ID: 2, StartTime: 250})
	if wm, _ := s.MaxStartTime(); wm != 250 {
		t.Errorf("watermark = %d, want 250", wm)
	}
}

func TestHeroesOrdered(t *testing.T) {
	s := openTemp(t)
	_ = s.UpsertHeroes([]Hero{{ID: 8, Name: "b"}, {ID: 1, Name: "a"}})
	got, _ := s.Heroes()
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 8 {
		t.Fatalf("heroes not ordered by id: %+v", got)
	}
}
