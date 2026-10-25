package bundle

import (
	"encoding/json"
	"fmt"
	"os"

	"cmnet/internal/atomicfile"
	"cmnet/internal/draft"
	"cmnet/internal/features"
	"cmnet/internal/heroagg"
	"cmnet/internal/matrices"
	"cmnet/internal/model"
	"cmnet/internal/objectives"
)

// Bundle is the shareable layer-1 artifact: both models, the hero index map, and
// version metadata. Format is JSON for now (readable golden-test diffs); the file
// is a single self-contained artifact.
type Bundle struct {
	BundleVersion        string `json:"bundle_version"` // sha256 of the content with this field blank
	ModelSchemaVersion   int    `json:"model_schema_version"`
	FeatureSchemaVersion int    `json:"feature_schema_version"`
	CodeVersion          string `json:"code_version"`

	PatchTag        string `json:"patch_tag"`
	TrainWindowDays int    `json:"train_window_days"`
	MMRTier         string `json:"mmr_tier"`
	CreatedAt       string `json:"created_at"`

	// HeroIDs is the sorted hero id slice; the dense hero map is rebuilt from it.
	HeroIDs []int `json:"hero_ids"`

	// Next-action heads: picks and bans are modeled separately (they answer
	// different questions and happen at different phases of the CM sequence).
	PickPredictor *model.SoftmaxReg   `json:"pick_predictor"`
	BanPredictor  *model.SoftmaxReg   `json:"ban_predictor"`
	WinProb       *model.BinaryLogReg `json:"win_prob"`
	// Calibration (M5) for the win-prob model; nil/omitted means uncalibrated.
	Calibration *model.Platt `json:"calibration,omitempty"`

	// PhaseCalibration holds one Platt calibrator per number of picks on the
	// board (index 0..10), fitted on partial drafts. A probability read mid-draft
	// is a different quantity from one read on a full 5v5, so it needs its own
	// calibration; index 10 (or a nil slice) falls back to Calibration.
	PhaseCalibration []model.Platt `json:"phase_calibration,omitempty"`

	// FM is the optional factorization-machine draft evaluator (hero main effects
	// plus low-rank synergy/matchup factors). Nil unless training was asked for
	// it; when present the recommender prefers it over the linear win-prob for
	// final-composition strength.
	FM *model.FM `json:"fm,omitempty"`

	// StrengthStacker holds the fitted weights that combine the win-prob logit
	// with the synergy/counter differentials into the displayed draft strength.
	// Fitted on the validation block, never on the test data (a hand-tuned lambda
	// swept on the test set is not an honest estimate of anything).
	StrengthStacker *model.Stacker `json:"strength_stacker,omitempty"`

	// Synergy/counter matrices (M3), indexed by dense hero index. Empty on MVP
	// bundles; the recommender falls back to one-ply scoring when absent.
	SynergyMatrix [][]float64 `json:"synergy_matrix,omitempty"`
	CounterMatrix [][]float64 `json:"counter_matrix,omitempty"`

	// HeroPositions maps a Valve hero id to its eligible lane positions (1-5),
	// derived at train time (#8): from STRATZ per-position play when a token is
	// available, else coarsely from OpenDota hero roles. Used to penalize drafts
	// that can't field a plausible 1-5. Empty/omitted disables lining checks.
	HeroPositions map[int][]int `json:"hero_positions,omitempty"`

	// CMOrder is the Captains Mode schedule the training drafts were actually
	// played under, recovered from their recorded action order (train.InferOrder).
	// Serve drives the manual board and the planner from it, so a patch that
	// reshuffles the draft is picked up by retraining instead of by editing code.
	// Omitted when the cache holds too few complete drafts to read it from; serve
	// then uses draft.DefaultOrder.
	CMOrder draft.Order `json:"cm_order,omitempty"`

	// RolePrior is P(position|hero) over the CM corpus used at train time (§6.3).
	// Serve uses it to form a joint distribution over valid hero→position
	// assignments mid-draft (positions are latent until later picks resolve them).
	// Empty/omitted disables soft position features at inference; main effects still work.
	RolePrior map[int][5]float64 `json:"role_prior,omitempty"`

	// Objectives is the patch's map-objective spawn table (bounty/power/wisdom/lotus/
	// tormentor/day-night), baked at train time (M6). serve reads the live game clock
	// from GSI and renders deterministic upcoming-objective alerts from this table.
	// Empty/omitted disables the timer panel.
	Objectives []objectives.Spawn `json:"objectives,omitempty"`

	// PositionMeta maps each lane position (1-5) to that position's top meta heroes
	// for the patch, fetched from STRATZ at train time (#9). Each entry carries
	// popularity (Matches) and WinRate so the panel ranks by real per-position
	// signal instead of a single global popularity score. Empty/omitted makes the
	// recommender fall back to the coarse roles-based meta.
	PositionMeta map[int][]PosHero `json:"position_meta,omitempty"`

	// CorpusPositions / CorpusBans / CorpusLanes are the same kind of question as
	// PositionMeta, answered from the corpus this bundle was trained on rather than
	// from STRATZ's ranked all-pick aggregate.
	//
	// Kept alongside PositionMeta, not instead of it: the two describe different
	// populations (our Captains Mode cache vs a rank bracket's all-pick), so they
	// disagree, and pooling them would produce a number that describes neither.
	// Each panel says which it is showing.
	//
	// Empty on bundles trained before this existed; consumers check len() and fall
	// back, which is why no schema version had to change (§6).
	CorpusPositions []heroagg.HeroPosStat `json:"corpus_positions,omitempty"`
	CorpusBans      []heroagg.HeroBanStat `json:"corpus_bans,omitempty"`
	// CorpusLanes carries the lane pairings (1+5, 3+4, solo 2) with their lift over
	// an average lane. Display and explanation only for now: feeding it into the
	// scoring vector would change the feature layout, which is a FeatureSchemaVersion
	// bump and a coordinated bundle handout, not a quiet addition.
	CorpusLanes []heroagg.LaneStat `json:"corpus_lanes,omitempty"`
}

// PosHero is one hero's standing in a lane position's meta (bundle-baked, §9).
type PosHero struct {
	HeroID  int     `json:"hero_id"`
	Matches int     `json:"matches"`
	WinRate float64 `json:"win_rate"`
}

// HeroMap rebuilds the dense hero index map from the bundle's hero ids.
func (b *Bundle) HeroMap() *features.HeroMap {
	return features.NewHeroMap(b.HeroIDs)
}

// Matrices wraps the baked synergy/counter grids, or nil when the bundle carries
// none (MVP bundles; the recommender then falls back to one-ply scoring).
func (b *Bundle) Matrices() *matrices.Matrices {
	if len(b.SynergyMatrix) == 0 || len(b.CounterMatrix) == 0 {
		return nil
	}
	return &matrices.Matrices{Synergy: b.SynergyMatrix, Counter: b.CounterMatrix}
}

// Save finalizes the version hash and writes the bundle as JSON to path.
//
// The write is atomic and keeps the replaced bundle as <path>.bak. Training takes
// minutes and now also runs unattended in the background, so an interrupted save
// is a routine event rather than a freak one — and since Load verifies the content
// hash, a half-written bundle is not a stale bundle but an unloadable one, leaving
// the advisor with nothing to serve. The backup covers the other direction: a
// freshly trained bundle that turns out worse can be swapped back by hand.
func (b *Bundle) Save(path string) error {
	b.ModelSchemaVersion = ModelSchemaVersion
	b.FeatureSchemaVersion = features.FeatureSchemaVersion
	b.CodeVersion = CodeVersion

	version, err := computeVersion(b)
	if err != nil {
		return err
	}
	b.BundleVersion = version

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteWithBackup(path, data, 0o644)
}

// Load reads a bundle from path, verifies its content hash, and asserts schema
// compatibility. It refuses incompatible bundles (§6).
func Load(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("decode bundle: %w", err)
	}
	if err := b.verify(); err != nil {
		return nil, err
	}
	return &b, nil
}
