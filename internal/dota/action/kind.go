package action

// ActionKind is the high-level macro intent of the team.
// Stable vocabulary order matters for deterministic IDs.
type ActionKind string

const (
	KindHold             ActionKind = "HOLD"
	KindFarm             ActionKind = "FARM"
	KindPush             ActionKind = "PUSH"
	KindDefend           ActionKind = "DEFEND"
	KindControlObjective ActionKind = "CONTROL_OBJECTIVE"
	KindTakeObjective    ActionKind = "TAKE_OBJECTIVE"
	KindSmoke            ActionKind = "SMOKE"
	KindInvade           ActionKind = "INVADE"
	KindGroup            ActionKind = "GROUP"
	KindSplitMap         ActionKind = "SPLIT_MAP"
	KindSetVision        ActionKind = "SET_VISION"
	KindSiege            ActionKind = "SIEGE"
	KindReset            ActionKind = "RESET"
	KindRetreat          ActionKind = "RETREAT"
)

// KindVocab returns the deterministic stable ordering of action kinds.
func KindVocab() []ActionKind {
	return []ActionKind{
		KindHold,
		KindFarm,
		KindPush,
		KindDefend,
		KindControlObjective,
		KindTakeObjective,
		KindSmoke,
		KindInvade,
		KindGroup,
		KindSplitMap,
		KindSetVision,
		KindSiege,
		KindReset,
		KindRetreat,
	}
}

func KindID(kind ActionKind) (int, bool) {
	for i, k := range KindVocab() {
		if k == kind {
			return i, true
		}
	}
	return -1, false
}
