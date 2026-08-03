package tui

import (
	"fmt"
	"strings"
)

// viewPositions renders the patch meta per lane position (#9). Banned/taken heroes
// are already removed by the core, so the lists update as the draft progresses.
func (m model) viewPositions(width int) string {
	var b strings.Builder
	b.WriteString(m.th.title.Render("PATCH META BY POSITION"))
	b.WriteByte('\n')
	if len(m.result.PositionMeta) == 0 {
		b.WriteString(m.th.dim.Render("no position data (retrain with online access)"))
		return panel(m.th, width, b.String())
	}
	for _, pm := range m.result.PositionMeta {
		names := make([]string, 0, len(pm.Heroes))
		for _, h := range pm.Heroes {
			// Show the per-position win rate when the STRATZ-baked meta carries it;
			// the coarse roles fallback has no win rate (0) so we show just the name.
			if h.WinRate > 0 {
				names = append(names, fmt.Sprintf("%s %.0f%%", m.heroName(h.HeroID), h.WinRate*100))
			} else {
				names = append(names, m.heroName(h.HeroID))
			}
		}
		line := strings.Join(names, ", ")
		if line == "" {
			line = m.th.dim.Render("(none available)")
		}
		fmt.Fprintf(&b, "%s %s\n", m.th.highlight.Render(fmt.Sprintf("P%d", pm.Position)), line)
	}
	return panel(m.th, width, strings.TrimRight(b.String(), "\n"))
}

// viewAdvice renders fired item/advice rules against the enemy draft (#10).
func (m model) viewAdvice(width int) string {
	var b strings.Builder
	b.WriteString(m.th.title.Render("ITEM / PLAY ADVICE"))
	b.WriteByte('\n')
	for _, a := range m.result.ItemAdvice {
		b.WriteString("• " + a + "\n")
	}
	return panel(m.th, width, strings.TrimRight(b.String(), "\n"))
}
