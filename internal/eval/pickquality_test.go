package eval

import (
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/features"
)

func TestRankOfChosenSkipsTakenAndBreaksTiesByIndex(t *testing.T) {
	// Hero 2 is already taken, so it is neither a candidate nor a rank it could
	// occupy for someone else.
	scores := []float64{0.1, 0.9, 0.9, 0.5}
	taken := []bool{false, false, true, false}

	if rank, cands := rankOfChosen(scores, taken, 1); rank != 1 || cands != 3 {
		t.Errorf("best available hero: rank %d of %d, want 1 of 3", rank, cands)
	}
	if rank, _ := rankOfChosen(scores, taken, 3); rank != 2 {
		t.Errorf("middle hero rank = %d, want 2", rank)
	}
	if rank, _ := rankOfChosen(scores, taken, 0); rank != 3 {
		t.Errorf("worst hero rank = %d, want 3", rank)
	}

	// Ties go to the lower dense index, matching how the shipped ranking breaks
	// them — otherwise the same draft could score differently between runs.
	tied := []float64{0.5, 0.5, 0.5}
	none := []bool{false, false, false}
	for i, want := range []int{1, 2, 3} {
		if rank, _ := rankOfChosen(tied, none, i); rank != want {
			t.Errorf("tie at index %d: rank %d, want %d", i, rank, want)
		}
	}
}

// A ranker that always prefers the hero the WINNING side took must score AUC 1,
// and one that always prefers the loser's picks must score 0. This pins down the
// direction of the Dire-side inversion and of the match-level aggregation: get
// either backwards and these two land the wrong way round.
func TestPickQualityDirection(t *testing.T) {
	hm := features.NewHeroMap([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})

	// The WINNING side takes the low hero ids. Both matches have that property but
	// opposite winners, so a ranker that prefers low ids predicts both correctly
	// while a ranker that ignores the board cannot — a constant score would give
	// both matches the same value and separate nothing.
	draft := func(id int64, radiantWins bool) cache.Match {
		m := cache.Match{ID: id, StartTime: 1_700_000_000 + id, RadiantWin: radiantWins, Source: cache.SourcePublicCM}
		ord := 0
		for i, h := range []int{11, 12} {
			m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: false, Team: i % 2})
			ord++
		}
		low, high := []int{1, 2, 3, 4, 5}, []int{6, 7, 8, 9, 10}
		winner, loser := 0, 1
		if !radiantWins {
			winner, loser = 1, 0
		}
		for i := range low {
			m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: low[i], IsPick: true, Team: winner})
			ord++
			m.PickBans = append(m.PickBans, cache.PickBan{Ord: ord, HeroID: high[i], IsPick: true, Team: loser})
			ord++
		}
		return m
	}
	test := []cache.Match{draft(1, true), draft(2, false)}

	// The mirror image: prefers HIGH hero ids, which the losing side takes. It must
	// land at the opposite end of the scale, which is what pins the sign down —
	// including the Dire-side inversion, since a sign error there would make both
	// rankers agree instead of oppose.
	//
	// (A ranker returning one constant for everything is NOT a neutral control
	// here: with every score equal, the index tie-break in rankOfChosen becomes
	// the ordering, so it behaves exactly like favour-low-ids.)
	favourHighIDs := pickRanker{
		name: "favour-high-ids",
		score: func(st pickState, taken []bool) []float64 {
			out := make([]float64, len(taken))
			for i := range out {
				out[i] = float64(i)
			}
			return out
		},
	}
	// Prefers low hero ids, which the winning side takes in this fixture.
	favourLowIDs := pickRanker{
		name: "favour-low-ids",
		score: func(st pickState, taken []bool) []float64 {
			out := make([]float64, len(taken))
			for i := range out {
				out[i] = -float64(i)
			}
			return out
		},
	}

	got, _ := evalPickQuality(test, hm, []pickRanker{favourLowIDs, favourHighIDs}, "T", 0)
	if len(got) != 2 {
		t.Fatalf("metrics = %d, want 2", len(got))
	}
	if got[0].N != 2 {
		t.Errorf("matches scored = %d, want 2", got[0].N)
	}
	// The winner takes the low ids in both matches, so a ranker that prefers low
	// ids ranks the winner's picks above the loser's every time.
	if got[0].AUC.Point != 1 {
		t.Errorf("ranker aligned with the winning side: auc %.3f, want 1.000", got[0].AUC.Point)
	}
	// Exactly inverted, which it can only be if the side inversion is right.
	if got[1].AUC.Point != 0 {
		t.Errorf("ranker aligned with the losing side: auc %.3f, want 0.000", got[1].AUC.Point)
	}
}

// Matches where one side never picks cannot be compared side-against-side and
// must be dropped rather than scored as a tie.
func TestPickQualitySkipsOneSidedDrafts(t *testing.T) {
	hm := features.NewHeroMap([]int{1, 2, 3, 4})
	oneSided := cache.Match{
		ID: 1, StartTime: 1_700_000_000, RadiantWin: true, Source: cache.SourcePublicCM,
		PickBans: []cache.PickBan{
			{Ord: 0, HeroID: 4, IsPick: false, Team: 0},
			{Ord: 1, HeroID: 1, IsPick: true, Team: 0},
			{Ord: 2, HeroID: 2, IsPick: true, Team: 0},
		},
	}
	flat := pickRanker{
		name:  "flat",
		score: func(st pickState, taken []bool) []float64 { return make([]float64, len(taken)) },
	}
	got, _ := evalPickQuality([]cache.Match{oneSided}, hm, []pickRanker{flat}, "T", 0)
	if got[0].N != 0 {
		t.Errorf("one-sided draft scored %d matches, want 0", got[0].N)
	}
}
