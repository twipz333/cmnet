package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"cmnet/internal/draft"
)

// gsiMsg carries a fresh draft snapshot parsed from a GSI event.
type gsiMsg struct{ snap *draft.Snapshot }

// waitForGSI returns a command that blocks on the next GSI snapshot and wraps it
// in a gsiMsg. When the channel closes it yields nil (no further events).
func waitForGSI(ch <-chan *draft.Snapshot) tea.Cmd {
	return func() tea.Msg {
		snap, ok := <-ch
		if !ok {
			return nil
		}
		return gsiMsg{snap: snap}
	}
}
