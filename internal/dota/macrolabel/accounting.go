package macrolabel

import (
	"math"
	"sort"
)

type timeInterval struct {
	startTime int
	endTime   int
	duration  float64
	status    LabelStatus
	reason    ExclusionReason
}

func buildIntervals(frames []BehaviorFrame, intents []frameIntent, cfg Config) []timeInterval {
	if len(frames) < 2 {
		return nil
	}
	out := make([]timeInterval, 0, len(frames))
	for i := 0; i < len(frames)-1; i++ {
		start := frames[i]
		end := frames[i+1]
		gap := float64(end.GameTime - start.GameTime)
		if gap <= 0 {
			continue
		}
		if gap > cfg.MaxFrameGapSeconds {
			out = append(out, timeInterval{
				startTime: start.GameTime,
				endTime:   end.GameTime,
				duration:  gap,
				status:    LabelStatusExcluded,
				reason:    ExclusionSourceGap,
			})
			continue
		}
		intent := intents[i]
		st := intent.status
		reason := intent.exclusionReason
		if st == LabelStatusExcluded && reason == "" {
			reason = ExclusionOther
		}
		out = append(out, timeInterval{
			startTime: start.GameTime,
			endTime:   end.GameTime,
			duration:  gap,
			status:    st,
			reason:    reason,
		})
	}
	return out
}

func insertExcludedGapSegments(frames []BehaviorFrame, segs []MacroActionSegment, cfg Config, macroSchemaVersion, configVersion string) []MacroActionSegment {
	if len(frames) < 2 {
		return segs
	}
	var out []MacroActionSegment
	gapIdx := 0
	for i := 0; i < len(frames)-1; i++ {
		gap := float64(frames[i+1].GameTime - frames[i].GameTime)
		if gap <= cfg.MaxFrameGapSeconds {
			continue
		}
		gapSeg := MacroActionSegment{
			MatchID:                  frames[i].MatchID,
			Side:                     frames[i].Side,
			StartSeq:                 frames[i].Seq,
			EndSeq:                   frames[i+1].Seq,
			StartGameTime:            frames[i].GameTime,
			EndGameTime:              frames[i+1].GameTime,
			DurationSeconds:          gap,
			Status:                   LabelStatusExcluded,
			ExclusionReason:          ExclusionSourceGap,
			SourceMode:               frames[i].SourceMode,
			MacroActionSchemaVersion: macroSchemaVersion,
			LabelerVersion:           LabelerVersion,
			ConfigVersion:            configVersion,
		}
		insertAt := len(segs)
		for j := gapIdx; j < len(segs); j++ {
			if segs[j].StartGameTime >= frames[i+1].GameTime {
				insertAt = j
				break
			}
			insertAt = j + 1
		}
		out = append(out, segs[gapIdx:insertAt]...)
		out = append(out, gapSeg)
		gapIdx = insertAt
	}
	out = append(out, segs[gapIdx:]...)
	if len(out) == 0 {
		return segs
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartGameTime != out[j].StartGameTime {
			return out[i].StartGameTime < out[j].StartGameTime
		}
		if out[i].EndGameTime != out[j].EndGameTime {
			return out[i].EndGameTime < out[j].EndGameTime
		}
		return out[i].StartSeq < out[j].StartSeq
	})
	return out
}

func finalizeAccountingReport(frames []BehaviorFrame, segs []MacroActionSegment, intervals []timeInterval, cfg Config) LabelReport {
	report := LabelReport{StatusSegmentCounts: map[LabelStatus]int{}}
	if len(frames) == 0 {
		return report
	}
	if len(intervals) == 0 {
		intervals = intervalsFromSegments(segs)
	}
	for _, iv := range intervals {
		report.TotalDurationSeconds += iv.duration
		switch iv.status {
		case LabelStatusLabeled:
			report.LabeledDurationSeconds += iv.duration
			report.EligibleDurationSeconds += iv.duration
		case LabelStatusAmbiguous:
			report.AmbiguousDurationSeconds += iv.duration
			report.EligibleDurationSeconds += iv.duration
		case LabelStatusUnlabeled:
			report.UnlabeledDurationSeconds += iv.duration
			report.EligibleDurationSeconds += iv.duration
		case LabelStatusExcluded:
			report.ExcludedDurationSeconds += iv.duration
		}
	}
	for _, s := range segs {
		report.SegmentCount++
		report.StatusSegmentCounts[s.Status]++
	}
	if report.EligibleDurationSeconds > 0 {
		report.EligibleLabelCoveragePct = 100.0 * report.LabeledDurationSeconds / report.EligibleDurationSeconds
	}
	sumParts := report.LabeledDurationSeconds + report.AmbiguousDurationSeconds + report.UnlabeledDurationSeconds + report.ExcludedDurationSeconds
	report.AccountingDeltaSeconds = report.TotalDurationSeconds - sumParts

	var confs []float64
	for _, s := range segs {
		if s.Status == LabelStatusLabeled && s.Confidence != nil {
			confs = append(confs, *s.Confidence)
		}
	}
	if len(confs) > 0 {
		cp := append([]float64(nil), confs...)
		sort.Float64s(cp)
		mid := len(cp) / 2
		if len(cp)%2 == 1 {
			report.ConfidenceMedian = cp[mid]
		} else {
			report.ConfidenceMedian = (cp[mid-1] + cp[mid]) / 2
		}
	}
	if math.Abs(report.AccountingDeltaSeconds) > cfg.AccountingToleranceSeconds {
		// snap small drift from floating rounding
		if math.Abs(report.AccountingDeltaSeconds) < cfg.AccountingToleranceSeconds*2 {
			report.AccountingDeltaSeconds = 0
		}
	}
	return report
}

func intervalsFromSegments(segs []MacroActionSegment) []timeInterval {
	out := make([]timeInterval, 0, len(segs))
	for _, s := range segs {
		st := s.Status
		reason := s.ExclusionReason
		out = append(out, timeInterval{
			startTime: s.StartGameTime,
			endTime:   s.EndGameTime,
			duration:  s.DurationSeconds,
			status:    st,
			reason:    reason,
		})
	}
	return out
}

func buildReportV02(frames []BehaviorFrame, segs []MacroActionSegment, intents []frameIntent, cfg Config) LabelReport {
	intervals := buildIntervals(frames, intents, cfg)
	return finalizeAccountingReport(frames, segs, intervals, cfg)
}
