package macrolabel

import "cmnet/internal/dota/worldmap"

// BehaviorEvent is one normalized behavioral anchor for detectors.
type BehaviorEvent struct {
	Kind     string `json:"kind"`
	GameTime *int   `json:"game_time,omitempty"`
	Value    *int   `json:"value,omitempty"`
	Side     string `json:"side,omitempty"`
	Meta     string `json:"meta,omitempty"`
}

// HeroBehavior is minimal per-hero behavior needed by detectors.
type HeroBehavior struct {
	Hero          string              `json:"hero"`
	Observed      bool                `json:"observed"`
	PrimaryRegion *worldmap.RegionID  `json:"primary_region,omitempty"`
	Regions       []worldmap.RegionID `json:"regions,omitempty"`
}

// TeamSpatialBehavior is taken from MapState-derived facts.
type TeamSpatialBehavior struct {
	CentroidRegion  worldmap.RegionID `json:"centroid_region"`
	GroupedCount    int               `json:"grouped_count"`
	GroupedRatio    float64           `json:"grouped_ratio"`
	Spread          float64           `json:"spread"`
	MainGroupRegion worldmap.RegionID `json:"main_group_region"`
}

// LaneBehavior is taken from LaneState-derived facts.
type LaneBehavior struct {
	Lane worldmap.LaneID `json:"lane"`

	OurFrontAvailable    bool    `json:"our_front_available"`
	OurFrontTeamProgress float64 `json:"our_front_team_progress,omitempty"`
	OurFrontDepth        string  `json:"our_front_depth,omitempty"`

	VisibleEnemyFrontAvailable    bool    `json:"visible_enemy_front_available"`
	VisibleEnemyFrontTeamProgress float64 `json:"visible_enemy_front_team_progress,omitempty"`
	VisibleEnemyFrontDepth        string  `json:"visible_enemy_front_depth,omitempty"`

	ObservedContact bool     `json:"observed_contact"`
	FrontGap        *float64 `json:"front_gap,omitempty"`
}

// StructuresBehavior is a minimal subset needed for evidence anchors.
type StructuresBehavior struct {
	EnemyTowerKillRecent    bool `json:"enemy_tower_kill_recent"`
	EnemyBarracksKillRecent bool `json:"enemy_barracks_kill_recent"`
}

// ObjectivesBehavior includes Roshan subset available in current player-visible capture.
type ObjectivesBehavior struct {
	RoshanVisible            bool `json:"roshan_visible"`
	RoshanKnownDeadAt        *int `json:"roshan_known_dead_at,omitempty"`
	RoshanAlliesNear         int  `json:"roshan_allies_near"`
	RoshanVisibleEnemiesNear int  `json:"roshan_visible_enemies_near"`
	OurBaseAlliesNear        int  `json:"our_base_allies_near"`
	EnemyBaseAlliesNear      int  `json:"enemy_base_allies_near"`
}

// BehavioralFrame is the ordered input for labeler.
type BehaviorFrame struct {
	MatchID string `json:"match_id"`
	Side    string `json:"side"`

	Seq        uint64     `json:"seq"`
	GameTime   int        `json:"game_time"`
	SourceMode SourceMode `json:"source_mode"`

	Heroes            []HeroBehavior      `json:"heroes"`
	Team              TeamSpatialBehavior `json:"team"`
	Lanes             []LaneBehavior      `json:"lanes"`
	Structures        StructuresBehavior  `json:"structures"`
	Objectives        ObjectivesBehavior  `json:"objectives"`
	Events            []BehaviorEvent     `json:"events"`
	AlliedWardRegions []worldmap.RegionID `json:"allied_ward_regions"`
}
