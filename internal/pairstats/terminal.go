package pairstats

import (
	"fmt"
	"strings"
)

// Text renders the report as a plain-text terminal summary.
func (r *Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Joint matches: %d  (parsed: %d)\n", r.TotalMatches, r.ParsedMatches)
	fmt.Fprintf(&b, "Mode: %s   anchor=%d\n\n", r.Mode, r.Anchor)

	// Summary table, one row per focus player, columns aligned.
	fmt.Fprintf(&b, "%-22s %5s %6s %9s %6s %6s %12s\n",
		"player", "games", "win%", "avgIMP", "avgGPM", "avgKDA", "lane@10")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 74))
	for _, id := range r.Focus {
		a := r.Agg[id]
		if a == nil {
			continue
		}
		imp := "n/a"
		if v, ok := a.AvgIMP(); ok {
			imp = fmt.Sprintf("%+.1f", v)
		}
		lane := "n/a"
		if v, ok := a.LaneWinPct(); ok {
			lane = fmt.Sprintf("%2.0f%% (%d/%d)", v, a.LaneWin, a.LaneTot)
		}
		fmt.Fprintf(&b, "%-22s %5d %5.0f%% %9s %6.0f %6.2f %12s\n",
			label(a), a.Matches, a.WinPct(), imp, a.AvgGPM(), a.AvgKDA(), lane)
	}

	// Per-player top heroes.
	for _, id := range r.Focus {
		a := r.Agg[id]
		if a == nil {
			continue
		}
		fmt.Fprintf(&b, "\n%s — top heroes:\n", label(a))
		for _, h := range a.TopHeroes(5) {
			fmt.Fprintf(&b, "  %-22s %2d games  %3.0f%% win\n", h.Key, h.Games, h.WinPct())
		}
	}
	return b.String()
}

// label is "Name (id)" when a name is known, else just the id.
func label(a *PlayerAgg) string {
	if a.Name != "" {
		return fmt.Sprintf("%s (%d)", a.Name, a.AccountID)
	}
	return fmt.Sprintf("%d", a.AccountID)
}
