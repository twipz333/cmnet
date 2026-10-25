package pairstats

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// WriteXLSX writes a workbook: a Summary sheet plus one per-match sheet per
// focus player.
func (r *Report) WriteXLSX(path string) error {
	f := excelize.NewFile()
	defer f.Close()

	const summary = "Summary"
	f.SetSheetName(f.GetSheetName(0), summary)

	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})

	f.SetSheetRow(summary, "A1", &[]any{
		"player", "account_id", "games", "wins", "win%", "avg_imp", "avg_gpm", "avg_kda", "lane_win", "lane_total", "lane_win%",
	})
	f.SetCellStyle(summary, "A1", "K1", bold)

	row := 2
	for _, id := range r.Focus {
		a := r.Agg[id]
		if a == nil {
			continue
		}
		imp := any("n/a")
		if v, ok := a.AvgIMP(); ok {
			imp = round1(v)
		}
		lanePct := any("n/a")
		if v, ok := a.LaneWinPct(); ok {
			lanePct = round1(v)
		}
		cell := fmt.Sprintf("A%d", row)
		f.SetSheetRow(summary, cell, &[]any{
			a.Name, a.AccountID, a.Matches, a.Wins, round1(a.WinPct()),
			imp, round1(a.AvgGPM()), round2(a.AvgKDA()), a.LaneWin, a.LaneTot, lanePct,
		})
		row++
	}

	for _, id := range r.Focus {
		a := r.Agg[id]
		if a == nil {
			continue
		}
		name := sheetName(a)
		if _, err := f.NewSheet(name); err != nil {
			return err
		}
		f.SetSheetRow(name, "A1", &[]any{
			"match_id", "date", "hero", "position", "result", "kills", "deaths", "assists", "gpm", "imp", "lane_lead@10",
		})
		f.SetCellStyle(name, "A1", "K1", bold)
		ri := 2
		for _, m := range r.PerMatch[id] {
			res := "L"
			if m.Win {
				res = "W"
			}
			impv := any("")
			if m.IMP != nil {
				impv = *m.IMP
			}
			lanev := any("")
			if m.LaneLead10 != nil {
				lanev = *m.LaneLead10
			}
			f.SetSheetRow(name, fmt.Sprintf("A%d", ri), &[]any{
				m.MatchID, m.Time.Format("2006-01-02"), m.HeroName, m.Position, res,
				m.Kills, m.Deaths, m.Assists, m.GPM, impv, lanev,
			})
			ri++
		}
	}

	return f.SaveAs(path)
}

// sheetName builds a unique, Excel-legal (<=31 chars) sheet name per player.
func sheetName(a *PlayerAgg) string {
	base := a.Name
	if base == "" {
		base = fmt.Sprintf("%d", a.AccountID)
	}
	name := fmt.Sprintf("%s_%d", base, a.AccountID)
	// Strip characters Excel forbids in sheet names: : \ / ? * [ ]
	clean := make([]rune, 0, len(name))
	for _, c := range name {
		switch c {
		case ':', '\\', '/', '?', '*', '[', ']':
			clean = append(clean, '_')
		default:
			clean = append(clean, c)
		}
	}
	if len(clean) > 31 {
		clean = clean[:31]
	}
	return string(clean)
}
