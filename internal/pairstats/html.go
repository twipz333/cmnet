package pairstats

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"os"
)

// htmlView is the flattened model handed to the HTML template.
type htmlView struct {
	Generated     string
	Mode          string
	Anchor        int64
	TotalMatches  int
	ParsedMatches int
	Players       []htmlPlayer
	WinChart      template.HTML
	LaneChart     template.HTML
	ImpChart      template.HTML
	ImpTimeline   template.HTML
	LaneTimeline  template.HTML
}

type htmlPlayer struct {
	Label      string
	AccountID  int64
	Avatar     string
	ProfileURI string
	Initial    string
	Color      string
	Matches    int
	WinPct     string
	AvgIMP     string
	AvgGPM     string
	AvgKDA     string
	Lane       string
	TopHeroes  []htmlBucket
	PerMatch   []MatchRow
}

type htmlBucket struct {
	Name   string
	Games  int
	WinPct string
}

var palette = []string{"#4f9dde", "#e0894f", "#5fc97a", "#c75fce"}

const htmlTmpl = `<!DOCTYPE html>
<html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Pair stats — {{.Mode}}</title>
<style>
 :root{--card:#1e2235;--card2:#252a42;--ink:#e8eaf2;--muted:#9aa0b8;--line:#343a57}
 *{box-sizing:border-box}
 body{font-family:system-ui,Segoe UI,Roboto,sans-serif;margin:0;padding:28px;
   background:linear-gradient(135deg,#0f1020 0%,#1a1d35 50%,#241a35 100%);color:var(--ink)}
 .wrap{max-width:1100px;margin:0 auto}
 h1{font-size:26px;margin:0 0 4px} h2{font-size:19px;margin:30px 0 12px;color:#cdd2ea}
 .meta{color:var(--muted);font-size:13px;margin-bottom:8px}
 .versus{display:flex;align-items:stretch;gap:16px;margin-top:18px}
 .pcard{flex:1;background:var(--card);border:1px solid var(--line);border-radius:14px;padding:18px;
   box-shadow:0 6px 24px rgba(0,0,0,.35)}
 .phead{display:flex;align-items:center;gap:14px;margin-bottom:14px}
 .avatar{width:64px;height:64px;border-radius:12px;object-fit:cover;border:2px solid var(--accent)}
 .avatar.ph{display:flex;align-items:center;justify-content:center;font-size:28px;font-weight:700;
   background:var(--accent);color:#10131f}
 .pname{font-size:19px;font-weight:700} .pname a{color:inherit;text-decoration:none}
 .pid{color:var(--muted);font-size:12px}
 .stats{display:grid;grid-template-columns:1fr 1fr;gap:10px}
 .stat{background:var(--card2);border-radius:10px;padding:10px 12px}
 .stat .k{color:var(--muted);font-size:11px;text-transform:uppercase;letter-spacing:.04em}
 .stat .v{font-size:21px;font-weight:700;margin-top:2px}
 .vs{display:flex;align-items:center;justify-content:center;font-size:24px;font-weight:800;color:var(--muted);width:36px}
 .cards{display:flex;flex-wrap:wrap;gap:16px}
 .lightcard{background:#fff;border-radius:12px;padding:10px;box-shadow:0 4px 16px rgba(0,0,0,.3)}
 .cols{display:flex;flex-wrap:wrap;gap:16px}
 .col{flex:1;min-width:320px;background:var(--card);border:1px solid var(--line);border-radius:12px;padding:14px}
 table{border-collapse:collapse;width:100%;font-size:13px}
 th,td{border-bottom:1px solid var(--line);padding:6px 8px;text-align:right}
 th:first-child,td:first-child{text-align:left}
 thead th{color:var(--muted);font-weight:600;border-bottom:1px solid var(--line)}
 .win{color:#5fc97a;font-weight:700} .loss{color:#e0726b;font-weight:700}
 details summary{cursor:pointer;color:#cdd2ea;margin:8px 0}
 .foot{margin-top:28px;color:var(--muted);font-size:12px}
 .foot a{color:#7aa7e0}
</style></head><body><div class="wrap">

<h1>Pair stats</h1>
<p class="meta">{{.Mode}} · anchor={{.Anchor}} · joint matches <b style="color:#e8eaf2">{{.TotalMatches}}</b> (parsed {{.ParsedMatches}}) · {{.Generated}}</p>

<div class="versus">
{{range $i, $p := .Players}}
 {{if $i}}<div class="vs">VS</div>{{end}}
 <div class="pcard" style="--accent:{{$p.Color}}">
  <div class="phead">
   {{if $p.Avatar}}<img class="avatar" src="{{$p.Avatar}}" alt="">{{else}}<div class="avatar ph">{{$p.Initial}}</div>{{end}}
   <div><div class="pname">{{if $p.ProfileURI}}<a href="{{$p.ProfileURI}}" target="_blank" rel="noopener">{{$p.Label}}</a>{{else}}{{$p.Label}}{{end}}</div>
   <div class="pid">id {{$p.AccountID}}</div></div>
  </div>
  <div class="stats">
   <div class="stat"><div class="k">games</div><div class="v">{{$p.Matches}}</div></div>
   <div class="stat"><div class="k">win rate</div><div class="v">{{$p.WinPct}}</div></div>
   <div class="stat"><div class="k">avg IMP</div><div class="v">{{$p.AvgIMP}}</div></div>
   <div class="stat"><div class="k">lane @10</div><div class="v">{{$p.Lane}}</div></div>
   <div class="stat"><div class="k">avg GPM</div><div class="v">{{$p.AvgGPM}}</div></div>
   <div class="stat"><div class="k">avg KDA</div><div class="v">{{$p.AvgKDA}}</div></div>
  </div>
 </div>
{{end}}
</div>

<h2>Comparison</h2>
<div class="cards">
 <div class="lightcard">{{.WinChart}}</div>
 <div class="lightcard">{{.LaneChart}}</div>
 <div class="lightcard">{{.ImpChart}}</div>
</div>

<h2>Timeline</h2>
<div class="cards">
 <div class="lightcard">{{.ImpTimeline}}</div>
 <div class="lightcard">{{.LaneTimeline}}</div>
</div>

<h2>Top heroes</h2>
<div class="cols">
{{range .Players}}
 <div class="col"><div class="pname" style="font-size:15px;margin-bottom:8px;color:{{.Color}}">{{.Label}}</div>
 <table><thead><tr><th>hero</th><th>games</th><th>win%</th></tr></thead><tbody>
 {{range .TopHeroes}}<tr><td>{{.Name}}</td><td>{{.Games}}</td><td>{{.WinPct}}</td></tr>
 {{end}}</tbody></table></div>
{{end}}
</div>

<h2>All matches</h2>
<div class="cols">
{{range .Players}}
 <div class="col"><details><summary>{{.Label}} — {{len .PerMatch}} matches</summary>
 <table><thead><tr><th>date</th><th>hero</th><th>pos</th><th>R</th><th>K/D/A</th><th>GPM</th><th>IMP</th><th>lane@10</th></tr></thead><tbody>
 {{range .PerMatch}}<tr><td>{{.Time.Format "01-02"}}</td><td>{{.HeroName}}</td><td>{{.Position}}</td>
 <td class="{{if .Win}}win{{else}}loss{{end}}">{{if .Win}}W{{else}}L{{end}}</td>
 <td>{{.Kills}}/{{.Deaths}}/{{.Assists}}</td><td>{{.GPM}}</td>
 <td>{{if .IMP}}{{.IMP}}{{else}}–{{end}}</td><td>{{if .LaneLead10}}{{.LaneLead10}}{{else}}–{{end}}</td></tr>
 {{end}}</tbody></table></details></div>
{{end}}
</div>

<p class="foot">Powered by <a href="https://stratz.com" target="_blank" rel="noopener">STRATZ</a> · generated by advisor pairstats</p>
</div></body></html>`

// WriteHTML renders the report to a standalone HTML file at path.
func (r *Report) WriteHTML(path string) error {
	v := htmlView{
		Generated:     r.Generated.Format("2006-01-02 15:04 UTC"),
		Mode:          r.Mode,
		Anchor:        r.Anchor,
		TotalMatches:  r.TotalMatches,
		ParsedMatches: r.ParsedMatches,
	}
	var winLabels, laneLabels, impLabels []string
	var winVals, laneVals, impVals []float64
	var impSeries, laneSeries []lineSeries

	for i, id := range r.Focus {
		a := r.Agg[id]
		if a == nil {
			continue
		}
		color := palette[i%len(palette)]
		prof := r.Profiles[id]
		hp := htmlPlayer{
			Label:      label(a),
			AccountID:  a.AccountID,
			Avatar:     prof.Avatar,
			ProfileURI: prof.ProfileURI,
			Initial:    initial(a),
			Color:      color,
			Matches:    a.Matches,
			WinPct:     fmt.Sprintf("%.0f%%", a.WinPct()),
			AvgGPM:     fmt.Sprintf("%.0f", a.AvgGPM()),
			AvgKDA:     fmt.Sprintf("%.2f", a.AvgKDA()),
			PerMatch:   r.PerMatch[id],
		}
		if imp, ok := a.AvgIMP(); ok {
			hp.AvgIMP = fmt.Sprintf("%+.1f", imp)
			impLabels = append(impLabels, label(a))
			impVals = append(impVals, imp)
		} else {
			hp.AvgIMP = "n/a"
		}
		if lane, ok := a.LaneWinPct(); ok {
			hp.Lane = fmt.Sprintf("%.0f%%", lane)
			laneLabels = append(laneLabels, label(a))
			laneVals = append(laneVals, lane)
		} else {
			hp.Lane = "n/a"
		}
		winLabels = append(winLabels, label(a))
		winVals = append(winVals, a.WinPct())
		for _, h := range a.TopHeroes(8) {
			hp.TopHeroes = append(hp.TopHeroes, htmlBucket{Name: h.Key, Games: h.Games, WinPct: fmt.Sprintf("%.0f%%", h.WinPct())})
		}
		v.Players = append(v.Players, hp)

		// Timeline series: x = joint-match ordinal, y = imp / lane lead.
		var ix, iy, lx, ly []float64
		for n, row := range r.PerMatch[id] {
			if row.IMP != nil {
				ix = append(ix, float64(n+1))
				iy = append(iy, float64(*row.IMP))
			}
			if row.LaneLead10 != nil {
				lx = append(lx, float64(n+1))
				ly = append(ly, float64(*row.LaneLead10))
			}
		}
		impSeries = append(impSeries, lineSeries{Label: shortLabel(a), Color: color, X: ix, Y: iy})
		laneSeries = append(laneSeries, lineSeries{Label: shortLabel(a), Color: color, X: lx, Y: ly})
	}

	impLo, impHi := impRange(impVals)
	v.WinChart = svgBars("Win rate %", winLabels, winVals, "%", 0, 100)
	v.LaneChart = svgBars("Lane win @10 %", laneLabels, laneVals, "%", 0, 100)
	v.ImpChart = svgBars("Avg IMP (parsed)", impLabels, impVals, "", impLo, impHi)
	v.ImpTimeline = svgLine("IMP per match", impSeries, 520, 240)
	v.LaneTimeline = svgLine("Lane net-worth lead @10 per match", laneSeries, 520, 240)

	tmpl, err := template.New("report").Parse(htmlTmpl)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// initial returns an uppercase first letter for the avatar placeholder.
func initial(a *PlayerAgg) string {
	for _, r := range a.Name {
		return string(r)
	}
	return "?"
}

// impRange returns the [min, max] reference for the IMP bars. Zero is always
// included so the sign of each value is visually unambiguous, and a small pad is
// added so the extreme value isn't a zero-width bar. Bars then scale monotonically
// with value: a less-negative IMP is always longer than a more-negative one.
func impRange(vals []float64) (float64, float64) {
	lo, hi := 0.0, 0.0
	for _, v := range vals {
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	pad := (hi - lo) * 0.12
	if pad == 0 {
		pad = 1
	}
	return lo - pad, hi + pad
}
