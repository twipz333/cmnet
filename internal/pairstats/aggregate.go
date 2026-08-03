// Package pairstats turns a set of joint STRATZ matches (two or more players who
// played on the same team) into head-to-head aggregate statistics and renders
// them to terminal text, JSON, an HTML report, PNG charts and an .xlsx workbook.
//
// All computation is deterministic: matches are sorted by (startDateTime, id) and
// every ranked/top-N listing has an explicit tie-break, so the same input yields
// byte-identical output. The package is pure presentation/analysis over data the
// stratz client already fetched; it performs no I/O of its own except writing the
// requested output files.
package pairstats

import (
	"fmt"
	"sort"
	"time"

	"cmnet/internal/sources/stratz"
)

// Bucket counts games/wins for a hero or a position.
type Bucket struct {
	Key    string // display label (hero localized name or position)
	HeroID int    // 0 for position buckets
	Games  int
	Wins   int
}

// WinPct returns win rate in percent (0 when no games).
func (b *Bucket) WinPct() float64 {
	if b.Games == 0 {
		return 0
	}
	return 100 * float64(b.Wins) / float64(b.Games)
}

// MatchRow is one focus player's line in one joint match (chronological).
type MatchRow struct {
	MatchID    int64
	Time       time.Time
	HeroID     int
	HeroName   string
	Position   string
	Win        bool
	Kills      int
	Deaths     int
	Assists    int
	GPM        int
	IMP        *int // nil when the match was not parsed by STRATZ
	LaneLead10 *int // networth lead vs the same-position enemy at minute 10; nil if unavailable
}

// KDA returns (kills+assists)/max(1,deaths).
func (r MatchRow) KDA() float64 {
	return float64(r.Kills+r.Assists) / float64(max(r.Deaths, 1))
}

// PlayerAgg is the rolled-up performance of one focus player across the joint set.
type PlayerAgg struct {
	AccountID int64
	Name      string

	Matches int
	Wins    int

	ImpSum int
	ImpN   int // matches with a non-null imp (parsed)

	GpmSum  int
	XpmSum  int
	LhSum   int
	KdaSum  float64
	LaneWin int
	LaneTot int

	Heroes    map[int]*Bucket
	Positions map[string]*Bucket
}

func (p *PlayerAgg) WinPct() float64 {
	if p.Matches == 0 {
		return 0
	}
	return 100 * float64(p.Wins) / float64(p.Matches)
}

func (p *PlayerAgg) AvgIMP() (float64, bool) {
	if p.ImpN == 0 {
		return 0, false
	}
	return float64(p.ImpSum) / float64(p.ImpN), true
}

func (p *PlayerAgg) AvgGPM() float64 {
	if p.Matches == 0 {
		return 0
	}
	return float64(p.GpmSum) / float64(p.Matches)
}

func (p *PlayerAgg) AvgKDA() float64 {
	if p.Matches == 0 {
		return 0
	}
	return p.KdaSum / float64(p.Matches)
}

func (p *PlayerAgg) LaneWinPct() (float64, bool) {
	if p.LaneTot == 0 {
		return 0, false
	}
	return 100 * float64(p.LaneWin) / float64(p.LaneTot), true
}

// TopHeroes returns the n most-played heroes, ranked by games then win% then id.
func (p *PlayerAgg) TopHeroes(n int) []*Bucket {
	out := make([]*Bucket, 0, len(p.Heroes))
	for _, b := range p.Heroes {
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Games != out[j].Games {
			return out[i].Games > out[j].Games
		}
		if out[i].WinPct() != out[j].WinPct() {
			return out[i].WinPct() > out[j].WinPct()
		}
		return out[i].HeroID < out[j].HeroID
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// Profile is a player's public identity used for richer rendering (HTML avatars
// and profile links). All fields may be empty.
type Profile struct {
	Name       string
	Avatar     string
	ProfileURI string
}

// Report is the full head-to-head result for one joint pull.
type Report struct {
	Generated     time.Time
	Anchor        int64   // public profile used to enumerate matches
	Focus         []int64 // the players being compared (input order preserved)
	Mode          string  // human-readable resolution mode
	TotalMatches  int
	ParsedMatches int // matches where at least one focus player had a non-null imp

	Agg      map[int64]*PlayerAgg
	PerMatch map[int64][]MatchRow
	Profiles map[int64]Profile
}

// prettyPos turns "POSITION_1" into "pos1"; unknown values pass through.
func prettyPos(s string) string {
	switch s {
	case "POSITION_1":
		return "pos1 (carry)"
	case "POSITION_2":
		return "pos2 (mid)"
	case "POSITION_3":
		return "pos3 (off)"
	case "POSITION_4":
		return "pos4 (soft sup)"
	case "POSITION_5":
		return "pos5 (hard sup)"
	case "":
		return "unknown"
	}
	return s
}

// laneLead10 returns p's networth minus the same-position enemy's networth at
// minute 10, when both per-minute arrays reach that far. ok is false otherwise.
func laneLead10(m stratz.JointMatch, p stratz.JointPlayer) (int, bool) {
	if len(p.NetworthPerMinute) <= 10 {
		return 0, false
	}
	for _, e := range m.Players {
		if e.IsRadiant == p.IsRadiant || e.Position != p.Position {
			continue
		}
		if len(e.NetworthPerMinute) <= 10 {
			return 0, false
		}
		return p.NetworthPerMinute[10] - e.NetworthPerMinute[10], true
	}
	return 0, false
}

// Build aggregates matches for the focus account ids. names maps account_id to a
// persona name (optional), heroNames maps hero id to localized name (optional).
func Build(matches []stratz.JointMatch, focus []int64, anchor int64, mode string, profiles map[int64]Profile, heroNames map[int]string) *Report {
	// Deterministic order: oldest→newest, id as tie-break.
	sorted := append([]stratz.JointMatch(nil), matches...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].StartDateTime != sorted[j].StartDateTime {
			return sorted[i].StartDateTime < sorted[j].StartDateTime
		}
		return sorted[i].ID < sorted[j].ID
	})

	focusSet := make(map[int64]bool, len(focus))
	for _, id := range focus {
		focusSet[id] = true
	}

	r := &Report{
		Generated:    time.Now().UTC(),
		Anchor:       anchor,
		Focus:        append([]int64(nil), focus...),
		Mode:         mode,
		TotalMatches: len(sorted),
		Agg:          make(map[int64]*PlayerAgg, len(focus)),
		PerMatch:     make(map[int64][]MatchRow, len(focus)),
		Profiles:     profiles,
	}
	for _, id := range focus {
		r.Agg[id] = &PlayerAgg{
			AccountID: id,
			Name:      profiles[id].Name,
			Heroes:    make(map[int]*Bucket),
			Positions: make(map[string]*Bucket),
		}
	}

	heroName := func(id int) string {
		if n, ok := heroNames[id]; ok && n != "" {
			return n
		}
		return fmt.Sprintf("hero#%d", id)
	}

	for _, m := range sorted {
		parsed := false
		for _, p := range m.Players {
			if !focusSet[p.SteamAccountID] {
				continue
			}
			a := r.Agg[p.SteamAccountID]
			a.Matches++
			if p.IsVictory {
				a.Wins++
			}
			if p.IMP != nil {
				a.ImpSum += *p.IMP
				a.ImpN++
				parsed = true
			}
			a.GpmSum += p.GoldPerMinute
			a.XpmSum += p.ExperiencePerMinute
			a.LhSum += p.NumLastHits

			row := MatchRow{
				MatchID:  m.ID,
				Time:     time.Unix(m.StartDateTime, 0).UTC(),
				HeroID:   p.HeroID,
				HeroName: heroName(p.HeroID),
				Position: prettyPos(p.Position),
				Win:      p.IsVictory,
				Kills:    p.Kills,
				Deaths:   p.Deaths,
				Assists:  p.Assists,
				GPM:      p.GoldPerMinute,
				IMP:      p.IMP,
			}
			a.KdaSum += row.KDA()

			if lead, ok := laneLead10(m, p); ok {
				a.LaneTot++
				if lead > 0 {
					a.LaneWin++
				}
				v := lead
				row.LaneLead10 = &v
			}

			hb := a.Heroes[p.HeroID]
			if hb == nil {
				hb = &Bucket{Key: heroName(p.HeroID), HeroID: p.HeroID}
				a.Heroes[p.HeroID] = hb
			}
			hb.Games++
			if p.IsVictory {
				hb.Wins++
			}

			pos := prettyPos(p.Position)
			pb := a.Positions[pos]
			if pb == nil {
				pb = &Bucket{Key: pos}
				a.Positions[pos] = pb
			}
			pb.Games++
			if p.IsVictory {
				pb.Wins++
			}

			r.PerMatch[p.SteamAccountID] = append(r.PerMatch[p.SteamAccountID], row)
		}
		if parsed {
			r.ParsedMatches++
		}
	}
	return r
}
