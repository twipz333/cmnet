package behavioraudit

import (
	"fmt"
	"log"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

// ActionView is the user-facing MacroAction presentation.
// Numeric ActionID stays for schema/NDJSON/metrics; UI uses Name/Description.
type ActionView struct {
	ActionID    int    `json:"action_id"`
	ActionName  string `json:"action_name"`
	Description string `json:"action_description,omitempty"`
	Known       bool   `json:"known"`
}

// NamedCompeting is CompetingCandidate + human-readable fields for the reviewer UI.
type NamedCompeting struct {
	ActionID      int      `json:"action_id"`
	ActionName    string   `json:"action_name"`
	Description   string   `json:"action_description,omitempty"`
	Confidence    float64  `json:"confidence"`
	EvidenceTypes []string `json:"evidence_types,omitempty"`
	ScoreGap      float64  `json:"score_gap,omitempty"`
	Unknown       bool     `json:"unknown,omitempty"`
}

// NamedRejected is RejectedCandidate + human-readable fields.
type NamedRejected struct {
	ActionID      int      `json:"action_id"`
	ActionName    string   `json:"action_name"`
	Description   string   `json:"action_description,omitempty"`
	Confidence    float64  `json:"confidence"`
	Reason        string   `json:"reason"`
	EvidenceTypes []string `json:"evidence_types,omitempty"`
	Unknown       bool     `json:"unknown,omitempty"`
}

// HumanActionName returns the canonical display name for an action ID
// from the given MacroAction schema space.
func HumanActionName(space action.ActionSpace, id int) string {
	return ResolveActionView(space, id).ActionName
}

// ResolveActionView looks up id in the canonical registry.
// Unknown IDs become "UNKNOWN ACTION (id=N)" and are logged.
func ResolveActionView(space action.ActionSpace, id int) ActionView {
	a, ok := space.ByID(id)
	if !ok {
		log.Printf("behavioraudit: unknown MacroAction id=%d (schema=%s)", id, space.SchemaVersion())
		return ActionView{
			ActionID:   id,
			ActionName: fmt.Sprintf("UNKNOWN ACTION (id=%d)", id),
			Known:      false,
		}
	}
	return ActionView{
		ActionID:    id,
		ActionName:  a.String(), // KIND [TARGET] via action.HumanString
		Description: HumanDefinition(a.Kind, a.Target),
		Known:       true,
	}
}

// CheckMacroSchema reports whether audit schema matches the loaded registry.
func CheckMacroSchema(got string) (ok bool, want string) {
	want = action.SchemaVersion
	if got == "" {
		return false, want
	}
	return got == want, want
}

// ValidateCasesMacroSchema fails fast on incompatible MacroActionSchemaVersion.
// Empty version is allowed with a warning (legacy rows); wrong non-empty version is an error.
func ValidateCasesMacroSchema(cases []AuditCase) error {
	want := action.SchemaVersion
	empty := 0
	for _, c := range cases {
		got := c.MacroActionSchemaVersion
		if got == "" {
			empty++
			continue
		}
		if got != want {
			return fmt.Errorf("macro schema mismatch: case %q has %q, reviewer expects %q — refusing to map action IDs",
				c.ID, got, want)
		}
	}
	if empty > 0 {
		log.Printf("behavioraudit: warning: %d cases missing macro_action_schema_version (assuming %s)", empty, want)
	}
	return nil
}

// EnrichCompeting attaches canonical names/descriptions for the API.
func EnrichCompeting(space action.ActionSpace, in []macrolabel.CompetingCandidate) []NamedCompeting {
	if len(in) == 0 {
		return nil
	}
	out := make([]NamedCompeting, 0, len(in))
	for _, c := range in {
		v := ResolveActionView(space, c.ActionID)
		out = append(out, NamedCompeting{
			ActionID:      c.ActionID,
			ActionName:    v.ActionName,
			Description:   v.Description,
			Confidence:    c.Confidence,
			EvidenceTypes: c.EvidenceTypes,
			ScoreGap:      c.ScoreGap,
			Unknown:       !v.Known,
		})
	}
	return out
}

// EnrichRejected attaches canonical names/descriptions for the API.
func EnrichRejected(space action.ActionSpace, in []macrolabel.RejectedCandidate) []NamedRejected {
	if len(in) == 0 {
		return nil
	}
	out := make([]NamedRejected, 0, len(in))
	for _, c := range in {
		v := ResolveActionView(space, c.ActionID)
		out = append(out, NamedRejected{
			ActionID:      c.ActionID,
			ActionName:    v.ActionName,
			Description:   v.Description,
			Confidence:    c.Confidence,
			Reason:        c.Reason,
			EvidenceTypes: c.EvidenceTypes,
			Unknown:       !v.Known,
		})
	}
	return out
}
