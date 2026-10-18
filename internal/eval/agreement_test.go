package eval

import (
	"strings"
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/features"
)

// agreementFixture is one two-sided CM draft, enough to drive scoring states.
func agreementFixture() []cache.Match {
	return []cache.Match{{
		ID: 1, StartTime: 1_700_000_000, RadiantWin: true, Source: cache.SourcePublicCM,
		PickBans: []cache.PickBan{
			{Ord: 0, HeroID: 6, IsPick: false, Team: 0},
			{Ord: 1, HeroID: 1, IsPick: true, Team: 0},
			{Ord: 2, HeroID: 2, IsPick: true, Team: 1},
			{Ord: 3, HeroID: 3, IsPick: true, Team: 0},
			{Ord: 4, HeroID: 4, IsPick: true, Team: 1},
		},
	}}
}

// scoreByIndex ranks candidates by dense index, scaled and offset. The offset is
// the thing under test: adding a per-state constant must not reorder anything.
func scoreByIndex(name string, scale, offset float64) pickRanker {
	return pickRanker{
		name: name,
		score: func(st pickState, taken []bool) []float64 {
			out := make([]float64, len(taken))
			for i := range out {
				out[i] = float64(i)*scale + offset
			}
			return out
		},
	}
}

// The whole point of the diagnostic: a variant that only shifts every score by the
// same amount is reported as inert, however large the shift.
func TestAgreementFlagsAdditiveConstantAsInert(t *testing.T) {
	hm := features.NewHeroMap([]int{1, 2, 3, 4, 5, 6})
	ref := scoreByIndex(winProbRankerName, 1, 0)
	shifted := scoreByIndex("advisor", 1, 100) // same order, huge offset

	_, agree := evalPickQuality(agreementFixture(), hm, []pickRanker{ref, shifted}, "T", 0)
	if len(agree) != 1 {
		t.Fatalf("agreements = %d, want 1", len(agree))
	}
	a := agree[0]
	if a.States == 0 {
		t.Fatal("no states observed")
	}
	if !a.Inert() {
		t.Errorf("constant-offset variant not reported inert: %+v", a)
	}
	if a.Inversions != 0 {
		t.Errorf("inversions = %.2f, want 0", a.Inversions)
	}
	if a.DiffSpread != 0 {
		t.Errorf("diff spread = %.4f, want 0 for a pure offset", a.DiffSpread)
	}
	if a.ScoreSpreadA == 0 {
		t.Error("score spread is 0; the fixture cannot distinguish inert from constant scores")
	}
	if out := formatAgreements(agree); !strings.Contains(out, "INERT") {
		t.Errorf("report does not call out the inert component:\n%s", out)
	}
}

// A variant that genuinely reorders must not be reported as inert.
func TestAgreementDetectsRealReordering(t *testing.T) {
	hm := features.NewHeroMap([]int{1, 2, 3, 4, 5, 6})
	ref := scoreByIndex(winProbRankerName, 1, 0)
	reversed := scoreByIndex("advisor", -1, 0) // exactly inverted order

	_, agree := evalPickQuality(agreementFixture(), hm, []pickRanker{ref, reversed}, "T", 0)
	a := agree[0]
	if a.Inert() {
		t.Error("a reversed ranker was reported as inert")
	}
	if a.Inversions == 0 {
		t.Error("inversions = 0 for a reversed ranker")
	}
	if a.SameTop1 != 0 {
		t.Errorf("same-top1 = %.2f, want 0 for a reversed ranker", a.SameTop1)
	}
	if a.DiffSpread == 0 {
		t.Error("diff spread = 0 for a ranker that changes the order")
	}
	if out := formatAgreements(agree); strings.Contains(out, "INERT") {
		t.Errorf("report wrongly calls a live component inert:\n%s", out)
	}
}

// Frequency baselines are not variants of the model and must not be compared.
func TestAgreementSkipsBaselines(t *testing.T) {
	hm := features.NewHeroMap([]int{1, 2, 3, 4, 5, 6})
	rankers := []pickRanker{
		scoreByIndex(winProbRankerName, 1, 0),
		scoreByIndex(popularityRankerName, -1, 0),
		scoreByIndex("advisor", 2, 0),
	}
	_, agree := evalPickQuality(agreementFixture(), hm, rankers, "T", 0)
	if len(agree) != 1 || agree[0].A != "advisor" {
		t.Fatalf("agreements = %+v, want only the advisor variant", agree)
	}
}

// Without the one-ply reference there is nothing to compare against, and the
// diagnostic must stay silent rather than invent a reference.
func TestAgreementNeedsTheReferenceRanker(t *testing.T) {
	hm := features.NewHeroMap([]int{1, 2, 3, 4, 5, 6})
	rankers := []pickRanker{scoreByIndex("advisor", 1, 0), scoreByIndex("matrices", 2, 0)}
	_, agree := evalPickQuality(agreementFixture(), hm, rankers, "T", 0)
	if len(agree) != 0 {
		t.Errorf("agreements = %+v, want none without %q", agree, winProbRankerName)
	}
	if out := formatAgreements(agree); out != "" {
		t.Errorf("empty agreement set still printed a block:\n%s", out)
	}
}
