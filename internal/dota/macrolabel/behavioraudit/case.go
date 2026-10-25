package behavioraudit

import (
	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

// TrajectoryPoint is one sampled hero position for SVG rendering.
type TrajectoryPoint struct {
	Hero     string  `json:"hero"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	GameTime int     `json:"game_time"`
	Observed bool    `json:"observed"`
}

// MapRegionMark labels a coarse map area for SVG when trajectories are absent.
type MapRegionMark struct {
	ID    string  `json:"id"`
	Label string  `json:"label,omitempty"`
	CX    float64 `json:"cx"`
	CY    float64 `json:"cy"`
}

// TimelineMarker marks BEFORE / AUTO / AFTER windows on the strip.
type TimelineMarker struct {
	Role      string `json:"role"` // BEFORE | AUTO | AFTER
	StartTime int    `json:"start_time"`
	EndTime   int    `json:"end_time"`
}

// EvidenceSummary is a compact human-facing evidence view.
type EvidenceSummary struct {
	Types           []string `json:"types,omitempty"`
	Descriptions    []string `json:"descriptions,omitempty"`
	GroupedRatio    float64  `json:"grouped_ratio,omitempty"`
	Spread          float64  `json:"spread,omitempty"`
	MainGroupRegion string   `json:"main_group_region,omitempty"`
}

// GroupingSummary mirrors BehaviorSummary for the UI.
type GroupingSummary struct {
	GroupedRatio    float64 `json:"grouped_ratio"`
	Spread          float64 `json:"spread"`
	MainGroupRegion string  `json:"main_group_region"`
}

// LaneSummary is a coarse lane presence hint from evidence/region names.
type LaneSummary struct {
	PrimaryLane string   `json:"primary_lane,omitempty"`
	Mentioned   []string `json:"mentioned,omitempty"`
}

// AuditCase is one review unit for the Behavior Audit UI.
type AuditCase struct {
	ID string `json:"id"`

	MatchID string `json:"match_id"`
	Side    string `json:"side"`

	StartGameTime   int     `json:"start_game_time"`
	EndGameTime     int     `json:"end_game_time"`
	DurationSeconds float64 `json:"duration_seconds"`

	AutoStatus       macrolabel.LabelStatus    `json:"auto_status"`
	AutoAction       *action.MacroAction       `json:"auto_action,omitempty"`
	AutoConfidence   *float64                  `json:"auto_confidence,omitempty"`
	AmbiguityReason  macrolabel.AmbiguityReason `json:"ambiguity_reason,omitempty"`
	Competing        []macrolabel.CompetingCandidate `json:"competing,omitempty"`
	RejectedCandidates []macrolabel.RejectedCandidate `json:"rejected_candidates,omitempty"`

	EvidenceSummary EvidenceSummary  `json:"evidence_summary"`
	Timeline        []TimelineMarker `json:"timeline_markers"`
	Trajectories    []TrajectoryPoint `json:"trajectories,omitempty"`
	EnemyMarkers    []TrajectoryPoint `json:"enemy_markers,omitempty"`
	Grouping        *GroupingSummary `json:"grouping_summary,omitempty"`
	Lane            *LaneSummary     `json:"lane_summary,omitempty"`
	MapRegions      []MapRegionMark  `json:"map_regions,omitempty"`

	Definition string `json:"definition,omitempty"`

	Review *HumanReview `json:"review,omitempty"`

	BehaviorAuditSchemaVersion string `json:"behavior_audit_schema_version"`
	BehaviorReviewerVersion    string `json:"behavior_reviewer_version"`
	LabelerVersion             string `json:"labeler_version,omitempty"`
	MacroActionSchemaVersion   string `json:"macro_action_schema_version,omitempty"`
	SampleSeed                 int64  `json:"sample_seed"`

	SourceMode macrolabel.SourceMode `json:"source_mode,omitempty"`
	SmokeAnchorID string             `json:"smoke_anchor_id,omitempty"`

	// RevealAlts is set by the API for LABELED cases after REJECTED (or ?reveal=1).
	RevealAlts bool `json:"reveal_alts,omitempty"`
}