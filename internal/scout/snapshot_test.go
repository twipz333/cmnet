package scout

import (
	"math"
	"path/filepath"
	"testing"
)

// stubHM maps hero ids 1..n to dense indices 0..n-1.
type stubHM struct{ n int }

func (s stubHM) Len() int { return s.n }
func (s stubHM) Index(id int) (int, bool) {
	if id >= 1 && id <= s.n {
		return id - 1, true
	}
	return 0, false
}

func sampleSnapshot() *OpponentSnapshot {
	return &OpponentSnapshot{
		FrozenAt:         "2026-06-02T00:00:00Z",
		BundleVersionRef: "abc",
		SourceTag:        "opendota",
		Layer2: &Layer2{
			WindowDays: 30,
			Players: []PlayerAffinity{
				{AccountID: 1, Nickname: "p1", Resolved: true, Heroes: []HeroAffinity{{HeroID: 1, Games: 10, WinRate: 0.6, Aff: 0.4}}},
				{AccountID: 2, Nickname: "p2", Resolved: true, Heroes: []HeroAffinity{{HeroID: 1, Games: 5, WinRate: 0.5, Aff: 0.2}, {HeroID: 3, Games: 8, WinRate: 0.7, Aff: 0.5}}},
			},
		},
		Layer3: &Layer3{
			EventWindow:    "evt",
			DraftsObserved: 2,
			EntryAccountID: 1,
			TeamDrafts: []TeamDraft{
				{MatchID: 100, Radiant: true, Picks: []int{1, 2}, Bans: []int{4}},
				{MatchID: 101, Radiant: false, Picks: []int{1}, Bans: []int{3}},
			},
		},
	}
}

func TestAff2Vector(t *testing.T) {
	s := sampleSnapshot()
	vec := s.Aff2Vector(stubHM{n: 4})
	if vec == nil {
		t.Fatal("nil aff2 vector")
	}
	// Hero 1 (idx0): 0.4 + 0.2 = 0.6; hero 3 (idx2): 0.5.
	if math.Abs(vec[0]-0.6) > 1e-9 {
		t.Errorf("aff2[0] = %v, want 0.6", vec[0])
	}
	if math.Abs(vec[2]-0.5) > 1e-9 {
		t.Errorf("aff2[2] = %v, want 0.5", vec[2])
	}
}

func TestAff3Vector(t *testing.T) {
	s := sampleSnapshot()
	vec, n := s.Aff3Vector(stubHM{n: 4})
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}
	// Hero 1 picked in both drafts -> count 2; hero 2 once.
	if vec[0] != 2 {
		t.Errorf("aff3[0] = %v, want 2", vec[0])
	}
	if vec[1] != 1 {
		t.Errorf("aff3[1] = %v, want 1", vec[1])
	}
}

func TestAffVectorsNilWhenAbsent(t *testing.T) {
	empty := &OpponentSnapshot{}
	if empty.Aff2Vector(stubHM{n: 3}) != nil {
		t.Error("aff2 should be nil with no layer2")
	}
	if v, n := empty.Aff3Vector(stubHM{n: 3}); v != nil || n != 0 {
		t.Error("aff3 should be nil/0 with no layer3")
	}
}

func TestSnapshotSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.json")
	s := sampleSnapshot()
	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if s.SnapshotVersion == "" {
		t.Fatal("version not set")
	}
	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.SnapshotVersion != s.SnapshotVersion {
		t.Errorf("version mismatch: %q vs %q", loaded.SnapshotVersion, s.SnapshotVersion)
	}
	if loaded.Layer3.DraftsObserved != 2 {
		t.Errorf("layer3 not preserved: %+v", loaded.Layer3)
	}
}
