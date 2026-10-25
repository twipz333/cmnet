package pairstats

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
)

// lineSeries is one polyline on an svgLine chart. X and Y must be equal length.
type lineSeries struct {
	Label string
	Color string
	X     []float64
	Y     []float64
}

// svgBars renders a labelled horizontal bar chart as an inline SVG string.
// Bar length is monotonic in value over [minRef, maxRef]: minRef → empty,
// maxRef → full. This keeps ordering correct for signed metrics (e.g. a −1.3
// IMP draws a LONGER bar than −7.4, since −1.3 is the larger value).
func svgBars(title string, labels []string, values []float64, suffix string, minRef, maxRef float64) template.HTML {
	const w, rowH, padL = 460, 30, 160
	h := 44 + rowH*len(values)
	span := maxRef - minRef
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" width="%d" height="%d" font-family="sans-serif" font-size="13">`, w, h, w, h)
	fmt.Fprintf(&b, `<text x="8" y="22" font-size="15" font-weight="bold" fill="#1a1a2e">%s</text>`, template.HTMLEscapeString(title))
	barMax := float64(w - padL - 70)
	for i, v := range values {
		y := 38 + i*rowH
		frac := 0.0
		if span > 0 {
			frac = (v - minRef) / span
		}
		frac = math.Max(0, math.Min(1, frac))
		// Keep a sliver visible even at the minimum so the row reads as a bar.
		bw := math.Max(2, barMax*frac)
		col := palette[i%len(palette)]
		fmt.Fprintf(&b, `<text x="8" y="%d" fill="#333">%s</text>`, y+15, template.HTMLEscapeString(labels[i]))
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%.1f" height="18" rx="3" fill="%s"/>`, padL, y+3, bw, col)
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" fill="#333">%.1f%s</text>`, float64(padL)+bw+6, y+16, v, suffix)
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

// svgLine renders a multi-series line chart (handles negative values, draws a
// zero baseline, gridlines, a y-range and a legend). x values are plotted on a
// shared linear axis spanning the min/max across all series.
func svgLine(title string, series []lineSeries, w, h int) template.HTML {
	const padL, padR, padT, padB = 44, 16, 46, 26
	plotW := float64(w - padL - padR)
	plotH := float64(h - padT - padB)

	// Axis ranges across all series.
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	any := false
	for _, s := range series {
		for i := range s.X {
			any = true
			minX, maxX = math.Min(minX, s.X[i]), math.Max(maxX, s.X[i])
			minY, maxY = math.Min(minY, s.Y[i]), math.Max(maxY, s.Y[i])
		}
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" width="%d" height="%d" font-family="sans-serif" font-size="12">`, w, h, w, h)
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%d" height="%d" fill="#ffffff" rx="6"/>`, w, h)
	fmt.Fprintf(&b, `<text x="10" y="20" font-size="14" font-weight="bold" fill="#1a1a2e">%s</text>`, template.HTMLEscapeString(title))
	if !any {
		fmt.Fprintf(&b, `<text x="10" y="%d" fill="#888">no data</text></svg>`, h/2)
		return template.HTML(b.String())
	}
	if minX == maxX {
		maxX = minX + 1
	}
	if minY == maxY {
		maxY = minY + 1
	}
	// Pad the y-range a touch for headroom.
	pad := (maxY - minY) * 0.08
	minY -= pad
	maxY += pad

	sx := func(x float64) float64 { return padL + (x-minX)/(maxX-minX)*plotW }
	sy := func(y float64) float64 { return float64(padT) + (1-(y-minY)/(maxY-minY))*plotH }

	// Axes box + y min/max labels.
	fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%.1f" stroke="#ccc"/>`, padL, padT, padL, float64(padT)+plotH)
	fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#ccc"/>`, padL, float64(padT)+plotH, float64(padL)+plotW, float64(padT)+plotH)
	fmt.Fprintf(&b, `<text x="6" y="%d" fill="#666" font-size="10">%.0f</text>`, padT+8, maxY)
	fmt.Fprintf(&b, `<text x="6" y="%.0f" fill="#666" font-size="10">%.0f</text>`, float64(padT)+plotH, minY)

	// Zero baseline when the range crosses it.
	if minY < 0 && maxY > 0 {
		zy := sy(0)
		fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#bbb" stroke-dasharray="4 3"/>`, padL, zy, float64(padL)+plotW, zy)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#999" font-size="10">0</text>`, float64(padL)+plotW-10, zy-3)
	}

	for si, s := range series {
		if len(s.X) == 0 {
			continue
		}
		var path bytes.Buffer
		for i := range s.X {
			cmd := "L"
			if i == 0 {
				cmd = "M"
			}
			fmt.Fprintf(&path, "%s%.1f %.1f ", cmd, sx(s.X[i]), sy(s.Y[i]))
		}
		fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="%s" stroke-width="2"/>`, path.String(), s.Color)
		for i := range s.X {
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="2" fill="%s"/>`, sx(s.X[i]), sy(s.Y[i]), s.Color)
		}
		// Legend swatch + label, top-right.
		lx := float64(w-padR) - 120
		ly := float64(padT) + 4 + float64(si)*16
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="10" height="10" fill="%s" rx="2"/>`, lx, ly, s.Color)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#333" font-size="11">%s</text>`, lx+14, ly+9, template.HTMLEscapeString(s.Label))
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
