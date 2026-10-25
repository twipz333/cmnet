package eval

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"cmnet/internal/blend"
	"cmnet/internal/cache"
	"cmnet/internal/features"
	"cmnet/internal/model"
	"cmnet/internal/roles"
)

// LadderRung is one step of the §6.6 personalization ladder.
type LadderRung struct {
	Name     string
	N        int // scored actions
	LogLoss  float64
	Brier    float64
	ECE      float64
	TopK     []int
	Acc      []CI
	MeanRank float64
	// VsPrev is paired (this − previous) on the same matches. Empty on rung 1.
	VsPrevTop5    CI
	VsPrevLogLoss CI
	// Proven is true when at least one VsPrev CI excludes zero (improvement).
	Proven bool
	// Note explains undervpowered rungs (e.g. no roster accounts in corpus).
	Note string
}

// FamiliarSplit scores familiar vs unknown acting sides separately.
type FamiliarSplit struct {
	Familiar LadderRung
	Unknown  LadderRung
}

// PhaseSplit scores by CM phase index (0..5).
type PhaseSplit struct {
	Phase int
	Rung  LadderRung
}

// LadderReport is the §6.6 output for one fold.
type LadderReport struct {
	Rungs    []LadderRung
	Familiar *FamiliarSplit // nil when too few labeled actions
	ByPhase  []PhaseSplit
}

const familiarMinGames = 5

// evalLadder runs rungs 1→5 on CM test drafts. corpus is train∪older history for as-of.
func evalLadder(corpus, train, test []cache.Match, pred *model.Predictor, hm *features.HeroMap, prior *roles.Prior, opts Options) LadderReport {
	if len(opts.TopK) == 0 {
		opts.TopK = []int{1, 3, 5}
	}
	idx := buildAsOfIndex(corpus)
	stepBL := &Baseline{Name: "popularity|step", counts: map[baselineKey][]float64{}, keyOf: func(ord int, isPick bool, actor int) baselineKey {
		return baselineKey{a: ord}
	}}
	stepBL.Fit(train, hm)

	roleAff := roleAffinityVector(prior, hm)

	type rungFn func(st features.PredictorState, taken []bool, m cache.Match, team int) []float64
	rungs := []struct {
		name string
		fn   rungFn
		note string
	}{
		{"1.popularity|step", func(st features.PredictorState, taken []bool, m cache.Match, team int) []float64 {
			return normalizeScores(stepBL.Score(st.Ord, st.IsPick, st.Actor, taken))
		}, ""},
		{"2.model", func(st features.PredictorState, taken []bool, m cache.Match, team int) []float64 {
			return pred.DistributionMaskedIdx(st, taken)
		}, ""},
		{"3.model+player", func(st features.PredictorState, taken []bool, m cache.Match, team int) []float64 {
			p1 := pred.DistributionMaskedIdx(st, taken)
			aff2, _ := idx.layer2For(m, team, st.IsPick, hm)
			return blend.Blend(p1, aff2, nil, 0, blend.DefaultWeights())
		}, ""},
		{"4.+roles", func(st features.PredictorState, taken []bool, m cache.Match, team int) []float64 {
			p1 := pred.DistributionMaskedIdx(st, taken)
			aff2, _ := idx.layer2For(m, team, st.IsPick, hm)
			// The role prior describes what a side still needs to PLAY; it says
			// nothing about what it bans, so ban steps get no role term.
			if roleAff != nil && st.IsPick {
				if aff2 == nil {
					aff2 = append([]float64(nil), roleAff...)
				} else {
					merged := append([]float64(nil), aff2...)
					for i := range merged {
						if i < len(roleAff) {
							merged[i] += roleAff[i]
						}
					}
					aff2 = merged
				}
			}
			return blend.Blend(p1, aff2, nil, 0, blend.DefaultWeights())
		}, ""},
		{"5.+stack", func(st features.PredictorState, taken []bool, m cache.Match, team int) []float64 {
			p1 := pred.DistributionMaskedIdx(st, taken)
			aff2, _ := idx.layer2For(m, team, st.IsPick, hm)
			aff3, n := idx.stackAff3(m, team, m.StartTime, hm, !st.IsPick)
			return blend.Blend(p1, aff2, aff3, n, blend.DefaultWeights())
		}, ""},
	}

	scored := make([]ladderScore, len(rungs))
	for i := range scored {
		scored[i] = scoreLadderRung(test, rungs[i].fn, hm, opts, idx)
		scored[i].name = rungs[i].name
	}

	out := LadderReport{Rungs: make([]LadderRung, len(scored))}
	for i, s := range scored {
		out.Rungs[i] = s.toRung(opts)
		if i > 0 {
			out.Rungs[i].VsPrevTop5 = pairedTopK(s, scored[i-1], 5, opts.Bootstrap)
			out.Rungs[i].VsPrevLogLoss = pairedLogLoss(s, scored[i-1], opts.Bootstrap)
			out.Rungs[i].Proven = out.Rungs[i].VsPrevTop5.Significant() || out.Rungs[i].VsPrevLogLoss.Significant()
		}
	}

	// Familiar / unknown on rung 3 (player profiles).
	if fam, unk, ok := splitFamiliar(test, rungs[2].fn, hm, opts, idx); ok {
		out.Familiar = &FamiliarSplit{Familiar: fam.toRung(opts), Unknown: unk.toRung(opts)}
	}
	out.ByPhase = phaseSplits(test, rungs[1].fn, hm, opts, idx)

	// Annotate undervpowered player rung.
	if idx.accounts == 0 {
		out.Rungs[2].Note = "no account_ids in corpus — rung undervpowered"
		out.Rungs[3].Note = "depends on rung 3"
		out.Rungs[4].Note = "depends on rung 3"
	}
	return out
}

type heroEvt struct {
	t    int64
	hero int
}

type asOfIndex struct {
	byAccount map[int64][]heroEvt // sorted by t
	// matches with players, sorted by start time, for stack lookup
	withPlayers []cache.Match
	accounts    int
}

func buildAsOfIndex(matches []cache.Match) *asOfIndex {
	idx := &asOfIndex{byAccount: map[int64][]heroEvt{}}
	for _, m := range matches {
		if len(m.Players) == 0 {
			continue
		}
		idx.withPlayers = append(idx.withPlayers, m)
		for _, p := range m.Players {
			if p.AccountID == nil || *p.AccountID == 0 || p.HeroID <= 0 {
				continue
			}
			id := *p.AccountID
			idx.byAccount[id] = append(idx.byAccount[id], heroEvt{t: m.StartTime, hero: p.HeroID})
		}
	}
	for id, ev := range idx.byAccount {
		sort.SliceStable(ev, func(i, j int) bool { return ev[i].t < ev[j].t })
		idx.byAccount[id] = ev
	}
	idx.accounts = len(idx.byAccount)
	sort.SliceStable(idx.withPlayers, func(i, j int) bool {
		return idx.withPlayers[i].StartTime < idx.withPlayers[j].StartTime
	})
	return idx
}

func (idx *asOfIndex) teamAccounts(m cache.Match, radiant bool) []int64 {
	var out []int64
	seen := map[int64]bool{}
	for _, p := range m.Players {
		if p.IsRadiant != radiant || p.AccountID == nil || *p.AccountID == 0 {
			continue
		}
		id := *p.AccountID
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (idx *asOfIndex) teamAff2(m cache.Match, team int, before int64, hm *features.HeroMap) (aff []float64, familiar bool) {
	radiant := team == 0
	accs := idx.teamAccounts(m, radiant)
	if len(accs) == 0 {
		return nil, false
	}
	aff = make([]float64, hm.Len())
	nPlayers := 0
	for _, id := range accs {
		games := 0
		counts := map[int]int{}
		for _, e := range idx.byAccount[id] {
			if e.t >= before {
				break
			}
			counts[e.hero]++
			games++
		}
		if games >= familiarMinGames {
			familiar = true
		}
		if games == 0 {
			continue
		}
		for hero, c := range counts {
			if i, ok := hm.Index(hero); ok {
				aff[i] += float64(c) / (float64(games) + 5)
			}
		}
		nPlayers++
	}
	if nPlayers == 0 {
		return nil, familiar
	}
	return aff, familiar
}

// layer2For is the layer-2 evidence for one action by team, as serve blends it
// (recommender.blendEnemy): a pick is evidence about what the acting side plays,
// so it uses the acting side's own comfort; a ban is aimed at the OTHER side's
// heroes, so it uses theirs. familiar always reports on the acting side.
func (idx *asOfIndex) layer2For(m cache.Match, team int, isPick bool, hm *features.HeroMap) (aff []float64, familiar bool) {
	if isPick {
		return idx.teamAff2(m, team, m.StartTime, hm)
	}
	_, familiar = idx.teamAff2(m, team, m.StartTime, hm)
	aff, _ = idx.teamAff2(m, 1-team, m.StartTime, hm)
	return aff, familiar
}

// stackAff3 is layer 3 for team's stack: what that stack picked (or, with bans,
// banned) in its earlier matches, capped like a Battle Cup's depth. Only the
// stack's own side counts — the opponents' picks in those matches say nothing
// about this team.
func (idx *asOfIndex) stackAff3(m cache.Match, team int, before int64, hm *features.HeroMap, bans bool) (aff []float64, n int) {
	accs := idx.teamAccounts(m, team == 0)
	if len(accs) < 2 {
		return nil, 0
	}
	want := map[int64]bool{}
	for _, id := range accs {
		want[id] = true
	}
	aff = make([]float64, hm.Len())
	for _, prev := range idx.withPlayers {
		if prev.StartTime >= before || prev.ID == m.ID {
			continue
		}
		overlap, onRadiant := 0, 0
		for _, p := range prev.Players {
			if p.AccountID != nil && want[*p.AccountID] {
				overlap++
				if p.IsRadiant {
					onRadiant++
				}
			}
		}
		if overlap < 2 {
			continue
		}
		side := 1 // the side most of the stack played on in that match
		if 2*onRadiant >= overlap {
			side = 0
		}
		n++
		for _, pb := range prev.PickBans {
			if pb.IsPick == bans || pb.Team != side {
				continue
			}
			if i, ok := hm.Index(pb.HeroID); ok {
				aff[i]++
			}
		}
		if n >= 3 { // cap like Battle Cup depth
			break
		}
	}
	if n == 0 {
		return nil, 0
	}
	return aff, n
}

func roleAffinityVector(prior *roles.Prior, hm *features.HeroMap) []float64 {
	if prior == nil || len(prior.Prob) == 0 {
		return nil
	}
	aff := make([]float64, hm.Len())
	has := false
	for _, id := range hm.IDs() {
		i, ok := hm.Index(id)
		if !ok {
			continue
		}
		row, ok := prior.Prob[id]
		if !ok {
			continue
		}
		max := row[0]
		for _, v := range row[1:] {
			if v > max {
				max = v
			}
		}
		aff[i] = max
		has = true
	}
	if !has {
		return nil
	}
	return aff
}

type ladderScore struct {
	name   string
	n      int
	llSum  float64
	brSum  float64
	eceN   int
	probs  [][]float64 // optional heavy — skip storing all; compute running
	labels []int
	// per-match clusters for bootstrap
	hitTop5 []float64
	trials  []float64
	llMatch []float64 // sum logloss per match
	nMatch  []float64
	rankSum float64
}

func scoreLadderRung(test []cache.Match, fn func(st features.PredictorState, taken []bool, m cache.Match, team int) []float64, hm *features.HeroMap, opts Options, idx *asOfIndex) ladderScore {
	n := hm.Len()
	var s ladderScore
	var allProbs [][]float64
	var allLabels []int

	for _, m := range test {
		if !hasBan(m) {
			continue
		}
		pbs := orderedPickBans(m)
		firstPick := 0
		if len(pbs) > 0 {
			firstPick = pbs[0].Team
		}
		taken := make([]bool, n)
		var rPicks, dPicks, rBans, dBans []int
		var mHits, mTrials, mLL float64

		for ord, pb := range pbs {
			trueIdx, ok := hm.Index(pb.HeroID)
			if ok {
				st := features.PredictorState{
					RadiantPicks: rPicks, DirePicks: dPicks,
					RadiantBans: rBans, DireBans: dBans,
					IsPick: pb.IsPick, Actor: pb.Team, Ord: ord, FirstPick: firstPick,
				}
				dist := normalizeScores(fn(st, taken, m, pb.Team))
				rank := rankOf(dist, trueIdx)
				s.rankSum += float64(rank)
				s.n++
				mTrials++
				if rank <= 5 {
					mHits++
				}
				pTrue := 0.0
				if trueIdx < len(dist) {
					pTrue = dist[trueIdx]
				}
				mLL += -math.Log(clampProb(pTrue))
				allProbs = append(allProbs, dist)
				allLabels = append(allLabels, trueIdx)
			}
			switch {
			case pb.IsPick && pb.Team == 0:
				rPicks = append(rPicks, pb.HeroID)
			case pb.IsPick && pb.Team == 1:
				dPicks = append(dPicks, pb.HeroID)
			case !pb.IsPick && pb.Team == 0:
				rBans = append(rBans, pb.HeroID)
			default:
				dBans = append(dBans, pb.HeroID)
			}
			if ok {
				taken[trueIdx] = true
			}
		}
		if mTrials == 0 {
			continue
		}
		s.hitTop5 = append(s.hitTop5, mHits)
		s.trials = append(s.trials, mTrials)
		s.llMatch = append(s.llMatch, mLL)
		s.nMatch = append(s.nMatch, mTrials)
	}
	s.probs = allProbs
	s.labels = allLabels
	if s.n > 0 {
		s.llSum = MultiClassLogLoss(allProbs, allLabels) * float64(s.n)
		s.brSum = MultiClassBrier(allProbs, allLabels) * float64(s.n)
	}
	_ = idx
	return s
}

func (s ladderScore) toRung(opts Options) LadderRung {
	r := LadderRung{Name: s.name, N: s.n, TopK: opts.TopK}
	if s.n == 0 {
		return r
	}
	r.LogLoss = s.llSum / float64(s.n)
	r.Brier = s.brSum / float64(s.n)
	r.ECE = MultiClassECE(s.probs, s.labels, 10)
	r.MeanRank = s.rankSum / float64(s.n)
	r.Acc = make([]CI, len(opts.TopK))
	for ki, k := range opts.TopK {
		// Recompute hits for this k from stored probs — expensive; for k=5 use hitTop5
		if k == 5 && len(s.hitTop5) == len(s.trials) {
			r.Acc[ki] = RateCI(s.hitTop5, s.trials, opts.Bootstrap)
			continue
		}
		// fallback: approximate from full probs
		h := make([]float64, 0, len(s.trials))
		// Without per-match k-hits for arbitrary k, recompute point estimate only
		var hit, trial float64
		off := 0
		for mi, tr := range s.trials {
			var mh float64
			for a := 0; a < int(tr); a++ {
				if off < len(s.probs) {
					if rankOf(s.probs[off], s.labels[off]) <= k {
						mh++
						hit++
					}
					off++
					trial++
				}
			}
			h = append(h, mh)
			_ = mi
		}
		r.Acc[ki] = RateCI(h, s.trials, opts.Bootstrap)
		_ = hit
	}
	return r
}

func pairedTopK(a, b ladderScore, k int, boot int) CI {
	if k != 5 {
		return CI{}
	}
	ca := make([]Cluster, len(a.hitTop5))
	cb := make([]Cluster, len(b.hitTop5))
	n := len(a.hitTop5)
	if len(b.hitTop5) < n {
		n = len(b.hitTop5)
	}
	ca = ca[:n]
	cb = cb[:n]
	for i := 0; i < n; i++ {
		ca[i] = Cluster{Scores: []float64{a.hitTop5[i]}, Labels: []float64{a.trials[i]}}
		cb[i] = Cluster{Scores: []float64{b.hitTop5[i]}, Labels: []float64{b.trials[i]}}
	}
	return PairedBootstrapDiff(ca, cb, boot, func(h, t []float64) float64 {
		var sh, st float64
		for i := range h {
			sh += h[i]
			st += t[i]
		}
		if st == 0 {
			return 0
		}
		return sh / st
	})
}

func pairedLogLoss(a, b ladderScore, boot int) CI {
	n := len(a.llMatch)
	if len(b.llMatch) < n {
		n = len(b.llMatch)
	}
	ca := make([]Cluster, n)
	cb := make([]Cluster, n)
	for i := 0; i < n; i++ {
		ca[i] = Cluster{Scores: []float64{a.llMatch[i]}, Labels: []float64{a.nMatch[i]}}
		cb[i] = Cluster{Scores: []float64{b.llMatch[i]}, Labels: []float64{b.nMatch[i]}}
	}
	return PairedBootstrapDiff(ca, cb, boot, func(ll, cnt []float64) float64 {
		var s, c float64
		for i := range ll {
			s += ll[i]
			c += cnt[i]
		}
		if c == 0 {
			return 0
		}
		return s / c
	})
}

func splitFamiliar(test []cache.Match, fn func(st features.PredictorState, taken []bool, m cache.Match, team int) []float64, hm *features.HeroMap, opts Options, idx *asOfIndex) (fam, unk ladderScore, ok bool) {
	// Score into two buckets by whether acting team is familiar as-of.
	n := hm.Len()
	var famS, unkS ladderScore
	for _, m := range test {
		if !hasBan(m) {
			continue
		}
		pbs := orderedPickBans(m)
		firstPick := 0
		if len(pbs) > 0 {
			firstPick = pbs[0].Team
		}
		taken := make([]bool, n)
		var rPicks, dPicks, rBans, dBans []int
		for ord, pb := range pbs {
			trueIdx, okIdx := hm.Index(pb.HeroID)
			if okIdx {
				_, familiar := idx.teamAff2(m, pb.Team, m.StartTime, hm)
				st := features.PredictorState{
					RadiantPicks: rPicks, DirePicks: dPicks,
					RadiantBans: rBans, DireBans: dBans,
					IsPick: pb.IsPick, Actor: pb.Team, Ord: ord, FirstPick: firstPick,
				}
				dist := normalizeScores(fn(st, taken, m, pb.Team))
				target := &unkS
				if familiar {
					target = &famS
				}
				pTrue := 0.0
				if trueIdx < len(dist) {
					pTrue = dist[trueIdx]
				}
				target.n++
				target.llSum += -math.Log(clampProb(pTrue))
				target.rankSum += float64(rankOf(dist, trueIdx))
				target.probs = append(target.probs, dist)
				target.labels = append(target.labels, trueIdx)
			}
			switch {
			case pb.IsPick && pb.Team == 0:
				rPicks = append(rPicks, pb.HeroID)
			case pb.IsPick && pb.Team == 1:
				dPicks = append(dPicks, pb.HeroID)
			case !pb.IsPick && pb.Team == 0:
				rBans = append(rBans, pb.HeroID)
			default:
				dBans = append(dBans, pb.HeroID)
			}
			if okIdx {
				taken[trueIdx] = true
			}
		}
	}
	if famS.n < 50 || unkS.n < 50 {
		return ladderScore{}, ladderScore{}, false
	}
	famS.brSum = MultiClassBrier(famS.probs, famS.labels) * float64(famS.n)
	unkS.brSum = MultiClassBrier(unkS.probs, unkS.labels) * float64(unkS.n)
	famS.name, unkS.name = "familiar", "unknown"
	return famS, unkS, true
}

func phaseSplits(test []cache.Match, fn func(st features.PredictorState, taken []bool, m cache.Match, team int) []float64, hm *features.HeroMap, opts Options, idx *asOfIndex) []PhaseSplit {
	n := hm.Len()
	by := make([]ladderScore, features.CMPhases)
	for i := range by {
		by[i].name = fmt.Sprintf("phase%d", i)
	}
	for _, m := range test {
		if !hasBan(m) {
			continue
		}
		pbs := orderedPickBans(m)
		firstPick := 0
		if len(pbs) > 0 {
			firstPick = pbs[0].Team
		}
		taken := make([]bool, n)
		var rPicks, dPicks, rBans, dBans []int
		for ord, pb := range pbs {
			trueIdx, okIdx := hm.Index(pb.HeroID)
			if okIdx {
				phase, _ := features.PhaseOf(ord)
				if phase < 0 || phase >= len(by) {
					phase = 0
				}
				st := features.PredictorState{
					RadiantPicks: rPicks, DirePicks: dPicks,
					RadiantBans: rBans, DireBans: dBans,
					IsPick: pb.IsPick, Actor: pb.Team, Ord: ord, FirstPick: firstPick,
				}
				dist := normalizeScores(fn(st, taken, m, pb.Team))
				pTrue := 0.0
				if trueIdx < len(dist) {
					pTrue = dist[trueIdx]
				}
				by[phase].n++
				by[phase].llSum += -math.Log(clampProb(pTrue))
				by[phase].rankSum += float64(rankOf(dist, trueIdx))
				by[phase].probs = append(by[phase].probs, dist)
				by[phase].labels = append(by[phase].labels, trueIdx)
			}
			switch {
			case pb.IsPick && pb.Team == 0:
				rPicks = append(rPicks, pb.HeroID)
			case pb.IsPick && pb.Team == 1:
				dPicks = append(dPicks, pb.HeroID)
			case !pb.IsPick && pb.Team == 0:
				rBans = append(rBans, pb.HeroID)
			default:
				dBans = append(dBans, pb.HeroID)
			}
			if okIdx {
				taken[trueIdx] = true
			}
		}
	}
	var out []PhaseSplit
	for ph := range by {
		if by[ph].n < 50 {
			continue
		}
		by[ph].brSum = MultiClassBrier(by[ph].probs, by[ph].labels) * float64(by[ph].n)
		out = append(out, PhaseSplit{Phase: ph, Rung: by[ph].toRung(opts)})
	}
	_ = idx
	return out
}

func formatLadder(l LadderReport) string {
	if len(l.Rungs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("personalization ladder (§6.6):\n")
	b.WriteString("  rung                      N   logloss   brier    ECE   top5              Δtop5 vs prev        proven\n")
	for _, r := range l.Rungs {
		top5 := "—"
		if len(r.Acc) > 0 {
			for i, k := range r.TopK {
				if k == 5 {
					top5 = r.Acc[i].String()
					break
				}
			}
		}
		delta := "—"
		proven := ""
		if r.Name != l.Rungs[0].Name {
			delta = r.VsPrevTop5.String()
			if r.Proven {
				proven = "yes"
			} else {
				proven = "no (CI∋0)"
			}
		}
		fmt.Fprintf(&b, "  %-22s %6d  %7.4f  %6.4f  %5.3f  %-16s  %-20s  %s",
			r.Name, r.N, r.LogLoss, r.Brier, r.ECE, top5, delta, proven)
		if r.Note != "" {
			fmt.Fprintf(&b, "  [%s]", r.Note)
		}
		b.WriteByte('\n')
	}
	if l.Familiar != nil {
		b.WriteString("  familiar/unknown (rung 3):\n")
		fmt.Fprintf(&b, "    familiar N=%d logloss=%.4f meanRank=%.2f\n", l.Familiar.Familiar.N, l.Familiar.Familiar.LogLoss, l.Familiar.Familiar.MeanRank)
		fmt.Fprintf(&b, "    unknown  N=%d logloss=%.4f meanRank=%.2f\n", l.Familiar.Unknown.N, l.Familiar.Unknown.LogLoss, l.Familiar.Unknown.MeanRank)
	}
	if len(l.ByPhase) > 0 {
		b.WriteString("  by phase (rung 2 model):\n")
		for _, p := range l.ByPhase {
			fmt.Fprintf(&b, "    phase %d N=%d logloss=%.4f meanRank=%.2f\n", p.Phase, p.Rung.N, p.Rung.LogLoss, p.Rung.MeanRank)
		}
	}
	return b.String()
}
