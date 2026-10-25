package tui

import (
	"fmt"
	"strings"
)

// panel wraps content in the themed border at a fixed width.
func panel(th theme, width int, content string) string {
	w := width - 2
	if w < 10 {
		w = 10
	}
	return th.border.Width(w).Render(content)
}

// viewStatus renders the bottom status bar: data freshness, bundle version, our
// side, and input source. GSI status is "manual" until M2.
func (m model) viewStatus(width int) string {
	bundleVer := m.cfg.BundleVersion
	if len(bundleVer) > 12 {
		bundleVer = bundleVer[:12]
	}
	patch := m.cfg.PatchTag
	if patch == "" {
		patch = "?"
	}
	input := "input: manual"
	if m.gsiMode {
		if m.gsiSeen {
			input = "GSI: ● connected"
		} else {
			input = "GSI: ○ waiting"
		}
	}
	parts := []string{
		fmt.Sprintf("patch: %s", patch),
		fmt.Sprintf("bundle: %s", bundleVer),
		fmt.Sprintf("you: %s", m.cfg.OurTeam),
		input,
	}
	if m.status != "" {
		parts = append(parts, m.th.dire.Render(m.status))
	}
	parts = append(parts, m.th.dim.Render("? help"))
	bar := m.th.status.Render(strings.Join(parts, "  •  "))
	return m.th.status.Width(width).Render(bar)
}
