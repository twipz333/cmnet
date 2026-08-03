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
