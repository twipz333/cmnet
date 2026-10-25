package train

import (
	"fmt"

	"cmnet/internal/cache"
	"cmnet/internal/draft"
)

// MinOrderDrafts is the fewest complete Captains Mode drafts InferOrder needs
// before it trusts what it reads. Below it the bundle ships no order and serve
// keeps the built-in one.
const MinOrderDrafts = 20

// minOrderAgreement is the share of complete drafts that must follow the inferred
// schedule exactly. A cache that straddles a draft-order change splits between
// two schedules; reporting the majority as if it were the order would be wrong for
// a large minority of the data, so such a cache yields no order at all.
const minOrderAgreement = 0.8

// OrderReport describes what InferOrder found, for the training log.
type OrderReport struct {
	Order     draft.Order // nil when no trustworthy order was found
	Drafts    int         // complete 24-action drafts examined
	Agreement float64     // share of those drafts matching Order exactly
}

// InferOrder recovers the Captains Mode schedule from cached drafts.
//
// Each complete draft (24 recorded actions with bans) is rewritten in first/second
// terms — the team of its first action is the first-pick team — and the schedule
// is the per-step majority. The result is returned only if it is a structurally
// valid CM order, enough drafts were seen, and most drafts follow it exactly.
//
// Deterministic: matches are scanned in the order given and ties in a step's vote
// resolve to the built-in schedule's slot.
func InferOrder(matches []cache.Match) OrderReport {
	def := draft.DefaultOrder()
	var votes [draft.CMSteps]map[draft.Slot]int
	for i := range votes {
		votes[i] = map[draft.Slot]int{}
	}
	var rows []draft.Order
	for _, m := range matches {
		o, ok := draftSlots(m)
		if !ok {
			continue
		}
		rows = append(rows, o)
		for i, s := range o {
			votes[i][s]++
		}
	}
	rep := OrderReport{Drafts: len(rows)}
	if len(rows) < MinOrderDrafts {
		return rep
	}

	inferred := make(draft.Order, draft.CMSteps)
	candidates := []draft.Slot{
		{Second: false, Action: draft.Ban}, {Second: true, Action: draft.Ban},
		{Second: false, Action: draft.Pick}, {Second: true, Action: draft.Pick},
	}
	for i := range inferred {
		best, bestN := def[i], votes[i][def[i]]
		for _, c := range candidates { // fixed order: no map iteration decides a tie
			if n := votes[i][c]; n > bestN {
				best, bestN = c, n
			}
		}
		inferred[i] = best
	}
	if !inferred.Valid() {
		return rep
	}
	match := 0
	for _, o := range rows {
		if o.Equal(inferred) {
			match++
		}
	}
	rep.Agreement = float64(match) / float64(len(rows))
	if rep.Agreement < minOrderAgreement {
		return rep
	}
	rep.Order = inferred
	return rep
}

// draftSlots rewrites one match's recorded actions in first/second terms. ok is
// false unless the match carries a full, real CM draft.
func draftSlots(m cache.Match) (draft.Order, bool) {
	if !hasRealDraftOrder(m) {
		return nil, false
	}
	pbs := orderedPickBans(m)
	if len(pbs) != draft.CMSteps {
		return nil, false
	}
	first := pbs[0].Team
	out := make(draft.Order, len(pbs))
	for i, pb := range pbs {
		a := draft.Ban
		if pb.IsPick {
			a = draft.Pick
		}
		out[i] = draft.Slot{Second: pb.Team != first, Action: a}
	}
	return out, true
}

// Note renders the report as one training-log line, flagging the case that needs
// a human: the data's schedule differs from the one compiled into the encoder.
func (r OrderReport) Note(encoderPhases []int) string {
	if r.Order == nil {
		return fmt.Sprintf("cm order: not inferred (%d complete drafts, agreement %.0f%%) — serve uses the built-in schedule",
			r.Drafts, r.Agreement*100)
	}
	msg := fmt.Sprintf("cm order: inferred from %d complete drafts (%.0f%% agree), phases %v",
		r.Drafts, r.Agreement*100, r.Order.Phases())
	if !r.Order.Equal(draft.DefaultOrder()) {
		msg += " — DIFFERS from draft.DefaultOrder; the bundle carries the data's order"
	}
	if !equalInts(r.Order.Phases(), encoderPhases) {
		msg += fmt.Sprintf(" — WARNING: predictor phase features assume phases %v; update features.cmPhaseBounds and bump FeatureSchemaVersion",
			encoderPhases)
	}
	return msg
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
