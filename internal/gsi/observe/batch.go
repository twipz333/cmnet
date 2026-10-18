package observe

// Batch is everything noticed in a single GSI payload. It is a tick snapshot,
// not accumulated knowledge.
type Batch struct {
	Meta Meta

	Match MatchObservation
	Self  SelfObservation

	Minimap        []MinimapEntity
	MinimapPresent bool
	Heroes         []HeroPosition
	Creeps         []CreepObservation
	Wards          []WardObservation
	Roshan         []RoshanObservation

	OwnBuildings []BuildingObservation
	Structures   []StructureObservation

	Events []Event

	Diagnostics Diagnostics
}

// Diagnostics counts skippable oddities in one payload. They never fail Extract.
type Diagnostics struct {
	UnknownMinimapUnits int
	UnknownEvents       int
	DroppedHeroNoise    int
	MinimapEntities     int
}

// MatchObservation is the map block. Pointers are unset when the field is absent.
type MatchObservation struct {
	Present      bool
	GameTime     *int
	ClockTime    *int
	GameState    *string
	RadiantScore *int
	DireScore    *int
	Daytime      *bool
	Paused       *bool
	MatchID      *string
	WinTeam      *string
}

// SelfObservation is the local player's player+hero+abilities+items.
type SelfObservation struct {
	Present bool

	HeroID   *int
	HeroName *string
	Position *Vec2

	Level *int
	XP    *int

	Alive          *bool
	RespawnSeconds *int

	HP        *int
	MaxHP     *int
	HPPercent *int

	Mana        *int
	MaxMana     *int
	ManaPercent *int

	Gold           *int
	GoldReliable   *int
	GoldUnreliable *int
	GPM            *int
	XPM            *int

	Kills    *int
	Deaths   *int
	Assists  *int
	LastHits *int
	Denies   *int

	BuybackCost     *int
	BuybackCooldown *int

	Smoked          *bool
	AghanimsScepter *bool
	AghanimsShard   *bool

	PlayerSlot *int
	TeamSlot   *int
	TeamName   *string
	Team       Team

	Abilities []AbilityObservation
	Items     ItemSet
}

// AbilityObservation is one abilityN slot.
type AbilityObservation struct {
	Slot        string
	Name        string
	Level       *int
	CanCast     *bool
	Cooldown    *float64
	MaxCooldown *float64
	Ultimate    *bool
	Charges     *int
	MaxCharges  *int
	Passive     *bool
}

// ItemObservation is one inventory/backpack/stash/TP/neutral slot.
type ItemObservation struct {
	Slot        string
	Name        string
	CanCast     *bool
	Cooldown    *float64
	MaxCooldown *float64
	Charges     *int
	Passive     *bool
	Purchaser   *int
}

// ItemSet groups the local player's item slots.
type ItemSet struct {
	Inventory []ItemObservation // slot0-5
	Backpack  []ItemObservation // slot6-8
	Stash     []ItemObservation
	TP        *ItemObservation
	Neutral   []ItemObservation
}

// MinimapEntity is one minimap object from this payload. PayloadKey (o123) is
// debugging metadata for this tick only and must not be used as identity.
type MinimapEntity struct {
	PayloadKey      string
	UnitName        string
	DisplayName     string
	Team            Team
	Image           string
	Position        Vec2
	Yaw             *int
	VisionRange     *int
	ObservationTime *int
	Kind            EntityKind
}

// HeroPosition is a roster-candidate hero observation after junk filtering.
// One hero may produce several of these in the same tick (illusions).
type HeroPosition struct {
	Hero        string
	Team        Team
	Position    Vec2
	Image       string
	ImageKind   HeroImageKind
	Yaw         *int
	VisionRange *int
}

// CreepObservation is a currently visible creep. Creeps have no identity
// across ticks.
type CreepObservation struct {
	UnitName string
	Team     Team
	Class    CreepClass
	Position Vec2
	Image    string
}

// WardObservation is a currently visible ward.
type WardObservation struct {
	UnitName string
	Team     Team
	Class    WardClass
	Position Vec2
	Image    string
}

// RoshanObservation is Roshan when he is on the minimap with a valid position.
type RoshanObservation struct {
	Position    Vec2
	Image       string
	Team        Team
	VisionRange *int
}

// BuildingObservation is an own-building HP reading from GSI buildings.
type BuildingObservation struct {
	UnitName  string
	Team      Team
	Class     StructureClass
	Lane      LaneHint
	Health    *int
	MaxHealth *int
	Alive     *bool
}

// StructureObservation is a structure seen on the minimap this tick (own or enemy).
type StructureObservation struct {
	UnitName string
	Team     Team
	Class    StructureClass
	Lane     LaneHint
	Position *Vec2
	Image    string
}
