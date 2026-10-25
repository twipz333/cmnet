package behavioraudit

// ReviewStatus is the human judgment on an audit case.
type ReviewStatus string

const (
	ReviewConfirmed     ReviewStatus = "CONFIRMED"
	ReviewRejected      ReviewStatus = "REJECTED"
	ReviewUnclear       ReviewStatus = "UNCLEAR"
	ReviewNoClearAction ReviewStatus = "NO_CLEAR_ACTION"
	ReviewSkipped       ReviewStatus = "SKIPPED"
	ReviewBothOK        ReviewStatus = "BOTH_OK"
	ReviewNeither       ReviewStatus = "NEITHER"
)

// HumanConfidence is subjective certainty of the review.
type HumanConfidence string

const (
	ConfidenceHigh   HumanConfidence = "HIGH"
	ConfidenceMedium HumanConfidence = "MEDIUM"
	ConfidenceLow    HumanConfidence = "LOW"
)

// ReviewReason explains why auto label was rejected or left unclear.
type ReviewReason string

const (
	ReasonTaxonomyGap          ReviewReason = "TAXONOMY_GAP"
	ReasonTaxonomyOverlap      ReviewReason = "TAXONOMY_OVERLAP"
	ReasonUnclearObservability ReviewReason = "UNCLEAR_OBSERVABILITY"
	ReasonOther                ReviewReason = "OTHER"
)

// HumanReview is the persisted human judgment for one AuditCase.
type HumanReview struct {
	Status           ReviewStatus    `json:"status"`
	AcceptedAuto     bool            `json:"accepted_auto"`
	HumanActionID    *int            `json:"human_action_id,omitempty"`
	HumanAltActionIDs []int          `json:"human_alt_action_ids,omitempty"`
	Confidence       HumanConfidence `json:"confidence,omitempty"`
	Reason           ReviewReason    `json:"reason,omitempty"`
	Note             string          `json:"note,omitempty"`
	ReviewedAt       string          `json:"reviewed_at,omitempty"`
}

func (r *HumanReview) IsReviewed() bool {
	if r == nil {
		return false
	}
	switch r.Status {
	case ReviewConfirmed, ReviewRejected, ReviewUnclear, ReviewNoClearAction,
		ReviewSkipped, ReviewBothOK, ReviewNeither:
		return true
	default:
		return false
	}
}
