// Package train assembles training samples from cached drafts and produces a
// versioned bundle. It is shared by the `advisor train` command and the
// determinism tests so both exercise the exact same deterministic pipeline.
package train

import (
	"fmt"
	"math"
	"sort"
	"time"

	"cmnet/internal/bundle"
	"cmnet/internal/cache"
	"cmnet/internal/features"
	"cmnet/internal/matrices"
	"cmnet/internal/model"
	"cmnet/internal/objectives"
)

// Options controls bundle metadata and training hyperparameters.
type Options struct {
	WindowDays int
	PatchTag   string // stamped into the bundle
	MMRTier    string // stamped into the bundle
	CreatedAt  string // RFC3339; if empty, time.Now().UTC() is used at Bundle build

	// Data quality (M5).
	HalfLifeDays float64 // recency half-life in days; <0 disables, 0 => default
	FilterPatch  string  // if set, only train on matches with this patch tag
	MinAvgMMR    int     // if >0, only train on matches with avg_mmr >= this

	Epochs int
	LR     float64
	L2     float64

	// ValFrac is the fraction of the (time-sorted) tail of the training matches
	// held out as a validation block for early stopping and calibration. 0 uses
	// DefaultValFrac; negative disables validation entirely (fixed-epoch training
	// and calibration fitted on the training rows, i.e. the pre-M5 behaviour).
	ValFrac float64
	// Patience is the early-stopping patience in evaluations; 0 uses the model
	// default, negative disables early stopping.
	Patience int

	// NoPrefix trains the win-prob head on FINAL compositions only, the pre-M5
	// behaviour. Prefix training (the default) also emits partial-board rows so
	// the model is in-distribution for the mid-draft queries the live advisor
	// actually makes.
	NoPrefix bool
	// MatrixPrior overrides the synergy/counter pseudo-count (0 => default).
	MatrixPrior float64
	// MatrixNoShrink disables empirical-Bayes shrinkage of the pair matrices,
	// restoring the pre-fix behaviour where every cell keeps its raw lift. Use
	// only to reproduce old bundles: without shrinkage the cells that beam search
	// likes best are the ones fitted on the fewest games.
	MatrixNoShrink bool

	// FMRank trains a factorization machine of this latent rank alongside the
	// linear win-prob head. 0 skips it (the linear head alone), which keeps
	// training fast; -1 uses the model default rank.
	FMRank int

	// SourceWeights scales each match's training weight by where it came from
	// (cache.SourcePro / SourcePublicCM / SourceBulk). Missing sources weigh 1.
	//
	// This is how pro drafts get used as AUXILIARY data rather than as equals:
	// they are real Captains Mode, but a different population from the amateur
	// games the advisor runs in — coordinated teams, fixed roles, bans aimed at
	// named players. Down-weighting keeps their signal without letting them
	// define the meta the model learns.
	SourceWeights map[string]float64

	// MinRankTier drops matches below this average rank tier (40 Archon … 80
	// Immortal). Matches with an unknown tier are kept: the old cache has no
	// provenance, and silently discarding all of it would be worse than a slightly
	// mixed pool.
	MinRankTier int

	// Progress, if set, receives human-readable progress lines during training
	// (predictor GD is the slow part). Not serialized.
	Progress func(string) `json:"-"`
}

// DefaultHalfLifeDays is the recency half-life: a match this many days older than
// the newest gets half the weight.
//
// Measured, not guessed (`advisor eval --sweep-half-life 7,14,30,60,-1`). On a
// ~79-day cache the next-action predictor improves monotonically as the window
// SHRINKS — top-5 goes 0.194 (uniform) → 0.208 (30d) → 0.246 (14d) → 0.268 (7d) —
// even though a 7-day half-life throws away 79% of the effective sample size. What
// the enemy picks is a fact about this week's meta, and stale drafts describe a
// game that no longer exists.
//
// Win-prob is indifferent to the same sweep (0.485-0.491 across all settings, and
// 0.599 vs 0.603 overall at 7 vs 14 days, well inside the confidence interval):
// individual hero strength drifts slowly even as pick popularity churns. So the
// short half-life costs the outcome model nothing and buys the predictor a lot.
const DefaultHalfLifeDays = 7.0

// DefaultValFrac is the share of the training matches (newest tail) reserved for
// early stopping and calibration.
const DefaultValFrac = 0.15

// splitByTime cuts the tail fraction of matches (assumed ascending by start_time)
// into a validation block. frac <= 0 disables the split. The cut is deterministic
// and match-level.
func splitByTime(matches []cache.Match, frac float64) (fit, val []cache.Match) {
	if frac <= 0 || len(matches) < 20 {
		return matches, nil
	}
	if frac >= 1 {
		frac = DefaultValFrac
	}
	sorted := make([]cache.Match, len(matches))
	copy(sorted, matches)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartTime < sorted[j].StartTime })
	cut := int(float64(len(sorted)) * (1 - frac))
	if cut < 1 {
		cut = 1
	}
	if cut >= len(sorted) {
		cut = len(sorted) - 1
	}
	return sorted[:cut], sorted[cut:]
}

// curveLogger reports the loss curve sparsely: dense per-evaluation output would
// drown the progress log, but the shape (and whether validation loss turned back
// up) is exactly what tells under- from over-fitting.
func curveLogger(progress func(string), name string) func(model.CurvePoint) {
	var last int
	return func(p model.CurvePoint) {
		if p.Epoch-last < 50 && p.Epoch != 10 {
			return
		}
		last = p.Epoch
		if math.IsNaN(p.ValLoss) {
			progress(fmt.Sprintf("  %s epoch %d: train %.4f", name, p.Epoch, p.TrainLoss))
			return
		}
		progress(fmt.Sprintf("  %s epoch %d: train %.4f val %.4f", name, p.Epoch, p.TrainLoss, p.ValLoss))
	}
}

// WithDefaults fills unset hyperparameters with the model defaults.
func (o Options) withDefaults() Options {
	if o.Epochs == 0 {
		o.Epochs = model.DefaultEpochs
	}
	if o.LR == 0 {
		o.LR = model.DefaultLR
	}
	if o.L2 == 0 {
		o.L2 = model.DefaultL2
	}
	if o.HalfLifeDays == 0 {
		o.HalfLifeDays = DefaultHalfLifeDays
	}
	if o.ValFrac == 0 {
		o.ValFrac = DefaultValFrac
	} else if o.ValFrac < 0 {
		o.ValFrac = 0
	}
	if o.Patience == 0 {
		o.Patience = model.DefaultPatience
	} else if o.Patience < 0 {
		o.Patience = 0
	}
	return o
}

// FilterMatches keeps only matches passing the patch / MMR / rank filters.
//
// Note what is NOT dropped: a match whose avg MMR or rank tier is unknown passes
// the corresponding filter. Most of the existing cache predates provenance
// recording, and a filter that silently discarded all of it would quietly shrink
// the training set to nothing while looking like it worked.
func FilterMatches(matches []cache.Match, patch string, minMMR, minRankTier int) []cache.Match {
	if patch == "" && minMMR <= 0 && minRankTier <= 0 {
		return matches
	}
	out := make([]cache.Match, 0, len(matches))
	for _, m := range matches {
		// "Unknown passes" applies to every one of these, not just rank. avg_mmr was
		// never written by any collector and patch only started being recorded with
		// the richer detail fetch, so demanding a value here did not narrow the
		// training set — it emptied it, silently, while the flag looked honoured.
		if patch != "" && m.Patch != "" && m.Patch != patch {
			continue
		}
		if minMMR > 0 && m.AvgMMR != nil && *m.AvgMMR < minMMR {
			continue
		}
		if minRankTier > 0 && m.RankTier != nil && *m.RankTier < minRankTier {
			continue
		}
		out = append(out, m)
	}
	return out
}

// withBansMatches keeps only matches that carry at least one ban action — i.e.
// real Captains Mode drafts fetched from full match detail, as opposed to the
// picks-only bulk from public_matches. Order is preserved (deterministic).
func withBansMatches(matches []cache.Match) []cache.Match {
	out := make([]cache.Match, 0, len(matches))
	for _, m := range matches {
		for _, pb := range m.PickBans {
			if !pb.IsPick {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// recencyWeight returns 0.5^(ageDays/halfLife). halfLife<=0 disables weighting (1).
func recencyWeight(startTime, refTime int64, halfLife float64) float64 {
	if halfLife <= 0 {
		return 1
	}
	ageDays := float64(refTime-startTime) / 86400.0
	if ageDays <= 0 {
		return 1
	}
	return math.Pow(0.5, ageDays/halfLife)
}

func maxStartTime(matches []cache.Match) int64 {
	var ref int64
	for _, m := range matches {
		if m.StartTime > ref {
			ref = m.StartTime
		}
	}
	return ref
}

// Samples is the full set of training rows replayed from cached drafts.
type Samples struct {
	WinProb []model.WinProbSample
	// Board is the number of picks on the board for the matching WinProb row
	// (10 for a final composition). Used to fit per-phase calibration.
	Board []int
	Pick  []model.PredictorSample
	Ban   []model.PredictorSample
}

// DefaultProWeight is the training weight of pro Captains Mode drafts relative to
// public games.
//
// It is 1 (no down-weighting) because measurement says the knob currently does
// nothing: at 0.25 versus 1.0 the predictor is bit-identical and win-prob moves by
// 0.001, inside the noise. The reason is structural — the predictor trains ONLY on
// Captains Mode drafts, and every CM draft in the cache is a pro game, so scaling
// them all is scaling by a constant. Win-prob barely notices because pro games are
// ~9% of its pool.
//
// Down-weighting only starts to mean something once public CM drafts exist to
// weigh pro games AGAINST (`sync --public-cm`). Until then, cutting pro to 0.25
// would only dilute the sole real Captains Mode signal in favour of all-pick pubs.
const DefaultProWeight = 1.0

// ProWeight builds a source-weight map from a single pro weight. A weight of 1
// (or less than 0) means "no source weighting at all".
func ProWeight(w float64) map[string]float64 {
	if w == 0 {
		w = DefaultProWeight
	}
	if w < 0 || w == 1 {
		return nil // no weighting at all
	}
	return map[string]float64{cache.SourcePro: w}
}

// DefaultSourceWeights down-weights pro Captains Mode relative to public games.
// 0.25 is the "auxiliary data" end of the usual 0.1-0.3 range: enough for the pro
// drafts to inform rare heroes, not enough for them to set the meta.
func DefaultSourceWeights() map[string]float64 {
	return map[string]float64{cache.SourcePro: 0.25}
}

// sourceWeight is the multiplier for a match's provenance (1 when unweighted or
// unknown).
func sourceWeight(weights map[string]float64, source string) float64 {
	if len(weights) == 0 {
		return 1
	}
	if w, ok := weights[source]; ok {
		return w
	}
	return 1
}

// SampleOptions controls which rows get generated.
type SampleOptions struct {
	// SourceWeights scales each match's weight by provenance (see Options).
	SourceWeights map[string]float64

	// Prefix additionally emits win-prob rows for every PARTIAL board state, not
	// just the final 5v5. The live advisor queries strength mid-draft, so a model
	// trained only on complete lineups is answering out-of-distribution questions
	// exactly when it matters most.
	Prefix bool
}

// prefixWeightScale keeps a match's total influence constant when it is expanded
// into many prefix rows: one outcome must not count eleven times just because it
// was observed at eleven board sizes.
func prefixWeightScale(rows int) float64 {
	if rows <= 0 {
		return 1
	}
	return 1 / float64(rows)
}

// BuildSamples replays cached drafts into training rows in deterministic order
// (match order, then draft order). Each row carries the recency weight of its
// match (newer matches weigh more); halfLifeDays<=0 yields uniform weights.
func BuildSamples(matches []cache.Match, hm *features.HeroMap, halfLifeDays float64, opts SampleOptions) Samples {
	var out Samples
	ref := maxStartTime(matches)

	for _, m := range matches {
		weight := recencyWeight(m.StartTime, ref, halfLifeDays) * sourceWeight(opts.SourceWeights, m.Source)
		pbs := orderedPickBans(m)

		// The team acting first defines the CM seat order; the cache does not
		// store it, but the first recorded action reveals it.
		firstPick := 0
		if len(pbs) > 0 {
			firstPick = pbs[0].Team
		}

		y := 0.0
		if m.RadiantWin {
			y = 1.0
		}

		var rPicks, dPicks, rBans, dBans []int
		var boards [][2][]int // snapshots of (radiant, dire) picks after each pick

		for ord, pb := range pbs {
			if idx, ok := hm.Index(pb.HeroID); ok {
				row := model.PredictorSample{
					X: hm.EncodePredictor(features.PredictorState{
						RadiantPicks: rPicks, DirePicks: dPicks,
						RadiantBans: rBans, DireBans: dBans,
						IsPick:    pb.IsPick,
						Actor:     pb.Team,
						Ord:       ord,
						FirstPick: firstPick,
					}),
					Label:  idx,
					Weight: weight,
				}
				if pb.IsPick {
					out.Pick = append(out.Pick, row)
				} else {
					out.Ban = append(out.Ban, row)
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
			if pb.IsPick && opts.Prefix {
				boards = append(boards, [2][]int{
					append([]int(nil), rPicks...),
					append([]int(nil), dPicks...),
				})
			}
		}

		if !opts.Prefix {
			out.WinProb = append(out.WinProb, model.WinProbSample{
				X: hm.EncodeWinProb(rPicks, dPicks), Y: y, Weight: weight,
			})
			out.Board = append(out.Board, len(rPicks)+len(dPicks))
			continue
		}
		scale := prefixWeightScale(len(boards))
		for _, b := range boards {
			out.WinProb = append(out.WinProb, model.WinProbSample{
				X: hm.EncodeWinProb(b[0], b[1]), Y: y, Weight: weight * scale,
			})
			out.Board = append(out.Board, len(b[0])+len(b[1]))
		}
	}
	return out
}

// orderedPickBans returns a match's pick/ban actions sorted by draft order. The
// cache preserves order, but a defensive sort keeps sample generation identical
// regardless of how rows came back.
func orderedPickBans(m cache.Match) []cache.PickBan {
	pbs := append([]cache.PickBan(nil), m.PickBans...)
	sort.SliceStable(pbs, func(i, j int) bool { return pbs[i].Ord < pbs[j].Ord })
	return pbs
}

// Bundle trains both layer-1 models from matches over heroIDs and returns the
// assembled (but not yet saved) bundle.
func Bundle(matches []cache.Match, heroIDs []int, opts Options) *bundle.Bundle {
	opts = opts.withDefaults()
	hm := features.NewHeroMap(heroIDs)

	progress := opts.Progress
	if progress == nil {
		progress = func(string) {}
	}

	matches = FilterMatches(matches, opts.FilterPatch, opts.MinAvgMMR, opts.MinRankTier)

	// Two data pools by design (the user's split): the next-pick predictor learns
	// only from REAL Captains Mode drafts — those carry bans and a true draft order,
	// which is exactly the signal "what does the enemy pick given these bans". The
	// win-prob head and the synergy/counter matrices instead use the FULL pool
	// (bulk picks-only public matches + CM) for volume, since they only need final
	// compositions and outcome, not draft sequence.
	cmMatches := withBansMatches(matches)
	predSource := cmMatches
	if len(predSource) == 0 {
		// No ban data cached yet (e.g. only the picks-only bulk sync has run, or the
		// synthetic test fixtures): fall back to the full pool so the predictor still
		// trains. It degrades to the old order-free bag-of-picks behaviour.
		predSource = matches
		progress("predictor: no CM ban data — training on full pool (run sync to fetch CM drafts)")
	}

	// Three-way split by TIME, at the MATCH level (never at the sample level: one
	// match yields many predictor rows and, with prefix training, many win-prob
	// rows — splitting samples would leak the same draft into both sides). The
	// validation block drives early stopping and calibration; the caller's own
	// held-out test set stays untouched.
	wpFit, wpVal := splitByTime(matches, opts.ValFrac)
	predFit, predVal := splitByTime(predSource, opts.ValFrac)

	sampleOpts := SampleOptions{Prefix: !opts.NoPrefix, SourceWeights: opts.SourceWeights}
	wpSamples := BuildSamples(wpFit, hm, opts.HalfLifeDays, sampleOpts)
	wpValSamples := BuildSamples(wpVal, hm, opts.HalfLifeDays, sampleOpts)
	predSamples := BuildSamples(predFit, hm, opts.HalfLifeDays, SampleOptions{})
	predValSamples := BuildSamples(predVal, hm, opts.HalfLifeDays, SampleOptions{})

	progress(fmt.Sprintf("samples: win-prob %d rows (ESS %.0f) + %d val | pick %d / ban %d rows (+%d/%d val) | %d CM drafts of %d matches",
		len(wpSamples.WinProb), model.EffectiveSampleSize(model.SampleWeights(wpSamples.WinProb)), len(wpValSamples.WinProb),
		len(predSamples.Pick), len(predSamples.Ban), len(predValSamples.Pick), len(predValSamples.Ban),
		len(cmMatches), len(matches)))

	fit := model.FitConfig{Epochs: opts.Epochs, LR: opts.LR, L2: opts.L2, Patience: opts.Patience}

	progress("training win-prob…")
	wpFitCfg := fit
	wpFitCfg.Curve = curveLogger(progress, "win-prob")
	wp := model.TrainWinProb(wpSamples.WinProb, wpValSamples.WinProb, hm.WinProbLen(), wpFitCfg)

	progress(fmt.Sprintf("training next-action heads (%d epochs max)…", opts.Epochs))
	report := func(name string) func(int) {
		return func(epoch int) {
			if epoch%100 == 0 || epoch == opts.Epochs {
				progress(fmt.Sprintf("  %s epoch %d/%d", name, epoch, opts.Epochs))
			}
		}
	}
	pickCfg, banCfg := fit, fit
	pickCfg.Curve = curveLogger(progress, "pick-head")
	banCfg.Curve = curveLogger(progress, "ban-head")
	pickHead := model.TrainPredictor(predSamples.Pick, predValSamples.Pick, hm.Len(), hm.PredictorLen(), pickCfg, report("pick"))
	var banHead *model.SoftmaxReg
	if len(predSamples.Ban) > 0 {
		banHead = model.TrainPredictor(predSamples.Ban, predValSamples.Ban, hm.Len(), hm.PredictorLen(), banCfg, report("ban"))
	} else {
		progress("ban head: no ban rows in the training pool — skipped (picks-only cache)")
	}

	// Calibrate win-prob (M5) on the VALIDATION block, not on the training
	// predictions: fitting Platt on the same rows the model was fit to is leakage
	// and makes the reported ECE optimistic. Falls back to the training samples
	// only when there is no validation block at all.
	progress("calibrating + matrices…")
	calibSource := wpValSamples
	if len(calibSource.WinProb) == 0 {
		calibSource = wpSamples
	}
	calib := fitCalibration(wp, calibSource.WinProb)
	phaseCalib := fitPhaseCalibration(wp, calibSource)

	// Synergy/counter matrices (M3): pair lift with empirical-Bayes shrinkage.
	mx := matrices.Compute(matches, hm, matrices.Options{Prior: opts.MatrixPrior, NoShrink: opts.MatrixNoShrink})
	progress(mx.SignalNote())

	// Optional factorization machine: the linear head cannot express "these two
	// heroes are worth more together than apart" at all, and the matrices express
	// it only pairwise and outside the model. The FM learns both from the same
	// objective the win-prob is scored on.
	var fm *model.FM
	if opts.FMRank != 0 {
		rank := opts.FMRank
		if rank < 0 {
			rank = model.DefaultFMRank
		}
		progress(fmt.Sprintf("training factorization machine (rank %d)…", rank))
		fm = model.TrainFM(fmSamples(wpSamples), fmSamples(wpValSamples), hm.Len(), model.FMConfig{
			K: rank, Epochs: opts.Epochs, LR: opts.LR,
			Patience: opts.Patience,
			Curve:    curveLogger(progress, "fm"),
		})
	}

	// Strength stacker: fitted on the validation block so the weights that combine
	// win-prob with the matrices are estimated out-of-sample (§7).
	stacker := fitStrengthStacker(wp, mx, calibSource, opts)

	createdAt := opts.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}
	return &bundle.Bundle{
		PatchTag:         opts.PatchTag,
		TrainWindowDays:  opts.WindowDays,
		MMRTier:          opts.MMRTier,
		CreatedAt:        createdAt,
		HeroIDs:          hm.IDs(),
		PickPredictor:    pickHead,
		BanPredictor:     banHead,
		WinProb:          wp,
		FM:               fm,
		Calibration:      &calib,
		PhaseCalibration: phaseCalib,
		StrengthStacker:  stacker,
		SynergyMatrix:    mx.Synergy,
		CounterMatrix:    mx.Counter,
		Objectives:       objectives.DefaultObjectives(),
	}
}

// fitCalibration fits Platt scaling for the win-prob model. Callers pass the
// validation rows; the model never saw them, so the fitted calibrator is honest.
func fitCalibration(wp *model.BinaryLogReg, samples []model.WinProbSample) model.Platt {
	if len(samples) == 0 {
		return model.Identity()
	}
	raw := make([]float64, len(samples))
	labels := make([]float64, len(samples))
	for i, s := range samples {
		raw[i] = wp.Predict(s.X)
		labels[i] = s.Y
	}
	return model.FitPlatt(raw, labels, model.DefaultEpochs, model.DefaultLR)
}

// minPhaseCalibRows is the smallest per-board sample that gets its own calibrator;
// below it the global one is used instead of fitting two parameters on noise.
const minPhaseCalibRows = 200

// fitPhaseCalibration fits one calibrator per board size (number of picks on the
// board). A probability read after two picks and one read on a full 5v5 are
// different quantities: the early one should sit much closer to 0.5, and a single
// calibrator fitted on final compositions cannot express that. Board sizes with
// too little data fall back to the identity, which defers to the global Platt.
func fitPhaseCalibration(wp *model.BinaryLogReg, s Samples) []model.Platt {
	if len(s.WinProb) == 0 || len(s.Board) != len(s.WinProb) {
		return nil
	}
	const maxBoard = 10
	raw := make([][]float64, maxBoard+1)
	labels := make([][]float64, maxBoard+1)
	for i, smp := range s.WinProb {
		b := s.Board[i]
		if b < 0 || b > maxBoard {
			continue
		}
		raw[b] = append(raw[b], wp.Predict(smp.X))
		labels[b] = append(labels[b], smp.Y)
	}
	out := make([]model.Platt, maxBoard+1)
	fitted := false
	for b := range out {
		if len(raw[b]) < minPhaseCalibRows {
			out[b] = model.Identity()
			continue
		}
		out[b] = model.FitPlatt(raw[b], labels[b], model.DefaultEpochs, model.DefaultLR)
		fitted = true
	}
	if !fitted {
		return nil
	}
	return out
}

// fitStrengthStacker learns how to combine the win-prob logit with the synergy and
// counter differentials, on validation rows only.
func fitStrengthStacker(wp *model.BinaryLogReg, mx *matrices.Matrices, s Samples, opts Options) *model.Stacker {
	if mx == nil || len(s.WinProb) == 0 {
		return nil
	}
	rows := make([][]float64, 0, len(s.WinProb))
	labels := make([]float64, 0, len(s.WinProb))
	weights := make([]float64, 0, len(s.WinProb))
	for _, smp := range s.WinProb {
		r, d := decodeSides(smp.X)
		if len(r) == 0 || len(d) == 0 {
			continue
		}
		rows = append(rows, model.StackerFeatures(
			wp.Predict(smp.X),
			mx.MeanSynergy(r)-mx.MeanSynergy(d),
			mx.MeanCounter(r, d),
			0, // role coverage differential is filled in by the caller-side features
		))
		labels = append(labels, smp.Y)
		weights = append(weights, smp.Weight)
	}
	if len(rows) < minPhaseCalibRows {
		return nil
	}
	cfg := model.FitConfig{Epochs: opts.Epochs, LR: opts.LR, L2: opts.L2}
	return model.FitStacker(rows, labels, weights, cfg)
}

// fmSamples converts win-prob rows into the factorization machine's per-side
// index form. Both heads see exactly the same rows (including prefix boards), so
// any AUC difference between them is the model, not the data.
func fmSamples(s Samples) []model.FMSample {
	out := make([]model.FMSample, 0, len(s.WinProb))
	for _, smp := range s.WinProb {
		r, d := decodeSides(smp.X)
		out = append(out, model.FMSample{Radiant: r, Dire: d, Y: smp.Y, Weight: smp.Weight})
	}
	return out
}

// decodeSides recovers the dense indices of each side from an antisymmetric
// win-prob feature vector (+1 radiant, -1 dire).
func decodeSides(x []float64) (radiant, dire []int) {
	for i, v := range x {
		switch {
		case v > 0:
			radiant = append(radiant, i)
		case v < 0:
			dire = append(dire, i)
		}
	}
	return radiant, dire
}
