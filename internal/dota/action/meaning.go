package action

import "fmt"

// Meaning returns a human-readable explanation for this exact action.
//
// Must be deterministic (no maps).
func Meaning(kind ActionKind, target ActionTarget) string {
	switch kind {
	case KindHold:
		return "Keep the current team macro posture; do not force a new macro objective"
	case KindFarm:
		switch target {
		case TargetOurJungle:
			return "Prioritize converting our jungle area into resources"
		case TargetOurTriangle:
			return "Prioritize converting our triangle area into resources"
		case TargetEnemyJungle:
			return "Convert enemy jungle space into resources (farm enemy side without direct contest commitment)"
		case TargetEnemyTriangle:
			return "Convert enemy triangle space into resources (farm enemy side without direct contest commitment)"
		}
	case KindPush:
		switch target {
		case TargetTop:
			return "Advance the top lane as the team's next macro intent"
		case TargetMid:
			return "Advance the mid lane as the team's next macro intent"
		case TargetBot:
			return "Advance the bottom lane as the team's next macro intent"
		}
	case KindDefend:
		switch target {
		case TargetTop:
			return "Shift macro resources to defend the top lane / area"
		case TargetMid:
			return "Shift macro resources to defend the mid lane / area"
		case TargetBot:
			return "Shift macro resources to defend the bottom lane / area"
		case TargetOurBase:
			return "Shift macro resources to defend our base area"
		}
	case KindControlObjective:
		switch target {
		case TargetRoshan:
			return "Secure and hold space around Roshan (may delay starting objective)"
		case TargetTormentor:
			return "Secure and hold space around Tormentor (may delay starting objective)"
		}
	case KindTakeObjective:
		switch target {
		case TargetRoshan:
			return "Directly start/finish killing Roshan"
		case TargetTormentor:
			return "Directly start/finish killing Tormentor"
		}
	case KindSmoke:
		switch target {
		case TargetTop:
			return "Use smoke to search for contact in top macro area"
		case TargetMid:
			return "Use smoke to search for contact in mid macro area"
		case TargetBot:
			return "Use smoke to search for contact in bot macro area"
		case TargetEnemyJungle:
			return "Use smoke to enter enemy jungle and create contact"
		case TargetEnemyTriangle:
			return "Use smoke to enter enemy triangle and create contact"
		}
	case KindInvade:
		switch target {
		case TargetEnemyJungle:
			return "Actively enter and contest enemy jungle space"
		case TargetEnemyTriangle:
			return "Actively enter and contest enemy triangle space"
		}
	case KindGroup:
		return "Form a tighter main group without a specific objective target"
	case KindSplitMap:
		return "Intentionally distribute resources across the map instead of one main group"
	case KindSetVision:
		switch target {
		case TargetTop:
			return "Prioritize setting/updating vision in the top macro area"
		case TargetMid:
			return "Prioritize setting/updating vision in the mid macro area"
		case TargetBot:
			return "Prioritize setting/updating vision in the bottom macro area"
		case TargetRoshan:
			return "Prioritize setting/updating vision around the Roshan pit"
		case TargetEnemyJungle:
			return "Prioritize setting/updating vision in enemy jungle"
		case TargetEnemyTriangle:
			return "Prioritize setting/updating vision in enemy triangle"
		}
	case KindSiege:
		if target == TargetEnemyBase {
			return "Organize a coordinated highground siege on the enemy base"
		}
	case KindReset:
		return "Stop current macro activity and re-organize (controlled disengage/recommit readiness)"
	case KindRetreat:
		return "Urgently increase distance from threat; stop the current unsafe macro commitment"
	}
	return fmt.Sprintf("meaning missing for %s/%s", kind, target)
}
