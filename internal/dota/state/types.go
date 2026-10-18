package state

import "cmnet/internal/dota/worldmap"

const SchemaVersion = "dota-state-v0.1"

type SourceKind string

const (
	SourceObserved SourceKind = "OBSERVED"
	SourceHistory  SourceKind = "HISTORY"
	SourceDerived  SourceKind = "DERIVED"
	SourceBelief   SourceKind = "BELIEF"
)

type MatchPhase string

const (
	PhaseUnknown MatchPhase = "UNKNOWN"
	PhaseEarly   MatchPhase = "EARLY"
	PhaseMid     MatchPhase = "MID"
	PhaseLate    MatchPhase = "LATE"
)

type OptionalInt struct {
	Value     int        `json:"value,omitempty"`
	Available bool       `json:"available"`
	Source    SourceKind `json:"source,omitempty"`
}

type OptionalFloat struct {
	Value     float64    `json:"value,omitempty"`
	Available bool       `json:"available"`
	Source    SourceKind `json:"source,omitempty"`
}

type OptionalBool struct {
	Value     bool       `json:"value,omitempty"`
	Available bool       `json:"available"`
	Source    SourceKind `json:"source,omitempty"`
}

type OptionalString struct {
	Value     string     `json:"value,omitempty"`
	Available bool       `json:"available"`
	Source    SourceKind `json:"source,omitempty"`
}

type OptionalRegion struct {
	Value     worldmap.RegionID `json:"value,omitempty"`
	Available bool              `json:"available"`
	Source    SourceKind        `json:"source,omitempty"`
}

type OptionalPosition struct {
	X         float64    `json:"x,omitempty"`
	Y         float64    `json:"y,omitempty"`
	Available bool       `json:"available"`
	Source    SourceKind `json:"source,omitempty"`
}

type Meta struct {
	SchemaVersion       string         `json:"schema_version"`
	MapGeometryVersion  string         `json:"map_geometry_version,omitempty"`
	LaneGeometryVersion string         `json:"lane_geometry_version,omitempty"`
	RegionGraphVersion  string         `json:"region_graph_version,omitempty"`
	BeliefModelVersion  string         `json:"belief_model_version,omitempty"`
	MatchID             OptionalString `json:"match_id"`
	GameTime            OptionalInt    `json:"game_time"`
	ClockTime           OptionalInt    `json:"clock_time"`
	SourceSeq           uint64         `json:"source_seq"`
	GeneratedAtGameTime OptionalInt    `json:"generated_at_game_time"`
	OurSide             OptionalString `json:"our_side"`
}

type Match struct {
	Phase        MatchPhase     `json:"phase"`
	GameState    OptionalString `json:"game_state"`
	RadiantScore OptionalInt    `json:"radiant_score"`
	DireScore    OptionalInt    `json:"dire_score"`
	Paused       OptionalBool   `json:"paused"`
}

type ItemState struct {
	Name      string        `json:"name,omitempty"`
	SlotClass string        `json:"slot_class"`
	Ready     OptionalBool  `json:"ready"`
	Charges   OptionalInt   `json:"charges"`
	Cooldown  OptionalFloat `json:"cooldown_remaining"`
}

type AbilityState struct {
	Name     string        `json:"name,omitempty"`
	Level    OptionalInt   `json:"level"`
	Ready    OptionalBool  `json:"ready"`
	Cooldown OptionalFloat `json:"cooldown_remaining"`
	Ultimate OptionalBool  `json:"ultimate"`
}

type Self struct {
	Hero             OptionalString `json:"hero"`
	Region           OptionalRegion `json:"region"`
	Alive            OptionalBool   `json:"alive"`
	RespawnSeconds   OptionalInt    `json:"respawn_seconds"`
	Level            OptionalInt    `json:"level"`
	HPPct            OptionalInt    `json:"hp_pct"`
	ManaPct          OptionalInt    `json:"mana_pct"`
	Gold             OptionalInt    `json:"gold"`
	GPM              OptionalInt    `json:"gpm"`
	XPM              OptionalInt    `json:"xpm"`
	BuybackAvailable OptionalBool   `json:"buyback_available"`
	BuybackCooldown  OptionalInt    `json:"buyback_cooldown"`
	Smoked           OptionalBool   `json:"smoked"`
	Items            []ItemState    `json:"items"`
	Abilities        []AbilityState `json:"abilities"`
}

type Team struct {
	ObservedAllyCount  int              `json:"observed_ally_count"`
	GroupedCount       int              `json:"grouped_count"`
	GroupedRatio       OptionalFloat    `json:"grouped_ratio"`
	AllyGrouped        OptionalBool     `json:"ally_grouped"`
	MainGroupRegion    OptionalRegion   `json:"main_group_region"`
	MainGroupCentroid  OptionalPosition `json:"main_group_centroid"`
	Spread             OptionalFloat    `json:"spread"`
	VisibleEnemyCount  int              `json:"visible_enemy_count"`
	MissingEnemyCount  int              `json:"missing_enemy_count"`
	UnlocalizedEnemies int              `json:"unlocalized_enemy_count"`
	MeanMissingAge     OptionalFloat    `json:"mean_missing_age"`
	MaxMissingAge      OptionalFloat    `json:"max_missing_age"`
	MeanUnmodeledMass  OptionalFloat    `json:"mean_unmodeled_mass"`
	MaxUnmodeledMass   OptionalFloat    `json:"max_unmodeled_mass"`
}

type AllyHero struct {
	Hero              string              `json:"hero"`
	CurrentlyObserved bool                `json:"currently_observed"`
	Regions           []worldmap.RegionID `json:"regions"`
	PrimaryRegion     OptionalRegion      `json:"primary_region"`
}

type BeliefMass struct {
	Region worldmap.RegionID `json:"region"`
	Mass   float64           `json:"mass"`
}

type EnemyHero struct {
	Hero                  string              `json:"hero"`
	Visible               bool                `json:"visible"`
	CurrentAmbiguous      bool                `json:"current_ambiguous"`
	CurrentRegions        []worldmap.RegionID `json:"current_regions"`
	LastSeenRegions       []worldmap.RegionID `json:"last_seen_regions"`
	LastSeenAge           OptionalFloat       `json:"last_seen_age"`
	BeliefStatus          string              `json:"belief_status"`
	BeliefRegionMass      []BeliefMass        `json:"belief_region_mass"`
	BeliefUnmodeledMass   float64             `json:"belief_unmodeled_mass"`
	BeliefEntropy         OptionalFloat       `json:"belief_entropy"`
	WalkingReachableCount int                 `json:"walking_reachable_region_count"`
	WalkingReachable      []worldmap.RegionID `json:"walking_reachable_regions"`
}

type Lane struct {
	OurWaveCount                int            `json:"our_wave_count"`
	OurFrontAvailable           bool           `json:"our_front_available"`
	OurFrontTeamProgress        OptionalFloat  `json:"our_front_team_progress"`
	OurFrontDepth               OptionalString `json:"our_front_depth"`
	OurFrontCreepCount          OptionalInt    `json:"our_front_creep_count"`
	VisibleEnemyWaveCount       int            `json:"visible_enemy_wave_count"`
	VisibleEnemyFrontAvailable  bool           `json:"visible_enemy_front_available"`
	VisibleEnemyFrontProgress   OptionalFloat  `json:"visible_enemy_front_team_progress"`
	VisibleEnemyFrontDepth      OptionalString `json:"visible_enemy_front_depth"`
	VisibleEnemyFrontCreepCount OptionalInt    `json:"visible_enemy_front_creep_count"`
	EnemyLastSeenFrontAvailable bool           `json:"enemy_last_seen_front_available"`
	EnemyLastSeenFrontProgress  OptionalFloat  `json:"enemy_last_seen_front_team_progress"`
	EnemyLastSeenFrontDepth     OptionalString `json:"enemy_last_seen_front_depth"`
	EnemyLastSeenFrontAge       OptionalFloat  `json:"enemy_last_seen_front_age"`
	ObservedContact             OptionalBool   `json:"observed_contact"`
	FrontGap                    OptionalFloat  `json:"front_gap"`
}

type Lanes struct {
	Top Lane `json:"top"`
	Mid Lane `json:"mid"`
	Bot Lane `json:"bot"`
}

type StructureKnowledge string

const (
	StructureKnowledgeUnknown            StructureKnowledge = "UNKNOWN"
	StructureKnowledgeSeenAlive          StructureKnowledge = "SEEN_ALIVE"
	StructureKnowledgeLikelyDestroyed    StructureKnowledge = "LIKELY_DESTROYED"
	StructureKnowledgeConfirmedDestroyed StructureKnowledge = "CONFIRMED_DESTROYED"
)

type OwnLaneStructures struct {
	T1Alive       OptionalBool `json:"t1_alive"`
	T2Alive       OptionalBool `json:"t2_alive"`
	T3Alive       OptionalBool `json:"t3_alive"`
	MeleeRaxAlive OptionalBool `json:"melee_rax_alive"`
	RangeRaxAlive OptionalBool `json:"range_rax_alive"`
}

type EnemyLaneStructures struct {
	T1       StructureKnowledge `json:"t1"`
	T2       StructureKnowledge `json:"t2"`
	T3       StructureKnowledge `json:"t3"`
	MeleeRax StructureKnowledge `json:"melee_rax"`
	RangeRax StructureKnowledge `json:"range_rax"`
}

type Structures struct {
	OurTop   OwnLaneStructures   `json:"our_top"`
	OurMid   OwnLaneStructures   `json:"our_mid"`
	OurBot   OwnLaneStructures   `json:"our_bot"`
	EnemyTop EnemyLaneStructures `json:"enemy_top"`
	EnemyMid EnemyLaneStructures `json:"enemy_mid"`
	EnemyBot EnemyLaneStructures `json:"enemy_bot"`
}

type RoshanPit struct {
	AlliesNear         int `json:"allies_near"`
	VisibleEnemiesNear int `json:"visible_enemies_near"`
}

type Objectives struct {
	RoshanVisible     OptionalBool  `json:"roshan_visible"`
	RoshanLastSeenAge OptionalFloat `json:"roshan_last_seen_age"`
	PitA              RoshanPit     `json:"pit_a"`
	PitB              RoshanPit     `json:"pit_b"`
}

type RecentEvents struct {
	RecentHeroKillAge OptionalFloat `json:"recent_hero_kill_age"`
	RecentSmokeAge    OptionalFloat `json:"recent_smoke_age"`
	RecentGlyphAge    OptionalFloat `json:"recent_glyph_age"`
}

type Quality struct {
	RosterReady           bool `json:"roster_ready"`
	MapStateAvailable     bool `json:"map_state_available"`
	LaneStateAvailable    bool `json:"lane_state_available"`
	BeliefStateAvailable  bool `json:"belief_state_available"`
	ObservedAllyCount     int  `json:"observed_ally_count"`
	VisibleEnemyCount     int  `json:"visible_enemy_count"`
	MissingEnemyCount     int  `json:"missing_enemy_count"`
	UnlocalizedEnemyCount int  `json:"unlocalized_enemy_count"`
	GSIShardFresh         bool `json:"gsi_payload_fresh"`
}

type State struct {
	Meta         Meta         `json:"meta"`
	Match        Match        `json:"match"`
	Self         Self         `json:"self"`
	Team         Team         `json:"team"`
	Allies       []AllyHero   `json:"allies"`
	Enemies      []EnemyHero  `json:"enemies"`
	Lanes        Lanes        `json:"lanes"`
	Structures   Structures   `json:"structures"`
	Objectives   Objectives   `json:"objectives"`
	RecentEvents RecentEvents `json:"recent_events"`
	Quality      Quality      `json:"quality"`
}

type FeatureDef struct {
	Name                string `json:"name"`
	Type                string `json:"type"`
	Meaning             string `json:"meaning"`
	Source              string `json:"source"`
	Availability        string `json:"availability"`
	ConfidenceSemantics string `json:"confidence_semantics"`
	EncodingHint        string `json:"encoding_hint"`
}

type NumericFeatureVector struct {
	SchemaVersion    string    `json:"schema_version"`
	FeatureNames     []string  `json:"feature_names"`
	Values           []float64 `json:"values"`
	AvailabilityMask []uint8   `json:"availability_mask"`
}
