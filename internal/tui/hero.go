package tui

import (
	"fmt"

	"cmnet/internal/session"
)

// Hero is the display record for a hero. It is an alias for the session type so
// that callers (cmd/advisor) and both front ends speak the same vocabulary.
type Hero = session.Hero

// heroItem adapts a Hero to the bubbles/list item interface.
type heroItem struct{ h Hero }

func (i heroItem) Title() string       { return i.h.Label() }
func (i heroItem) Description() string { return fmt.Sprintf("id %d", i.h.ID) }
func (i heroItem) FilterValue() string { return i.h.Label() }
