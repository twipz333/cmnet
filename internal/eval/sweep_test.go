package eval

import (
	"testing"

	"cmnet/internal/cache"
)

// sweepMatches builds n CM-shaped drafts, one per hour, so a time split has
// something to cut. The drafts only need enough structure for training to run —
// the test asserts which SPLIT each sweep ran on, never the metric values.
func sweepMatches(n int) []cache.Match {
	heroes := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	out := make([]cache.Match, 0, n)
	for i := range n {
		var pbs []cache.PickBan
		for j := range 4 { // bans first, so hasBan() sees a real CM draft
			pbs = append(pbs, cache.PickBan{
				Ord: j, HeroID: heroes[(i+j)%len(heroes)], IsPick: false, Team: j % 2,
			})
		}
		for j := range 10 {
			pbs = append(pbs, cache.PickBan{
				Ord: 4 + j, HeroID: heroes[(i+j)%len(heroes)], IsPick: true, Team: j % 2,
			})
		}
		out = append(out, cache.Match{
			ID:         int64(i + 1),
			StartTime:  int64(1_700_000_000 + i*3600),
			RadiantWin: i%2 == 0,
			Source:     cache.SourcePublicCM,
			PickBans:   pbs,
		})
	}
	return out
}

// The half-life sweep must be computed per fold, on that fold's own training
// split. It used to run once after the fold loop, on the last fold's split, while
// being printed as if it described the whole run — which quietly turns a
// three-window check back into a single-window one.
func TestSweepIsPerFoldNotLastFoldOnly(t *testing.T) {
	heroIDs := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	rep, err := Run(sweepMatches(300), heroIDs, Options{
		TestFrac:      0.2,
		Folds:         3,
		Bootstrap:     0,
		Epochs:        1, // the split is what matters here, not convergence
		SweepHalfLife: []float64{7, 30},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rep.Folds) != 3 {
		t.Fatalf("folds = %d, want 3", len(rep.Folds))
	}

	var prev int
	for _, f := range rep.Folds {
		if len(f.HalfLife) != 2 {
			t.Fatalf("fold %d: sweep rows = %d, want 2", f.Fold, len(f.HalfLife))
		}
		// Rolling splits train on everything before their own test block, so each
		// fold's sweep must see strictly more rows than the previous fold's. Equal
		// counts across folds are the signature of the old bug.
		n := f.HalfLife[0].NominalN
		if n == 0 {
			t.Fatalf("fold %d: sweep NominalN = 0", f.Fold)
		}
		if f.Fold > 1 && n <= prev {
			t.Errorf("fold %d sweep trained on %d rows, fold %d on %d: each fold must use its own split",
				f.Fold, n, f.Fold-1, prev)
		}
		prev = n
	}
}
