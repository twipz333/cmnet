package eval

import (
	"fmt"
	"sort"
	"strings"

	"cmnet/internal/bundle"
	"cmnet/internal/cache"
	"cmnet/internal/features"
	"cmnet/internal/model"
	"cmnet/internal/train"
)

// Options tunes the evaluation. Zero values pick sensible defaults.
type Options struct {
	TestFrac     float64 // fraction of the (time-sorted) tail held out for testing; default 0.2
	HalfLifeDays float64 // recency half-life passed to training; 0 => train default, <0 => uniform
	TopK         []int   // predictor top-k cutoffs to report; default {1,3,5}

	// TrainDomain restricts which matches the models are TRAINED on, so we can
	// ablate the domain-shift hypothesis. "all" (default) uses everything, "cm"
	// only matches with bans, "ap" only picks-only matches. The TEST split is
	// never filtered — it is always segmented into CM/AP so results stay
	// comparable across domains.
	TrainDomain string

	// Bootstrap is the number of match-level resamples behind every confidence
	// interval; 0 disables (point estimates only). Without intervals a 0.548 and
	// a 0.570 on a 458-match CM slice look like a ranking when they are one draw.
	Bootstrap int

	// Folds runs the whole evaluation on N rolling time splits instead of one, so
	// a result has to survive several different "recent meta" windows. 0/1 => a
	// single split.
	Folds int

	// SweepHalfLife evaluates each of these recency half-lives (days) and reports
	// the effective sample size each implies.
	SweepHalfLife []float64

	// FMRank trains a factorization machine of this rank alongside the linear
	// head and scores it as another strength variant. 0 skips it.
	FMRank int

	// NoPrefix trains the win-prob head on final compositions only (the pre-M5
	// behaviour), for ablating what prefix training is worth.
	NoPrefix bool

	// L2 / Epochs override the training hyperparameters, so a sweep can be run
	// from the command line instead of by editing constants.
	L2     float64
	Epochs int

	// ProWeight scales pro Captains Mode drafts relative to public games, so the
	// auxiliary-data assumption can be tested rather than assumed. 0 => training
	// default, 1 => no down-weighting.
	ProWeight float64
}

func (o Options) withDefaults() Options {
	if o.TestFrac <= 0 || o.TestFrac >= 1 {
		o.TestFrac = 0.2
	}
	if len(o.TopK) == 0 {
		o.TopK = []int{1, 3, 5}
	}
	if o.TrainDomain == "" {
		o.TrainDomain = "all"
	}
	if o.Folds <= 0 {
		o.Folds = 1
	}
	return o
}

// filterTrainDomain keeps only the matches used for training under the chosen
// domain. The test set is never passed through this.
func filterTrainDomain(matches []cache.Match, domain string) []cache.Match {
	switch domain {
	case "cm":
		out := matches[:0:0]
		for _, m := range matches {
			if hasBan(m) {
				out = append(out, m)
			}
		}
		return out
	case "ap":
		out := matches[:0:0]
		for _, m := range matches {
			if !hasBan(m) {
				out = append(out, m)
			}
		}
		return out
	default: // "all"
		return matches
	}
}

// WinProbMetrics is one scored group of win-prob predictions.
type WinProbMetrics struct {
	Group   string
	N       int
	LogLoss float64
	Brier   float64
	AUC     CI
	ECE     float64
	Base    float64 // positive base rate in the group (constant-predictor reference)
}

// PhaseMetrics scores partial-draft strength at a given number of picks on the board.
type PhaseMetrics struct {
	PicksOnBoard int
	N            int
	LogLoss      float64
	Brier        float64
	AUC          float64
}

// PredictorMetrics is one next-action ranker's accuracy on the test CM drafts.
type PredictorMetrics struct {
	Name     string
	N        int
	TopK     []int
	Acc      []CI    // aligned with TopK
	MeanRank float64 // mean 1-based rank of the true hero (lower is better)
}

// StrengthMetrics scores one draft-strength variant on final compositions.
type StrengthMetrics struct {
	Name   string
	Group  string
	N      int
	AUC    CI
	VsBase CI // paired difference against the plain win-prob variant
}

// HalfLifeRow is one point of the recency-half-life sweep.
type HalfLifeRow struct {
	HalfLifeDays  float64
	ESS           float64 // effective sample size of the training weights
	NominalN      int
	WinProbAUC    float64 // CM group
	PredictorTop5 float64
}

// FoldReport is the evaluation of a single time split.
type FoldReport struct {
	Fold         int
	TrainMatches int
	TestMatches  int
	SplitTime    int64

	WinProb   []WinProbMetrics
	Strength  []StrengthMetrics
	Phase     []PhaseMetrics
	Predictor []PredictorMetrics // model first, then the frequency baselines
}

// Report is the full evaluation result.
type Report struct {
	TrainDomain string
	Bootstrap   int
	Folds       []FoldReport
	HalfLife    []HalfLifeRow
}

// hasBan reports whether a match carries at least one ban (proxy for a real CM
// draft, as opposed to the picks-only public bulk).
func hasBan(m cache.Match) bool {
	for _, pb := range m.PickBans {
		if !pb.IsPick {
			return true
		}
	}
	return false
}

// finalComp extracts each team's picked hero ids from a cached match.
func finalComp(m cache.Match) (radiant, dire []int) {
	for _, pb := range m.PickBans {
		if !pb.IsPick {
			continue
		}
		if pb.Team == 0 {
			radiant = append(radiant, pb.HeroID)
		} else {
			dire = append(dire, pb.HeroID)
		}
	}
	return radiant, dire
}

// Run trains layer-1 models on the older fraction of matches and scores them on
// the held-out newer fraction, optionally over several rolling splits. It trains
// into memory only — nothing is written to disk, so evaluating never disturbs a
// working bundle.
func Run(matches []cache.Match, heroIDs []int, opts Options) (*Report, error) {
	opts = opts.withDefaults()
	if len(matches) < 10 {
		return nil, fmt.Errorf("need at least 10 matches to evaluate, have %d", len(matches))
	}

	sorted := make([]cache.Match, len(matches))
	copy(sorted, matches)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartTime < sorted[j].StartTime })

	rep := &Report{TrainDomain: opts.TrainDomain, Bootstrap: opts.Bootstrap}

	for f := 0; f < opts.Folds; f++ {
		trainSet, testSet, err := foldSplit(sorted, opts, f)
		if err != nil {
			return nil, err
		}
		fr := evalFold(trainSet, testSet, heroIDs, opts)
		fr.Fold = f + 1
		rep.Folds = append(rep.Folds, *fr)
	}

	if len(opts.SweepHalfLife) > 0 {
		trainSet, testSet, err := foldSplit(sorted, opts, opts.Folds-1)
		if err != nil {
			return nil, err
		}
		rep.HalfLife = sweepHalfLife(trainSet, testSet, heroIDs, opts)
	}
	return rep, nil
}

// foldSplit produces the fold-th rolling time split. Fold 0 is the earliest test
// window; the last fold's test block is the newest data. Each fold trains only on
// what precedes its own test block, so no fold ever sees its future.
func foldSplit(sorted []cache.Match, opts Options, fold int) (trainSet, testSet []cache.Match, err error) {
	n := len(sorted)
	testSize := int(float64(n) * opts.TestFrac)
	if testSize < 1 {
		testSize = 1
	}
	offset := (opts.Folds - 1 - fold) * testSize
	end := n - offset
	start := end - testSize
	if start < 1 || end > n {
		return nil, nil, fmt.Errorf("not enough matches for %d folds at test-frac %.2f", opts.Folds, opts.TestFrac)
	}
	trainSet = filterTrainDomain(sorted[:start], opts.TrainDomain)
	testSet = sorted[start:end]
	if len(trainSet) == 0 {
		return nil, nil, fmt.Errorf("no training matches after domain filter %q", opts.TrainDomain)
	}
	return trainSet, testSet, nil
}

// evalFold trains on trainSet and scores everything on testSet.
func evalFold(trainSet, testSet []cache.Match, heroIDs []int, opts Options) *FoldReport {
	b := train.Bundle(trainSet, heroIDs, train.Options{
		HalfLifeDays:  opts.HalfLifeDays,
		FMRank:        opts.FMRank,
		NoPrefix:      opts.NoPrefix,
		L2:            opts.L2,
		Epochs:        opts.Epochs,
		SourceWeights: train.ProWeight(opts.ProWeight),
	})
	hm := features.NewHeroMap(b.HeroIDs)
	wp := &model.WinProb{
		Model: b.WinProb, HeroMap: hm,
		Calib: b.Calibration, PhaseCalib: b.PhaseCalibration,
	}
	pred := &model.Predictor{Pick: b.PickPredictor, Ban: b.BanPredictor, HeroMap: hm}

	fr := &FoldReport{TrainMatches: len(trainSet), TestMatches: len(testSet)}
	if len(testSet) > 0 {
		fr.SplitTime = testSet[0].StartTime
	}
	fr.WinProb = evalWinProb(testSet, wp, opts.Bootstrap)
	fr.Strength = evalStrength(testSet, wp, b, hm, opts.Bootstrap)
	fr.Phase = evalPhase(testSet, wp)
	fr.Predictor = evalPredictors(trainSet, testSet, pred, hm, opts)
	return fr
}

// evalWinProb scores final-composition win probability on the test set, segmented
// into all / CM (has bans) / AP (picks-only).
func evalWinProb(test []cache.Match, wp *model.WinProb, boot int) []WinProbMetrics {
	type bucket struct {
		probs, labels []float64
		clusters      []Cluster
	}
	all, cm, ap := &bucket{}, &bucket{}, &bucket{}
	add := func(b *bucket, p, y float64) {
		b.probs = append(b.probs, p)
		b.labels = append(b.labels, y)
		b.clusters = append(b.clusters, Cluster{Scores: []float64{p}, Labels: []float64{y}})
	}
	for _, m := range test {
		r, d := finalComp(m)
		if len(r) == 0 || len(d) == 0 {
			continue
		}
		p := wp.RadiantWinProb(r, d)
		y := 0.0
		if m.RadiantWin {
			y = 1
		}
		add(all, p, y)
		if hasBan(m) {
			add(cm, p, y)
		} else {
			add(ap, p, y)
		}
	}
	mk := func(name string, b *bucket) WinProbMetrics {
		return WinProbMetrics{
			Group:   name,
			N:       len(b.probs),
			LogLoss: LogLoss(b.probs, b.labels),
			Brier:   Brier(b.probs, b.labels),
			AUC:     BootstrapCI(b.clusters, boot, AUC),
			ECE:     ECE(b.probs, b.labels, 10),
			Base:    BaseRate(b.labels),
		}
	}
	return []WinProbMetrics{mk("all", all), mk("CM", cm), mk("AP", ap)}
}

// strengthVariant is one way of turning a final composition into a ranking score.
type strengthVariant struct {
	name  string
	score func(radiant, dire []int) float64
}

// evalStrength compares the draft-strength variants: the plain win-prob model, the
// matrices on their own, and the fitted stacker that combines them. Each carries a
// bootstrap CI and the PAIRED difference against the plain win-prob — the only
// comparison that means anything at these sample sizes, since both variants are
// scored on the identical matches and the shared noise cancels.
func evalStrength(test []cache.Match, wp *model.WinProb, b *bundle.Bundle, hm *features.HeroMap, boot int) []StrengthMetrics {
	mx := b.Matrices()
	variants := []strengthVariant{{
		name:  "win-prob",
		score: func(r, d []int) float64 { return wp.RadiantWinProb(r, d) },
	}}
	if mx != nil {
		variants = append(variants, strengthVariant{
			name: "matrices",
			score: func(r, d []int) float64 {
				ri, di := denseIdx(hm, r), denseIdx(hm, d)
				return mx.MeanSynergy(ri) - mx.MeanSynergy(di) + mx.MeanCounter(ri, di)
			},
		})
		if b.FM != nil {
			variants = append(variants, strengthVariant{
				name: "fm",
				score: func(r, d []int) float64 {
					return b.FM.Predict(denseIdx(hm, r), denseIdx(hm, d))
				},
			})
		}
		if b.StrengthStacker != nil {
			variants = append(variants, strengthVariant{
				name: "stacker",
				score: func(r, d []int) float64 {
					ri, di := denseIdx(hm, r), denseIdx(hm, d)
					return b.StrengthStacker.Apply(model.StackerFeatures(
						wp.RawWinProb(r, d),
						mx.MeanSynergy(ri)-mx.MeanSynergy(di),
						mx.MeanCounter(ri, di),
						0,
					))
				},
			})
		}
	}

	groups := []string{"all", "CM", "AP"}
	clusters := make([][3][]Cluster, len(variants)) // [variant][group]
	for _, m := range test {
		r, d := finalComp(m)
		if len(r) == 0 || len(d) == 0 {
			continue
		}
		y := 0.0
		if m.RadiantWin {
			y = 1
		}
		gi := 1
		if !hasBan(m) {
			gi = 2
		}
		for vi, v := range variants {
			c := Cluster{Scores: []float64{v.score(r, d)}, Labels: []float64{y}}
			clusters[vi][0] = append(clusters[vi][0], c)
			clusters[vi][gi] = append(clusters[vi][gi], c)
		}
	}

	var out []StrengthMetrics
	for vi, v := range variants {
		for gi, g := range groups {
			m := StrengthMetrics{
				Name:  v.name,
				Group: g,
				N:     len(clusters[vi][gi]),
				AUC:   BootstrapCI(clusters[vi][gi], boot, AUC),
			}
			if vi > 0 {
				m.VsBase = PairedBootstrapDiff(clusters[vi][gi], clusters[0][gi], boot, AUC)
			}
			out = append(out, m)
		}
	}
	return out
}

// denseIdx maps hero ids to their dense matrix indices, dropping unknowns.
func denseIdx(hm *features.HeroMap, ids []int) []int {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if i, ok := hm.Index(id); ok {
			out = append(out, i)
		}
	}
	return out
}

// evalPhase measures how well the partial-draft strength (the exact call the live
// advisor makes) predicts the final outcome, grouped by how many heroes are on the
// board. CM test drafts only (they carry real pick order).
func evalPhase(test []cache.Match, wp *model.WinProb) []PhaseMetrics {
	type acc struct{ probs, labels []float64 }
	byBoard := map[int]*acc{}
	for _, m := range test {
		if !hasBan(m) {
			continue
		}
		y := 0.0
		if m.RadiantWin {
			y = 1
		}
		var r, d []int
		for _, pb := range orderedPickBans(m) {
			if !pb.IsPick {
				continue
			}
			if pb.Team == 0 {
				r = append(r, pb.HeroID)
			} else {
				d = append(d, pb.HeroID)
			}
			board := len(r) + len(d)
			p := wp.RadiantWinProb(r, d)
			a := byBoard[board]
			if a == nil {
				a = &acc{}
				byBoard[board] = a
			}
			a.probs = append(a.probs, p)
			a.labels = append(a.labels, y)
		}
	}
	boards := make([]int, 0, len(byBoard))
	for k := range byBoard {
		boards = append(boards, k)
	}
	sort.Ints(boards)
	out := make([]PhaseMetrics, 0, len(boards))
	for _, k := range boards {
		a := byBoard[k]
		out = append(out, PhaseMetrics{
			PicksOnBoard: k,
			N:            len(a.probs),
			LogLoss:      LogLoss(a.probs, a.labels),
			Brier:        Brier(a.probs, a.labels),
			AUC:          AUC(a.probs, a.labels),
		})
	}
	return out
}

// nextActionRanker scores every hero for one upcoming draft action.
type nextActionRanker struct {
	name  string
	score func(st features.PredictorState, taken []bool) []float64
}

// modelRanker wraps the trained heads as a ranker.
func modelRanker(pred *model.Predictor) nextActionRanker {
	return nextActionRanker{
		name: "model",
		score: func(st features.PredictorState, taken []bool) []float64 {
			return pred.DistributionMaskedIdx(st, taken)
		},
	}
}

// evalPredictors scores the model and the frequency baselines on the same actions,
// under the same availability mask the live UI applies.
func evalPredictors(trainSet, test []cache.Match, pred *model.Predictor, hm *features.HeroMap, opts Options) []PredictorMetrics {
	rankers := []nextActionRanker{modelRanker(pred)}
	for _, bl := range BaselineKinds(hm) {
		bl.Fit(trainSet, hm)
		b := bl
		rankers = append(rankers, nextActionRanker{
			name: b.Name,
			score: func(st features.PredictorState, taken []bool) []float64 {
				return b.Score(st.Ord, st.IsPick, st.Actor, taken)
			},
		})
	}

	out := make([]PredictorMetrics, len(rankers))
	for i, r := range rankers {
		out[i] = scoreRanker(test, r, hm, opts)
	}
	return out
}

// scoreRanker replays every CM test draft through one ranker.
func scoreRanker(test []cache.Match, r nextActionRanker, hm *features.HeroMap, opts Options) PredictorMetrics {
	if len(opts.TopK) == 0 {
		opts.TopK = []int{1, 3, 5}
	}
	n := hm.Len()
	hits := make([][]float64, len(opts.TopK)) // per-match hit counts, for the bootstrap
	trials := make([]float64, 0, len(test))
	var total int
	var rankSum float64

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
		matchHits := make([]float64, len(opts.TopK))
		var matchTrials float64

		for ord, pb := range pbs {
			trueIdx, ok := hm.Index(pb.HeroID)
			if ok {
				st := features.PredictorState{
					RadiantPicks: rPicks, DirePicks: dPicks,
					RadiantBans: rBans, DireBans: dBans,
					IsPick:    pb.IsPick,
					Actor:     pb.Team,
					Ord:       ord,
					FirstPick: firstPick,
				}
				rank := rankOf(r.score(st, taken), trueIdx)
				rankSum += float64(rank)
				total++
				matchTrials++
				for ki, k := range opts.TopK {
					if rank <= k {
						matchHits[ki]++
					}
				}
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
		if matchTrials == 0 {
			continue
		}
		trials = append(trials, matchTrials)
		for ki := range opts.TopK {
			hits[ki] = append(hits[ki], matchHits[ki])
		}
	}

	acc := make([]CI, len(opts.TopK))
	for ki := range opts.TopK {
		acc[ki] = RateCI(hits[ki], trials, opts.Bootstrap)
	}
	mean := 0.0
	if total > 0 {
		mean = rankSum / float64(total)
	}
	return PredictorMetrics{Name: r.name, N: total, TopK: opts.TopK, Acc: acc, MeanRank: mean}
}

// rankOf returns the 1-based rank of index target within scores. Ties are broken
// pessimistically-neutrally: an equal-scoring entry does not out-rank the target.
func rankOf(scores []float64, target int) int {
	if target >= len(scores) {
		return len(scores)
	}
	tv := scores[target]
	rank := 1
	for i, v := range scores {
		if i == target {
			continue
		}
		if v > tv {
			rank++
		}
	}
	return rank
}

// sweepHalfLife retrains at each half-life and reports what the recency weighting
// actually costs: the nominal match count stays put while the EFFECTIVE sample
// size (Kish ESS) collapses, and past some point that costs more accuracy than the
// staleness it was meant to fix.
func sweepHalfLife(trainSet, testSet []cache.Match, heroIDs []int, opts Options) []HalfLifeRow {
	hm := features.NewHeroMap(heroIDs)
	var out []HalfLifeRow
	for _, hl := range opts.SweepHalfLife {
		b := train.Bundle(trainSet, heroIDs, train.Options{HalfLifeDays: hl})
		wp := &model.WinProb{Model: b.WinProb, HeroMap: hm, Calib: b.Calibration, PhaseCalib: b.PhaseCalibration}
		pred := &model.Predictor{Pick: b.PickPredictor, Ban: b.BanPredictor, HeroMap: hm}

		samples := train.BuildSamples(trainSet, hm, hl, train.SampleOptions{})
		weights := model.SampleWeights(samples.WinProb)

		var cmAUC float64
		for _, g := range evalWinProb(testSet, wp, 0) {
			if g.Group == "CM" {
				cmAUC = g.AUC.Point
			}
		}
		pm := scoreRanker(testSet, modelRanker(pred), hm, Options{TopK: []int{5}})

		row := HalfLifeRow{
			HalfLifeDays: hl,
			ESS:          model.EffectiveSampleSize(weights),
			NominalN:     len(weights),
			WinProbAUC:   cmAUC,
		}
		if len(pm.Acc) > 0 {
			row.PredictorTop5 = pm.Acc[0].Point
		}
		out = append(out, row)
	}
	return out
}

// String renders the report as a human-readable block.
func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "eval: train-domain=%s | %d fold(s)", r.TrainDomain, len(r.Folds))
	if r.Bootstrap > 0 {
		fmt.Fprintf(&b, " | %d match-level bootstrap resamples (95%% CI)", r.Bootstrap)
	} else {
		b.WriteString(" | no bootstrap: point estimates only, differences under ~0.05 AUC are unreadable")
	}
	b.WriteString("\n")

	for _, f := range r.Folds {
		fmt.Fprintf(&b, "\n=== fold %d: %d train / %d test matches (time-split)\n", f.Fold, f.TrainMatches, f.TestMatches)

		b.WriteString("\nWIN-PROB (final comp, held-out):\n")
		fmt.Fprintf(&b, "  %-4s %7s %8s %8s %-22s %7s %7s\n", "grp", "n", "logloss", "brier", "auc", "ece", "base")
		for _, m := range f.WinProb {
			fmt.Fprintf(&b, "  %-4s %7d %8.4f %8.4f %-22s %7.3f %7.3f\n",
				m.Group, m.N, m.LogLoss, m.Brier, m.AUC.String(), m.ECE, m.Base)
		}

		if len(f.Strength) > 0 {
			b.WriteString("\nDRAFT STRENGTH variants → final outcome (AUC, paired Δ vs win-prob):\n")
			fmt.Fprintf(&b, "  %-9s %-4s %7s %-22s %-24s\n", "variant", "grp", "n", "auc", "Δ vs win-prob")
			for _, m := range f.Strength {
				delta := ""
				if m.Name != "win-prob" {
					delta = m.VsBase.String()
					if m.VsBase.Significant() {
						delta += " *"
					}
				}
				fmt.Fprintf(&b, "  %-9s %-4s %7d %-22s %-24s\n", m.Name, m.Group, m.N, m.AUC.String(), delta)
			}
			b.WriteString("  (* = 95% interval excludes zero)\n")
		}

		b.WriteString("\nPARTIAL-DRAFT STRENGTH → final outcome (CM test), by picks on board:\n")
		fmt.Fprintf(&b, "  %6s %7s %8s %8s %7s\n", "board", "n", "logloss", "brier", "auc")
		for _, p := range f.Phase {
			fmt.Fprintf(&b, "  %6d %7d %8.4f %8.4f %7.3f\n", p.PicksOnBoard, p.N, p.LogLoss, p.Brier, p.AUC)
		}

		b.WriteString("\nNEXT-ACTION RANKERS (CM test, all actions, unavailable heroes masked):\n")
		if len(f.Predictor) > 0 {
			fmt.Fprintf(&b, "  %-20s %7s %9s", "ranker", "n", "meanRank")
			for _, k := range f.Predictor[0].TopK {
				fmt.Fprintf(&b, " %-22s", fmt.Sprintf("top-%d", k))
			}
			b.WriteString("\n")
		}
		for _, m := range f.Predictor {
			fmt.Fprintf(&b, "  %-20s %7d %9.1f", m.Name, m.N, m.MeanRank)
			for _, a := range m.Acc {
				fmt.Fprintf(&b, " %-22s", a.String())
			}
			b.WriteString("\n")
		}
	}

	if len(r.HalfLife) > 0 {
		b.WriteString("\nRECENCY HALF-LIFE SWEEP (what the weighting costs in effective data):\n")
		fmt.Fprintf(&b, "  %9s %9s %9s %7s %9s %9s\n", "half-life", "nominal", "ESS", "ESS%", "CM auc", "top-5")
		for _, h := range r.HalfLife {
			label := fmt.Sprintf("%.0fd", h.HalfLifeDays)
			if h.HalfLifeDays <= 0 {
				label = "uniform"
			}
			pct := 0.0
			if h.NominalN > 0 {
				pct = 100 * h.ESS / float64(h.NominalN)
			}
			fmt.Fprintf(&b, "  %9s %9d %9.0f %6.1f%% %9.3f %9.3f\n",
				label, h.NominalN, h.ESS, pct, h.WinProbAUC, h.PredictorTop5)
		}
	}
	return b.String()
}
