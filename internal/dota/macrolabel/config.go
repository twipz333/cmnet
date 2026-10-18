package macrolabel

// Config is versioned labeler heuristic configuration.
type Config struct {
	ConfigVersion string `json:"config_version"`

	LookbackSeconds  float64 `json:"lookback_seconds"`
	LookaheadSeconds float64 `json:"lookahead_seconds"`

	MinConfidenceToLabel float64 `json:"min_confidence_to_label"`
	AmbiguityMargin      float64 `json:"ambiguity_margin"`

	MinimumActionDurationSeconds float64 `json:"minimum_action_duration_seconds"`
	MaxUnlabeledGapSeconds       float64 `json:"max_unlabeled_gap_seconds"`

	// v0.2 audit / accounting
	MaxFrameGapSeconds         float64 `json:"max_frame_gap_seconds"`
	SmokeFollowSeconds         float64 `json:"smoke_follow_seconds"`
	MaxRejectedCandidates      int     `json:"max_rejected_candidates"`
	AccountingToleranceSeconds float64 `json:"accounting_tolerance_seconds"`

	// Evidence thresholds.
	MinHeroesInAreaForFarm    int     `json:"min_heroes_in_area_for_farm"`
	MinLaneFrontDeltaPush     float64 `json:"min_lane_front_delta_push"`
	MinGroupedRatioDeltaGroup float64 `json:"min_grouped_ratio_delta_group"`

	SmokeTargetClearFraction float64 `json:"smoke_target_clear_fraction"`
}

const DefaultConfigVersion = "macro-labeler-config-v0.2"

func DefaultConfig() Config {
	return Config{
		ConfigVersion:    DefaultConfigVersion,
		LookbackSeconds:  6,
		LookaheadSeconds: 8,

		MinConfidenceToLabel: 0.65,
		AmbiguityMargin:      0.05,

		MinimumActionDurationSeconds: 6,
		MaxUnlabeledGapSeconds:       2,

		MaxFrameGapSeconds:         15,
		SmokeFollowSeconds:         25,
		MaxRejectedCandidates:      5,
		AccountingToleranceSeconds: 1.0,

		MinHeroesInAreaForFarm:    3,
		MinLaneFrontDeltaPush:     0.12,
		MinGroupedRatioDeltaGroup: 0.10,

		SmokeTargetClearFraction: 0.55,
	}
}
