package action

// SchemaV0 is the machine-readable action registry for macro-action-v0.1.
//
// This registry is static schema: it does not depend on map geometry or state.
type SchemaV0 struct {
	SchemaVersion string              `json:"schema_version"`
	Actions       []SchemaActionEntry `json:"actions"`
}

type SchemaActionEntry struct {
	ID      int          `json:"id"`
	Kind    ActionKind   `json:"kind"`
	Target  ActionTarget `json:"target"`
	Meaning string       `json:"meaning"`
}

// RegistryV0 returns the canonical action registry for SchemaVersion.
func RegistryV0() SchemaV0 {
	s := DefaultSpaceV0()
	entries := make([]SchemaActionEntry, 0, len(s.actions))
	for _, a := range s.actions {
		entries = append(entries, SchemaActionEntry{
			ID:      a.ID,
			Kind:    a.Kind,
			Target:  a.Target,
			Meaning: Meaning(a.Kind, a.Target),
		})
	}
	return SchemaV0{SchemaVersion: SchemaVersion, Actions: entries}
}
