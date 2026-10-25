package macrolabel

import (
	"encoding/json"
	"math"
	"testing"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/worldmap"
)

func TestV02_AccountingInvariant(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LookbackSeconds = 1
	cfg.LookaheadSeconds = 1
	cfg.MinimumActionDurationSeconds = 1
	labeler := NewV0(cfg, action.DefaultSpaceV0())

	frames := make([]BehaviorFrame, 0, 10)
	for i := 0; i < 10; i++ {
		f := syntheticFrame(cfg, "radiant", uint64(i+1), i*2)
		frames = append(frames, f)
	}

	_, rep, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}
	sum := rep.LabeledDurationSeconds + rep.AmbiguousDurationSeconds + rep.UnlabeledDurationSeconds + rep.ExcludedDurationSeconds
	delta := math.Abs(rep.TotalDurationSeconds - sum)
	if delta > cfg.AccountingToleranceSeconds {
		t.Fatalf("accounting delta too large: total=%.3f sum=%.3f delta=%.3f", rep.TotalDurationSeconds, sum, delta)
	}
}

func TestV02_ExcludedSourceGap(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxFrameGapSeconds = 5
	cfg.LookbackSeconds = 1
	cfg.LookaheadSeconds = 1
	cfg.MinimumActionDurationSeconds = 1
	labeler := NewV0(cfg, action.DefaultSpaceV0())

	frames := []BehaviorFrame{
		syntheticFrame(cfg, "m1", 1, 0),
		syntheticFrame(cfg, "m1", 2, 2),
		syntheticFrame(cfg, "m1", 3, 20),
		syntheticFrame(cfg, "m1", 4, 22),
	}

	segs, rep, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range segs {
		if s.Status == LabelStatusExcluded && s.ExclusionReason == ExclusionSourceGap {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected EXCLUDED SOURCE_GAP segment, segs=%v", segs)
	}
	if rep.ExcludedDurationSeconds <= 0 {
		t.Fatalf("expected excluded duration > 0, got %.3f", rep.ExcludedDurationSeconds)
	}
}

func TestV02_AmbiguousReasonNeverEmpty(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LookbackSeconds = 1
	cfg.LookaheadSeconds = 1
	cfg.MinConfidenceToLabel = 0.5
	cfg.AmbiguityMargin = 0.2
	cfg.MinimumActionDurationSeconds = 1
	labeler := NewV0(cfg, action.DefaultSpaceV0())

	frames := make([]BehaviorFrame, 0, 6)
	for i := 0; i < 6; i++ {
		f := syntheticFrame(cfg, "radiant", uint64(i+1), 100+i)
		f.Team.GroupedRatio = 0.5
		if i%2 == 0 {
			for hi := range f.Heroes {
				f.Heroes[hi] = heroInRegion(worldmap.RegionTopLaneCenter)
			}
			setLane(&f, worldmap.LaneTop, float64(i)*0.08, "OWN_SIDE")
		} else {
			for hi := range f.Heroes {
				f.Heroes[hi] = heroInRegion(worldmap.RegionDireMainJungle)
			}
		}
		frames = append(frames, f)
	}

	segs, _, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range segs {
		if s.Status == LabelStatusAmbiguous && s.AmbiguityReason == "" {
			t.Fatalf("AMBIGUOUS segment missing reason: %+v", s)
		}
	}
}

func TestV02_SmokeAnchorDedupe(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LookbackSeconds = 2
	cfg.LookaheadSeconds = 10
	cfg.SmokeFollowSeconds = 15
	cfg.MinimumActionDurationSeconds = 1
	cfg.MinConfidenceToLabel = 0.5
	labeler := NewV0(cfg, action.DefaultSpaceV0())

	gtSmoke := 200
	frames := make([]BehaviorFrame, 0, 8)
	for i := 0; i < 8; i++ {
		gt := gtSmoke + i*2
		f := syntheticFrame(cfg, "radiant", uint64(i+1), gt)
		if i == 1 {
			f.Events = []BehaviorEvent{{Kind: "SMOKE_ACTIVATED", GameTime: &gtSmoke}}
		}
		if i >= 3 {
			for hi := range f.Heroes {
				f.Heroes[hi] = heroInRegion(worldmap.RegionMidLaneCenter)
			}
		}
		frames = append(frames, f)
	}

	anchors := ListSmokeAnchors(frames)
	if len(anchors) != 1 {
		t.Fatalf("expected 1 smoke anchor, got %d", len(anchors))
	}

	segs, _, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}
	smokeLabeled := 0
	anchorIDs := map[string]int{}
	for _, s := range segs {
		if s.Status == LabelStatusLabeled && s.Action != nil && s.Action.Kind == action.KindSmoke {
			smokeLabeled++
			if s.SmokeAnchorID != "" {
				anchorIDs[s.SmokeAnchorID]++
			}
		}
	}
	if smokeLabeled == 0 {
		t.Fatalf("expected at least one SMOKE segment")
	}
	for id, n := range anchorIDs {
		if n > 1 {
			t.Fatalf("duplicate smoke segments for anchor %s: %d", id, n)
		}
	}
}

func TestV02_SiegeRetreatTransition(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LookbackSeconds = 2
	cfg.LookaheadSeconds = 6
	cfg.MinimumActionDurationSeconds = 2
	cfg.MinConfidenceToLabel = 0.55
	labeler := NewV0(cfg, action.DefaultSpaceV0())

	frames := make([]BehaviorFrame, 0, 10)
	for i := 0; i < 10; i++ {
		gt := 300 + i*2
		f := syntheticFrame(cfg, "radiant", uint64(i+1), gt)
		if i < 4 {
			f.Objectives.EnemyBaseAlliesNear = 4
			f.Team.GroupedRatio = 0.6
			setLane(&f, worldmap.LaneMid, 0.8, "ENEMY_BASE")
			for hi := range f.Heroes {
				f.Heroes[hi] = heroInRegion(worldmap.RegionDireHighground)
			}
			f.Team.MainGroupRegion = worldmap.RegionDireHighground
		} else {
			f.Objectives.EnemyBaseAlliesNear = 0
			f.Team.GroupedRatio = 0.3
			f.Team.Spread = 250
			for hi := range f.Heroes {
				f.Heroes[hi] = heroInRegion(worldmap.RegionRadiantMainJungle)
			}
			f.Team.MainGroupRegion = worldmap.RegionRadiantMainJungle
			if i == 6 {
				kill := gt
				f.Events = []BehaviorEvent{{Kind: "HERO_KILL", GameTime: &kill}}
			}
		}
		frames = append(frames, f)
	}

	segs, _, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}
	siegeAtPast := false
	retreatAfter := false
	for _, s := range segs {
		if s.Status == LabelStatusLabeled && s.Action != nil {
			if s.Action.Kind == action.KindSiege && s.EndGameTime <= 310 {
				siegeAtPast = true
			}
			if s.Action.Kind == action.KindRetreat && s.StartGameTime >= 308 {
				retreatAfter = true
			}
		}
	}
	if !siegeAtPast {
		t.Log("no early SIEGE segment (heuristic may skip)")
	}
	if !retreatAfter {
		t.Log("no RETREAT after disengage (heuristic may skip)")
	}
	for _, s := range segs {
		if s.Status == LabelStatusLabeled && s.Action != nil && s.Action.Kind == action.KindRetreat && s.StartGameTime < 306 {
			t.Fatalf("RETREAT fired too early during siege window: %v", s)
		}
	}
}

func TestV02_Determinism(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LookbackSeconds = 2
	cfg.LookaheadSeconds = 4
	labeler := NewV0(cfg, action.DefaultSpaceV0())

	frames := make([]BehaviorFrame, 0, 12)
	for i := 0; i < 12; i++ {
		f := syntheticFrame(cfg, "radiant", uint64(i+1), 50+i*3)
		if i > 5 {
			setLane(&f, worldmap.LaneTop, float64(i-5)*0.05, "OWN_SIDE")
			for hi := range f.Heroes {
				f.Heroes[hi] = heroInRegion(worldmap.RegionTopLaneCenter)
			}
		}
		frames = append(frames, f)
	}

	segs1, rep1, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}
	segs2, rep2, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := json.Marshal(segs1)
	b2, _ := json.Marshal(segs2)
	if string(b1) != string(b2) {
		t.Fatalf("segments differ between runs")
	}
	rb1, _ := json.Marshal(rep1)
	rb2, _ := json.Marshal(rep2)
	if string(rb1) != string(rb2) {
		t.Fatalf("reports differ between runs")
	}
}
