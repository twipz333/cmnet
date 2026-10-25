// Package recommender turns a draft state plus the layer-1 models into a ranked
// list of pick/ban recommendations and an enemy-move prediction. All ordering is
// deterministic (rounded scores + hero_id tie-break) so every machine agrees (§6).
package recommender

// The json tags on these types are the web front end's wire format: it renders
// whatever fields Result carries, so a new field reaches the browser without any
// transport change. Keep the tags stable once shipped.

// Recommendation is a single suggested hero for the current step.
type Recommendation struct {
	HeroID int     `json:"heroId"`
	Score  float64 `json:"score"`
	Why    string  `json:"why"`
	// Role is the MAP lane (1-5) under the current board's assignment posterior (§6.3).
	// Zero when no role prior is loaded.
	Role int `json:"role,omitempty"`
	// Player is the ally nickname the capability layer assigns to this hero (§6.5).
	// Empty when no full ally roster is loaded.
	Player string `json:"player,omitempty"`
	// Factors are the parts Score was assembled from, where they exist separately.
	//
	// Deliberately excluded from the wire format (`json:"-"`): the web DTO and the
	// cross-platform determinism golden both serialise this struct, and a field that
	// changes neither the ranking nor what is displayed has no business changing
	// either of those. It is read in-process by the decision log (§6.1), which needs
	// to know why a score was what it was long after the fact.
	Factors *Factors `json:"-"`
}

// Factors is the decomposition of a recommendation score. Only the parts the core
// actually computes separately are here: a ban score is literally built from these
// three terms plus weights, and the ally-comfort bonus is an explicit offset. The
// pick score from beam search is NOT decomposable this way — it is the outcome of a
// rollout, not a sum of named contributions — so for picks only WinProb is set, and
// nothing here pretends otherwise (see §6.4).
type Factors struct {
	// Base is the pick score before the comfort offset.
	//
	// Deliberately NOT called a win probability: with beam search on it is the value
	// of a rollout — win-prob plus synergy, counter and role-coverage adjustments —
	// which is not a probability and is not confined to [0,1]. Only in the one-ply
	// fallback does it happen to be a calibrated win-prob. Naming it after the
	// quantity it sometimes is would put a wrong label on the majority of records.
	Base float64 `json:"base,omitempty"`
	// BaseIsWinProb reports whether Base is that calibrated win probability (one-ply
	// scoring) rather than a beam rollout value, so a reader never has to guess
	// which of the two a record holds.
	BaseIsWinProb bool `json:"baseIsWinProb,omitempty"`
	// Comfort is how much ally comfort moved the pick score (#3).
	Comfort float64 `json:"comfort,omitempty"`
	// Capability is how much the §6.5 feasibility penalty moved the pick score
	// (typically <= 0). Zero when no roster is loaded.
	Capability float64 `json:"capability,omitempty"`
	// EnemyProb is P(the enemy takes this next) — the base of a ban score.
	EnemyProb float64 `json:"enemyProb,omitempty"`
	// Threat is the candidate's mean counter win rate against our picks, and
	// EnemySynergy its mean synergy with the enemy's. Both centered at 0.5.
	Threat       float64 `json:"threat,omitempty"`
	EnemySynergy float64 `json:"enemySynergy,omitempty"`
	// AltDeficit is how much the enemy's post-ban outlook worsens if we ban this hero (§6.7).
	AltDeficit float64 `json:"altDeficit,omitempty"`
}

// EnemyPick is a predicted next enemy action.
type EnemyPick struct {
	HeroID int     `json:"heroId"`
	Prob   float64 `json:"prob"`
	// Source tags where the signal came from for UI transparency (§9): "meta"
	// (layer 1 only), "player" (layer-2 affinity contributed), or "event" (layer-3
	// team-this-event contributed). Scoring is unified; this is display-only.
	Source string `json:"source"`

	// Threat is how much this hero beats OUR current picks: the mean counter win
	// rate against them, centered so 0 is neutral and +0.05 means "wins the
	// matchup by five points". Zero without matrices or against an empty board.
	Threat float64 `json:"threat"`
	// Fit is how well this hero synergizes with the ENEMY's current picks, on the
	// same centered scale.
	Fit float64 `json:"fit"`
	// Danger combines Threat and Fit with the ban weights: one number for "how much
	// does it cost us if they get this". The core computes it so no front end has to
	// (§8) — and so both front ends agree on the same value.
	Danger float64 `json:"danger"`
}

// Result is everything the TUI renders for the current draft state. The core
// computes and orders it; the TUI must not re-sort or recompute (§8).
type Result struct {
	// OurTurn reports whether the current step belongs to our team.
	OurTurn bool `json:"ourTurn"`
	// IsPick reports whether the current step is a pick (vs a ban).
	IsPick bool `json:"isPick"`
	// Recs is the ranked recommendation list for the current step (empty if the
	// draft is complete).
	Recs []Recommendation `json:"recs"`
	// EnemyPred is the ranked predicted next enemy move.
	EnemyPred []EnemyPick `json:"enemyPred"`
	// EnemyStep says which action EnemyPred is for: on the enemy's turn the current
	// step, on ours their next step in the schedule. Nil when they have none left.
	EnemyStep *EnemyStep `json:"enemyStep,omitempty"`
	// PoolAlert lists our preferred-pool heroes (#4) that are still available,
	// ranked by how likely the enemy is to pick/ban them next — i.e. the ones to
	// secure or expect contested. Empty when no hero pool is configured.
	PoolAlert []EnemyPick `json:"poolAlert"`
	// SafeToLet is the counterpart of the ban list: heroes the enemy is likely to
	// take that are NOT dangerous to our draft — the ones worth letting through.
	//
	// A ban list alone answers "what should I take away" but never "what can I
	// ignore", and under draft-clock pressure the second question is the one that
	// wastes bans. Membership is decided by comparing each candidate's Danger
	// against the median of the same candidate pool, not against an absolute
	// threshold: what counts as harmless depends on how threatening the rest of the
	// board is. Empty without matrices (no threat signal to reason about).
	SafeToLet []EnemyPick `json:"safeToLet"`
	// PositionMeta is the patch meta per lane position (#9): top available heroes
	// for each of positions 1-5, with taken/banned heroes already removed. Empty
	// when hero positions are unknown.
	PositionMeta []PositionMeta `json:"positionMeta"`
	// ItemAdvice is the advice (#10) fired by the rule DSL against the enemy draft.
	ItemAdvice []string `json:"itemAdvice"`
	// Strength is the win-prob of the picks CURRENTLY on the board (before this
	// step's action), from our perspective and Radiant's. Nil until ≥1 hero picked.
	Strength *DraftStrength `json:"strength"`
}

// DraftStrength is the calibrated win-prob evaluation of the current board picks.
// OurProb is from our team's perspective; RadiantProb is always Radiant's. Both are
// rounded for platform stability (§6). RadiantPicks/DirePicks report how many heroes
// each side has so the UI can flag a still-incomplete (and thus noisy) read.
type DraftStrength struct {
	OurProb      float64 `json:"ourProb"`
	RadiantProb  float64 `json:"radiantProb"`
	RadiantPicks int     `json:"radiantPicks"`
	DirePicks    int     `json:"direPicks"`
}

// MetaHero is one hero in a position's meta list.
type MetaHero struct {
	HeroID int     `json:"heroId"`
	Score  float64 `json:"score"`
	// WinRate is the hero's win rate in this position (0..1), when known from the
	// STRATZ-baked position meta; 0 when the meta is the coarse roles fallback.
	WinRate float64 `json:"winRate"`
}

// PositionMeta is the meta hero list for a single lane position (1-5).
type PositionMeta struct {
	Position int        `json:"position"`
	Heroes   []MetaHero `json:"heroes"`
}

// EnemyStep identifies the enemy action a prediction refers to.
type EnemyStep struct {
	Ord    int  `json:"ord"`    // 0-based index in the draft sequence
	IsPick bool `json:"isPick"` // false = ban
}
