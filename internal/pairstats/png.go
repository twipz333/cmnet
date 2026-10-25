package pairstats

import (
	"fmt"
	"os"

	chart "github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

// WritePNG renders chart images using prefix, returning the files written.
// Two images: a win/lane bar comparison and an IMP-per-match timeline.
func (r *Report) WritePNG(prefix string) ([]string, error) {
	var files []string

	bars := make([]chart.Value, 0, len(r.Focus)*2)
	for i, id := range r.Focus {
		a := r.Agg[id]
		if a == nil {
			continue
		}
		col := drawing.ColorFromHex(palette[i%len(palette)][1:])
		nm := truncRunes(shortLabel(a), 9)
		bars = append(bars, chart.Value{
			Value: round1(a.WinPct()), Label: nm + " win",
			Style: chart.Style{FillColor: col, StrokeColor: col},
		})
		if lane, ok := a.LaneWinPct(); ok {
			lc := col.WithAlpha(150)
			bars = append(bars, chart.Value{
				Value: round1(lane), Label: nm + " lane",
				Style: chart.Style{FillColor: lc, StrokeColor: lc},
			})
		}
	}
	if len(bars) > 0 {
		const barW, barSpace = 54, 26
		bc := chart.BarChart{
			Title:      "Win % vs Lane-win @10 %",
			TitleStyle: chart.Style{FontSize: 15},
			// Generous bottom padding so the per-bar labels are never clipped.
			Background: chart.Style{Padding: chart.Box{Top: 55, Left: 20, Right: 20, Bottom: 70}},
			Height:     430,
			Width:      120 + (barW+barSpace)*len(bars),
			BarWidth:   barW,
			BarSpacing: barSpace,
			XAxis:      chart.Style{FontSize: 11, TextRotationDegrees: 0},
			YAxis: chart.YAxis{
				Range: &chart.ContinuousRange{Min: 0, Max: 100},
				Style: chart.Style{FontSize: 10},
			},
			Bars: bars,
		}
		p := prefix + "_winrate.png"
		if err := renderChartBar(bc, p); err != nil {
			return files, err
		}
		files = append(files, p)
	}

	// IMP-per-match timeline: one continuous series per focus player.
	var series []chart.Series
	for i, id := range r.Focus {
		a := r.Agg[id]
		if a == nil {
			continue
		}
		var xs, ys []float64
		n := 0.0
		for _, row := range r.PerMatch[id] {
			n++
			if row.IMP == nil {
				continue
			}
			xs = append(xs, n)
			ys = append(ys, float64(*row.IMP))
		}
		if len(xs) < 2 {
			continue
		}
		col := drawing.ColorFromHex(palette[i%len(palette)][1:])
		series = append(series, chart.ContinuousSeries{
			Name:    shortLabel(a),
			XValues: xs, YValues: ys,
			Style: chart.Style{StrokeColor: col, StrokeWidth: 2},
		})
	}
	if len(series) > 0 {
		c := chart.Chart{
			Title:      "IMP per match (parsed)",
			TitleStyle: chart.Style{FontSize: 15},
			// Extra top padding so the legend sits clear of the title.
			Background: chart.Style{Padding: chart.Box{Top: 64, Left: 10, Right: 20, Bottom: 40}},
			Height:     380,
			Width:      760,
			XAxis:      chart.XAxis{Name: "match #", Style: chart.Style{FontSize: 10}},
			YAxis:      chart.YAxis{Name: "IMP", Style: chart.Style{FontSize: 10}},
			Series:     series,
		}
		c.Elements = []chart.Renderable{chart.Legend(&c)}
		p := prefix + "_imp_timeline.png"
		if err := renderChart(c, p); err != nil {
			return files, err
		}
		files = append(files, p)
	}
	return files, nil
}

func renderChartBar(bc chart.BarChart, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := bc.Render(chart.PNG, f); err != nil {
		return fmt.Errorf("render %s: %w", path, err)
	}
	return nil
}

func renderChart(c chart.Chart, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := c.Render(chart.PNG, f); err != nil {
		return fmt.Errorf("render %s: %w", path, err)
	}
	return nil
}

// shortLabel is the persona name when known, else the account id.
func shortLabel(a *PlayerAgg) string {
	if a.Name != "" {
		return a.Name
	}
	return fmt.Sprintf("%d", a.AccountID)
}
