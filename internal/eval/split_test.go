package eval

import (
	"testing"

	"cmnet/internal/cache"
)

// corpus builds a time-sorted mix: every `every`-th match is a CM draft (carries a
// ban), the rest are picks-only bulk. ids ascend with time.
func corpus(n, every int) []cache.Match {
	out := make([]cache.Match, 0, n)
	for i := 0; i < n; i++ {
		m := cache.Match{
			ID:        int64(i + 1),
			StartTime: int64(1_700_000_000 + i*600),
			PickBans:  []cache.PickBan{{HeroID: 1, IsPick: true, Team: 0}},
		}
		if every > 0 && i%every == 0 {
			m.Source = cache.SourcePublicCM
			m.PickBans = append(m.PickBans, cache.PickBan{HeroID: 2, IsPick: false, Team: 1})
		}
		out = append(out, m)
	}
	return out
}

func countTargets(ms []cache.Match, domain string) int {
	n := 0
	for _, m := range ms {
		if isTargetDomain(m, domain) {
			n++
		}
	}
	return n
}

// withFreshBulk appends picks-only matches newer than everything in base — the
// real collection pattern, where the bulk pool arrives several times faster than
// public Captains Mode.
func withFreshBulk(base []cache.Match, n int) []cache.Match {
	out := append([]cache.Match{}, base...)
	last := base[len(base)-1]
	for i := 0; i < n; i++ {
		out = append(out, cache.Match{
			ID:        last.ID + int64(i) + 1,
			StartTime: last.StartTime + int64(i+1)*600,
			PickBans:  []cache.PickBan{{HeroID: 1, IsPick: true, Team: 0}},
		})
	}
	return out
}

// The defect this exists for (§7.2.1 of the audit): between two real runs the
// corpus grew by 5 816 matches and the CM test set *fell* from 723 to 478, because
// a tail of "the newest 20% of everything" fills up with whatever arrives fastest.
// Under stratification the target count follows how much target data exists.
func TestStratifiedSplitSurvivesFreshBulk(t *testing.T) {
	opts := Options{TestFrac: 0.2, Folds: 1}.withDefaults()

	before := corpus(500, 5) // 100 CM drafts among 500 matches
	after := withFreshBulk(before, 500)

	_, testBefore, modeBefore, err := foldSplit(before, opts, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, testAfter, modeAfter, err := foldSplit(after, opts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if modeBefore != "stratified:cm" || modeAfter != "stratified:cm" {
		t.Fatalf("split modes = %q / %q, want stratified", modeBefore, modeAfter)
	}

	gotBefore, gotAfter := countTargets(testBefore, "cm"), countTargets(testAfter, "cm")
	if gotBefore != gotAfter {
		t.Errorf("CM matches in test: %d before the bulk arrived, %d after — collecting more "+
			"bulk must not change the size of the target test set", gotBefore, gotAfter)
	}
	if want := 20; gotBefore != want {
		t.Errorf("CM matches in test = %d, want %d (20%% of 100 CM drafts)", gotBefore, want)
	}
}

// The same corpora without stratification: the tail fills with fresh bulk and the
// target test set collapses. Asserted so the fix cannot quietly become a no-op.
func TestUnstratifiedSplitLosesTargetsToFreshBulk(t *testing.T) {
	opts := Options{TestFrac: 0.2, Folds: 1, StratifySplit: "none"}.withDefaults()

	before := corpus(500, 5)
	after := withFreshBulk(before, 500)

	_, testBefore, mode, err := foldSplit(before, opts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "corpus-tail" {
		t.Fatalf("split mode = %q, want corpus-tail", mode)
	}
	_, testAfter, _, err := foldSplit(after, opts, 0)
	if err != nil {
		t.Fatal(err)
	}
	gotBefore, gotAfter := countTargets(testBefore, "cm"), countTargets(testAfter, "cm")
	if gotAfter >= gotBefore {
		t.Errorf("unstratified: CM in test went %d → %d; this test documents the defect and "+
			"should shrink", gotBefore, gotAfter)
	}
}

// Folds must stay disjoint in time and ordered oldest-first, and no fold may train
// on data from its own test window.
func TestStratifiedFoldsAreDisjointAndOrdered(t *testing.T) {
	opts := Options{TestFrac: 0.2, Folds: 3}.withDefaults()
	all := corpus(1000, 5) // 200 CM drafts

	var prevEnd int64
	for f := 0; f < opts.Folds; f++ {
		train, test, _, err := foldSplit(all, opts, f)
		if err != nil {
			t.Fatalf("fold %d: %v", f, err)
		}
		if len(test) == 0 {
			t.Fatalf("fold %d: empty test block", f)
		}
		first, last := test[0].StartTime, test[len(test)-1].StartTime
		if first <= prevEnd {
			t.Errorf("fold %d starts at %d, not after the previous fold's end %d", f, first, prevEnd)
		}
		prevEnd = last

		for _, m := range train {
			if m.StartTime >= first {
				t.Fatalf("fold %d trains on a match at %d, inside its own test window (starts %d)",
					f, m.StartTime, first)
			}
		}
		if got := countTargets(test, "cm"); got != 40 {
			t.Errorf("fold %d: %d target matches in test, want 40 (20%% of 200)", f, got)
		}
	}
}

// Splitting a run across processes is only sound if each process sees exactly the
// window it would have seen in one process. Assert the geometry rather than trust
// it: a silently shifted window would produce three logs that look combinable and
// are not.
func TestOnlyFoldMatchesTheFullRunGeometry(t *testing.T) {
	all := corpus(1000, 5)
	full := Options{TestFrac: 0.2, Folds: 3}.withDefaults()

	for f := 0; f < full.Folds; f++ {
		wantTrain, wantTest, wantMode, err := foldSplit(all, full, f)
		if err != nil {
			t.Fatalf("fold %d: %v", f, err)
		}
		one := Options{TestFrac: 0.2, Folds: 3, OnlyFold: f + 1}.withDefaults()
		gotTrain, gotTest, gotMode, err := foldSplit(all, one, f)
		if err != nil {
			t.Fatalf("only-fold %d: %v", f+1, err)
		}
		if len(gotTrain) != len(wantTrain) || len(gotTest) != len(wantTest) || gotMode != wantMode {
			t.Errorf("only-fold %d: %d/%d %q, want %d/%d %q",
				f+1, len(gotTrain), len(gotTest), gotMode, len(wantTrain), len(wantTest), wantMode)
		}
		if len(gotTest) > 0 && gotTest[0].ID != wantTest[0].ID {
			t.Errorf("only-fold %d starts at match %d, want %d", f+1, gotTest[0].ID, wantTest[0].ID)
		}
	}
}

// An out-of-range fold must fail loudly: in a parallel run it would otherwise be
// one process quietly producing an empty report.
func TestOnlyFoldOutOfRangeIsAnError(t *testing.T) {
	all := corpus(1000, 5)
	_, err := Run(all, []int{1, 2, 3, 4, 5, 6}, Options{Folds: 2, OnlyFold: 3})
	if err == nil {
		t.Fatal("only-fold 3 of 2 folds accepted; want an error")
	}
}

// Too few target matches to slice into folds: fall back rather than hand back an
// empty training set.
func TestStratificationFallsBackWhenTargetsAreScarce(t *testing.T) {
	opts := Options{TestFrac: 0.2, Folds: 1}.withDefaults()
	sparse := corpus(200, 0) // no CM at all
	_, test, mode, err := foldSplit(sparse, opts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "corpus-tail" {
		t.Errorf("mode = %q, want corpus-tail when there is no target data", mode)
	}
	if len(test) == 0 {
		t.Error("fallback split produced no test set")
	}
}

func TestPublicCMStratificationIgnoresProDrafts(t *testing.T) {
	all := corpus(500, 5)
	// Relabel half the CM drafts as pro: they are CM, but not the target population.
	for i := range all {
		if all[i].Source == cache.SourcePublicCM && i%10 == 0 {
			all[i].Source = cache.SourcePro
		}
	}
	opts := Options{TestFrac: 0.2, Folds: 1, StratifySplit: "public-cm"}.withDefaults()
	_, test, mode, err := foldSplit(all, opts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "stratified:public-cm" {
		t.Fatalf("mode = %q", mode)
	}
	if got, want := countTargets(test, "public-cm"), 10; got != want {
		t.Errorf("public-cm matches in test = %d, want %d (20%% of 50)", got, want)
	}
}
