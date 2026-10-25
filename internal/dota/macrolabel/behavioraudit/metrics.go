package behavioraudit

import (
	"fmt"
	"sort"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

// BehaviorReport summarizes human behavior-audit reviews.
type BehaviorReport struct {
	TotalCases    int  `json:"total_cases"`
	ReviewedCount int  `json:"reviewed_count"`
	PendingCount  int  `json:"pending_count"`
	Incomplete    bool `json:"incomplete"`

	Confirmed int `json:"confirmed"`
	Rejected  int `json:"rejected"`
	Unclear   int `json:"unclear"`

	// BehaviorPrecision = CONFIRMED/(CONFIRMED+REJECTED); UNCLEAR excluded.
	BehaviorPrecision float64 `json:"behavior_precision"`

	PerActionPrecision map[string]float64      `json:"per_action_precision,omitempty"`
	PerActionCounts    map[string]ActionCounts `json:"per_action_counts,omitempty"`

	ConfidenceWeightedPrecision float64 `json:"confidence_weighted_precision"`

	UnlabeledRecoveryRate float64 `json:"unlabeled_recovery_rate"`
	RejectedTop1Recovery  float64 `json:"rejected_top1_recovery"`
	RejectedTop3Recovery  float64 `json:"rejected_top3_recovery"`

	AmbiguityChoseA  int `json:"ambiguity_chose_a"`
	AmbiguityChoseB  int `json:"ambiguity_chose_b"`
	AmbiguityBothOK  int `json:"ambiguity_both_ok"`
	AmbiguityNeither int `json:"ambiguity_neither"`
	AmbiguityUnclear int `json:"ambiguity_unclear"`

	TaxonomyGapRate          float64 `json:"taxonomy_gap_rate"`
	TaxonomyOverlapRate      float64 `json:"taxonomy_overlap_rate"`
	UnclearObservabilityRate float64 `json:"unclear_observability_rate"`

	CoverageNote    string   `json:"coverage_note"`
	CorrectnessNote string   `json:"correctness_note"`

	RecommendedNextInvestigation []string `json:"recommended_next_investigation,omitempty"`
}

// ActionCounts holds confirmed/rejected for one auto action.
type ActionCounts struct {
	Confirmed int `json:"confirmed"`
	Rejected  int `json:"rejected"`
}

// BuildReport computes BehaviorReport from reviewed cases.
func BuildReport(cases []AuditCase) BehaviorReport {
	r := BehaviorReport{
		TotalCases:         len(cases),
		PerActionPrecision: map[string]float64{},
		PerActionCounts:    map[string]ActionCounts{},
	}

	var confirmed, rejected, unclear int
	var confConfirmed, confRejected int
	var unlabTotal, unlabRecovered int
	var top1Hit, top1Denom, top3Hit, top3Denom int
	var reasonGap, reasonOverlap, reasonObs, reasonDenom int
	var reviewed int

	for _, c := range cases {
		if c.Review == nil || !c.Review.IsReviewed() {
			continue
		}
		reviewed++
		rev := c.Review
		if rev.Status == ReviewSkipped {
			continue
		}

		weighted := rev.Confidence == ConfidenceHigh || rev.Confidence == ConfidenceMedium || rev.Confidence == ""

		switch rev.Status {
		case ReviewConfirmed:
			confirmed++
			bumpAction(&r, c, true)
			if weighted {
				confConfirmed++
			}
		case ReviewRejected:
			rejected++
			bumpAction(&r, c, false)
			if weighted {
				confRejected++
			}
			tallyReason(rev.Reason, &reasonGap, &reasonOverlap, &reasonObs, &reasonDenom)
		case ReviewUnclear:
			unclear++
		case ReviewBothOK:
			r.AmbiguityBothOK++
		case ReviewNeither:
			r.AmbiguityNeither++
			tallyReason(rev.Reason, &reasonGap, &reasonOverlap, &reasonObs, &reasonDenom)
		}

		if c.AutoStatus == macrolabel.LabelStatusAmbiguous && len(c.Competing) >= 2 {
			switch rev.Status {
			case ReviewConfirmed, ReviewRejected:
				if rev.HumanActionID != nil {
					if *rev.HumanActionID == c.Competing[0].ActionID {
						r.AmbiguityChoseA++
					} else if *rev.HumanActionID == c.Competing[1].ActionID {
						r.AmbiguityChoseB++
					}
				}
			case ReviewUnclear:
				r.AmbiguityUnclear++
			}
		}

		if c.AutoStatus == macrolabel.LabelStatusUnlabeled {
			unlabTotal++
			if rev.HumanActionID != nil {
				unlabRecovered++
			}
		}

		if rev.HumanActionID != nil && len(c.RejectedCandidates) > 0 {
			sorted := append([]macrolabel.RejectedCandidate{}, c.RejectedCandidates...)
			sort.SliceStable(sorted, func(i, j int) bool {
				if sorted[i].Confidence != sorted[j].Confidence {
					return sorted[i].Confidence > sorted[j].Confidence
				}
				return sorted[i].ActionID < sorted[j].ActionID
			})
			hid := *rev.HumanActionID
			top1Denom++
			if len(sorted) > 0 && sorted[0].ActionID == hid {
				top1Hit++
			}
			top3Denom++
			for i := 0; i < len(sorted) && i < 3; i++ {
				if sorted[i].ActionID == hid {
					top3Hit++
					break
				}
			}
		}
	}

	r.ReviewedCount = reviewed
	r.PendingCount = r.TotalCases - reviewed
	r.Incomplete = r.PendingCount > 0
	r.Confirmed = confirmed
	r.Rejected = rejected
	r.Unclear = unclear

	denom := confirmed + rejected
	if denom > 0 {
		r.BehaviorPrecision = float64(confirmed) / float64(denom)
	}
	cwDenom := confConfirmed + confRejected
	if cwDenom > 0 {
		r.ConfidenceWeightedPrecision = float64(confConfirmed) / float64(cwDenom)
	}
	if unlabTotal > 0 {
		r.UnlabeledRecoveryRate = float64(unlabRecovered) / float64(unlabTotal)
	}
	if top1Denom > 0 {
		r.RejectedTop1Recovery = float64(top1Hit) / float64(top1Denom)
	}
	if top3Denom > 0 {
		r.RejectedTop3Recovery = float64(top3Hit) / float64(top3Denom)
	}
	if reasonDenom > 0 {
		r.TaxonomyGapRate = float64(reasonGap) / float64(reasonDenom)
		r.TaxonomyOverlapRate = float64(reasonOverlap) / float64(reasonDenom)
		r.UnclearObservabilityRate = float64(reasonObs) / float64(reasonDenom)
	}

	for key, ac := range r.PerActionCounts {
		d := ac.Confirmed + ac.Rejected
		if d > 0 {
			r.PerActionPrecision[key] = float64(ac.Confirmed) / float64(d)
		}
	}

	r.CoverageNote = fmt.Sprintf("coverage is separate: reviewed %d/%d cases; pending=%d", reviewed, r.TotalCases, r.PendingCount)
	r.CorrectnessNote = fmt.Sprintf("correctness (behavior precision) uses only CONFIRMED+REJECTED=%d; UNCLEAR=%d excluded", denom, unclear)
	r.RecommendedNextInvestigation = recommendNext(r)
	return r
}

func tallyReason(reason ReviewReason, gap, overlap, obs, denom *int) {
	if reason == "" {
		return
	}
	*denom++
	switch reason {
	case ReasonTaxonomyGap:
		*gap++
	case ReasonTaxonomyOverlap:
		*overlap++
	case ReasonUnclearObservability:
		*obs++
	}
}

func bumpAction(r *BehaviorReport, c AuditCase, confirmed bool) {
	key := "UNLABELED"
	if c.AutoAction != nil {
		key = c.AutoAction.String()
	} else if c.AutoStatus == macrolabel.LabelStatusAmbiguous && len(c.Competing) > 0 {
		if a, ok := action.DefaultSpaceV0().ByID(c.Competing[0].ActionID); ok {
			key = "AMB:" + a.String()
		}
	}
	ac := r.PerActionCounts[key]
	if confirmed {
		ac.Confirmed++
	} else {
		ac.Rejected++
	}
	r.PerActionCounts[key] = ac
}

func recommendNext(r BehaviorReport) []string {
	var out []string
	if r.Incomplete {
		out = append(out, fmt.Sprintf("Завершить ревью: осталось %d из %d", r.PendingCount, r.TotalCases))
	}
	if r.BehaviorPrecision < 0.7 && (r.Confirmed+r.Rejected) >= 10 {
		out = append(out, "Низкая behavior precision — разобрать топ REJECTED по kind/target")
	}
	if r.TaxonomyGapRate >= 0.2 && (r.Rejected+r.AmbiguityNeither) >= 5 {
		out = append(out, "Высокая доля TAXONOMY_GAP — проверить пробелы в ActionSpace / детекторах")
	}
	if r.TaxonomyOverlapRate >= 0.2 {
		out = append(out, "Высокая доля TAXONOMY_OVERLAP — уточнить границы kind/target")
	}
	if r.UnclearObservabilityRate >= 0.25 {
		out = append(out, "Частая UNCLEAR_OBSERVABILITY — усилить player-visible evidence / GSI покрытие")
	}
	if r.UnlabeledRecoveryRate >= 0.3 {
		out = append(out, "UNLABELED recovery заметный — проверить missed detectors / пороги")
	}
	if r.RejectedTop1Recovery >= 0.4 {
		out = append(out, "Rejected Top-1 часто совпадает с человеком — смотреть ranking/margin")
	}
	type pair struct {
		k string
		p float64
		n int
	}
	var low []pair
	for k, ac := range r.PerActionCounts {
		n := ac.Confirmed + ac.Rejected
		if n < 3 {
			continue
		}
		p := r.PerActionPrecision[k]
		if p < 0.6 {
			low = append(low, pair{k: k, p: p, n: n})
		}
	}
	sort.SliceStable(low, func(i, j int) bool {
		if low[i].p != low[j].p {
			return low[i].p < low[j].p
		}
		return low[i].k < low[j].k
	})
	for i := 0; i < len(low) && i < 3; i++ {
		out = append(out, fmt.Sprintf("Низкая precision у %s (%.0f%%, n=%d)", low[i].k, 100*low[i].p, low[i].n))
	}
	if len(out) == 0 {
		out = append(out, "Недостаточно сигнала для рекомендации — продолжить ревью")
	}
	return out
}