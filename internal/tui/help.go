package tui

import "strings"

// viewHelp renders the full-screen help overlay (toggled with '?').
func (m model) viewHelp() string {
	var b strings.Builder
	b.WriteString(m.th.title.Render("HELP — key bindings"))
	b.WriteString("\n\n")
	m.help.ShowAll = true
	b.WriteString(m.help.View(m.keys))
	b.WriteString("\n\n")
	b.WriteString(m.th.dim.Render("Pick heroes with ↑/↓, filter with /, apply with enter."))
	b.WriteString("\n")
	b.WriteString(m.th.dim.Render("All recommendations come from the deterministic core; the UI only renders."))
	b.WriteString("\n")
	b.WriteString(m.th.dim.Render("Theme: " + m.th.name + " (press t to cycle)"))
	b.WriteString("\n\n")
	b.WriteString(m.th.status.Render("Press ? to return."))
	return m.th.border.Width(m.width - 2).Render(b.String())
}
