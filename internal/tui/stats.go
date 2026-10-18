package tui

import (
	"fmt"
	"sort"
	"strings"

	"cmnet/internal/session"
)

// The Statistics projection lives in session so the web front end can serve the
// same screen; these aliases keep the terminal code reading as before.
type (
	StatsView   = session.StatsView
	StatsPlayer = session.StatsPlayer
	StatsSmurf  = session.StatsSmurf
	StatsHero   = session.StatsHero
)

// selectablePlayer is a player addressable by the Statistics cursor for deletion.
type selectablePlayer struct {
	AccountID int64
	Ally      bool
	Label     string
}

// selectablePlayers flattens enemies then allies into a cursor-addressable list.
// A plain function, not a method: StatsView is an alias for a type in session.
func selectablePlayers(s StatsView) []selectablePlayer {
	var out []selectablePlayer
	add := func(players []StatsPlayer, ally bool) {
		for _, p := range players {
			label := p.Nickname
			if label == "" {
				label = fmt.Sprintf("account %d", p.AccountID)
			}
			out = append(out, selectablePlayer{AccountID: p.AccountID, Ally: ally, Label: label})
		}
	}
	add(s.EnemyPlayers, false)
	add(s.AllyPlayers, true)
	return out
}

// viewStats renders the Statistics screen.
func (s shell) viewStats() string {
	var b strings.Builder
	b.WriteString(s.th.title.Render("STATISTICS"))
	b.WriteString("\n")
	b.WriteString(s.th.dim.Render(s.stats.SnapshotPath))
	b.WriteString("\n\n")

	// The cursor selects across enemies then allies for deletion (#2).
	sel := selectablePlayers(s.stats)
	var selID int64 = -1
	selAlly := false
	if len(sel) > 0 && s.statsSel >= 0 && s.statsSel < len(sel) {
		selID, selAlly = sel[s.statsSel].AccountID, sel[s.statsSel].Ally
	}

	b.WriteString(s.th.highlight.Render("Enemies (layer 2)"))
	b.WriteString("\n")
	b.WriteString(renderPlayers(s.stats.EnemyPlayers, s.th, selID, selAlly, false))
	if s.stats.Layer3Drafts > 0 {
		b.WriteString(s.th.dim.Render(fmt.Sprintf("  + %d team drafts this event (layer 3)\n", s.stats.Layer3Drafts)))
	}
	b.WriteString("\n")

	b.WriteString(s.th.highlight.Render("Allies"))
	b.WriteString("\n")
	b.WriteString(renderPlayers(s.stats.AllyPlayers, s.th, selID, selAlly, true))
	b.WriteString("\n")

	b.WriteString(s.th.highlight.Render("Hero pool"))
	b.WriteString("\n")
	if len(s.stats.HeroPool) == 0 {
		b.WriteString(s.th.dim.Render("  (empty)\n"))
	} else {
		names := make([]string, 0, len(s.stats.HeroPool))
		for _, h := range s.stats.HeroPool {
			names = append(names, h.Name)
		}
		b.WriteString("  " + strings.Join(names, ", ") + "\n")
	}
	b.WriteString("\n")

	b.WriteString(s.th.highlight.Render("Advice rules (#10)"))
	b.WriteString("\n")
	if strings.TrimSpace(s.stats.ItemRules) == "" {
		b.WriteString(s.th.dim.Render("  (none — press i to add)\n"))
	} else {
		for _, ln := range strings.Split(s.stats.ItemRules, "\n") {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			b.WriteString("  " + ln + "\n")
		}
	}
	b.WriteString("\n")

	b.WriteString(s.th.dim.Render("↑/↓: select player   d: delete   c: clear event drafts   e: enemies   a: allies   p: pool   i: rules   esc: back"))
	if s.result != "" {
		b.WriteString("\n" + s.result)
	}
	return b.String()
}

func renderPlayers(players []StatsPlayer, th theme, selID int64, selAlly, ally bool) string {
	if len(players) == 0 {
		return th.dim.Render("  (none)\n")
	}
	var b strings.Builder
	for _, p := range players {
		name := p.Nickname
		if name == "" {
			name = fmt.Sprintf("account %d", p.AccountID)
		}
		marker := "  "
		if ally == selAlly && p.AccountID == selID {
			marker = th.highlight.Render("> ")
		}
		if !p.Resolved {
			b.WriteString(marker + th.dire.Render(name+" — unresolved") + "\n")
			continue
		}
		top := topHeroNames(p.TopHeroes, 5)
		smurf := ""
		if p.Smurf != nil && p.Smurf.Label != "" {
			smurf = " [" + p.Smurf.Label + "]"
		}
		b.WriteString(fmt.Sprintf("%s%s%s: %s\n", marker, name, smurf, top))
	}
	return b.String()
}

// topHeroNames lists the most-played comfort heroes. opStats already sorts by
// games desc; we re-sort here so the TUI stays correct against older payloads.
func topHeroNames(heroes []StatsHero, n int) string {
	if len(heroes) == 0 {
		return "(no data)"
	}
	cp := append([]StatsHero(nil), heroes...)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].Games != cp[j].Games {
			return cp[i].Games > cp[j].Games
		}
		return cp[i].ID < cp[j].ID
	})
	if len(cp) > n {
		cp = cp[:n]
	}
	parts := make([]string, len(cp))
	for i, h := range cp {
		parts[i] = fmt.Sprintf("%s (%dg %.0f%%)", h.Name, h.Games, h.WinRate*100)
	}
	return strings.Join(parts, ", ")
}
