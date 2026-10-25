package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The alert list and the "which spawn have we already signalled" bookkeeping live
// in the session, so every front end alerts on the same objectives at the same
// clock. All that is left here is the terminal-specific bell and the rendering.

// beepCmd writes a terminal bell off the render loop; works under the alt screen.
func beepCmd() tea.Cmd {
	return func() tea.Msg {
		fmt.Fprint(os.Stderr, "\a")
		return nil
	}
}

// viewTimers renders the objective timer panel: the live clock plus each upcoming
// objective with a countdown. Due objectives are highlighted with a bullet.
func (m model) viewTimers(width int) string {
	var b strings.Builder
	b.WriteString(m.th.title.Render("OBJECTIVES"))
	if m.live != nil {
		b.WriteString("  " + m.th.dim.Render("clock "+clockMMSS(m.live.ClockTime)))
		if !m.live.Daytime {
			b.WriteString("  " + m.th.dim.Render("night"))
		}
	}
	b.WriteByte('\n')
	for _, a := range m.alerts {
		body := fmt.Sprintf("%-15s in %5s   (@ %s)", a.Label, mmss(a.NextIn), clockMMSS(a.NextAt))
		if a.Due {
			b.WriteString(m.th.highlight.Render("● " + body))
		} else {
			b.WriteString("  " + m.th.status.Render(body))
		}
		b.WriteByte('\n')
	}
	return panel(m.th, width, strings.TrimRight(b.String(), "\n"))
}

// mmss formats a non-negative duration in seconds as M:SS.
func mmss(sec int) string {
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// clockMMSS formats a game clock in seconds as M:SS, with a leading minus before
// the horn (negative clock).
func clockMMSS(sec int) string {
	sign := ""
	if sec < 0 {
		sign = "-"
		sec = -sec
	}
	return fmt.Sprintf("%s%d:%02d", sign, sec/60, sec%60)
}
