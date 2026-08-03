package tui

import (
	"fmt"
	"strings"
)

// viewRecs renders the ranked recommendations for the current step.
func (m model) viewRecs(width int) string {
	var b strings.Builder
	title := "RECOMMENDATIONS"
	if !m.result.OurTurn {
		title += m.th.dim.Render(" (enemy step)")
	} else if m.result.IsPick {
		title += " (pick)"
	} else {
		title += " (ban)"
	}
	b.WriteString(m.th.title.Render(title))
	b.WriteByte('\n')

	if len(m.result.Recs) == 0 {
		b.WriteString(m.th.dim.Render("no recommendations"))
		return panel(m.th, width, b.String())
	}
	for i, r := range m.result.Recs {
		line := fmt.Sprintf("%2d %-16s %6.3f  %s", i+1, m.heroName(r.HeroID), r.Score, m.th.dim.Render(r.Why))
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return panel(m.th, width, strings.TrimRight(b.String(), "\n"))
}
