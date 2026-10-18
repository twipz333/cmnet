package behavioraudit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
	"cmnet/internal/dota/worldmap"
)

// TrackSample is optional ally/enemy position sample from session replay.
type TrackSample struct {
	GameTime int
	Allies   []TrajectoryPoint
	Enemies  []TrajectoryPoint // VisibleNow or last-seen markers
}

// MaterializeConfig controls case building.
type MaterializeConfig struct {
	Seed           int64
	PadSeconds     int // BEFORE/AFTER pad; default 10
	TrackByTime    map[int]TrackSample
	Geometry       *worldmap.Geometry
}

// MaterializeCases builds AuditCase list from sampled segments.
// Trajectories are filled from TrackByTime when available; otherwise cases still build from segment evidence.
func MaterializeCases(segs []macrolabel.MacroActionSegment, cfg MaterializeConfig) []AuditCase {
	if cfg.PadSeconds <= 0 {
		cfg.PadSeconds = 10
	}
	out := make([]AuditCase, 0, len(segs))
	for _, s := range segs {
		out = append(out, materializeOne(s, cfg))
	}
	return out
}

func materializeOne(s macrolabel.MacroActionSegment, cfg MaterializeConfig) AuditCase {
	id := CaseID(s)
	evSum := buildEvidenceSummary(s)
	var grouping *GroupingSummary
	if s.BehaviorSummary != nil {
		grouping = &GroupingSummary{
			GroupedRatio:    s.BehaviorSummary.GroupedRatio,
			Spread:          s.BehaviorSummary.Spread,
			MainGroupRegion: s.BehaviorSummary.MainGroupRegion,
		}
	}
	lane := buildLaneSummary(s)
	regions := buildMapRegions(s, cfg.Geometry)

	def := ""
	if s.Action != nil {
		def = HumanDefinition(s.Action.Kind, s.Action.Target)
	}

	pad := cfg.PadSeconds
	beforeStart := s.StartGameTime - pad
	afterEnd := s.EndGameTime + pad
	timeline := []TimelineMarker{
		{Role: "BEFORE", StartTime: beforeStart, EndTime: s.StartGameTime},
		{Role: "AUTO", StartTime: s.StartGameTime, EndTime: s.EndGameTime},
		{Role: "AFTER", StartTime: s.EndGameTime, EndTime: afterEnd},
	}

	var traj, enemies []TrajectoryPoint
	if cfg.TrackByTime != nil {
		traj, enemies = collectTrajectories(cfg.TrackByTime, beforeStart, afterEnd)
	}

	c := AuditCase{
		ID:                         id,
		MatchID:                    s.MatchID,
		Side:                       s.Side,
		StartGameTime:              s.StartGameTime,
		EndGameTime:                s.EndGameTime,
		DurationSeconds:            s.DurationSeconds,
		AutoStatus:                 s.Status,
		AutoAction:                 s.Action,
		AutoConfidence:             s.Confidence,
		AmbiguityReason:            s.AmbiguityReason,
		Competing:                  s.Competing,
		RejectedCandidates:         s.RejectedCandidates,
		EvidenceSummary:            evSum,
		Timeline:                   timeline,
		Trajectories:               traj,
		EnemyMarkers:               enemies,
		Grouping:                   grouping,
		Lane:                       lane,
		MapRegions:                 regions,
		Definition:                 def,
		BehaviorAuditSchemaVersion: BehaviorAuditSchemaVersion,
		BehaviorReviewerVersion:    BehaviorReviewerVersion,
		LabelerVersion:             s.LabelerVersion,
		MacroActionSchemaVersion:   s.MacroActionSchemaVersion,
		SampleSeed:                 cfg.Seed,
		SourceMode:                 s.SourceMode,
		SmokeAnchorID:              s.SmokeAnchorID,
	}
	return c
}

// CaseID is a stable id for an audit case (same recipe as audit.AuditID).
func CaseID(seg macrolabel.MacroActionSegment) string {
	raw := fmt.Sprintf("%s|%s|%d|%d|%s", seg.MatchID, seg.Side, seg.StartGameTime, seg.EndGameTime, seg.Status)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}

func buildEvidenceSummary(s macrolabel.MacroActionSegment) EvidenceSummary {
	types := make([]string, 0, len(s.Evidence))
	descs := make([]string, 0, len(s.Evidence))
	seenT := map[string]struct{}{}
	for _, ev := range s.Evidence {
		if ev.Type != "" {
			if _, ok := seenT[ev.Type]; !ok {
				seenT[ev.Type] = struct{}{}
				types = append(types, ev.Type)
			}
		}
		if ev.Description != "" {
			descs = append(descs, ev.Description)
		} else if ev.Observed != "" {
			descs = append(descs, ev.Observed)
		}
		if len(descs) >= 8 {
			break
		}
	}
	sum := EvidenceSummary{Types: types, Descriptions: descs}
	if s.BehaviorSummary != nil {
		sum.GroupedRatio = s.BehaviorSummary.GroupedRatio
		sum.Spread = s.BehaviorSummary.Spread
		sum.MainGroupRegion = s.BehaviorSummary.MainGroupRegion
	}
	return sum
}

func buildLaneSummary(s macrolabel.MacroActionSegment) *LaneSummary {
	mentioned := map[string]struct{}{}
	add := func(t string) {
		u := strings.ToUpper(t)
		for _, lane := range []string{"TOP", "MID", "BOT"} {
			if strings.Contains(u, lane) {
				mentioned[lane] = struct{}{}
			}
		}
	}
	if s.Action != nil {
		add(string(s.Action.Target))
	}
	if s.BehaviorSummary != nil {
		add(s.BehaviorSummary.MainGroupRegion)
	}
	for _, ev := range s.Evidence {
		add(ev.Type)
		add(ev.Description)
		add(ev.Observed)
	}
	if len(mentioned) == 0 {
		return nil
	}
	order := []string{"TOP", "MID", "BOT"}
	ls := &LaneSummary{}
	for _, l := range order {
		if _, ok := mentioned[l]; ok {
			ls.Mentioned = append(ls.Mentioned, l)
		}
	}
	if len(ls.Mentioned) > 0 {
		ls.PrimaryLane = ls.Mentioned[0]
	}
	return ls
}

func buildMapRegions(s macrolabel.MacroActionSegment, geo *worldmap.Geometry) []MapRegionMark {
	var ids []string
	if s.BehaviorSummary != nil && s.BehaviorSummary.MainGroupRegion != "" {
		ids = append(ids, s.BehaviorSummary.MainGroupRegion)
	}
	if s.Action != nil {
		ids = append(ids, string(s.Action.Target))
	}
	out := make([]MapRegionMark, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		mark := MapRegionMark{ID: id, Label: id}
		if geo != nil {
			rid := worldmap.RegionID(id)
			if poly, ok := geo.RegionPolygon(rid); ok && len(poly) > 0 {
				var sx, sy float64
				for _, p := range poly {
					sx += p.X
					sy += p.Y
				}
				n := float64(len(poly))
				mark.CX = sx / n
				mark.CY = sy / n
			}
		}
		// Fallback centroids for action targets (approximate).
		if mark.CX == 0 && mark.CY == 0 {
			mark.CX, mark.CY = approxTargetCentroid(id)
		}
		out = append(out, mark)
	}
	return out
}

func approxTargetCentroid(id string) (float64, float64) {
	switch action.ActionTarget(id) {
	case action.TargetTop:
		return 0, 5000
	case action.TargetMid:
		return 0, 0
	case action.TargetBot:
		return 0, -5000
	case action.TargetOurBase:
		return -6000, -6000
	case action.TargetEnemyBase:
		return 6000, 6000
	case action.TargetRoshan:
		return -2200, 1600
	case action.TargetOurJungle:
		return -4000, -1000
	case action.TargetEnemyJungle:
		return 4000, 1000
	case action.TargetOurTriangle:
		return -3500, 3500
	case action.TargetEnemyTriangle:
		return 3500, -3500
	default:
		return 0, 0
	}
}

func collectTrajectories(byTime map[int]TrackSample, start, end int) (allies, enemies []TrajectoryPoint) {
	if start > end {
		start, end = end, start
	}
	// Sample ~1Hz: iterate integer seconds present in map within window.
	times := make([]int, 0, end-start+1)
	for t := start; t <= end; t++ {
		if _, ok := byTime[t]; ok {
			times = append(times, t)
		}
	}
	for _, t := range times {
		s := byTime[t]
		allies = append(allies, s.Allies...)
		enemies = append(enemies, s.Enemies...)
	}
	return allies, enemies
}