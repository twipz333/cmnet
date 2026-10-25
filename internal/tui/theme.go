package tui

import "github.com/charmbracelet/lipgloss"

// theme holds the lipgloss styles for the UI. Themes are swappable at runtime
// (the 't' key); the TUI is render-only so this never affects determinism.
type theme struct {
	name      string
	border    lipgloss.Style
	title     lipgloss.Style
	radiant   lipgloss.Style
	dire      lipgloss.Style
	dim       lipgloss.Style
	highlight lipgloss.Style
	status    lipgloss.Style
}

// palette is the small set of colors a theme is built from.
type palette struct {
	name                                       string
	title, radiant, dire, dim, highlight, stat string
}

var palettes = []palette{
	{name: "dusk", title: "63", radiant: "78", dire: "203", dim: "241", highlight: "220", stat: "245"},
	{name: "mono", title: "252", radiant: "250", dire: "244", dim: "240", highlight: "255", stat: "245"},
	{name: "neon", title: "201", radiant: "48", dire: "197", dim: "240", highlight: "226", stat: "159"},
}

func themeFrom(p palette) theme {
	return theme{
		name:      p.name,
		border:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
		title:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.title)),
		radiant:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.radiant)),
		dire:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.dire)),
		dim:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)),
		highlight: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.highlight)),
		status:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.stat)),
	}
}

// themeByIndex returns the theme at index (wrapping around).
func themeByIndex(i int) theme {
	return themeFrom(palettes[((i % len(palettes)) + len(palettes)) % len(palettes)])
}

func defaultTheme() theme { return themeByIndex(0) }

// themeCount is the number of available themes.
func themeCount() int { return len(palettes) }
