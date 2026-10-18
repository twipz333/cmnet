package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cmnet/internal/cache"
)

func cmMatch(id int64, t int64) cache.Match {
	return cache.Match{
		ID:        id,
		StartTime: t,
		PickBans:  []cache.PickBan{{Ord: 0, HeroID: 1, IsPick: false, Team: 0}, {Ord: 1, HeroID: 2, IsPick: true, Team: 0}},
	}
}

func apMatch(id int64, t int64) cache.Match {
	return cache.Match{
		ID:        id,
		StartTime: t,
		PickBans:  []cache.PickBan{{Ord: 0, HeroID: 1, IsPick: true, Team: 0}},
	}
}

func TestSealHoldoutNewestCM(t *testing.T) {
	var matches []cache.Match
	for i := 0; i < 20; i++ {
		matches = append(matches, cmMatch(int64(100+i), int64(1000+i)))
		matches = append(matches, apMatch(int64(200+i), int64(1000+i)))
	}
	h, err := SealHoldout(matches, 0.1, "cm")
	if err != nil {
		t.Fatal(err)
	}
	if h.N != 2 { // 10% of 20 CM
		t.Fatalf("n=%d want 2", h.N)
	}
	want := map[int64]bool{118: true, 119: true}
	for _, id := range h.MatchIDs {
		if !want[id] {
			t.Fatalf("unexpected id %d in holdout %v", id, h.MatchIDs)
		}
	}
	if err := h.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "holdout.json")
	matches := make([]cache.Match, 0, 30)
	for i := 0; i < 30; i++ {
		matches = append(matches, cmMatch(int64(i+1), int64(i+1)))
	}
	h, err := SealHoldout(matches, 0.1, "cm")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveHoldout(path, h, false); err != nil {
		t.Fatal(err)
	}
	if err := SaveHoldout(path, h, false); err == nil {
		t.Fatal("expected refuse overwrite without --force")
	}
	got, err := LoadHoldout(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.N != h.N || got.ContentSHA256 != h.ContentSHA256 {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", got, h)
	}
	bad := *h
	bad.ContentSHA256 = "deadbeef"
	raw, err := json.MarshalIndent(&bad, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHoldout(path); err == nil {
		t.Fatal("expected hash mismatch")
	}
}

func TestExcludeAndFinalSplit(t *testing.T) {
	var matches []cache.Match
	for i := 0; i < 20; i++ {
		matches = append(matches, cmMatch(int64(i+1), int64(i+1)))
	}
	h, err := SealHoldout(matches, 0.1, "cm")
	if err != nil {
		t.Fatal(err)
	}
	rest := Exclude(matches, h)
	if len(rest) != 18 {
		t.Fatalf("exclude len=%d want 18", len(rest))
	}
	set := h.IDSet()
	for _, m := range rest {
		if set[m.ID] {
			t.Fatalf("excluded id %d still present", m.ID)
		}
	}
	train, test, err := FinalSplit(matches, h)
	if err != nil {
		t.Fatal(err)
	}
	if len(test) != h.N || len(train) != len(matches)-h.N {
		t.Fatalf("final split train=%d test=%d", len(train), len(test))
	}
	for _, m := range test {
		if !set[m.ID] {
			t.Fatalf("test id %d not in holdout", m.ID)
		}
	}
}

func TestSealFracBounds(t *testing.T) {
	matches := []cache.Match{cmMatch(1, 1), cmMatch(2, 2)}
	if _, err := SealHoldout(matches, 0, "cm"); err == nil {
		t.Fatal("want error for frac=0")
	}
	if _, err := SealHoldout(matches, 0.5, "cm"); err == nil {
		t.Fatal("want error for frac=0.5")
	}
}
