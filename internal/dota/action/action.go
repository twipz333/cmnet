package action

import "fmt"

// MacroAction is a factorized team macro intent.
//
// Equality semantics: same schema_version + kind + target.
type MacroAction struct {
	SchemaVersion string       `json:"schema_version"`
	ID            int          `json:"id"`
	Kind          ActionKind   `json:"kind"`
	Target        ActionTarget `json:"target"`
}

func (a MacroAction) Equal(b MacroAction) bool {
	return a.SchemaVersion == b.SchemaVersion && a.Kind == b.Kind && a.Target == b.Target
}

func (a MacroAction) String() string {
	return HumanString(a.Kind, a.Target)
}

// HumanString formats kind+target without printing "KIND NONE".
func HumanString(kind ActionKind, target ActionTarget) string {
	if target == TargetNone {
		return string(kind)
	}
	return fmt.Sprintf("%s %s", kind, target)
}
