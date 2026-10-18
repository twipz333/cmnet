package lanestate

import (
	"sort"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/track"
)

// Observation is one tick of projected, clustered waves. Stateless output.
type Observation struct {
	GeometryVersion string
	MatchID         string
	Seq             uint64
	GameTime        *int
	ClockTime       *int
	Paused          bool
	OurSide         observe.Team
	MinimapPresent  bool

	Top LaneTick
	Mid LaneTick
	Bot LaneTick

	Stats ObserveStats
}

// LaneTick is current-tick groups for both sides. Empty enemy groups with
// MinimapPresent mean they were not observed, not that the section was missing.
type LaneTick struct {
	Lane    worldmap.LaneID
	Radiant []WaveGroup
	Dire    []WaveGroup
}

// ObserveStats are per-tick diagnostics.
type ObserveStats struct {
	LaneCreepsSeen     int
	LaneCreepsAssigned int
	UnknownTooFar      int
	Ambiguous          int
	NonLaneSkipped     int
	UnknownTeamSkipped int
	WaveGroupsCreated  int
}

type projectedCreep struct {
	lane    worldmap.LaneID
	side    observe.Team
	class   observe.CreepClass
	pos     worldmap.WorldPosition
	absDist float64
	absProg worldmap.AbsoluteProgress
}

// Build projects current TrackedState creeps onto LaneGeometry. No history.
func Build(st track.State, g *worldmap.LaneGeometry) Observation {
	obs := Observation{
		MatchID:        st.Clock.MatchID,
		Seq:            st.Clock.Seq,
		GameTime:       cloneInt(st.Clock.GameTime),
		ClockTime:      cloneInt(st.Clock.ClockTime),
		Paused:         st.Clock.Paused,
		OurSide:        st.Roster.AllySide,
		MinimapPresent: st.MinimapPresent,
	}
	if g != nil {
		obs.GeometryVersion = g.Version()
	}
	obs.Top.Lane = worldmap.LaneTop
	obs.Mid.Lane = worldmap.LaneMid
	obs.Bot.Lane = worldmap.LaneBot
	if g == nil || !st.MinimapPresent {
		return obs
	}

	var projected []projectedCreep
	now := st.Clock.GameTime
	if now == nil {
		now = st.Clock.ClockTime
	}
	for _, c := range st.Creeps {
		if !c.Class.IsStandardLane() {
			obs.Stats.NonLaneSkipped++
			continue
		}
		obs.Stats.LaneCreepsSeen++
		if c.Team != observe.TeamRadiant && c.Team != observe.TeamDire {
			obs.Stats.UnknownTeamSkipped++
			continue
		}
		pr := g.Project(worldmap.FromInt(c.Position.X, c.Position.Y))
		if pr.Ambiguous {
			obs.Stats.Ambiguous++
			continue
		}
		if !pr.Assigned {
			obs.Stats.UnknownTooFar++
			continue
		}
		obs.Stats.LaneCreepsAssigned++
		projected = append(projected, projectedCreep{
			lane:    pr.Lane,
			side:    c.Team,
			class:   c.Class,
			pos:     worldmap.FromInt(c.Position.X, c.Position.Y),
			absDist: pr.AbsoluteDistance,
			absProg: pr.AbsoluteProgress,
		})
	}

	gap := g.WaveClusterGap()
	for _, lane := range []worldmap.LaneID{worldmap.LaneTop, worldmap.LaneMid, worldmap.LaneBot} {
		for _, side := range []observe.Team{observe.TeamRadiant, observe.TeamDire} {
			groups := clusterWave(filter(projected, lane, side), gap, now, g, side)
			obs.Stats.WaveGroupsCreated += len(groups)
			switch {
			case lane == worldmap.LaneTop && side == observe.TeamRadiant:
				obs.Top.Radiant = groups
			case lane == worldmap.LaneTop && side == observe.TeamDire:
				obs.Top.Dire = groups
			case lane == worldmap.LaneMid && side == observe.TeamRadiant:
				obs.Mid.Radiant = groups
			case lane == worldmap.LaneMid && side == observe.TeamDire:
				obs.Mid.Dire = groups
			case lane == worldmap.LaneBot && side == observe.TeamRadiant:
				obs.Bot.Radiant = groups
			case lane == worldmap.LaneBot && side == observe.TeamDire:
				obs.Bot.Dire = groups
			}
		}
	}
	return obs
}

func filter(in []projectedCreep, lane worldmap.LaneID, side observe.Team) []projectedCreep {
	var out []projectedCreep
	for _, c := range in {
		if c.lane == lane && c.side == side {
			out = append(out, c)
		}
	}
	return out
}

func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

func fromRadiant(side observe.Team) bool { return side == observe.TeamRadiant }

func sortGroups(gs []WaveGroup) {
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].CenterAbsoluteDistance != gs[j].CenterAbsoluteDistance {
			return gs[i].CenterAbsoluteDistance < gs[j].CenterAbsoluteDistance
		}
		return gs[i].CreepCount > gs[j].CreepCount
	})
}
