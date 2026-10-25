package macrolabel

import "cmnet/internal/dota/action"

// NewV0 constructs MacroActionLabeler v0.2 (audit-hardened weak supervision).
func NewV0(cfg Config, space action.ActionSpace) MacroActionLabeler {
	return MacroActionLabeler{space: space, cfg: cfg}
}
