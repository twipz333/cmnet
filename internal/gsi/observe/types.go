package observe

import "cmnet/internal/draft"

// Team is a GSI minimap/building team id. Radiant=2, Dire=3, Neutral=4.
type Team int

const (
	TeamUnknown Team = 0
	TeamRadiant Team = 2
	TeamDire    Team = 3
	TeamNeutral Team = 4
)

// DraftTeam converts a GSI team id into a draft.Team. ok is false for
// spectator/neutral/unknown.
func (t Team) DraftTeam() (draft.Team, bool) {
	switch t {
	case TeamRadiant:
		return draft.Radiant, true
	case TeamDire:
		return draft.Dire, true
	default:
		return 0, false
	}
}

func teamFromGSI(v int) Team {
	switch v {
	case 2:
		return TeamRadiant
	case 3:
		return TeamDire
	case 4:
		return TeamNeutral
	default:
		return TeamUnknown
	}
}

func teamFromName(s string) Team {
	switch s {
	case "radiant", "goodguys":
		return TeamRadiant
	case "dire", "badguys":
		return TeamDire
	default:
		return TeamUnknown
	}
}

// Vec2 is a map coordinate in GSI units.
type Vec2 struct {
	X int
	Y int
}

// Origin reports whether p is the (0,0) noise coordinate.
func (p Vec2) Origin() bool { return p.X == 0 && p.Y == 0 }

func (p Vec2) Equal(q Vec2) bool { return p.X == q.X && p.Y == q.Y }
