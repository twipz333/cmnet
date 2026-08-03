package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cmnet/internal/scout"
)

// writeSnapshot saves a snapshot with one enemy and one hero pool entry, so a
// later overwrite is obvious.
func writeSnapshot(t *testing.T, path string) {
	t.Helper()
	snap := &scout.OpponentSnapshot{
		SourceTag: "opendota",
		Layer2: &scout.Layer2{
			WindowDays: 30,
			Players: []scout.PlayerAffinity{
				{AccountID: 42, Nickname: "someone", Resolved: true},
			},
		},
		HeroPool: []int{1, 2, 3},
	}
	if err := snap.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
}

// A snapshot that fails its hash check must not be silently replaced by an empty
// one: every caller merges into what it gets back and saves, so answering "empty"
// throws away the user's scouted opponents, hero pool and advice rules.
func TestEditSnapshotRefusesUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	writeSnapshot(t, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Replace(string(data), `"source_tag": "opendota"`, `"source_tag": "tampered"`, 1)
	if corrupt == string(data) {
		t.Fatal("test setup: nothing was corrupted")
	}
	if err := os.WriteFile(path, []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}

	err = editSnapshot(path, func(s *scout.OpponentSnapshot) error {
		s.HeroPool = []int{99}
		return nil
	})
	if err == nil {
		t.Fatal("editSnapshot accepted a snapshot that fails hash verification")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("error should explain the refusal, got: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != corrupt {
		t.Error("the unreadable snapshot was modified; it must be left untouched")
	}
}

// Every save must leave the version it replaced next to it, so a bad overwrite is
// recoverable without re-scouting the enemy team minutes before a match.
func TestSaveKeepsPreviousVersionAsBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	writeSnapshot(t, path)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := editSnapshot(path, func(s *scout.OpponentSnapshot) error {
		s.Layer2 = nil // the destructive change we want to be able to undo
		return nil
	}); err != nil {
		t.Fatalf("editSnapshot: %v", err)
	}

	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("no backup written: %v", err)
	}
	if string(backup) != string(before) {
		t.Error("backup does not match the version that was replaced")
	}
	// And it must still be a loadable snapshot, not just bytes.
	restored, err := scout.LoadSnapshot(path + ".bak")
	if err != nil {
		t.Fatalf("backup does not load: %v", err)
	}
	if restored.Layer2 == nil || len(restored.Layer2.Players) != 1 {
		t.Error("backup lost the data it was supposed to preserve")
	}
}

// A missing file is the one case where starting from empty is correct.
func TestEditSnapshotCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := editSnapshot(path, func(s *scout.OpponentSnapshot) error {
		s.HeroPool = []int{7}
		return nil
	}); err != nil {
		t.Fatalf("editSnapshot on a missing file: %v", err)
	}
	got, err := scout.LoadSnapshot(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.HeroPool) != 1 || got.HeroPool[0] != 7 {
		t.Errorf("hero pool = %v, want [7]", got.HeroPool)
	}
}

// Concurrent edits must not lose each other's writes. Without serialization each
// goroutine loads the same copy, and the last save wins with a valid hash — a
// silent loss of everything the others did.
func TestEditSnapshotConcurrentWritesDoNotLoseData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	writeSnapshot(t, path)

	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each writer appends one distinct ally, mirroring how scout merges.
			err := editSnapshot(path, func(s *scout.OpponentSnapshot) error {
				if s.Allies == nil {
					s.Allies = &scout.Layer2{WindowDays: 30}
				}
				s.Allies.Players = append(s.Allies.Players, scout.PlayerAffinity{
					AccountID: int64(1000 + i), Resolved: true,
				})
				return nil
			})
			if err != nil {
				t.Errorf("editSnapshot: %v", err)
			}
		}(i)
	}
	wg.Wait()

	got, err := scout.LoadSnapshot(path)
	if err != nil {
		t.Fatalf("load after concurrent writes: %v", err)
	}
	if got.Allies == nil || len(got.Allies.Players) != n {
		count := 0
		if got.Allies != nil {
			count = len(got.Allies.Players)
		}
		t.Errorf("allies = %d, want %d (writes were lost)", count, n)
	}
	// The pre-existing data must survive every one of those writes.
	if got.Layer2 == nil || len(got.Layer2.Players) != 1 {
		t.Error("the original enemy layer was lost")
	}
	if len(got.HeroPool) != 3 {
		t.Errorf("hero pool = %v, want the original 3 entries", got.HeroPool)
	}
}
