package behavioraudit

import (
	"sort"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

// SampleConfig controls StratifiedSample sizing.
type SampleConfig struct {
	Size            int
	Seed            int64
	IncludeExcluded bool
	TargetLabeled   int // default 40
	TargetAmbiguous int // default 20 (or all if fewer)
	TargetUnlabStrong int // default 40
	TargetUnlabWeak   int // default 20
}

func (c SampleConfig) withDefaults() SampleConfig {
	if c.Size <= 0 {
		c.Size = 120
	}
	if c.TargetLabeled <= 0 {
		c.TargetLabeled = 40
	}
	if c.TargetAmbiguous <= 0 {
		c.TargetAmbiguous = 20
	}
	if c.TargetUnlabStrong <= 0 {
		c.TargetUnlabStrong = 40
	}
	if c.TargetUnlabWeak <= 0 {
		c.TargetUnlabWeak = 20
	}
	return c
}

// StratifiedSample selects ~N episode-level cases per TZ sampling rules.
// Deterministic for a fixed seed.
func StratifiedSample(segs []macrolabel.MacroActionSegment, cfg SampleConfig) []macrolabel.MacroActionSegment {
	cfg = cfg.withDefaults()
	episodes := dedupeEpisodes(segs, cfg.IncludeExcluded)
	if len(episodes) == 0 {
		return nil
	}

	var must, labeled, ambiguous, unlabStrong, unlabWeak []macrolabel.MacroActionSegment
	mustIDs := map[string]struct{}{}

	for _, e := range episodes {
		key := episodeKey(e)
		if isMustInclude(e) {
			if _, ok := mustIDs[key]; !ok {
				must = append(must, e)
				mustIDs[key] = struct{}{}
			}
			continue
		}
		switch e.Status {
		case macrolabel.LabelStatusLabeled:
			labeled = append(labeled, e)
		case macrolabel.LabelStatusAmbiguous:
			ambiguous = append(ambiguous, e)
		case macrolabel.LabelStatusUnlabeled:
			if isStrongRejected(e) {
				unlabStrong = append(unlabStrong, e)
			} else {
				unlabWeak = append(unlabWeak, e)
			}
		case macrolabel.LabelStatusExcluded:
			if cfg.IncludeExcluded {
				unlabWeak = append(unlabWeak, e)
			}
		}
	}

	// Oversample rare labeled kinds: sort by ascending kind frequency, then shuffle stably.
	labeled = oversampleRareKinds(labeled, cfg.Seed+11)

	picked := make([]macrolabel.MacroActionSegment, 0, cfg.Size)
	seen := map[string]struct{}{}
	add := func(rows []macrolabel.MacroActionSegment) {
		for _, r := range rows {
			k := episodeKey(r)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			picked = append(picked, r)
		}
	}

	// 1) Must-include (all smoke-related + all RETREAT labeled).
	add(must)

	// 2) Ambiguous: all, capped at TargetAmbiguous.
	ambTake := takePerm(ambiguous, cfg.TargetAmbiguous, cfg.Seed+21)
	if len(ambiguous) <= cfg.TargetAmbiguous {
		ambTake = ambiguous
	}
	add(ambTake)

	// 3) Labeled fill toward TargetLabeled (must already counted).
	labeledNeed := cfg.TargetLabeled - countStatus(picked, macrolabel.LabelStatusLabeled)
	if labeledNeed < 0 {
		labeledNeed = 0
	}
	add(takePerm(labeled, labeledNeed, cfg.Seed+31))

	// 4) Unlabeled strong / weak.
	strongNeed := cfg.TargetUnlabStrong - countStatusFiltered(picked, macrolabel.LabelStatusUnlabeled, true)
	if strongNeed < 0 {
		strongNeed = 0
	}
	add(takePerm(unlabStrong, strongNeed, cfg.Seed+41))

	weakNeed := cfg.TargetUnlabWeak - countStatusFiltered(picked, macrolabel.LabelStatusUnlabeled, false)
	if weakNeed < 0 {
		weakNeed = 0
	}
	add(takePerm(unlabWeak, weakNeed, cfg.Seed+51))

	// 5) Fill remaining slots from leftovers in stable priority order.
	if len(picked) < cfg.Size {
		leftovers := append([]macrolabel.MacroActionSegment{}, labeled...)
		leftovers = append(leftovers, ambiguous...)
		leftovers = append(leftovers, unlabStrong...)
		leftovers = append(leftovers, unlabWeak...)
		leftovers = takePerm(leftovers, len(leftovers), cfg.Seed+61)
		for _, r := range leftovers {
			if len(picked) >= cfg.Size {
				break
			}
			k := episodeKey(r)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			picked = append(picked, r)
		}
	}

	if len(picked) > cfg.Size {
		// Keep all must-includes; trim from the end of non-must.
		picked = trimPreferMust(picked, mustIDs, cfg.Size)
	}

	sort.SliceStable(picked, func(i, j int) bool {
		if picked[i].StartGameTime != picked[j].StartGameTime {
			return picked[i].StartGameTime < picked[j].StartGameTime
		}
		if picked[i].EndGameTime != picked[j].EndGameTime {
			return picked[i].EndGameTime < picked[j].EndGameTime
		}
		return episodeKey(picked[i]) < episodeKey(picked[j])
	})
	return picked
}

func isMustInclude(e macrolabel.MacroActionSegment) bool {
	if e.SmokeAnchorID != "" {
		return true
	}
	if e.Action != nil && e.Action.Kind == action.KindSmoke {
		return true
	}
	if e.Status == macrolabel.LabelStatusAmbiguous {
		for _, c := range e.Competing {
			if a, ok := action.DefaultSpaceV0().ByID(c.ActionID); ok && a.Kind == action.KindSmoke {
				return true
			}
		}
	}

	if e.Status == macrolabel.LabelStatusLabeled && e.Action != nil && e.Action.Kind == action.KindRetreat {
		return true
	}
	return false
}

func isStrongRejected(e macrolabel.MacroActionSegment) bool {
	if len(e.RejectedCandidates) == 0 {
		return false
	}
	best := 0.0
	for _, r := range e.RejectedCandidates {
		if r.Confidence > best {
			best = r.Confidence
		}
	}
	return best >= 0.35 || len(e.RejectedCandidates) >= 2
}

func dedupeEpisodes(segs []macrolabel.MacroActionSegment, includeExcluded bool) []macrolabel.MacroActionSegment {
	filtered := make([]macrolabel.MacroActionSegment, 0, len(segs))
	for _, s := range segs {
		if s.Status == macrolabel.LabelStatusExcluded && !includeExcluded {
			continue
		}
		filtered = append(filtered, s)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].StartGameTime != filtered[j].StartGameTime {
			return filtered[i].StartGameTime < filtered[j].StartGameTime
		}
		return filtered[i].EndGameTime < filtered[j].EndGameTime
	})

	if len(filtered) == 0 {
		return nil
	}

	out := []macrolabel.MacroActionSegment{filtered[0]}
	for i := 1; i < len(filtered); i++ {
		prev := &out[len(out)-1]
		cur := filtered[i]
		if sameCoherentEpisode(*prev, cur) {
			// Merge contiguous window into previous.
			if cur.EndGameTime > prev.EndGameTime {
				prev.EndGameTime = cur.EndGameTime
				prev.EndSeq = cur.EndSeq
				prev.DurationSeconds = float64(prev.EndGameTime - prev.StartGameTime)
			}
			continue
		}
		out = append(out, cur)
	}
	return out
}

func sameCoherentEpisode(a, b macrolabel.MacroActionSegment) bool {
	if a.MatchID != b.MatchID || a.Side != b.Side || a.Status != b.Status {
		return false
	}
	aid, bid := -1, -1
	if a.Action != nil {
		aid = a.Action.ID
	}
	if b.Action != nil {
		bid = b.Action.ID
	}
	if aid != bid {
		return false
	}
	// Contiguous or overlapping in time.
	if b.StartGameTime > a.EndGameTime+1 {
		return false
	}
	return true
}

func episodeKey(e macrolabel.MacroActionSegment) string {
	aid := -1
	if e.Action != nil {
		aid = e.Action.ID
	}
	return e.MatchID + "|" + e.Side + "|" + string(e.Status) + "|" +
		itoa(e.StartGameTime) + "|" + itoa(e.EndGameTime) + "|" + itoa(aid)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func oversampleRareKinds(labeled []macrolabel.MacroActionSegment, seed int64) []macrolabel.MacroActionSegment {
	if len(labeled) == 0 {
		return labeled
	}
	freq := map[action.ActionKind]int{}
	for _, s := range labeled {
		if s.Action != nil {
			freq[s.Action.Kind]++
		}
	}
	sort.SliceStable(labeled, func(i, j int) bool {
		fi, fj := 0, 0
		var ki, kj action.ActionKind
		if labeled[i].Action != nil {
			ki = labeled[i].Action.Kind
			fi = freq[ki]
		}
		if labeled[j].Action != nil {
			kj = labeled[j].Action.Kind
			fj = freq[kj]
		}
		if fi != fj {
			return fi < fj // rare first
		}
		if ki != kj {
			return ki < kj
		}
		return labeled[i].StartGameTime < labeled[j].StartGameTime
	})
	// Mild deterministic shuffle within rarity bands using seed.
	return rotateBySeed(labeled, seed)
}

func rotateBySeed(in []macrolabel.MacroActionSegment, seed int64) []macrolabel.MacroActionSegment {
	if len(in) == 0 {
		return in
	}
	out := make([]macrolabel.MacroActionSegment, len(in))
	copy(out, in)
	perm := deterministicPerm(len(out), seed)
	tmp := make([]macrolabel.MacroActionSegment, len(out))
	for i, p := range perm {
		tmp[i] = out[p]
	}
	return tmp
}

func takePerm(rows []macrolabel.MacroActionSegment, n int, seed int64) []macrolabel.MacroActionSegment {
	if n <= 0 || len(rows) == 0 {
		return nil
	}
	if n > len(rows) {
		n = len(rows)
	}
	perm := deterministicPerm(len(rows), seed)
	out := make([]macrolabel.MacroActionSegment, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, rows[perm[i]])
	}
	return out
}

func countStatus(rows []macrolabel.MacroActionSegment, st macrolabel.LabelStatus) int {
	n := 0
	for _, r := range rows {
		if r.Status == st {
			n++
		}
	}
	return n
}

func countStatusFiltered(rows []macrolabel.MacroActionSegment, st macrolabel.LabelStatus, strong bool) int {
	n := 0
	for _, r := range rows {
		if r.Status != st {
			continue
		}
		if isStrongRejected(r) == strong {
			n++
		}
	}
	return n
}

func trimPreferMust(picked []macrolabel.MacroActionSegment, mustIDs map[string]struct{}, size int) []macrolabel.MacroActionSegment {
	if len(picked) <= size {
		return picked
	}
	must := make([]macrolabel.MacroActionSegment, 0)
	rest := make([]macrolabel.MacroActionSegment, 0)
	for _, p := range picked {
		if _, ok := mustIDs[episodeKey(p)]; ok {
			must = append(must, p)
		} else {
			rest = append(rest, p)
		}
	}
	out := append([]macrolabel.MacroActionSegment{}, must...)
	need := size - len(out)
	if need < 0 {
		return must[:size]
	}
	if need > len(rest) {
		need = len(rest)
	}
	out = append(out, rest[:need]...)
	return out
}

func deterministicPerm(n int, seed int64) []int {
	idxs := make([]int, n)
	for i := range idxs {
		idxs[i] = i
	}
	x := uint64(seed)
	if x == 0 {
		x = 1
	}
	for i := n - 1; i > 0; i-- {
		x = x*6364136223846793005 + 1
		j := int(x % uint64(i+1))
		idxs[i], idxs[j] = idxs[j], idxs[i]
	}
	return idxs
}