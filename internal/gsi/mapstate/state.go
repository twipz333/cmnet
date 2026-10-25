package mapstate

import "cmnet/internal/dota/worldmap"

// State is an immutable spatial snapshot derived from TrackedState + Geometry.
type State struct {
	GeometryVersion string

	Self    HeroSpatial
	Allies  []HeroSpatial
	Enemies []HeroSpatial

	AllyTeam TeamSpatial

	VisibleEnemyCount      int
	MissingEnemyCount      int
	VisibleEnemiesByRegion []RegionCount

	RoshanPitA     ObjectiveProximity
	RoshanPitB     ObjectiveProximity
	OurTormentor   ObjectiveProximity
	EnemyTormentor ObjectiveProximity
	OurBase        ObjectiveProximity
	EnemyBase      ObjectiveProximity
}

// HeroSpatial is the macro location of one roster hero.
type HeroSpatial struct {
	Hero    string
	Visible bool

	Positions []worldmap.WorldPosition
	Regions   []worldmap.RegionID
	// PrimaryRegion is set only when every current position maps to the same region.
	PrimaryRegion *worldmap.RegionID

	LastSeenPositions  []worldmap.WorldPosition
	LastSeenRegions    []worldmap.RegionID
	LastSeenAgeSeconds *float64
}

// TeamSpatial is the ally grouping summary. Centroid uses the main cluster only.
type TeamSpatial struct {
	ObservedCount  int
	GroupedCount   int
	GroupedRatio   float64
	AllyGrouped    bool
	Centroid       *worldmap.WorldPosition
	CentroidRegion worldmap.RegionID
	Spread         float64 // mean distance to cluster centroid; 0 if <2 members
}

// ObjectiveProximity is visible-only. Missing enemies are never treated as present.
type ObjectiveProximity struct {
	Landmark           worldmap.LandmarkID
	AlliesNear         int
	VisibleEnemiesNear int
	VisibleEnemyDelta  int // allies_near - visible_enemies_near; not a safety claim
	AllyHeroes         []string
	VisibleEnemyHeroes []string
}

// RegionCount is one bucket of visible_enemies_by_region.
type RegionCount struct {
	Region worldmap.RegionID
	Count  int
}
