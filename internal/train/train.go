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
	"cmnet/internal/roles"
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

	// HeroPositions is the eligibility map (hero → lanes 1-5) used for role
	// coverage in the strength stacker. Built by the caller before Bundle so the
	// stacker is fitted on the same feature the live recommender will apply.
	HeroPositions map[int][]int

	// RolePrior is P(position|hero) for soft coverage / occupancy. Nil keeps the
	// stacker Role feature at zero only when both prior and positions are absent;
	// a nil prior with positions still yields uniform soft coverage.
	RolePrior *roles.Prior

	// Progress, if set, receives human-readable progress lines during training
	// (predictor GD is the slow part). Not serialized.
	Progress func(string) `json:"-"`
}

// DefaultHalfLifeDays is the recency half-life: a match this many days older than
// the newest gets half the weight.
//
// Measured over THREE rolling windows, which is the whole point of the number
// (`advisor eval --sweep-half-life 7,14,30,-1 --folds 3`, 58631-match cache with a
// real public-CM corpus). A single window cannot pick this parameter: the spread of
// predictor top-5 BETWEEN folds is 0.206/0.248/0.235, while the spread ACROSS
// half-lives within one fold is ~0.005. Measure on one split and you will "prove"
// whichever value you happened to run.
//
// What actually survives all three windows:
//   - Predictor top-5 cannot tell 7d, 14d and 30d apart — the winner changes with
//     the window (7d, then 14d, then a tie) and the gaps are 0.001-0.006. Only
//     uniform is consistently worse (0.199/0.238/0.229), so recency weighting earns
//     its keep, but its exact value does not matter to this head.
//   - Win-prob on CM never gets worse as the half-life grows across 7d/14d/30d, in
//     any fold: 0.547/0.519/0.549 at 7d → 0.565/0.543/0.553 at 30d, i.e. +0.018,
//     +0.024 and +0.004 (fold 3 is a tie between 14d and 30d). A 7-day half-life is
//     the worst of the three in all three windows, and in two of them by more than
//     the predictor gains anywhere.
//
// So 30 days: it loses to the per-window best predictor score by 0.001-0.005 (inside
// the noise), takes nearly all of the available CM win-prob gain, and keeps 76-95%
// of the effective sample size instead of the 27-41% a 7-day half-life leaves.
//
// It is a compromise, not a proven optimum, and the honest caveats are: uniform
// actually scores highest on CM in two of the three folds (0.571/0.545 vs 30d's
// 0.565/0.543) but is the worst setting for the predictor everywhere, and the sweep
// reports point estimates — no paired bootstrap CIs per sweep point — so adjacent
// values are not separated by anything you could call significant.
//
// An earlier default of 7 came from a sweep on a single window, before the cache had
// public CM drafts; that measurement did not replicate. Re-measure this when the
// corpus composition changes again — not just when the data grows.
const DefaultHalfLifeDays = 30.0

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
		if hasRealDraftOrder(m) {
			out = append(out, m)
		}
	}
	return out
}

// hasRealDraftOrder reports whether a match's action sequence is the order the
// draft actually happened in.
//
// Bans are the tell. A Captains Mode draft is stored with its true sequence, and
// only such a draft has bans; the bulk public-match scan has no draft data at all
// and its heroes are written down with a SYNTHETIC order — all five Radiant picks,
// then all five Dire ones (see publicToCacheMatch). Reading that order as a draft
// invents board states that cannot occur, like five heroes facing an empty side.
func hasRealDraftOrder(m cache.Match) bool {
	for _, pb := range m.PickBans {
		if !pb.IsPick {
			return true
		}
	}
	return false
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
// It is 1 (no down-weighting) by default, but be honest about the evidence: this
// value is NOT backed by a valid measurement.
//
// The comparison it used to cite (0.25 vs 1.0 leaves the predictor bit-identical
// and moves win-prob by 0.001) was run while SourceWeights were not passed to the
// predictor at all, so "bit-identical" measured the plumbing, not the data. That
// plumbing is fixed above; the 0.001 win-prob figure stands, since win-prob always
// did receive the weights and pro games are only ~9% of its pool.
//
// Re-run the comparison before trusting either value. Down-weighting can only mean
// something now that the cache holds public CM drafts to weigh pro games AGAINST —
// while every CM draft was a pro game, scaling them all was scaling by a constant.
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
	//
	// Matches with a real draft order (see hasRealDraftOrder) are expanded along
	// the order they were played. Picks-only bulk matches have no such order, so
	// their prefixes are reconstructed by alternating sides instead of following
	// the roster array they happen to be stored in.
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


// playerPositions maps hero id → lane position for one match's roster. Heroes
// without a recorded position are omitted — EncodeWinProbPositions leaves their
// position slots at zero (main effects still fire).
func playerPositions(m cache.Match) (radiant, dire map[int]int) {
	radiant, dire = map[int]int{}, map[int]int{}
	for _, p := range m.Players {
		if p.Position == nil || p.HeroID == 0 {
			continue
		}
		pos := *p.Position
		if pos < 1 || pos > 5 {
			continue
		}
		if p.IsRadiant {
			radiant[p.HeroID] = pos
		} else {
			dire[p.HeroID] = pos
		}
	}
	if len(radiant) == 0 {
		radiant = nil
	}
	if len(dire) == 0 {
		dire = nil
	}
	return radiant, dire
}

// encodeWP builds a schema-v2 win-prob row, attaching hard positions when known.
func encodeWP(hm *features.HeroMap, rPicks, dPicks []int, rPos, dPos map[int]int) []float64 {
	if rPos == nil && dPos == nil {
		return hm.EncodeWinProb(rPicks, dPicks)
	}
	// Restrict maps to heroes currently on the board (prefix rows).
	filter := func(picks []int, pos map[int]int) map[int]int {
		if pos == nil || len(picks) == 0 {
			return nil
		}
		out := make(map[int]int, len(picks))
		for _, id := range picks {
			if p, ok := pos[id]; ok {
				out[id] = p
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return hm.EncodeWinProbPositions(rPicks, dPicks, filter(rPicks, rPos), filter(dPicks, dPos))
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
		// Decided once: a synthetic-order match has its boards rebuilt below, so
		// snapshotting the stored sequence for it would allocate ten slices per
		// match only to throw them away.
		realOrder := hasRealDraftOrder(m)

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
		rPos, dPos := playerPositions(m)

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
			if pb.IsPick && opts.Prefix && realOrder {
				boards = append(boards, [2][]int{
					append([]int(nil), rPicks...),
					append([]int(nil), dPicks...),
				})
			}
		}

		if !opts.Prefix {
			out.WinProb = append(out.WinProb, model.WinProbSample{
				X: encodeWP(hm, rPicks, dPicks, rPos, dPos), Y: y, Weight: weight,
			})
			out.Board = append(out.Board, len(rPicks)+len(dPicks))
			continue
		}
		// A bulk public match has no draft order: its heroes are stored five Radiant
		// then five Dire. Walking THAT sequence produces boards like "five heroes
		// versus nobody", which no draft passes through — so bulk prefixes are
		// rebuilt by alternating sides instead, giving 1v0, 1v1, 2v1 … 5v5.
		//
		// Dropping bulk prefixes altogether was tried first and measured WORSE:
		// pick-quality AUC on CM fell 0.555 → 0.504 and the 1-2 hero boards went
		// under 0.5 (0.446/0.441), i.e. actively inverted. Those 52k matches are
		// what teaches the model sparse boards at all; the 7k real drafts alone are
		// too few. The fiction worth removing is the LOPSIDEDNESS, not the prefixes.
		if !realOrder {
			boards = boards[:0]
			// Add one hero at a time, alternating sides, and fall back to whichever
			// side still has heroes once the other runs out. Clamping ceil(i/2) and
			// floor(i/2) instead would break on lopsided rosters (a 5v3 match would
			// emit 4v3 twice and never reach its own full composition).
			//
			// Which side starts alternates by match id so the extra hero of an odd
			// roster does not always fall to Radiant. It is a fixed function of the
			// id, so sample generation stays reproducible.
			nr, nd := 0, 0
			radiantFirst := m.ID%2 == 0
			for nr+nd < len(rPicks)+len(dPicks) {
				takeRadiant := nr < nd || (nr == nd && radiantFirst)
				if takeRadiant && nr >= len(rPicks) {
					takeRadiant = false
				} else if !takeRadiant && nd >= len(dPicks) {
					takeRadiant = true
				}
				if takeRadiant {
					nr++
				} else {
					nd++
				}
				boards = append(boards, [2][]int{
					append([]int(nil), rPicks[:nr]...),
					append([]int(nil), dPicks[:nd]...),
				})
			}
		}
		scale := prefixWeightScale(len(boards))
		for _, b := range boards {
			out.WinProb = append(out.WinProb, model.WinProbSample{
				X: encodeWP(hm, b[0], b[1], rPos, dPos), Y: y, Weight: weight * scale,
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
	// (bulk picks-only public matches + CM) for volume.
	//
	// Win-prob also learns partial boards from both pools, but reaches them
	// differently: CM drafts are replayed in the order they happened, while bulk
	// matches — which have no order — get theirs reconstructed by alternating
	// sides. See the prefix block in BuildSamples for why.
	cmMatches := withBansMatches(matches)
	orderRep := InferOrder(cmMatches)
	progress(orderRep.Note(features.PhaseBounds()))
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
	// The predictor gets the same provenance weighting as win-prob. It used to be
	// built with empty options, which silently made SourceWeights a win-prob-only
	// knob: any pro-weight experiment came back "bit-identical for the predictor"
	// because the weights never reached it, and that non-result was then read as
	// evidence that provenance does not matter to this head. It matters now that
	// the cache holds public CM drafts as well as pro ones — the predictor trains
	// on CM only, so before public CM existed every row it saw was pro and the
	// weighting really was a no-op. Prefix stays off: the predictor scores single
	// actions, not partial boards.
	predSampleOpts := SampleOptions{SourceWeights: opts.SourceWeights}
	wpSamples := BuildSamples(wpFit, hm, opts.HalfLifeDays, sampleOpts)
	wpValSamples := BuildSamples(wpVal, hm, opts.HalfLifeDays, sampleOpts)
	predSamples := BuildSamples(predFit, hm, opts.HalfLifeDays, predSampleOpts)
	predValSamples := BuildSamples(predVal, hm, opts.HalfLifeDays, predSampleOpts)

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
	mxOpts := matrices.Options{Prior: opts.MatrixPrior, NoShrink: opts.MatrixNoShrink}
	mx := matrices.Compute(matches, hm, mxOpts)
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
	//
	// The matrices it is fitted against must not have seen those rows either. The
	// bundle's matrices are counted over every match, validation block included,
	// so fitting on them would hand the stacker pair cells built from the very
	// outcomes it is asked to predict — and it would learn to trust synergy and
	// counter more than they deserve. It is fitted on matrices counted over the
	// fit block only (cross-fitting), then applied to the full ones at serve time.
	stacker := fitStrengthStacker(wp, hm, stackerMatrices(wpFit, len(wpValSamples.WinProb) > 0, hm, mxOpts, mx), calibSource, opts)

	createdAt := opts.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}
	b := &bundle.Bundle{
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
		HeroPositions:    opts.HeroPositions,
		CMOrder:          orderRep.Order,
	}
	if opts.RolePrior != nil && len(opts.RolePrior.Prob) > 0 {
		b.RolePrior = opts.RolePrior.Prob
	}
	return b
}

// fitCalibration fits Platt scaling for the win-prob model. Callers pass the
// validation rows; the model never saw them, so the fitted calibrator is honest.
func fitCalibration(wp *model.BinaryLogReg, samples []model.WinProbSample) model.Platt {
	if len(samples) == 0 {
		return model.Identity()
	}
	raw := make([]float64, len(samples))
	labels := make([]float64, len(samples))
	weights := make([]float64, len(samples))
	for i, s := range samples {
		raw[i] = wp.Predict(s.X)
		labels[i] = s.Y
		weights[i] = s.Weight
	}
	return model.FitPlattWeighted(raw, labels, weights, model.DefaultEpochs, model.DefaultLR)
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
	weights := make([][]float64, maxBoard+1)
	for i, smp := range s.WinProb {
		b := s.Board[i]
		if b < 0 || b > maxBoard {
			continue
		}
		raw[b] = append(raw[b], wp.Predict(smp.X))
		labels[b] = append(labels[b], smp.Y)
		weights[b] = append(weights[b], smp.Weight)
	}
	out := make([]model.Platt, maxBoard+1)
	fitted := false
	for b := range out {
		if len(raw[b]) < minPhaseCalibRows {
			out[b] = model.Identity()
			continue
		}
		out[b] = model.FitPlattWeighted(raw[b], labels[b], weights[b], model.DefaultEpochs, model.DefaultLR)
		fitted = true
	}
	if !fitted {
		return nil
	}
	return out
}

// stackerMatrices returns the matrices the strength stacker is fitted against:
// counted over the fit block alone when a validation block exists (the stacker is
// fitted on it), else the full matrices (no held-out rows to protect).
func stackerMatrices(fit []cache.Match, haveVal bool, hm *features.HeroMap, o matrices.Options, full *matrices.Matrices) *matrices.Matrices {
	if !haveVal {
		return full
	}
	return matrices.Compute(fit, hm, o)
}

// fitStrengthStacker learns how to combine the win-prob logit with the synergy and
// counter differentials, on validation rows only.
func fitStrengthStacker(wp *model.BinaryLogReg, hm *features.HeroMap, mx *matrices.Matrices, s Samples, opts Options) *model.Stacker {
	if mx == nil || len(s.WinProb) == 0 {
		return nil
	}
	rows := make([][]float64, 0, len(s.WinProb))
	labels := make([]float64, 0, len(s.WinProb))
	weights := make([]float64, 0, len(s.WinProb))
	n := hm.Len()
	for _, smp := range s.WinProb {
		r, d := decodeSides(smp.X, n)
		if len(r) == 0 || len(d) == 0 {
			continue
		}
		roleDiff := 0.0
		if opts.RolePrior != nil || len(opts.HeroPositions) > 0 {
			roleDiff = roles.CoverageDiff(idsOf(hm, r), idsOf(hm, d), opts.RolePrior, opts.HeroPositions)
		}
		rows = append(rows, model.StackerFeatures(
			wp.Predict(smp.X),
			mx.MeanSynergy(r)-mx.MeanSynergy(d),
			mx.MeanCounter(r, d),
			roleDiff,
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

// idsOf maps dense indices back to Valve hero ids.
func idsOf(hm *features.HeroMap, dense []int) []int {
	out := make([]int, len(dense))
	for i, d := range dense {
		out[i] = hm.ID(d)
	}
	return out
}

// fmSamples converts win-prob rows into the factorization machine's per-side
// index form. Both heads see exactly the same rows (including prefix boards), so
// any AUC difference between them is the model, not the data.
func fmSamples(s Samples) []model.FMSample {
	out := make([]model.FMSample, 0, len(s.WinProb))
	for _, smp := range s.WinProb {
		r, d := decodeSides(smp.X, 0)
		out = append(out, model.FMSample{Radiant: r, Dire: d, Y: smp.Y, Weight: smp.Weight})
	}
	return out
}

// decodeSides recovers the dense indices of each side from the MAIN block of an
// antisymmetric win-prob vector (+1 radiant, -1 dire). nHeroes is the hero-map
// size; the position block (schema v2) must not be scanned as heroes.
func decodeSides(x []float64, nHeroes int) (radiant, dire []int) {
	n := nHeroes
	if n <= 0 || n > len(x) {
		n = features.WinProbHeroes(len(x))
	}
	if n > len(x) {
		n = len(x)
	}
	for i := 0; i < n; i++ {
		switch {
		case x[i] > 0:
			radiant = append(radiant, i)
		case x[i] < 0:
			dire = append(dire, i)
		}
	}
	return radiant, dire
}
