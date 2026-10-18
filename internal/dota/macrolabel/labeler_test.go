package macrolabel

import (
	"testing"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/worldmap"
)

func TestLabeler_TakeRoshan(t *testing.T) {
	space := action.DefaultSpaceV0()
	cfg := DefaultConfig()
	cfg.LookbackSeconds = 1
	cfg.LookaheadSeconds = 1
	cfg.MinimumActionDurationSeconds = 2
	cfg.MinConfidenceToLabel = 0.65
	cfg.AmbiguityMargin = 0.05

	labeler := NewV0(cfg, space)

	frames := make([]BehaviorFrame, 0, 5)
	for i := 0; i < 5; i++ {
		gt := 10 + i
		knownDead := 12
		f := syntheticFrame(cfg, "radiant", uint64(i+1), gt)
		f.Objectives.RoshanVisible = true
		f.Objectives.RoshanKnownDeadAt = &knownDead
		frames = append(frames, f)
	}

	segs, _, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}

	if !anySegmentHas(segs, action.KindTakeObjective, action.TargetRoshan) {
		t.Fatalf("expected TAKE ROSHAN segment, got=%v", segs)
	}
}

func TestLabeler_ControlRoshan(t *testing.T) {
	space := action.DefaultSpaceV0()
	cfg := DefaultConfig()
	cfg.LookbackSeconds = 1
	cfg.LookaheadSeconds = 1
	cfg.MinimumActionDurationSeconds = 2
	cfg.MinConfidenceToLabel = 0.65
	cfg.AmbiguityMargin = 0.05

	labeler := NewV0(cfg, space)

	frames := make([]BehaviorFrame, 0, 5)
	for i := 0; i < 5; i++ {
		gt := 10 + i
		f := syntheticFrame(cfg, "radiant", uint64(i+1), gt)
		f.Objectives.RoshanVisible = true
		f.Objectives.RoshanAlliesNear = 4
		f.Team.GroupedRatio = 0.55
		// Put heroes into lane TOP so FARM OUR_JUNGLE doesn't compete with CONTROL.
		for hi := range f.Heroes {
			f.Heroes[hi] = heroInRegion(worldmap.RegionTopLaneCenter)
		}
		frames = append(frames, f)
	}

	segs, _, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}

	if !anySegmentHas(segs, action.KindControlObjective, action.TargetRoshan) {
		t.Fatalf("expected CONTROL ROSHAN segment, got=%v", segs)
	}
}

func TestLabeler_PushTop(t *testing.T) {
	space := action.DefaultSpaceV0()
	cfg := DefaultConfig()
	cfg.LookbackSeconds = 1
	cfg.LookaheadSeconds = 1
	cfg.MinimumActionDurationSeconds = 2
	cfg.MinConfidenceToLabel = 0.65
	cfg.AmbiguityMargin = 0.05
	cfg.MinLaneFrontDeltaPush = 0.12

	labeler := NewV0(cfg, space)

	// Make lane top progress increase ~0.14 across each [t-1..t+1] window.
	// progress(t) = (t-10)*0.07
	frames := make([]BehaviorFrame, 0, 6)
	for i := 0; i < 6; i++ {
		gt := 10 + i
		f := syntheticFrame(cfg, "radiant", uint64(i+1), gt)
		prog := float64(gt-10) * 0.07
		setLane(&f, worldmap.LaneTop, prog, "OWN_SIDE")
		// 5 heroes in TOP lane region.
		for hi := range f.Heroes {
			f.Heroes[hi] = heroInRegion(worldmap.RegionTopLaneCenter)
		}
		frames = append(frames, f)
	}

	segs, _, err := labeler.Label(frames)
	if err != nil {
		t.Fatal(err)
	}
	if !anySegmentHas(segs, action.KindPush, action.TargetTop) {
		t.Fatalf("expected PUSH TOP segment, got=%v", segs)
	}
}

func TestLabeler_FarmVsInvadeEnemyJungle(t *testing.T) {
	space := action.DefaultSpaceV0()
	cfg := DefaultConfig()
	cfg.LookbackSeconds = 1
	cfg.LookaheadSeconds = 1
	cfg.MinimumActionDurationSeconds = 1
	cfg.MinConfidenceToLabel = 0.65
	cfg.AmbiguityMargin = 0.05
	cfg.MinHeroesInAreaForFarm = 3
	cfg.MinLaneFrontDeltaPush = 0.12

	// Case A: FARM ENEMY_JUNGLE (no entry delta).
	{
		labeler := NewV0(cfg, space)
		frames := make([]BehaviorFrame, 0, 5)
		for i := 0; i < 5; i++ {
			gt := 10 + i
			f := syntheticFrame(cfg, "radiant", uint64(i+1), gt)
			f.Team.GroupedRatio = 0.4
			f.Team.Spread = 200
			for hi := range f.Heroes {
				f.Heroes[hi] = heroInRegion(worldmap.RegionDireMainJungle)
			}
			frames = append(frames, f)
		}
		segs, _, err := labeler.Label(frames)
		if err != nil {
			t.Fatal(err)
		}
		if !anySegmentHas(segs, action.KindFarm, action.TargetEnemyJungle) {
			t.Fatalf("expected FARM ENEMY_JUNGLE segment, got=%v", segs)
		}
	}

	// Case B: INVADE ENEMY_JUNGLE (entry transition).
	{
		labeler := NewV0(cfg, space)
		frames := make([]BehaviorFrame, 0, 5)
		for i := 0; i < 5; i++ {
			gt := 10 + i
			f := syntheticFrame(cfg, "radiant", uint64(i+1), gt)
			f.Team.GroupedRatio = 0.4
			f.Team.Spread = 200 - float64(i)*10
			for hi := range f.Heroes {
				f.Heroes[hi] = heroInRegion(worldmap.RegionRadiantMainJungle)
			}
			if i <= 1 {
				f.Heroes[0] = heroInRegion(worldmap.RegionDireMainJungle)
				f.Heroes[1] = heroInRegion(worldmap.RegionDireMainJungle)
			} else {
				f.Heroes[0] = heroInRegion(worldmap.RegionDireMainJungle)
				f.Heroes[1] = heroInRegion(worldmap.RegionDireMainJungle)
				f.Heroes[2] = heroInRegion(worldmap.RegionDireMainJungle)
				f.Heroes[3] = heroInRegion(worldmap.RegionDireMainJungle)
			}
			frames = append(frames, f)
		}
		segs, _, err := labeler.Label(frames)
		if err != nil {
			t.Fatal(err)
		}
		if !anySegmentHas(segs, action.KindInvade, action.TargetEnemyJungle) {
			t.Fatalf("expected INVADE ENEMY_JUNGLE segment, got=%v", segs)
		}
	}
}

func anySegmentHas(segs []MacroActionSegment, kind action.ActionKind, tgt action.ActionTarget) bool {
	for _, s := range segs {
		if s.Status == LabelStatusLabeled && s.Action != nil && s.Action.Kind == kind && s.Action.Target == tgt {
			return true
		}
	}
	return false
}

func syntheticFrame(cfg Config, side string, seq uint64, gameTime int) BehaviorFrame {
	_ = cfg

	f := BehaviorFrame{
		MatchID:    "m1",
		Side:       side,
		Seq:        seq,
		GameTime:   gameTime,
		SourceMode: SourceModePlayerVisibleGSI,
		Heroes: []HeroBehavior{
			heroInRegion(worldmap.RegionRadiantMainJungle),
			heroInRegion(worldmap.RegionRadiantMainJungle),
			heroInRegion(worldmap.RegionRadiantMainJungle),
			heroInRegion(worldmap.RegionRadiantMainJungle),
			heroInRegion(worldmap.RegionRadiantMainJungle),
		},
		Team: TeamSpatialBehavior{
			CentroidRegion:  worldmap.RegionRadiantMainJungle,
			GroupedCount:    3,
			GroupedRatio:    0.4,
			Spread:          200,
			MainGroupRegion: worldmap.RegionRadiantMainJungle,
		},
		Lanes: []LaneBehavior{
			{Lane: worldmap.LaneTop, OurFrontAvailable: true, OurFrontTeamProgress: 0, OurFrontDepth: "OWN_SIDE", VisibleEnemyFrontAvailable: false},
			{Lane: worldmap.LaneMid, OurFrontAvailable: true, OurFrontTeamProgress: 0, OurFrontDepth: "OWN_SIDE", VisibleEnemyFrontAvailable: false},
			{Lane: worldmap.LaneBot, OurFrontAvailable: true, OurFrontTeamProgress: 0, OurFrontDepth: "OWN_SIDE", VisibleEnemyFrontAvailable: false},
		},
		Structures: StructuresBehavior{},
		Objectives: ObjectivesBehavior{
			RoshanVisible:            false,
			RoshanKnownDeadAt:        nil,
			RoshanAlliesNear:         0,
			RoshanVisibleEnemiesNear: 0,
			OurBaseAlliesNear:        0,
			EnemyBaseAlliesNear:      0,
		},
		Events:            nil,
		AlliedWardRegions: nil,
	}
	return f
}

func setLane(f *BehaviorFrame, ln worldmap.LaneID, prog float64, depth string) {
	for i := range f.Lanes {
		if f.Lanes[i].Lane == ln {
			f.Lanes[i].OurFrontAvailable = true
			f.Lanes[i].OurFrontTeamProgress = prog
			f.Lanes[i].OurFrontDepth = depth
		}
	}
}

func heroInRegion(r worldmap.RegionID) HeroBehavior {
	id := r
	return HeroBehavior{Hero: "h", Observed: true, PrimaryRegion: &id, Regions: []worldmap.RegionID{r}}
}
