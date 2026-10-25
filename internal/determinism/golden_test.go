// Package determinism holds the cross-platform golden test required by §6/§12:
// one bundle + one input must produce a byte-identical ranking on every OS/arch.
//
// The test trains a bundle from a fixed synthetic dataset, runs a fixed draft
// input through the recommender, and compares the normalized JSON output against a
// committed golden file. Because the CI matrix runs this on both Windows and Linux
// against the same golden, any platform-dependent float divergence fails the test.
//
// Regenerate the golden with: UPDATE_GOLDEN=1 go test ./internal/determinism/
package determinism

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cmnet/internal/bundle"
	"cmnet/internal/cache"
	"cmnet/internal/draft"
	"cmnet/internal/model"
	"cmnet/internal/recommender"
	"cmnet/internal/train"
)

// fixedMatches is a small deterministic synthetic dataset: hero ids 1..6.
func fixedMatches() []cache.Match {
	pb := func(ord, hero, team int, pick bool) cache.PickBan {
		return cache.PickBan{Ord: ord, HeroID: hero, Team: team, IsPick: pick}
	}
	return []cache.Match{
		{
			ID: 1, StartTime: 100, RadiantWin: true,
			PickBans: []cache.PickBan{
				pb(0, 1, 0, false), pb(1, 2, 1, false),
				pb(2, 3, 0, true), pb(3, 4, 1, true),
				pb(4, 5, 0, true), pb(5, 6, 1, true),
			},
		},
		{
			ID: 2, StartTime: 200, RadiantWin: false,
			PickBans: []cache.PickBan{
				pb(0, 3, 0, false), pb(1, 4, 1, false),
				pb(2, 1, 0, true), pb(3, 6, 1, true),
				pb(4, 2, 0, true), pb(5, 5, 1, true),
			},
		},
		{
			ID: 3, StartTime: 300, RadiantWin: true,
			PickBans: []cache.PickBan{
				pb(0, 2, 0, false), pb(1, 5, 1, false),
				pb(2, 4, 0, true), pb(3, 1, 1, true),
				pb(4, 6, 0, true), pb(5, 3, 1, true),
			},
		},
	}
}

type snapshot struct {
	BundleVersion string                       `json:"bundle_version"`
	OurTurn       bool                         `json:"our_turn"`
	IsPick        bool                         `json:"is_pick"`
	Recs          []recommender.Recommendation `json:"recs"`
	EnemyPred     []recommender.EnemyPick      `json:"enemy_pred"`
	// Every user-visible ranked list belongs in the hash, not just the two oldest
	// ones: a panel outside the golden is a panel that can silently diverge between
	// two machines, which is the exact failure §6 exists to prevent.
	SafeToLet []recommender.EnemyPick `json:"safe_to_let"`
	PoolAlert []recommender.EnemyPick `json:"pool_alert"`
}

func buildSnapshot(t *testing.T) []byte {
	t.Helper()
	heroIDs := []int{1, 2, 3, 4, 5, 6}
	b := train.Bundle(fixedMatches(), heroIDs, train.Options{
		WindowDays: 31,
		PatchTag:   "test",
		MMRTier:    "test",
		CreatedAt:  "2026-01-01T00:00:00Z", // fixed so the version hash is stable
	})

	// Save + reload to exercise the version verification path too.
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := b.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := bundle.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	hm := loaded.HeroMap()
	rec := recommender.New(
		&model.WinProb{Model: loaded.WinProb, HeroMap: hm},
		&model.Predictor{Pick: loaded.PickPredictor, Ban: loaded.BanPredictor, HeroMap: hm},
	)

	// Fixed input: Radiant first pick, four opening bans applied (under the 7.34+
	// schedule the next step is Radiant's third ban).
	d := draft.New(draft.Radiant)
	for _, hero := range []int{1, 2, 3, 4} {
		if err := d.Apply(hero); err != nil {
			t.Fatalf("apply %d: %v", hero, err)
		}
	}
	res := rec.Recommend(d, draft.Radiant, recommender.DefaultTopN)

	snap := snapshot{
		BundleVersion: loaded.BundleVersion,
		OurTurn:       res.OurTurn,
		IsPick:        res.IsPick,
		Recs:          res.Recs,
		EnemyPred:     res.EnemyPred,
		SafeToLet:     res.SafeToLet,
		PoolAlert:     res.PoolAlert,
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestGoldenRankingByteIdentical(t *testing.T) {
	got := buildSnapshot(t)
	goldenPath := filepath.Join("testdata", "mvp_ranking.golden.json")

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden updated: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1 to create): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("ranking diverged from golden (platform float drift?).\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// Repeated runs in the same process must also be identical.
func TestRecommendIdempotent(t *testing.T) {
	first := string(buildSnapshot(t))
	second := string(buildSnapshot(t))
	if first != second {
		t.Error("two runs produced different output")
	}
}
