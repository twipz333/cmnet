package macrolabel

import (
	"cmnet/internal/dota/action"
)

// TemporalRole tags which temporal window produced a piece of evidence.
type TemporalRole string

const (
	TemporalRolePast    TemporalRole = "PAST"
	TemporalRoleCurrent TemporalRole = "CURRENT"
	TemporalRoleFuture  TemporalRole = "FUTURE"
)

// Evidence is machine-readable explanation for candidate scoring.
type Evidence struct {
	Type         string       `json:"type"`
	Weight       float64      `json:"weight,omitempty"`
	Description  string       `json:"description,omitempty"`
	Observed     string       `json:"observed,omitempty"`
	Value        *float64     `json:"value,omitempty"`
	TemporalRole TemporalRole `json:"temporal_role,omitempty"`
}

// Candidate is one possible MacroAction with evidence.
type Candidate struct {
	Action        action.MacroAction `json:"action"`
	EvidenceScore float64            `json:"evidence_score"`
	Confidence    float64            `json:"confidence"`
	Evidence      []Evidence         `json:"evidence"`
}

type CompetingCandidate struct {
	ActionID      int      `json:"action_id"`
	Confidence    float64  `json:"confidence"`
	EvidenceTypes []string `json:"evidence_types,omitempty"`
	ScoreGap      float64  `json:"score_gap,omitempty"`
}

type RejectedCandidate struct {
	ActionID      int      `json:"action_id"`
	Confidence    float64  `json:"confidence"`
	Reason        string   `json:"reason"`
	EvidenceTypes []string `json:"evidence_types,omitempty"`
}

type ResolverDiagnostics struct {
	CandidateCount         int     `json:"candidate_count"`
	TopConfidence          float64 `json:"top_confidence"`
	SecondConfidence       float64 `json:"second_confidence,omitempty"`
	Margin                 float64 `json:"margin,omitempty"`
	MinConfidenceThreshold float64 `json:"min_confidence_threshold"`
	AmbiguityMargin        float64 `json:"ambiguity_margin"`
	Resolution             string  `json:"resolution"`
}
