package macrolabel

import (
	"fmt"
	"sort"

	"cmnet/internal/dota/action"
)

type frameIntent struct {
	status          LabelStatus
	best            *Candidate
	top2            []Candidate
	ambiguityReason AmbiguityReason
	exclusionReason ExclusionReason
	rejected        []RejectedCandidate
	diagnostics     *ResolverDiagnostics
}

// MacroActionLabeler v0.2 performs behavioral labeling over a trajectory.
type MacroActionLabeler struct {
	space ActionSpaceProvider
	cfg   Config
}

// ActionSpaceProvider keeps labeler free from action implementation details.
type ActionSpaceProvider interface {
	SchemaVersion() string
	Validate(kind action.ActionKind, target action.ActionTarget) error
	New(kind action.ActionKind, target action.ActionTarget) (action.MacroAction, error)
	ByID(id int) (action.MacroAction, bool)
	IDOf(kind action.ActionKind, target action.ActionTarget) (int, bool)
}

// Label runs over the whole trajectory and returns labeled segments.
func (l MacroActionLabeler) Label(frames []BehaviorFrame) ([]MacroActionSegment, LabelReport, error) {
	if len(frames) == 0 {
		return nil, LabelReport{}, nil
	}
	if l.space == nil {
		return nil, LabelReport{}, fmt.Errorf("nil action space provider")
	}

	macroSchemaVersion := l.space.SchemaVersion()
	if macroSchemaVersion != action.SchemaVersion {
		return nil, LabelReport{}, fmt.Errorf(
			"macro-action schema mismatch: got %q want %q",
			macroSchemaVersion,
			action.SchemaVersion,
		)
	}

	smokeTimes := DedupeSmokeActivationTimes(frames)

	frameIntents := make([]frameIntent, len(frames))
	for i := range frames {
		if excluded, reason := frameExclusion(frames[i]); excluded {
			frameIntents[i] = frameIntent{status: LabelStatusExcluded, exclusionReason: reason}
			continue
		}
		cands := l.detectCandidates(frames, i, smokeTimes)
		frameIntents[i] = resolveIntentV02(l.cfg, cands)
	}

	segs := segmentFromIntents(frames, frameIntents, l.cfg, macroSchemaVersion, l.cfg.ConfigVersion)
	segs = insertExcludedGapSegments(frames, segs, l.cfg, macroSchemaVersion, l.cfg.ConfigVersion)
	segs = postprocessSegments(segs, l.cfg)
	anchors := detectSmokeAnchors(frames)
	segs = assignSmokeAnchorIDs(segs, anchors, l.space)

	report := buildReportV02(frames, segs, frameIntents, l.cfg)
	return segs, report, nil
}

func frameExclusion(f BehaviorFrame) (bool, ExclusionReason) {
	if f.GameTime < 0 {
		return true, ExclusionPreGame
	}
	if f.GameTime > 7200 {
		return true, ExclusionPostGame
	}
	return false, ""
}

func resolveIntentV02(cfg Config, cands []Candidate) frameIntent {
	intent := frameIntent{status: LabelStatusUnlabeled}
	if len(cands) == 0 {
		return intent
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Confidence != cands[j].Confidence {
			return cands[i].Confidence > cands[j].Confidence
		}
		return cands[i].Action.ID < cands[j].Action.ID
	})

	best := cands[0]
	diag := &ResolverDiagnostics{
		CandidateCount:         len(cands),
		TopConfidence:          best.Confidence,
		MinConfidenceThreshold: cfg.MinConfidenceToLabel,
		AmbiguityMargin:        cfg.AmbiguityMargin,
	}

	if best.Confidence < cfg.MinConfidenceToLabel {
		intent.status = LabelStatusUnlabeled
		intent.rejected = buildRejectedCandidates(cands, cfg, "below_threshold")
		diag.Resolution = "UNLABELED"
		intent.diagnostics = diag
		return intent
	}

	if len(cands) >= 2 {
		second := cands[1]
		diag.SecondConfidence = second.Confidence
		diag.Margin = best.Confidence - second.Confidence
		if second.Confidence >= cfg.MinConfidenceToLabel && diag.Margin <= cfg.AmbiguityMargin {
			intent.status = LabelStatusAmbiguous
			intent.best = &best
			intent.top2 = append([]Candidate(nil), cands[:2]...)
			intent.ambiguityReason = classifyAmbiguity(best, second, cands)
			intent.rejected = buildRejectedCandidates(cands[2:], cfg, "not_top_two")
			diag.Resolution = "AMBIGUOUS"
			intent.diagnostics = diag
			return intent
		}
	}

	intent.status = LabelStatusLabeled
	intent.best = &best
	n := minInt(2, len(cands))
	intent.top2 = append([]Candidate(nil), cands[:n]...)
	intent.rejected = buildRejectedCandidates(cands[1:], cfg, "not_top")
	diag.Resolution = "LABELED"
	intent.diagnostics = diag
	return intent
}

func classifyAmbiguity(best, second Candidate, all []Candidate) AmbiguityReason {
	if best.Confidence-second.Confidence <= 0.01 {
		return AmbiguityCloseScores
	}
	if best.Action.Kind == second.Action.Kind && best.Action.Target != second.Action.Target {
		return AmbiguityMultipleSpecific
	}
	if best.Action.Kind != second.Action.Kind {
		return AmbiguityConflictingEvidence
	}
	smokeTargets := 0
	for _, c := range all {
		if c.Action.Kind == action.KindSmoke && c.Confidence >= 0.5 {
			smokeTargets++
		}
	}
	if smokeTargets >= 2 {
		return AmbiguityMultipleTargets
	}
	if len(all) >= 3 {
		return AmbiguityMultipleSpecific
	}
	return AmbiguityInsufficientMargin
}

func buildRejectedCandidates(cands []Candidate, cfg Config, reason string) []RejectedCandidate {
	if len(cands) == 0 {
		return nil
	}
	limit := cfg.MaxRejectedCandidates
	if limit <= 0 {
		limit = 5
	}
	if len(cands) > limit {
		cands = cands[:limit]
	}
	out := make([]RejectedCandidate, 0, len(cands))
	for _, c := range cands {
		out = append(out, RejectedCandidate{
			ActionID:      c.Action.ID,
			Confidence:    c.Confidence,
			Reason:        reason,
			EvidenceTypes: evidenceTypes(c.Evidence),
		})
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (l MacroActionLabeler) detectCandidates(frames []BehaviorFrame, at int, smokeTimes []int) []Candidate {
	return detectCandidatesV0(l.cfg, l.space, frames, at, smokeTimes)
}

func segmentFromIntents(
	frames []BehaviorFrame,
	intents []frameIntent,
	cfg Config,
	macroSchemaVersion string,
	configVersion string,
) []MacroActionSegment {
	var segs []MacroActionSegment
	curStart := 0
	for i := 1; i <= len(frames); i++ {
		end := i
		if i == len(frames) || !sameIntent(frames, intents, end-1, end, cfg) {
			seg := buildSegment(frames, intents, curStart, end-1, macroSchemaVersion, configVersion)
			segs = append(segs, seg)
			curStart = i
		}
	}
	return segs
}

func sameIntent(frames []BehaviorFrame, intents []frameIntent, a, b int, cfg Config) bool {
	ia := intents[a]
	if b >= len(intents) {
		return false
	}
	ib := intents[b]
	if ia.status != ib.status {
		return false
	}
	if ia.status == LabelStatusExcluded {
		return ia.exclusionReason == ib.exclusionReason
	}
	if ia.status == LabelStatusAmbiguous {
		return ia.ambiguityReason == ib.ambiguityReason
	}
	if ia.status == LabelStatusLabeled {
		if ia.best == nil || ib.best == nil {
			return false
		}
		return ia.best.Action.ID == ib.best.Action.ID
	}
	return true
}

func buildSegment(
	frames []BehaviorFrame,
	intents []frameIntent,
	lo, hi int,
	macroSchemaVersion string,
	configVersion string,
) MacroActionSegment {
	start := frames[lo]
	end := frames[hi]
	intent := intents[lo]
	seg := MacroActionSegment{
		MatchID: start.MatchID,
		Side:    start.Side,

		StartSeq: start.Seq,
		EndSeq:   end.Seq,

		StartGameTime:   start.GameTime,
		EndGameTime:     end.GameTime,
		DurationSeconds: float64(end.GameTime - start.GameTime),

		Status:                   intent.status,
		AmbiguityReason:          intent.ambiguityReason,
		ExclusionReason:          intent.exclusionReason,
		RejectedCandidates:       append([]RejectedCandidate(nil), intent.rejected...),
		ResolverDiagnostics:      intent.diagnostics,
		SourceMode:               start.SourceMode,
		MacroActionSchemaVersion: macroSchemaVersion,
		LabelerVersion:           LabelerVersion,
		ConfigVersion:            configVersion,
		BehaviorSummary: &BehaviorSummary{
			GroupedRatio:    start.Team.GroupedRatio,
			Spread:          start.Team.Spread,
			MainGroupRegion: string(start.Team.MainGroupRegion),
		},
	}
	if intent.status == LabelStatusLabeled && intent.best != nil {
		a := intent.best.Action
		seg.Action = &a
		seg.Confidence = &intent.best.Confidence
		seg.Evidence = intent.best.Evidence
	}
	if intent.status == LabelStatusAmbiguous {
		if intent.ambiguityReason == "" {
			seg.AmbiguityReason = AmbiguityOther
		}
		for _, c := range intent.top2 {
			gap := 0.0
			if intent.best != nil {
				gap = intent.best.Confidence - c.Confidence
			}
			seg.Competing = append(seg.Competing, CompetingCandidate{
				ActionID:      c.Action.ID,
				Confidence:    c.Confidence,
				EvidenceTypes: evidenceTypes(c.Evidence),
				ScoreGap:      gap,
			})
		}
	}
	if intent.status == LabelStatusUnlabeled {
		seg.RejectedCandidates = append(seg.RejectedCandidates, intent.rejected...)
	}
	return seg
}

func evidenceTypes(in []Evidence) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, e := range in {
		out = append(out, e.Type)
	}
	sort.Strings(out)
	return out
}

func postprocessSegments(segs []MacroActionSegment, cfg Config) []MacroActionSegment {
	for i := range segs {
		if segs[i].Status != LabelStatusLabeled {
			continue
		}
		if segs[i].DurationSeconds < cfg.MinimumActionDurationSeconds {
			segs[i].Status = LabelStatusUnlabeled
			segs[i].Action = nil
			segs[i].Confidence = nil
			segs[i].Evidence = nil
			segs[i].Competing = nil
			segs[i].AmbiguityReason = ""
		}
	}
	for i := 0; i+2 < len(segs); i++ {
		if segs[i].Status == LabelStatusLabeled && segs[i+1].Status == LabelStatusUnlabeled && segs[i+2].Status == LabelStatusLabeled {
			if segs[i].Action != nil && segs[i+2].Action != nil && segs[i].Action.ID == segs[i+2].Action.ID {
				if segs[i+1].DurationSeconds <= cfg.MaxUnlabeledGapSeconds {
					segs[i].EndSeq = segs[i+2].EndSeq
					segs[i].EndGameTime = segs[i+2].EndGameTime
					segs[i].DurationSeconds = float64(segs[i].EndGameTime - segs[i].StartGameTime)
					segs = append(segs[:i+1], segs[i+3:]...)
					i--
				}
			}
		}
	}
	return segs
}
