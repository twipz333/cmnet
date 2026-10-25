package action

import "fmt"

// ActionEncoding is a compact categorical encoding for ML/dataset rows.
//
// In this v0 the structured action is fully represented by stable IDs.
type ActionEncoding struct {
	SchemaVersion string `json:"schema_version"`
	ActionID      int    `json:"action_id"`
	KindID        int    `json:"kind_id"`
	TargetID      int    `json:"target_id"`
}

// EncodeAction encodes MacroAction into stable categorical IDs.
func EncodeAction(a MacroAction) (ActionEncoding, error) {
	if a.SchemaVersion != SchemaVersion {
		return ActionEncoding{}, fmt.Errorf("unexpected schema_version: %s", a.SchemaVersion)
	}
	kindID, ok := KindID(a.Kind)
	if !ok {
		return ActionEncoding{}, fmt.Errorf("unknown kind: %s", a.Kind)
	}
	targetID, ok := TargetID(a.Target)
	if !ok {
		return ActionEncoding{}, fmt.Errorf("unknown target: %s", a.Target)
	}
	// ActionID is taken directly from the action itself.
	return ActionEncoding{SchemaVersion: SchemaVersion, ActionID: a.ID, KindID: kindID, TargetID: targetID}, nil
}
