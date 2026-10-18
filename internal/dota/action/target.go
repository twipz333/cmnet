package action

// ActionTarget is the semantic spatial/objective target for a macro action.
//
// Note: targets are intentionally NOT map regions.
type ActionTarget string

const (
	TargetNone ActionTarget = "NONE"

	// Lane targets (absolute, perspective-agnostic).
	TargetTop ActionTarget = "TOP"
	TargetMid ActionTarget = "MID"
	TargetBot ActionTarget = "BOT"

	// Neutral objectives.
	TargetRoshan    ActionTarget = "ROSHAN"
	TargetTormentor ActionTarget = "TORMENTOR"

	// Our-side / enemy-side relative areas.
	TargetOurJungle     ActionTarget = "OUR_JUNGLE"
	TargetOurTriangle   ActionTarget = "OUR_TRIANGLE"
	TargetEnemyJungle   ActionTarget = "ENEMY_JUNGLE"
	TargetEnemyTriangle ActionTarget = "ENEMY_TRIANGLE"

	TargetOurBase   ActionTarget = "OUR_BASE"
	TargetEnemyBase ActionTarget = "ENEMY_BASE"
)

// TargetVocab returns deterministic stable ordering of action targets.
func TargetVocab() []ActionTarget {
	return []ActionTarget{
		TargetNone,
		TargetTop, TargetMid, TargetBot,
		TargetRoshan, TargetTormentor,
		TargetOurJungle, TargetOurTriangle,
		TargetEnemyJungle, TargetEnemyTriangle,
		TargetOurBase, TargetEnemyBase,
	}
}

func TargetID(target ActionTarget) (int, bool) {
	for i, t := range TargetVocab() {
		if t == target {
			return i, true
		}
	}
	return -1, false
}
