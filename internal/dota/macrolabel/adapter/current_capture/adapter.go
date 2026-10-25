package current_capture

import (
	"sort"

	"cmnet/internal/dota/macrolabel"
	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/lanestate"
	"cmnet/internal/gsi/mapstate"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/track"
)

type Adapter struct {
	geo     *worldmap.Geometry
	laneGeo *worldmap.LaneGeometry
	side    string // "radiant" or "dire"

	laneTr *lanestate.Tracker
}

func NewAdapter(geo *worldmap.Geometry, laneGeo *worldmap.LaneGeometry, side string) Adapter {
	return Adapter{
		geo:     geo,
		laneGeo: laneGeo,
		side:    side,
		laneTr:  lanestate.New(laneGeo),
	}
}

// Frame builds a source-independent BehaviorFrame for one tracked tick.
// It returns ok=false if game_time is unavailable.
func (a Adapter) Frame(seq uint64, st track.State) (frame macrolabel.BehaviorFrame, ok bool, err error) {
	if st.Clock.GameTime == nil {
		return macrolabel.BehaviorFrame{}, false, nil
	}
	gt := *st.Clock.GameTime

	// LaneState is stateful, so we keep a lane tracker inside the adapter.
	obs := lanestate.Build(st, a.laneGeo)
	a.laneTr.Apply(obs)
	ls := a.laneTr.Snapshot()

	ms := mapstate.Build(st, a.geo)

	heroes := make([]macrolabel.HeroBehavior, 0, len(ms.Allies))
	for _, h := range ms.Allies {
		var pr *worldmap.RegionID
		if h.PrimaryRegion != nil {
			id := *h.PrimaryRegion
			pr = &id
		}
		r := h.Regions
		heroes = append(heroes, macrolabel.HeroBehavior{
			Hero:          h.Hero,
			Observed:      h.Visible,
			PrimaryRegion: pr,
			Regions:       append([]worldmap.RegionID(nil), r...),
		})
	}

	lanes := []macrolabel.LaneBehavior{toLaneBehavior(ls.Top), toLaneBehavior(ls.Mid), toLaneBehavior(ls.Bot)}

	// Roshan objective subset.
	roshanVisible := st.Roshan.VisibleNow
	var roshanDeadAt *int
	if st.Roshan.KnownDeadAt != nil {
		v := *st.Roshan.KnownDeadAt
		roshanDeadAt = &v
	}
	pitAAllies := ms.RoshanPitA.AlliesNear
	pitBAllies := ms.RoshanPitB.AlliesNear
	pitAVis := ms.RoshanPitA.VisibleEnemiesNear
	pitBVis := ms.RoshanPitB.VisibleEnemiesNear
	alliesNear := maxInt(pitAAllies, pitBAllies)
	visNear := maxInt(pitAVis, pitBVis)

	// Allied ward regions (positive observable evidence for SET_VISION).
	wardRegions := make([]worldmap.RegionID, 0, len(st.AlliedWards))
	for _, w := range st.AlliedWards {
		if !w.Observed {
			continue
		}
		p := worldmap.FromInt(w.Position.X, w.Position.Y)
		r := worldmap.Classify(a.geo, &p)
		wardRegions = append(wardRegions, r)
	}
	wardRegions = uniqueRegionsSorted(wardRegions)

	// Events: normalized behavioral anchors only.
	evOut := make([]macrolabel.BehaviorEvent, 0, len(st.RecentEvents))
	for _, ev := range st.RecentEvents {
		kind := ""
		switch ev.Kind {
		case observe.EventSmokeActivated:
			kind = "SMOKE_ACTIVATED"
		case observe.EventTowerKill:
			kind = "TOWER_KILL"
		case observe.EventBarracksKill:
			kind = "BARRACKS_KILL"
		case observe.EventGlyphUsed:
			kind = "GLYPH_USED"
		case observe.EventHeroKill:
			kind = "HERO_KILL"
		case observe.EventObserverWardKilled:
			kind = "OBSERVER_WARD_KILLED"
		case observe.EventSentryWardKilled:
			kind = "SENTRY_WARD_KILLED"
		default:
			continue
		}
		evOut = append(evOut, macrolabel.BehaviorEvent{
			Kind:     kind,
			GameTime: ev.GameTime,
			Value:    ev.Value,
		})
	}

	frame = macrolabel.BehaviorFrame{
		MatchID:    st.Clock.MatchID,
		Side:       a.side,
		Seq:        seq,
		GameTime:   gt,
		SourceMode: macrolabel.SourceModePlayerVisibleGSI,

		Heroes: heroes,
		Team: macrolabel.TeamSpatialBehavior{
			CentroidRegion:  ms.AllyTeam.CentroidRegion,
			GroupedCount:    ms.AllyTeam.GroupedCount,
			GroupedRatio:    ms.AllyTeam.GroupedRatio,
			Spread:          ms.AllyTeam.Spread,
			MainGroupRegion: ms.AllyTeam.CentroidRegion,
		},
		Lanes:      lanes,
		Structures: macrolabel.StructuresBehavior{},
		Objectives: macrolabel.ObjectivesBehavior{
			RoshanVisible:            roshanVisible,
			RoshanKnownDeadAt:        roshanDeadAt,
			RoshanAlliesNear:         alliesNear,
			RoshanVisibleEnemiesNear: visNear,
			OurBaseAlliesNear:        ms.OurBase.AlliesNear,
			EnemyBaseAlliesNear:      ms.EnemyBase.AlliesNear,
		},
		Events:            evOut,
		AlliedWardRegions: wardRegions,
	}
	return frame, true, nil
}

func toLaneBehavior(v lanestate.LaneView) macrolabel.LaneBehavior {
	ln := macrolabel.LaneBehavior{
		Lane:            v.Lane,
		ObservedContact: v.ObservedContact,
		FrontGap:        v.VisibleFrontPathGap,
	}
	ln.OurFrontAvailable = v.Our.HasFront
	if v.Our.HasFront {
		ln.OurFrontTeamProgress = float64(v.Our.FrontTeam)
		ln.OurFrontDepth = v.Our.FrontDepth.String()
	}
	ln.VisibleEnemyFrontAvailable = v.Enemy.HasVisibleFront
	if v.Enemy.HasVisibleFront {
		ln.VisibleEnemyFrontTeamProgress = float64(v.Enemy.VisibleFrontTeam)
		ln.VisibleEnemyFrontDepth = v.Enemy.VisibleFrontDepth.String()
	}
	return ln
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func uniqueRegionsSorted(in []worldmap.RegionID) []worldmap.RegionID {
	if len(in) == 0 {
		return nil
	}
	m := map[worldmap.RegionID]struct{}{}
	out := make([]worldmap.RegionID, 0, len(in))
	for _, r := range in {
		if _, ok := m[r]; ok {
			continue
		}
		m[r] = struct{}{}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
