package tui

import (
	"fmt"
	"strings"
)

// viewEnemy renders the predicted next enemy move.
func (m model) viewEnemy(width int) string {
	var b strings.Builder
	title := "ENEMY PREDICTION"
	if st := m.result.EnemyStep; st != nil {
		// On our turn this is the enemy's NEXT step, which may be a ban or a pick.
		if st.IsPick {
			title += " · next pick"
		} else {
			title += " · next ban"
		}
	}
	b.WriteString(m.th.title.Render(title))
	b.WriteByte('\n')

	if len(m.result.EnemyPred) == 0 {
		b.WriteString(m.th.dim.Render("no prediction"))
		return panel(m.th, width, b.String())
	}
	for i, e := range m.result.EnemyPred {
		src := ""
		switch e.Source {
		case "event":
			src = m.th.highlight.Render(" ●event")
		case "player":
			src = m.th.dim.Render(" ·player")
		}
		fmt.Fprintf(&b, "%2d %-16s %5.1f%%%s%s\n", i+1, m.heroName(e.HeroID), e.Prob*100, dangerNote(m.th, e.Danger), src)
	}
	return panel(m.th, width, strings.TrimRight(b.String(), "\n"))
}

// dangerNote renders how much a hero would cost us, and only when it is big enough
// to act on: a "+0.1" tag on every row would be noise wearing the costume of
// information. Danger is the same quantity the ban ranking uses, so this column
// explains that ranking rather than offering a second opinion on it.
func dangerNote(th theme, danger float64) string {
	switch {
	case danger >= 0.005:
		return th.highlight.Render(fmt.Sprintf(" ⚠%+.2f", danger))
	case danger <= -0.005:
		return th.dim.Render(fmt.Sprintf(" %+.2f", danger))
	default:
		return ""
	}
}

// viewSafe renders the heroes worth letting the enemy have: likely for them to
// take, harmless to us.
func (m model) viewSafe(width int) string {
	var b strings.Builder
	b.WriteString(m.th.title.Render("DON'T FEAR (let them have it)"))
	b.WriteByte('\n')
	// The full decomposition is spelled out here, unlike the prediction panel: this
	// list exists to justify conceding a hero, and "trust me" is not a justification.
	// vs-us = counter lift against our picks, w/them = synergy with the enemy draft,
	// cost = the two combined, the same number the ban ranking is sorted by.
	for i, e := range m.result.SafeToLet {
		fmt.Fprintf(&b, "%2d %-16s enemy %5.1f%%  vs us %+.1f%%  w/them %+.1f%%  cost %+.3f\n",
			i+1, m.heroName(e.HeroID), e.Prob*100, e.Threat*100, e.Fit*100, e.Danger)
	}
	return panel(m.th, width, strings.TrimRight(b.String(), "\n"))
}

// viewPool renders our preferred-pool heroes ranked by enemy contention (#4): the
// ones the enemy is most likely to grab next, so we know what to secure.
func (m model) viewPool(width int) string {
	var b strings.Builder
	b.WriteString(m.th.title.Render("YOUR POOL (contested)"))
	b.WriteByte('\n')
	if len(m.result.PoolAlert) == 0 {
		b.WriteString(m.th.dim.Render("no pool heroes available"))
		return panel(m.th, width, b.String())
	}
	for i, e := range m.result.PoolAlert {
		fmt.Fprintf(&b, "%2d %-16s enemy %5.1f%%\n", i+1, m.heroName(e.HeroID), e.Prob*100)
	}
	return panel(m.th, width, strings.TrimRight(b.String(), "\n"))
}
