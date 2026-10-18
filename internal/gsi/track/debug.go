package track

import (
	"encoding/json"
	"fmt"
	"strings"

	"cmnet/internal/gsi/observe"
)

// DebugSnapshot is a diagnostic JSON view, not a public API.
type DebugSnapshot struct {
	ClockTime  *int                  `json:"clock_time,omitempty"`
	GameTime   *int                  `json:"game_time,omitempty"`
	MatchID    string                `json:"match_id,omitempty"`
	Paused     bool                  `json:"paused,omitempty"`
	Self       *debugSelf            `json:"self,omitempty"`
	Allies     map[string]debugAlly  `json:"allies,omitempty"`
	Enemies    map[string]debugEnemy `json:"enemies,omitempty"`
	Roshan     debugRoshan           `json:"roshan"`
	Structures map[string]string     `json:"structures,omitempty"`
}

type debugSelf struct {
	Hero     string `json:"hero,omitempty"`
	Position []int  `json:"position,omitempty"`
	Alive    *bool  `json:"alive,omitempty"`
}

type debugAlly struct {
	Observed  bool    `json:"observed"`
	Positions [][]int `json:"positions,omitempty"`
}

type debugEnemy struct {
	Visible     bool     `json:"visible"`
	LastSeen    []int    `json:"last_seen,omitempty"`
	LastSeenAge *float64 `json:"last_seen_age,omitempty"`
	Positions   [][]int  `json:"visible_positions,omitempty"`
}

type debugRoshan struct {
	Visible  bool  `json:"visible"`
	LastSeen []int `json:"last_seen,omitempty"`
	Alive    bool  `json:"last_seen_alive,omitempty"`
}

// DebugJSON renders a human-readable diagnostic snapshot.
func (s State) DebugJSON() ([]byte, error) {
	d := DebugSnapshot{
		ClockTime:  s.Clock.ClockTime,
		GameTime:   s.Clock.GameTime,
		MatchID:    s.Clock.MatchID,
		Paused:     s.Clock.Paused,
		Allies:     map[string]debugAlly{},
		Enemies:    map[string]debugEnemy{},
		Structures: map[string]string{},
	}
	if s.Self.Present {
		ds := &debugSelf{}
		if s.Self.HeroName != nil {
			ds.Hero = *s.Self.HeroName
		}
		if s.Self.Position != nil {
			ds.Position = []int{s.Self.Position.X, s.Self.Position.Y}
		}
		ds.Alive = s.Self.Alive
		d.Self = ds
	}
	for _, a := range s.Allies {
		d.Allies[a.Hero] = debugAlly{Observed: a.CurrentlyObserved, Positions: vecs(a.PositionCandidates)}
	}
	for _, e := range s.Enemies {
		de := debugEnemy{
			Visible:     e.VisibleNow,
			LastSeenAge: e.LastSeenAgeSeconds,
			Positions:   vecs(e.VisiblePositions),
		}
		if len(e.LastSeenPositions) > 0 {
			p := e.LastSeenPositions[0]
			de.LastSeen = []int{p.X, p.Y}
		}
		d.Enemies[e.Hero] = de
	}
	d.Roshan = debugRoshan{Visible: s.Roshan.VisibleNow, Alive: s.Roshan.LastSeenAlive}
	if len(s.Roshan.LastSeenPositions) > 0 {
		p := s.Roshan.LastSeenPositions[0]
		d.Roshan.LastSeen = []int{p.X, p.Y}
	}
	for _, st := range s.EnemyStructures {
		d.Structures[st.UnitName] = st.Life.String()
	}
	return json.MarshalIndent(d, "", "  ")
}

func vecs(in []observe.Vec2) [][]int {
	if len(in) == 0 {
		return nil
	}
	out := make([][]int, len(in))
	for i, p := range in {
		out[i] = []int{p.X, p.Y}
	}
	return out
}

// FormatLive is a compact text snapshot for live --debug-state.
func (s State) FormatLive() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Game: %s | Dire %d - %d Radiant", formatClock(s.Clock), s.Clock.DireScore, s.Clock.RadiantScore)
	if s.Clock.Paused {
		b.WriteString("  PAUSED")
	}
	if s.Clock.MatchID != "" {
		fmt.Fprintf(&b, "  match=%s", s.Clock.MatchID)
	}
	b.WriteByte('\n')
	if !s.Roster.Valid() {
		b.WriteString("Roster: not ready\n")
	}

	b.WriteString("\nSelf:\n")
	if !s.Self.Present {
		b.WriteString("  (none)\n")
	} else {
		hero := ""
		if s.Self.HeroName != nil {
			hero = displayHero(*s.Self.HeroName)
		}
		fmt.Fprintf(&b, "  %s\n", emptyDash(hero))
		if s.Self.Position != nil {
			fmt.Fprintf(&b, "  pos=%s\n", formatVec(*s.Self.Position))
		} else {
			b.WriteString("  pos=UNKNOWN\n")
		}
		fmt.Fprintf(&b, "  HP=%s\n", formatPct(s.Self.HPPercent))
		fmt.Fprintf(&b, "  mana=%s\n", formatPct(s.Self.ManaPercent))
		fmt.Fprintf(&b, "  smoked=%s\n", formatBool(s.Self.Smoked))
	}

	b.WriteString("\nAllies:\n")
	if len(s.Allies) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, a := range s.Allies {
		pos := "UNKNOWN"
		if len(a.PositionCandidates) > 0 {
			pos = formatVec(a.PositionCandidates[0])
			if n := len(a.PositionCandidates); n > 1 {
				pos += fmt.Sprintf(" (+%d)", n-1)
			}
		}
		fmt.Fprintf(&b, "  %-16s %s\n", displayHero(a.Hero), pos)
	}

	b.WriteString("\nEnemies:\n")
	if len(s.Enemies) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, e := range s.Enemies {
		name := displayHero(e.Hero)
		if e.VisibleNow {
			pos := "UNKNOWN"
			if len(e.VisiblePositions) > 0 {
				pos = formatVec(e.VisiblePositions[0])
				if n := len(e.VisiblePositions); n > 1 {
					pos += fmt.Sprintf(" (+%d)", n-1)
				}
			}
			fmt.Fprintf(&b, "  %-16s VISIBLE  %s\n", name, pos)
			continue
		}
		age := "?"
		if e.LastSeenAgeSeconds != nil {
			age = fmt.Sprintf("%.1fs", *e.LastSeenAgeSeconds)
		}
		last := "UNKNOWN"
		if len(e.LastSeenPositions) > 0 {
			last = formatVec(e.LastSeenPositions[0])
		}
		fmt.Fprintf(&b, "  %-16s missing  %-6s last=%s\n", name, age, last)
	}

	b.WriteString("\nRoshan:\n")
	fmt.Fprintf(&b, "  visible=%v\n", s.Roshan.VisibleNow)
	if len(s.Roshan.LastSeenPositions) > 0 {
		fmt.Fprintf(&b, "  last_seen=%s\n", formatVec(s.Roshan.LastSeenPositions[0]))
	} else {
		b.WriteString("  last_seen=UNKNOWN\n")
	}

	b.WriteString("\nRecent events:\n")
	evs := s.RecentEvents
	if len(evs) > 5 {
		evs = evs[len(evs)-5:]
	}
	if len(evs) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, ev := range evs {
		clk := "--:--"
		if ev.GameTime != nil {
			clk = formatSeconds(*ev.GameTime)
		}
		extra := ""
		if ev.PlayerID != nil {
			extra += fmt.Sprintf(" player=%d", *ev.PlayerID)
		}
		if ev.ItemID != nil {
			extra += fmt.Sprintf(" item_id=%d", *ev.ItemID)
		}
		if ev.Building != nil {
			extra += " building=" + *ev.Building
		}
		fmt.Fprintf(&b, "  %s %s%s\n", clk, ev.Kind, extra)
	}
	return b.String()
}

func displayHero(unit string) string {
	s := strings.TrimPrefix(unit, "npc_dota_hero_")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return unit
	}
	parts := strings.Fields(s)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func formatClock(c Clock) string {
	if c.ClockTime != nil {
		return formatSeconds(*c.ClockTime)
	}
	if c.GameTime != nil {
		return formatSeconds(*c.GameTime)
	}
	return "--:--"
}

func formatSeconds(t int) string {
	neg := t < 0
	if neg {
		t = -t
	}
	out := fmt.Sprintf("%d:%02d", t/60, t%60)
	if neg {
		return "-" + out
	}
	return out
}

func formatVec(p observe.Vec2) string {
	return fmt.Sprintf("(%d, %d)", p.X, p.Y)
}

func formatPct(v *int) string {
	if v == nil {
		return "?"
	}
	return fmt.Sprintf("%d%%", *v)
}

func formatBool(v *bool) string {
	if v == nil {
		return "?"
	}
	return fmt.Sprintf("%v", *v)
}

func emptyDash(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
