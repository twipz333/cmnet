package audit

import (
	"fmt"

	"cmnet/internal/dota/action"
)

func ValidateHumanActionIDs(items []AuditItem, space action.ActionSpace) error {
	for _, it := range items {
		if it.Human == nil || it.Human.ActionID == nil {
			continue
		}
		id := *it.Human.ActionID
		if _, ok := space.ByID(id); !ok {
			return fmt.Errorf("audit_id=%s: invalid human action_id=%d", it.AuditID, id)
		}
	}
	return nil
}

func ValidateHumanKinds(items []AuditItem) error {
	allowed := map[string]struct{}{}
	for _, k := range action.KindVocab() {
		allowed[string(k)] = struct{}{}
	}
	for _, it := range items {
		if it.Human == nil || it.Human.Kind == "" {
			continue
		}
		if _, ok := allowed[it.Human.Kind]; !ok {
			return fmt.Errorf("audit_id=%s: invalid human kind=%q", it.AuditID, it.Human.Kind)
		}
	}
	return nil
}
