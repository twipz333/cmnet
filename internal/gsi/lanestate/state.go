package lanestate

import (
	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
)

// State is a read-only spatial snapshot of observed creep waves.
// It never contains predicted fog locations or lane-pressure advice.
type State struct {
	GeometryVersion    string
	MapGeometryVersion string
	MinimapPresent     bool
	OurSide            observe.Team

	Top LaneView
	Mid LaneView
	Bot LaneView
}

// LaneView is one lane from the local team's perspective.
type LaneView struct {
	Lane worldmap.LaneID

	Our   SideObservation
	Enemy EnemyObservation

	ObservedContact     bool
	VisibleFrontPathGap *float64 // nil unless both fronts currently observed
}

// SideObservation is currently observed allied waves. Empty means none seen
// this valid-minimap tick, not "use last seen".
type SideObservation struct {
	Waves      []WaveGroup
	Frontmost  *WaveGroup
	FrontTeam  worldmap.TeamProgress
	FrontAbs   worldmap.AbsoluteProgress
	FrontDepth worldmap.LaneDepth
	HasFront   bool
	CreepCount int
}

// EnemyObservation always names current data as visible. Last-seen is frozen.
type EnemyObservation struct {
	Visible           []WaveGroup
	VisibleFrontmost  *WaveGroup
	VisibleFrontTeam  worldmap.TeamProgress
	VisibleFrontAbs   worldmap.AbsoluteProgress
	VisibleFrontDepth worldmap.LaneDepth
	HasVisibleFront   bool

	LastSeen           []WaveGroup
	LastSeenFrontmost  *WaveGroup
	LastSeenFrontTeam  worldmap.TeamProgress
	LastSeenAt         *int
	LastSeenAgeSeconds *float64
}

// WaveGroup is one observed cluster of standard lane creeps. It is not an
// omniscient full wave: fog can hide part of it.
type WaveGroup struct {
	Lane worldmap.LaneID
	Side observe.Team

	CreepCount      int
	MeleeCount      int
	RangedCount     int
	FlagbearerCount int
	SiegeCount      int
	OtherCount      int

	Positions      []worldmap.WorldPosition
	CenterPosition worldmap.WorldPosition

	MinAbsoluteProgress    worldmap.AbsoluteProgress
	MaxAbsoluteProgress    worldmap.AbsoluteProgress
	CenterAbsoluteProgress worldmap.AbsoluteProgress
	FrontAbsoluteProgress  worldmap.AbsoluteProgress

	CenterTeamProgress worldmap.TeamProgress
	FrontTeamProgress  worldmap.TeamProgress

	CenterAbsoluteDistance float64
	FrontAbsoluteDistance  float64

	ObservedAtGameTime *int
}

func (s State) Lane(id worldmap.LaneID) LaneView {
	switch id {
	case worldmap.LaneTop:
		return s.Top
	case worldmap.LaneMid:
		return s.Mid
	case worldmap.LaneBot:
		return s.Bot
	default:
		return LaneView{Lane: worldmap.LaneUnknown}
	}
}
