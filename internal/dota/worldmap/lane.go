package worldmap

// LaneID is the stable identity of a lane path. UNKNOWN is "could not assign".
type LaneID string

const (
	LaneUnknown LaneID = "UNKNOWN"
	LaneTop     LaneID = "TOP"
	LaneMid     LaneID = "MID"
	LaneBot     LaneID = "BOT"
)

func (l LaneID) String() string { return string(l) }

func requiredLanes() []LaneID {
	return []LaneID{LaneTop, LaneMid, LaneBot}
}

// AbsoluteProgress is distance along the polyline from the Radiant end.
// 0 = Radiant, 1 = Dire. Independent of the local player's side.
type AbsoluteProgress float64

// TeamProgress is distance toward the enemy base from a given side.
// 0 = own base, 1 = enemy base.
type TeamProgress float64

// TowardEnemy converts Radiant-origin progress into team-relative progress.
func TowardEnemy(abs AbsoluteProgress, fromRadiant bool) TeamProgress {
	if fromRadiant {
		return TeamProgress(abs)
	}
	return TeamProgress(1 - float64(abs))
}

// LaneDepth is a coarse bucket of TeamProgress. Names are team-relative.
type LaneDepth string

const (
	DepthUnknown   LaneDepth = "UNKNOWN"
	DepthOwnBase   LaneDepth = "OWN_BASE"
	DepthOwnSide   LaneDepth = "OWN_SIDE"
	DepthCenter    LaneDepth = "CENTER"
	DepthEnemySide LaneDepth = "ENEMY_SIDE"
	DepthEnemyBase LaneDepth = "ENEMY_BASE"
)

func (d LaneDepth) String() string { return string(d) }

// LaneProjection is the result of projecting a world point onto lane paths.
type LaneProjection struct {
	Lane     LaneID
	Assigned bool
	// Ambiguous is true when two lanes are within ambiguity_epsilon.
	Ambiguous bool
	TooFar    bool

	Nearest          WorldPosition
	Distance         float64
	AbsoluteDistance float64
	AbsoluteProgress AbsoluteProgress
}
