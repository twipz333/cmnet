package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"cmnet/internal/recommender"
)

// matricesTopN is how many heroes to list per matchup column.
const matricesTopN = 12

// viewMatrices renders the synergy/counter inspection overlay (toggled with 'v')
// for the hero currently highlighted in the picker. Values are LIFT versus the
// heroes' solo win rates (see internal/matrices), shown as signed percentage points
// around neutral — so they read like the advantage/disadvantage figures on sites
// such as Dotabuff, not raw pair win rates. Browse heroes with ↑/↓; esc/v closes.
func (m model) viewMatrices() string {
	var b strings.Builder
	b.WriteString(m.th.title.Render("SYNERGY / COUNTER"))
	b.WriteByte('\n')

	it, ok := m.list.SelectedItem().(heroItem)
	if !ok {
		b.WriteString(m.th.dim.Render("\nNo hero selected. Press esc to return."))
		return m.th.border.Width(m.width - 2).Render(b.String())
	}
	hero := it.h
	b.WriteString(m.th.highlight.Render(hero.Label()))
	b.WriteString("  ")
	b.WriteString(m.th.dim.Render("(lift vs solo win rate; + = better than expected)"))
	b.WriteString("\n\n")

	syn, counters, counteredBy, found := m.sess.Matchups(hero.ID, matricesTopN)
	if !found {
		b.WriteString(m.th.dim.Render("No matrix data for this hero."))
		return m.th.border.Width(m.width - 2).Render(b.String())
	}

	colW := (m.width - 8) / 3
	if colW < 16 {
		colW = 16
	}
	cols := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.matchupColumn("Best teammates", syn, colW),
		m.matchupColumn("Strong vs", counters, colW),
		m.matchupColumn("Weak vs", counteredBy, colW),
	)
	b.WriteString(cols)
	b.WriteString("\n\n")
	b.WriteString(m.th.status.Render("↑/↓: change hero   •   esc / v: return"))
	return m.th.border.Width(m.width - 2).Render(b.String())
}

// matchupColumn renders one labeled column of hero matchup lifts.
func (m model) matchupColumn(title string, rows []recommender.HeroScore, width int) string {
	var b strings.Builder
	b.WriteString(m.th.title.Render(title))
	b.WriteByte('\n')
	if len(rows) == 0 {
		b.WriteString(m.th.dim.Render("— no data —"))
	}
	for _, r := range rows {
		pp := (r.Value - 0.5) * 100 // lift in percentage points
		val := fmt.Sprintf("%+.1f", pp)
		style := m.th.status.Render
		if pp >= 0 {
			style = m.th.radiant.Render
		} else {
			style = m.th.dire.Render
		}
		name := m.heroName(r.HeroID)
		fmt.Fprintf(&b, "%s %s\n", style(fmt.Sprintf("%5s", val)), name)
	}
	return lipgloss.NewStyle().Width(width).Render(strings.TrimRight(b.String(), "\n"))
}
