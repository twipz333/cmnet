package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewMain composes the full layout: draft board on top, then the hero picker,
// recommendations, and enemy prediction side by side, with the status bar below.
func (m model) viewMain() string {
	var middle string
	if m.gsiMode {
		// No hero picker in read-only GSI mode: split width between the two panels.
		recsW := m.width / 2
		enemyW := m.width - recsW
		middle = lipgloss.JoinHorizontal(lipgloss.Top, m.viewRecs(recsW), m.viewEnemy(enemyW))
	} else {
		lw := listWidth(m.width)
		rest := m.width - lw
		recsW := rest / 2
		enemyW := rest - recsW
		middle = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.list.View(),
			m.viewRecs(recsW),
			m.viewEnemy(enemyW),
		)
	}

	var b strings.Builder
	b.WriteString(m.viewBoard())
	b.WriteByte('\n')
	b.WriteString(middle)
	b.WriteByte('\n')
	// Pool-contention panel (#4), only when a hero pool is configured and some
	// pool heroes are still available.
	if len(m.result.PoolAlert) > 0 {
		b.WriteString(m.viewPool(m.width))
		b.WriteByte('\n')
	}
	// "Don't fear" (K1): only on ban steps, where the advice is actionable — on a
	// pick step the question of which enemy hero to ignore does not arise.
	if len(m.result.SafeToLet) > 0 && m.result.OurTurn && !m.result.IsPick {
		b.WriteString(m.viewSafe(m.width))
		b.WriteByte('\n')
	}
	// Patch meta per position (#9), only when position data is in the bundle.
	if len(m.result.PositionMeta) > 0 {
		b.WriteString(m.viewPositions(m.width))
		b.WriteByte('\n')
	}
	// Item/play advice (#10), when any rule fired.
	if len(m.result.ItemAdvice) > 0 {
		b.WriteString(m.viewAdvice(m.width))
		b.WriteByte('\n')
	}
	// Objective timers (M6): GSI mode only, once a live clock has arrived.
	if m.gsiMode && len(m.alerts) > 0 {
		b.WriteString(m.viewTimers(m.width))
		b.WriteByte('\n')
	}
	// Mid-draft enemy-add input / progress (#3).
	if m.addingEnemy {
		b.WriteString(panel(m.th, m.width, m.th.title.Render("ADD ENEMY")+"\n"+m.enemyInput.View()+"\n"+m.th.dim.Render("enter: scout & blend   •   esc: cancel")))
		b.WriteByte('\n')
	} else if m.addingReason {
		// §6.1: attached to the last step of ours, stored locally, never shared.
		b.WriteString(panel(m.th, m.width, m.th.title.Render("WHY NOT THE RECOMMENDATION")+"\n"+m.reasonInput.View()+"\n"+m.th.dim.Render("enter: record locally   •   esc: cancel")))
		b.WriteByte('\n')
	} else if m.busy != "" {
		b.WriteString(panel(m.th, m.width, m.th.dim.Render(m.busy)))
		b.WriteByte('\n')
	}
	b.WriteString(m.viewStatus(m.width))
	return b.String()
}
