package eval

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Rank agreement answers a question the headline metrics cannot: does a component
// change the ordering at all?
//
// Two runs found the full beam search (win-prob + synergy + counter + role
// coverage, depth 4) producing the same candidate order as one-ply win-prob, to
// three decimals on both AUC and mean rank. An AUC comparison cannot distinguish
// "the extra machinery is worthless" from "the extra machinery never ran": both
// look like a tie. This measures the ordering directly, so a component that is
// switched on but inert is visible as such instead of being read as a fair fight
// it happened to draw.
//
// It is deliberately reported per fold alongside the metrics rather than kept as a
// one-off script: the degeneracy went unnoticed across months of tuning precisely
// because nothing printed it.

// agreementTopN is how deep the ordering comparison looks. The user sees a handful
// of recommendations, so a reshuffle below that depth is real but not what anyone
// is deciding on; counting inversions over all ~110 candidates would also swamp the
// signal with churn among heroes nobody would pick.
const agreementTopN = 16

// RankAgreement compares two rankers scored on identical draft states.
type RankAgreement struct {
	Group string
	A, B  string // A is the variant under test, B the reference
	States int

	// SameTop1 is the fraction of states where both rankers name the same best
	// candidate — the single number most directly tied to what gets shown.
	SameTop1 float64
	// Top5Overlap is the mean size of the intersection of the two top-5 lists (0-5).
	Top5Overlap float64
	// Inversions is the mean number of candidate pairs ordered differently by the
	// two rankers within the top-N union, out of PairsPerState.
	Inversions    float64
	PairsPerState float64
	// StatesReordered counts the states where the two rankers disagreed about
	// anything at all. Reported as a count rather than folded into the mean because
	// a mean of 0.00 and a strict zero are different findings — "almost never
	// reorders" leaves a mechanism to explain, "never reorders" does not.
	StatesReordered int

	// ScoreSpreadA is the mean within-state standard deviation of A's scores, and
	// DiffSpread the same for (A − B). A diff spread near zero with a healthy score
	// spread is the signature of an additive constant: the component shifts every
	// candidate by the same amount and therefore cannot reorder anything (§3.2 of
	// the target architecture).
	ScoreSpreadA float64
	DiffSpread   float64
}

// Inert reports whether A never reordered anything relative to B.
func (a RankAgreement) Inert() bool {
	return a.States > 0 && a.StatesReordered == 0 && a.sameTop1All()
}

func (a RankAgreement) sameTop1All() bool { return a.SameTop1 == 1 }

// agreementAccumulator collects per-state comparisons for one pair of rankers.
type agreementAccumulator struct {
	a, b       int // ranker indices
	nameA      string
	nameB      string
	states     int
	sameTop1   int
	top5       float64
	inversions float64
	reordered  int
	pairs      float64
	spreadA    float64
	diffSpread float64
}

// newAgreements builds one accumulator per (variant, reference) pair, where the
// reference is the plain one-ply win-prob ranker. Frequency baselines are skipped:
// they are a different kind of answer, and "the meta ranker disagrees with the
// model" is not news.
func newAgreements(rankers []pickRanker) []*agreementAccumulator {
	ref := -1
	for i, rk := range rankers {
		if rk.name == winProbRankerName {
			ref = i
			break
		}
	}
	if ref < 0 {
		return nil
	}
	var out []*agreementAccumulator
	for i, rk := range rankers {
		if i == ref || rk.name == popularityRankerName || strings.HasPrefix(rk.name, "popularity") {
			continue
		}
		out = append(out, &agreementAccumulator{a: i, b: ref, nameA: rk.name, nameB: rankers[ref].name})
	}
	return out
}

// observe records one draft state, given every ranker's scores over the same
// candidate set.
func (acc *agreementAccumulator) observe(scores [][]float64, taken []bool) {
	sa, sb := scores[acc.a], scores[acc.b]
	if len(sa) == 0 || len(sb) == 0 {
		return
	}
	avail := make([]int, 0, len(taken))
	for i := range taken {
		if !taken[i] && i < len(sa) && i < len(sb) {
			avail = append(avail, i)
		}
	}
	if len(avail) < 2 {
		return
	}

	acc.states++
	acc.spreadA += stddev(sa, avail)
	acc.diffSpread += stddevDiff(sa, sb, avail)

	orderA := rankOrder(sa, avail)
	orderB := rankOrder(sb, avail)
	if orderA[0] == orderB[0] {
		acc.sameTop1++
	}
	acc.top5 += float64(overlap(orderA, orderB, 5))

	// Compare the ordering over the union of both top-N lists, so a candidate one
	// ranker promotes into view and the other does not is counted as the
	// disagreement it is.
	union := unionOf(orderA, orderB, agreementTopN)
	inv, pairs := 0, 0
	for i := 0; i < len(union); i++ {
		for j := i + 1; j < len(union); j++ {
			x, y := union[i], union[j]
			pairs++
			if before(sa, x, y) != before(sb, x, y) {
				inv++
			}
		}
	}
	acc.inversions += float64(inv)
	acc.pairs += float64(pairs)
	if inv > 0 {
		acc.reordered++
	}
}

func (acc *agreementAccumulator) result(group string) RankAgreement {
	r := RankAgreement{Group: group, A: acc.nameA, B: acc.nameB, States: acc.states}
	if acc.states == 0 {
		return r
	}
	n := float64(acc.states)
	r.SameTop1 = float64(acc.sameTop1) / n
	r.Top5Overlap = acc.top5 / n
	r.Inversions = acc.inversions / n
	r.StatesReordered = acc.reordered
	r.PairsPerState = acc.pairs / n
	r.ScoreSpreadA = acc.spreadA / n
	r.DiffSpread = acc.diffSpread / n
	return r
}

// before reports whether x outranks y under scores, using the same tie-break as the
// shipped ranking (dense index ascending) so agreement is measured against what a
// user would actually see.
func before(scores []float64, x, y int) bool {
	if scores[x] != scores[y] {
		return scores[x] > scores[y]
	}
	return x < y
}

// rankOrder sorts the available candidates best-first.
func rankOrder(scores []float64, avail []int) []int {
	out := append([]int{}, avail...)
	sort.SliceStable(out, func(i, j int) bool { return before(scores, out[i], out[j]) })
	return out
}

func overlap(a, b []int, k int) int {
	if len(a) < k {
		k = len(a)
	}
	if len(b) < k {
		k = len(b)
	}
	set := make(map[int]bool, k)
	for _, v := range a[:k] {
		set[v] = true
	}
	n := 0
	for _, v := range b[:k] {
		if set[v] {
			n++
		}
	}
	return n
}

// unionOf returns the union of the two top-k prefixes, in a deterministic order.
func unionOf(a, b []int, k int) []int {
	seen := map[int]bool{}
	var out []int
	add := func(ids []int) {
		for i := 0; i < len(ids) && i < k; i++ {
			if !seen[ids[i]] {
				seen[ids[i]] = true
				out = append(out, ids[i])
			}
		}
	}
	add(a)
	add(b)
	sort.Ints(out)
	return out
}

func stddev(s []float64, avail []int) float64 {
	if len(avail) < 2 {
		return 0
	}
	var mean float64
	for _, i := range avail {
		mean += s[i]
	}
	mean /= float64(len(avail))
	var ss float64
	for _, i := range avail {
		d := s[i] - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(avail)-1))
}

func stddevDiff(a, b []float64, avail []int) float64 {
	if len(avail) < 2 {
		return 0
	}
	var mean float64
	for _, i := range avail {
		mean += a[i] - b[i]
	}
	mean /= float64(len(avail))
	var ss float64
	for _, i := range avail {
		d := (a[i] - b[i]) - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(avail)-1))
}

// formatAgreements renders the diagnostic block, naming an inert component outright
// rather than leaving it to be inferred from four decimals.
func formatAgreements(rows []RankAgreement) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nRANK AGREEMENT (does the component change the ordering at all?):\n")
	fmt.Fprintf(&b, "  %-14s %-16s %7s %8s %9s %11s %10s %10s %10s\n",
		"grp", "variant", "states", "same#1", "top5∩", "inversions", "reordered", "spread", "Δspread")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-14s %-16s %7d %8.3f %9.2f %11.2f %10d %10.6f %10.6f\n",
			r.Group, r.A, r.States, r.SameTop1, r.Top5Overlap, r.Inversions, r.StatesReordered,
			r.ScoreSpreadA, r.DiffSpread)
	}
	for _, r := range rows {
		switch {
		case r.Inert():
			fmt.Fprintf(&b, "  ! %s/%s is INERT: identical order to %s in all %d states — it is switched on and changing nothing\n",
				r.Group, r.A, r.B, r.States)
		case r.States > 0 && float64(r.StatesReordered)/float64(r.States) < 0.01:
			fmt.Fprintf(&b, "  ! %s/%s is NEARLY INERT: it reorders in %d of %d states (%.2f%%) — whatever it adds is a\n"+
				"    per-state constant almost everywhere, so tuning its weight cannot rescue it\n",
				r.Group, r.A, r.StatesReordered, r.States,
				100*float64(r.StatesReordered)/float64(r.States))
		}
	}
	b.WriteString("  (Δspread ≈ 0 with a non-zero spread means the variant adds a per-state constant,\n" +
		"   which cannot reorder candidates no matter how large its weight)\n")
	return b.String()
}
