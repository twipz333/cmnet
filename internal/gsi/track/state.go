package track

import "cmnet/internal/gsi/observe"

// StructureLife is the v0 enemy-structure knowledge.
type StructureLife int

const (
	StructureUnknown StructureLife = iota
	StructureSeenAlive
	StructureLikelyDestroyed
	StructureConfirmedDestroyed
)

// StructureDestroyed is the historical alias for an identified kill.
const StructureDestroyed = StructureConfirmedDestroyed

func (s StructureLife) String() string {
	switch s {
	case StructureSeenAlive:
		return "SEEN_ALIVE"
	case StructureLikelyDestroyed:
		return "LIKELY_DESTROYED"
	case StructureConfirmedDestroyed:
		return "CONFIRMED_DESTROYED"
	default:
		return "UNKNOWN"
	}
}

// Clock is the time basis for ages. GameTime is preferred; ClockTime is the
// fallback. Wall-clock is never used for last_seen_age.
type Clock struct {
	Seq          uint64
	ReceivedAt   int64
	GameTime     *int
	ClockTime    *int
	Paused       bool
	GameState    string
	MatchID      string
	RadiantScore int
	DireScore    int
}

// Instant is the comparable game-clock used for ages. ok is false when neither
// game_time nor clock_time is available.
func (c Clock) Instant() (int, bool) {
	if c.GameTime != nil {
		return *c.GameTime, true
	}
	if c.ClockTime != nil {
		return *c.ClockTime, true
	}
	return 0, false
}

func ageSeconds(now Clock, seenAt *int) *float64 {
	if seenAt == nil {
		return nil
	}
	t, ok := now.Instant()
	if !ok {
		return nil
	}
	v := float64(t - *seenAt)
	if v < 0 {
		v = 0
	}
	return &v
}

// PositionSample is one observed coordinate plus when it was seen.
type PositionSample struct {
	Position observe.Vec2
	GameTime *int
	Image    string
}

// AllyHeroTrack is one allied hero.
type AllyHeroTrack struct {
	Hero                  string
	CurrentlyObserved     bool
	PositionCandidates    []observe.Vec2
	LastObservedAt        *int
	LastObservedPositions []observe.Vec2
}

// EnemyHeroTrack is one enemy hero. When not visible, Position is unknown;
// LastSeen* remains.
type EnemyHeroTrack struct {
	Hero               string
	VisibleNow         bool
	VisiblePositions   []observe.Vec2
	LastSeenAt         *int
	LastSeenPositions  []observe.Vec2
	MissingSince       *int
	LastSeenAgeSeconds *float64
}

// SelfState is the latest local-player observation.
type SelfState struct {
	observe.SelfObservation
}

// BuildingState is the last known own-building HP row.
type BuildingState struct {
	UnitName  string
	Team      observe.Team
	Class     observe.StructureClass
	Lane      observe.LaneHint
	Health    *int
	MaxHealth *int
	Alive     *bool
}

// EnemyStructureTrack is an enemy tower/rax/ancient.
type EnemyStructureTrack struct {
	UnitName string
	Team     observe.Team
	Class    observe.StructureClass
	Lane     observe.LaneHint
	Life     StructureLife
	LastSeen *int
	Position *observe.Vec2
}

// WardTrack is a currently or recently observed ward.
type WardTrack struct {
	Class    observe.WardClass
	Team     observe.Team
	Position observe.Vec2
	LastSeen *int
	Observed bool
}

// RoshanTrack is minimap-only Roshan memory. Disappearance is not death.
type RoshanTrack struct {
	VisibleNow        bool
	CurrentPositions  []observe.Vec2
	LastSeenAt        *int
	LastSeenPositions []observe.Vec2
	LastSeenAlive     bool
	KnownDeadAt       *int
}

// ItemPurchase is a recent ITEM_PURCHASE event. Hero is empty until a
// player-id map exists.
type ItemPurchase struct {
	PlayerID *int
	Hero     string
	Item     string
	ItemID   *int
	GameTime *int
}

// SmokeActivation is a SMOKE_ACTIVATED event.
type SmokeActivation struct {
	PlayerID   *int
	GameTime   *int
	ReceivedAt int64
}

// CreepObservation is a currently visible creep (no identity).
type CreepObservation = observe.CreepObservation

// Event is a recently accepted (deduplicated) GSI event.
type Event = observe.Event

// State is a read-only snapshot of tracker memory. Slices are copies.
type State struct {
	Clock  Clock
	Roster Roster

	Self SelfState

	Allies  []AllyHeroTrack
	Enemies []EnemyHeroTrack

	OwnBuildings    []BuildingState
	EnemyStructures []EnemyStructureTrack

	Creeps         []CreepObservation
	MinimapPresent bool

	AlliedWards        []WardTrack
	ObservedEnemyWards []WardTrack

	Roshan RoshanTrack

	RecentEvents    []Event
	RecentPurchases []ItemPurchase
	RecentSmokes    []SmokeActivation
}

// Stats are cheap counters for diagnostics. No metrics subsystem.
type Stats struct {
	PayloadsProcessed     int
	ParseErrors           int
	MinimapEntities       int
	HeroObservations      int
	UnknownMinimap        int
	UnknownEvents         int
	TrackerResets         int
	EventsAccepted        int
	EventsDeduped         int
	MultiHeroTicks        int
	CreepObservations     int
	RoshanObservations    int
	EventsByKind          map[observe.EventKind]int
	VisibilityAppeared    map[string]int
	VisibilityDisappeared map[string]int
}
