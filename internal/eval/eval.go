package eval

import (
	"fmt"
	"sort"
	"strings"

	"cmnet/internal/bundle"
	"cmnet/internal/cache"
	"cmnet/internal/features"
	"cmnet/internal/matrices"
	"cmnet/internal/model"
	"cmnet/internal/recommender"
	"cmnet/internal/roles"
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

	// OnlyFold evaluates just this fold (1-based) out of Folds, leaving the split
	// geometry identical to a full run.
	//
	// Folds are independent and training is single-threaded, so a three-fold run
	// spends an hour using one core out of however many the machine has. Running
	// one process per fold turns that hour into twenty minutes. 0 evaluates all
	// folds, as before.
	OnlyFold int

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

	// StratifySplit sizes the test window by the TARGET domain instead of by the
	// whole corpus: "cm" (default) puts TestFrac of the Captains Mode matches in
	// the test block, "public-cm" uses only public CM, "none" restores the old
	// behaviour of slicing the corpus tail blindly.
	//
	// Without this the tool measures itself worse the more data it collects. The
	// bulk pool grows several times faster than public CM, so a tail of "the newest
	// 20% of everything" holds a shrinking number of target matches — between two
	// real runs the corpus grew by 5 816 matches while the CM test set fell from
	// 723 to 478, and the confidence interval on the main metric got *wider*. The
	// split has to be anchored to the population the metric is about.
	StratifySplit string

	// Ladder enables the §6.6 personalization ladder (paired Δ per rung).
	Ladder bool

	// ExcludeHoldout removes sealed match ids from the corpus before any tuning
	// fold is cut (§6.8). Use this for every iterative / hyperparameter eval so the
	// final set stays untouched.
	ExcludeHoldout *Holdout

	// FinalHoldout scores only the sealed set in a single fold (train = rest of
	// corpus). Mutually exclusive with ExcludeHoldout; when set, Folds is ignored.
	FinalHoldout *Holdout
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
	if o.StratifySplit == "" {
		o.StratifySplit = "cm"
	}
	return o
}

// orUnknown labels a fold report built before split modes were recorded.
func orUnknown(mode string) string {
	if mode == "" {
		return "split mode unrecorded"
	}
	return mode
}

func shortSHA(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

// targetOf is which domain the reported target count refers to. With
// stratification off we still report the CM count — that is the number a reader
// needs to judge the interval, whether or not it was used to size the split.
func targetOf(stratify string) string {
	if stratify == "public-cm" {
		return "public-cm"
	}
	return "cm"
}

// isTargetDomain reports whether a match counts toward the stratified test size.
func isTargetDomain(m cache.Match, domain string) bool {
	switch domain {
	case "public-cm":
		return hasBan(m) && m.Source == cache.SourcePublicCM
	case "cm":
		return hasBan(m)
	default:
		return false
	}
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

// minSegment is the fewest matches a reported sub-population may have. The floor
// is 200 because 50 demonstrably is not enough: a 57-match CM-pro block scored
// 0.629 with a 95% interval of [0.472-0.788] — spanning "worse than a coin flip"
// to "excellent" — and still read like a result.
const minSegment = 200

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
	// TestTargets is how many target-domain matches landed in this test block, and
	// SplitMode how the block was sized. Both are printed because the headline
	// number is only as trustworthy as the size of the slice it was measured on,
	// and that size used to be an accident of how fast the bulk pool was growing.
	TestTargets int
	SplitMode   string

	WinProb   []WinProbMetrics
	Strength  []StrengthMetrics
	Phase     []PhaseMetrics
	Predictor []PredictorMetrics // model first, then the frequency baselines

	// PickQuality scores the ordering of candidates inside real draft states —
	// the question the advisor actually answers. See PickQualityMetrics.
	PickQuality []PickQualityMetrics

	// RankAgreement says whether the richer scoring variants reorder candidates at
	// all relative to plain one-ply win-prob. See RankAgreement.
	RankAgreement []RankAgreement

	// HalfLife is the recency sweep for THIS fold. The sweep lives per fold
	// because a single split cannot tell a real recency effect from the ordinary
	// variation between meta windows: the spread of top-5 across folds is an
	// order of magnitude wider than the spread across half-lives, so a sweep
	// measured on one window will happily "prove" whichever default it was run on.
	HalfLife []HalfLifeRow

	// Ladder is the §6.6 personalization ladder; empty when Options.Ladder is off.
	Ladder LadderReport
}

// Report is the full evaluation result.
type Report struct {
	TrainDomain string
	Bootstrap   int
	Folds       []FoldReport
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
	// Caught here rather than yielding an empty report: an out-of-range fold means
	// one process of a parallel run silently produced nothing, and the missing
	// window would only be noticed when someone tried to compare three logs.
	if opts.OnlyFold > opts.Folds {
		return nil, fmt.Errorf("only-fold %d is out of range for %d fold(s)", opts.OnlyFold, opts.Folds)
	}

	if opts.FinalHoldout != nil && opts.ExcludeHoldout != nil {
		return nil, fmt.Errorf("eval: pass either --final-holdout or --exclude-holdout, not both")
	}

	sorted := make([]cache.Match, len(matches))
	copy(sorted, matches)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartTime < sorted[j].StartTime })

	if opts.ExcludeHoldout != nil {
		if err := opts.ExcludeHoldout.Verify(); err != nil {
			return nil, err
		}
		before := len(sorted)
		sorted = Exclude(sorted, opts.ExcludeHoldout)
		if len(sorted) == before {
			return nil, fmt.Errorf("exclude-holdout: sealed %d ids but none were in the loaded corpus", opts.ExcludeHoldout.N)
		}
		if len(sorted) < 10 {
			return nil, fmt.Errorf("need at least 10 matches after excluding holdout, have %d", len(sorted))
		}
	}

	rep := &Report{TrainDomain: opts.TrainDomain, Bootstrap: opts.Bootstrap}

	if opts.FinalHoldout != nil {
		trainRaw, testSet, err := FinalSplit(sorted, opts.FinalHoldout)
		if err != nil {
			return nil, err
		}
		trainSet := filterTrainDomain(trainRaw, opts.TrainDomain)
		if len(trainSet) == 0 {
			return nil, fmt.Errorf("no training matches after domain filter %q", opts.TrainDomain)
		}
		fr := evalFold(trainSet, testSet, heroIDs, opts)
		fr.Fold = 1
		fr.SplitMode = fmt.Sprintf("final-holdout:%s n=%d sha=%s", opts.FinalHoldout.Stratify, opts.FinalHoldout.N, shortSHA(opts.FinalHoldout.ContentSHA256))
		for _, m := range testSet {
			if isTargetDomain(m, targetOf(opts.StratifySplit)) {
				fr.TestTargets++
			}
		}
		if len(opts.SweepHalfLife) > 0 {
			fr.HalfLife = sweepHalfLife(trainSet, testSet, heroIDs, opts)
		}
		rep.Folds = append(rep.Folds, *fr)
		return rep, nil
	}

	for f := 0; f < opts.Folds; f++ {
		if opts.OnlyFold > 0 && f+1 != opts.OnlyFold {
			continue
		}
		trainSet, testSet, mode, err := foldSplit(sorted, opts, f)
		if err != nil {
			return nil, err
		}
		if opts.ExcludeHoldout != nil {
			mode = mode + "; holdout-excluded"
		}
		fr := evalFold(trainSet, testSet, heroIDs, opts)
		fr.Fold = f + 1
		fr.SplitMode = mode
		for _, m := range testSet {
			if isTargetDomain(m, targetOf(opts.StratifySplit)) {
				fr.TestTargets++
			}
		}
		if len(opts.SweepHalfLife) > 0 {
			fr.HalfLife = sweepHalfLife(trainSet, testSet, heroIDs, opts)
		}
		rep.Folds = append(rep.Folds, *fr)
	}
	return rep, nil
}

// foldSplit produces the fold-th rolling time split. Fold 0 is the earliest test
// window; the last fold's test block is the newest data. Each fold trains only on
// what precedes its own test block, so no fold ever sees its future.
//
// The block is still a contiguous stretch of time — stratification changes how its
// boundaries are chosen, never that they are boundaries in time. Nothing is
// sampled across the split, so a fold cannot see its own future either way.
func foldSplit(sorted []cache.Match, opts Options, fold int) (trainSet, testSet []cache.Match, mode string, err error) {
	n := len(sorted)
	start, end, ok := stratifiedBounds(sorted, opts, fold)
	mode = "stratified:" + opts.StratifySplit
	if !ok {
		mode = "corpus-tail"
		testSize := int(float64(n) * opts.TestFrac)
		if testSize < 1 {
			testSize = 1
		}
		offset := (opts.Folds - 1 - fold) * testSize
		end = n - offset
		start = end - testSize
	}
	if start < 1 || end > n || start >= end {
		return nil, nil, mode, fmt.Errorf("not enough matches for %d folds at test-frac %.2f", opts.Folds, opts.TestFrac)
	}
	trainSet = filterTrainDomain(sorted[:start], opts.TrainDomain)
	testSet = sorted[start:end]
	if len(trainSet) == 0 {
		return nil, nil, mode, fmt.Errorf("no training matches after domain filter %q", opts.TrainDomain)
	}
	return trainSet, testSet, mode, nil
}

// stratifiedBounds sizes the test block so it holds TestFrac of the TARGET-domain
// matches, and returns the surrounding corpus indices. ok=false falls back to the
// unstratified tail — when stratification is off, or when there are too few target
// matches to cut into folds, in which case a fixed target count would just produce
// an empty training set instead of an honest measurement.
func stratifiedBounds(sorted []cache.Match, opts Options, fold int) (start, end int, ok bool) {
	if opts.StratifySplit == "none" {
		return 0, 0, false
	}
	var idx []int
	for i, m := range sorted {
		if isTargetDomain(m, opts.StratifySplit) {
			idx = append(idx, i)
		}
	}
	tn := len(idx)
	perFold := int(float64(tn) * opts.TestFrac)
	if perFold < 1 || perFold*opts.Folds >= tn {
		return 0, 0, false
	}
	endT := tn - (opts.Folds-1-fold)*perFold
	startT := endT - perFold
	if startT < 1 || endT > tn {
		return 0, 0, false
	}
	start = idx[startT]
	end = len(sorted)
	if endT < tn {
		end = idx[endT] // stop just before the next target match
	}
	return start, end, true
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
	if opts.Ladder {
		var prior *roles.Prior
		if len(b.RolePrior) > 0 {
			prior = &roles.Prior{Prob: b.RolePrior}
		}
		corpus := append(append([]cache.Match(nil), trainSet...), testSet...)
		fr.Ladder = evalLadder(corpus, trainSet, testSet, pred, hm, prior, opts)
	}
	// The advisor ranker needs the recommender wired the way serve wires it, so
	// the offline ordering is the one the user is shown.
	rec := recommender.New(wp, pred).
		WithMatrices(b.SynergyMatrix, b.CounterMatrix).
		WithStacker(b.StrengthStacker).
		WithPositions(b.HeroPositions).
		WithCMOrder(b.CMOrder)
	if len(b.RolePrior) > 0 {
		rec.WithRolePrior(&roles.Prior{Prob: b.RolePrior})
	}
	fr.PickQuality, fr.RankAgreement = evalPickQualityGroups(trainSet, testSet, wp, b.Matrices(), rec, hm, opts.Bootstrap)
	return fr
}

// evalPickQualityGroups scores candidate ordering on the CM drafts, once over all
// of them and once over public CM alone — the population the advisor runs in.
// The popularity baseline is fitted on the training half only.
func evalPickQualityGroups(trainSet, testSet []cache.Match, wp *model.WinProb, mx *matrices.Matrices, rec *recommender.Recommender, hm *features.HeroMap, boot int) ([]PickQualityMetrics, []RankAgreement) {
	pop := &Baseline{Name: popularityRankerName, counts: map[baselineKey][]float64{},
		keyOf: func(ord int, isPick bool, actor int) baselineKey { return baselineKey{a: ord} }}
	pop.Fit(trainSet, hm)
	rankers := []pickRanker{winProbRanker(wp, hm), baselinePickRanker(pop)}
	if mx != nil {
		rankers = append(rankers, matrixRanker(mx, hm))
	}
	if rec != nil {
		rankers = append(rankers, advisorRanker(rec, hm))
	}

	var out []PickQualityMetrics
	var agree []RankAgreement
	cmMetrics, cmAgree := evalPickQuality(cmSubset(testSet, ""), hm, rankers, "CM", boot)
	out = append(out, cmMetrics...)
	agree = append(agree, cmAgree...)
	// Same floor as the win-prob segments: below it the interval is wider than
	// anything it could tell you.
	if pub := cmSubset(testSet, cache.SourcePublicCM); len(pub) >= minSegment {
		pubMetrics, pubAgree := evalPickQuality(pub, hm, rankers, "CM-pub", boot)
		out = append(out, pubMetrics...)
		agree = append(agree, pubAgree...)
	}
	return out, agree
}

// evalWinProb scores final-composition win probability on the test set, segmented
// into all / CM (has bans) / AP (picks-only).
func evalWinProb(test []cache.Match, wp *model.WinProb, boot int) []WinProbMetrics {
	type bucket struct {
		probs, labels []float64
		clusters      []Cluster
	}
	all, cm, ap := &bucket{}, &bucket{}, &bucket{}
	// Captains Mode is TWO populations, and pooling them hides which one the model
	// is bad at. Pro drafts are coordinated teams where execution decides the game,
	// so draft-only prediction has a genuinely low ceiling there; public CM at pub
	// rank is the population this advisor is actually used in. A single "CM" number
	// is their average, and the average is not what anyone plays.
	cmPro, cmPub := &bucket{}, &bucket{}
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
			switch m.Source {
			case cache.SourcePro:
				add(cmPro, p, y)
			case cache.SourcePublicCM:
				add(cmPub, p, y)
			}
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
	out := []WinProbMetrics{mk("all", all), mk("CM", cm), mk("AP", ap)}
	if len(cmPro.probs) >= minSegment {
		out = append(out, mk("CM-pro", cmPro))
	}
	if len(cmPub.probs) >= minSegment {
		out = append(out, mk("CM-pub", cmPub))
	}
	return out
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
		// Every training option except the half-life must match the main fold, or
		// the sweep compares half-lives across differently-trained models and the
		// comparison is meaningless the moment anyone passes --pro-weight or --epochs.
		trainOpts := train.Options{
			HalfLifeDays:  hl,
			FMRank:        opts.FMRank,
			NoPrefix:      opts.NoPrefix,
			L2:            opts.L2,
			Epochs:        opts.Epochs,
			SourceWeights: train.ProWeight(opts.ProWeight),
		}
		b := train.Bundle(trainSet, heroIDs, trainOpts)
		wp := &model.WinProb{Model: b.WinProb, HeroMap: hm, Calib: b.Calibration, PhaseCalib: b.PhaseCalibration}
		pred := &model.Predictor{Pick: b.PickPredictor, Ban: b.BanPredictor, HeroMap: hm}

		// ESS has to be computed over the rows training actually sees: with the
		// default prefix expansion that is every partial board, not just the final
		// compositions, and provenance weighting shifts it too.
		samples := train.BuildSamples(trainSet, hm, hl, train.SampleOptions{
			Prefix:        !opts.NoPrefix,
			SourceWeights: trainOpts.SourceWeights,
		})
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
		fmt.Fprintf(&b, "\n=== fold %d: %d train / %d test matches (time-split, %s; %d target-domain matches in test)\n",
			f.Fold, f.TrainMatches, f.TestMatches, orUnknown(f.SplitMode), f.TestTargets)
		if f.SplitMode == "corpus-tail" {
			b.WriteString("  note: test block sized by the whole corpus — the target-domain count above is\n" +
				"        whatever the bulk pool left behind, not a quantity anyone chose\n")
		}
		if strings.HasPrefix(f.SplitMode, "final-holdout:") {
			b.WriteString("  FINAL HOLDOUT: sealed ids only — do not retune on this number; one-shot report.\n")
		}

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

		if len(f.PickQuality) > 0 {
			b.WriteString("\nPICK QUALITY (does the ordering of candidates track winning?):\n")
			fmt.Fprintf(&b, "  %-14s %-7s %7s %8s %-22s %-24s %9s\n",
				"ranker", "grp", "matches", "picks", "auc", "Δ vs popularity", "meanRank")
			for _, p := range f.PickQuality {
				delta := ""
				if p.Name != popularityRankerName {
					delta = p.VsPopularity.String()
					if p.VsPopularity.Significant() {
						delta += " *"
					}
				}
				fmt.Fprintf(&b, "  %-14s %-7s %7d %8d %-22s %-24s %9.3f\n",
					p.Name, p.Group, p.N, p.Actions, p.AUC.String(), delta, p.MeanNormRank)
			}
			b.WriteString("  (auc 0.5 = the ordering says nothing about who wins; the paired delta is\n")
			b.WriteString("   the honest number — winning teams pick popular heroes too. * = CI excludes zero.\n")
			b.WriteString("   Observational: it shows agreement with what winners did, not causation.)\n")
		}

		b.WriteString(formatAgreements(f.RankAgreement))
		b.WriteString(formatLadder(f.Ladder))

		if len(f.HalfLife) > 0 {
			b.WriteString("\nRECENCY HALF-LIFE SWEEP (what the weighting costs in effective data):\n")
			fmt.Fprintf(&b, "  %9s %9s %9s %7s %9s %9s\n", "half-life", "nominal", "ESS", "ESS%", "CM auc", "top-5")
			for _, h := range f.HalfLife {
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
	}
	return b.String()
}
