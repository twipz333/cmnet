package train

import (
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/draft"
)

// cmDraftUnder builds a complete 24-action CM match played under schedule o, with
// firstPick drafting first. Hero ids are unique per match.
func cmDraftUnder(id int64, o draft.Order, firstPick int) cache.Match {
	m := cache.Match{ID: id, StartTime: 1_700_000_000 + id, Source: cache.SourcePublicCM}
	for i, s := range o {
		team := firstPick
		if s.Second {
			team = 1 - firstPick
		}
		m.PickBans = append(m.PickBans, cache.PickBan{
			Ord: i, HeroID: 1 + i, IsPick: s.Action == draft.Pick, Team: team,
		})
	}
	return m
}

func TestInferOrderReadsScheduleFromDrafts(t *testing.T) {
	// A hypothetical future patch: F F S F S S S opening (still balanced).
	future := draft.DefaultOrder()
	future[3], future[4] = future[4], future[3]

	var ms []cache.Match
	for i := 0; i < MinOrderDrafts; i++ {
		ms = append(ms, cmDraftUnder(int64(i), future, i%2)) // both seatings
	}
	rep := InferOrder(ms)
	if rep.Order == nil || !rep.Order.Equal(future) {
		t.Fatalf("inferred %v, want the drafts' own schedule %v", rep.Order, future)
	}
	if rep.Agreement != 1 {
		t.Fatalf("agreement = %v, want 1", rep.Agreement)
	}
}

func TestInferOrderNeedsEnoughDrafts(t *testing.T) {
	var ms []cache.Match
	for i := 0; i < MinOrderDrafts-1; i++ {
		ms = append(ms, cmDraftUnder(int64(i), draft.DefaultOrder(), 0))
	}
	if rep := InferOrder(ms); rep.Order != nil {
		t.Fatalf("order inferred from %d drafts, want none below %d", len(ms), MinOrderDrafts)
	}
}

// A cache straddling a schedule change must not pretend one schedule holds.
func TestInferOrderRefusesSplitCache(t *testing.T) {
	other := draft.DefaultOrder()
	other[3], other[4] = other[4], other[3]
	var ms []cache.Match
	for i := 0; i < 30; i++ {
		o := draft.DefaultOrder()
		if i%2 == 0 {
			o = other
		}
		ms = append(ms, cmDraftUnder(int64(i), o, 0))
	}
	if rep := InferOrder(ms); rep.Order != nil {
		t.Fatalf("split cache produced order %v (agreement %.2f)", rep.Order, rep.Agreement)
	}
}

// Partial or picks-only matches carry no schedule and must be ignored.
func TestInferOrderSkipsIncompleteDrafts(t *testing.T) {
	var ms []cache.Match
	for i := 0; i < MinOrderDrafts; i++ {
		ms = append(ms, cmDraftUnder(int64(i), draft.DefaultOrder(), 1))
	}
	short := cmDraftUnder(999, draft.DefaultOrder(), 0)
	short.PickBans = short.PickBans[:20]
	ms = append(ms, short, bulkMatch(1000, true))
	rep := InferOrder(ms)
	if rep.Drafts != MinOrderDrafts || rep.Order == nil {
		t.Fatalf("drafts=%d order=%v, want %d complete drafts and an order", rep.Drafts, rep.Order, MinOrderDrafts)
	}
}
