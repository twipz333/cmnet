package tui

import (
	"fmt"
	"strings"

	"cmnet/internal/draft"
)

// heroName resolves a hero id to a display name, falling back to the id.
func (m model) heroName(id int) string {
	if id == draft.NoHero {
		return "___"
	}
	if n, ok := m.nameByID[id]; ok {
		return n
	}
	return fmt.Sprintf("#%d", id)
}

func (m model) heroNames(ids []int) string {
	if len(ids) == 0 {
		return m.th.dim.Render("—")
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = m.heroName(id)
	}
	return strings.Join(parts, ", ")
}

// viewBoard renders the draft board: both teams' picks and bans, plus the current
// phase and whose turn it is.
func (m model) viewBoard() string {
	teamStyle := func(t draft.Team) func(...string) string {
		if t == draft.Radiant {
			return m.th.radiant.Render
		}
		return m.th.dire.Render
	}

	var b strings.Builder
	b.WriteString(m.th.title.Render("DRAFT BOARD"))
	b.WriteString("  ")
	b.WriteString(m.phaseLine())
	if s := m.strengthLine(); s != "" {
		b.WriteString("  ")
		b.WriteString(s)
	}
	b.WriteByte('\n')

	for _, t := range []draft.Team{draft.Radiant, draft.Dire} {
		name := teamStyle(t)(fmt.Sprintf("%-8s", t.String()))
		fmt.Fprintf(&b, "%s picks: %s\n", name, m.heroNames(m.view.Picks(t)))
		fmt.Fprintf(&b, "%s  bans: %s\n", strings.Repeat(" ", 8), m.heroNames(m.view.Bans(t)))
	}
	return m.th.border.Width(m.width - 2).Render(strings.TrimRight(b.String(), "\n"))
}

// strengthLine renders the current draft's win-prob from our perspective, e.g.
// "Draft: 53.2% (you) [3v2]". Empty until at least one hero is picked. The bracket
// shows pick counts so a lopsided/early read is visible as provisional.
func (m model) strengthLine() string {
	s := m.result.Strength
	if s == nil {
		return ""
	}
	style := m.th.status.Render
	switch {
	case s.OurProb >= 0.55:
		style = m.th.highlight.Render
	case s.OurProb <= 0.45:
		style = m.th.dire.Render
	}
	ourPicks, enemyPicks := s.RadiantPicks, s.DirePicks
	if m.cfg.OurTeam == draft.Dire {
		ourPicks, enemyPicks = s.DirePicks, s.RadiantPicks
	}
	return style(fmt.Sprintf("Draft: %.1f%% (you) [%dv%d]", s.OurProb*100, ourPicks, enemyPicks))
}

func (m model) phaseLine() string {
	step, ok := m.view.Current()
	if !ok {
		return m.th.highlight.Render("draft complete")
	}
	turn := step.Team.String()
	mark := ""
	if step.Team == m.cfg.OurTeam {
		mark = m.th.highlight.Render(" (you)")
	}
	return m.th.status.Render(fmt.Sprintf("Phase: %s • Turn: %s%s", step.Action, turn, mark))
}
