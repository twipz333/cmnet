package macrolabel

import (
	"fmt"

	"cmnet/internal/dota/action"
)

type BehaviorSummary struct {
	GroupedRatio    float64 `json:"grouped_ratio"`
	Spread          float64 `json:"spread"`
	MainGroupRegion string  `json:"main_group_region"`
}

// MacroActionSegment is labeled temporal segment.
type MacroActionSegment struct {
	MatchID string `json:"match_id"`
	Side    string `json:"side"`

	StartSeq uint64 `json:"start_seq"`
	EndSeq   uint64 `json:"end_seq"`

	StartGameTime   int     `json:"start_game_time"`
	EndGameTime     int     `json:"end_game_time"`
	DurationSeconds float64 `json:"duration_seconds"`

	Status LabelStatus `json:"status"`

	Action     *action.MacroAction `json:"action,omitempty"`
	Confidence *float64            `json:"confidence,omitempty"`

	Evidence  []Evidence           `json:"evidence,omitempty"`
	Competing []CompetingCandidate `json:"competing_candidates,omitempty"`

	AmbiguityReason     AmbiguityReason      `json:"ambiguity_reason,omitempty"`
	ExclusionReason     ExclusionReason      `json:"exclusion_reason,omitempty"`
	RejectedCandidates  []RejectedCandidate  `json:"rejected_candidates,omitempty"`
	ResolverDiagnostics *ResolverDiagnostics `json:"resolver_diagnostics,omitempty"`
	BehaviorSummary     *BehaviorSummary     `json:"behavior_summary,omitempty"`
	SmokeAnchorID       string               `json:"smoke_anchor_id,omitempty"`

	SourceMode SourceMode `json:"source_mode"`

	MacroActionSchemaVersion string `json:"macro_action_schema_version"`
	LabelerVersion           string `json:"labeler_version"`
	ConfigVersion            string `json:"config_version"`
}

func (s MacroActionSegment) String() string {
	if s.Status == LabelStatusLabeled && s.Action != nil && s.Confidence != nil {
		return fmt.Sprintf("%s %d-%d %s conf=%.3f", s.Status, s.StartGameTime, s.EndGameTime, s.Action.String(), *s.Confidence)
	}
	if s.Status == LabelStatusExcluded && s.ExclusionReason != "" {
		return fmt.Sprintf("%s %d-%d reason=%s", s.Status, s.StartGameTime, s.EndGameTime, s.ExclusionReason)
	}
	return fmt.Sprintf("%s %d-%d", s.Status, s.StartGameTime, s.EndGameTime)
}

// LabelReport is a diagnostic snapshot produced by labeler.
type LabelReport struct {
	TotalDurationSeconds     float64 `json:"total_duration_seconds"`
	LabeledDurationSeconds   float64 `json:"labeled_duration_seconds"`
	AmbiguousDurationSeconds float64 `json:"ambiguous_duration_seconds"`
	UnlabeledDurationSeconds float64 `json:"unlabeled_duration_seconds"`

	ExcludedDurationSeconds  float64 `json:"excluded_duration_seconds"`
	EligibleDurationSeconds  float64 `json:"eligible_duration_seconds"`
	EligibleLabelCoveragePct float64 `json:"eligible_label_coverage_pct"`
	AccountingDeltaSeconds   float64 `json:"accounting_delta_seconds"`

	SegmentCount        int                 `json:"segment_count"`
	ConfidenceMedian    float64             `json:"confidence_median"`
	StatusSegmentCounts map[LabelStatus]int `json:"status_segment_counts,omitempty"`
}
