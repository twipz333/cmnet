'use strict';

// Charts for the Data screen: what the local cache actually holds.
//
// Hand-drawn SVG, no library. The binary embeds this file and the page runs from
// a local server with no network, so a CDN import is not an option — and the four
// forms here (stacked columns, horizontal bars) are a few dozen lines each.
//
// The palette is not improvised: these are validated categorical/ordinal steps for
// a dark surface (contrast >= 3:1 against the panel, adjacent-pair separation that
// survives colour-vision deficiency). Do not swap a hue here for one that "looks
// nicer" without re-validating — the aqua/orange pair in particular is what keeps
// the source chart readable for a deuteranope.
const VIZ = {
  // Categorical slots — identity, assigned in fixed order and never cycled.
  cat: ['#3987e5', '#d95926', '#199e70'],
  // Ordinal ramp — rank tiers are an ORDERED scale, so one hue, light to dark.
  //
  // Nine steps because Dota has nine medals (Herald…Immortal). Five steps meant
  // Archon through Immortal collapsed into the same dark blue, which is exactly
  // the end of the scale a player cares about telling apart. The steps are spaced
  // in lightness, not hue, so the order survives greyscale and colour-vision
  // deficiency; the lightest still clears 3:1 against the panel.
  ramp: [
    '#dbe9fb', '#bcd6f7', '#9ec5f4', '#7fb0f0', '#6da7ec',
    '#4a94e9', '#3987e5', '#256abf', '#184f95',
  ],
  // The "nothing here" step of the calendar: darker than the surface so an empty
  // day still reads as a cell, not as a hole in the grid.
  empty: '#222932',
  // "Not a series": absent data and the picks-only bulk pool, which is context
  // rather than something to compare against the drafts.
  muted: '#898781',
  surface: '#171b21',
  grid: '#2a323d',
};

// Each collection path keeps its colour for good, whatever the current ordering.
const SOURCE_COLORS = {
  'bulk': VIZ.cat[0],
  'pro': VIZ.cat[1],
  'public-cm': VIZ.cat[2],
  'unknown': VIZ.muted,
};

const SVG_NS = 'http://www.w3.org/2000/svg';

// CHART_CH is the average advance width of the value font at 12px, in pixels.
// Used to size gutters from label text without laying anything out first; an
// approximation is fine because it only ever adds a little slack.
const CHART_CH = 6.6;

// valueGutter returns the right pad needed for the longest value label plus the
// gap it sits after. A fixed pad let "12 345 · 24%" run past the viewBox on the
// longest bar and draw over the panel border.
function valueGutter(rows) {
  let longest = 0;
  for (const r of rows || []) {
    const text = r.valueLabel || String(r.value ?? '');
    longest = Math.max(longest, text.length);
  }
  return Math.ceil(longest * CHART_CH) + 14;
}

// chartWidth measures the box a chart is about to be drawn into.
//
// Charts are authored at the container's real pixel width so nothing is scaled.
// The element is not in the document yet when this is called, so the nearest
// already-laid-out ancestor is measured instead; the fallback covers the case
// where nothing has a width yet (a hidden tab).
function chartWidth(host, fallback) {
  let node = host;
  while (node && !node.clientWidth) node = node.parentElement;
  const w = node ? node.clientWidth : 0;
  return Math.max(320, w || fallback || 720);
}

function svg(tag, attrs) {
  const n = document.createElementNS(SVG_NS, tag);
  for (const k in attrs || {}) n.setAttribute(k, attrs[k]);
  return n;
}

// num formats a count the way the rest of the UI reads: grouped, Russian locale.
function num(n) {
  return (n || 0).toLocaleString('ru-RU');
}

function pct(part, whole) {
  if (!whole) return '0%';
  const v = (part / whole) * 100;
  return (v < 1 && v > 0 ? v.toFixed(1) : Math.round(v)) + '%';
}

// --- tooltip ----------------------------------------------------------------
//
// One element for every chart. Tooltips enhance and never gate: each value is
// also in the table view under the charts, so nothing is reachable by hover alone.

let tipEl = null;

function tip() {
  if (!tipEl) {
    tipEl = el('div', 'viz-tip');
    tipEl.hidden = true;
    document.body.append(tipEl);
  }
  return tipEl;
}

function showTip(evt, lines) {
  const t = tip();
  t.textContent = '';
  lines.forEach((line) => t.append(el('div', null, line)));
  t.hidden = false;
  const pad = 12;
  const r = t.getBoundingClientRect();
  let x = evt.clientX + pad;
  let y = evt.clientY + pad;
  if (x + r.width > window.innerWidth) x = evt.clientX - r.width - pad;
  if (y + r.height > window.innerHeight) y = evt.clientY - r.height - pad;
  t.style.left = x + 'px';
  t.style.top = y + 'px';
}

function hideTip() {
  if (tipEl) tipEl.hidden = true;
}

// hoverable wires a mark to the shared tooltip, keyboard included: focus shows
// what hover shows, so the values are not mouse-only.
function hoverable(node, lines) {
  node.setAttribute('tabindex', '0');
  node.addEventListener('mousemove', (e) => showTip(e, lines));
  node.addEventListener('mouseleave', hideTip);
  node.addEventListener('focus', (e) => {
    const r = node.getBoundingClientRect();
    showTip({ clientX: r.left + r.width / 2, clientY: r.top }, lines);
  });
  node.addEventListener('blur', hideTip);
  return node;
}

// --- mark geometry ----------------------------------------------------------

// barPath draws a bar with its data-end rounded (4px) and its baseline end square.
// dir: 'right' for horizontal bars, 'up' for columns. A radius larger than half
// the bar collapses to a plain rectangle rather than a lozenge.
function barPath(x, y, w, h, dir) {
  const r = Math.min(4, dir === 'right' ? w : h, (dir === 'right' ? h : w) / 2);
  if (r <= 0) return `M${x} ${y}h${w}v${h}h${-w}z`;
  if (dir === 'right') {
    return `M${x} ${y}h${w - r}a${r} ${r} 0 0 1 ${r} ${r}v${h - 2 * r}a${r} ${r} 0 0 1 ${-r} ${r}h${-(w - r)}z`;
  }
  return `M${x} ${y + r}a${r} ${r} 0 0 1 ${r} ${-r}h${w - 2 * r}a${r} ${r} 0 0 1 ${r} ${r}v${h - r}h${-w}z`;
}

// --- charts -----------------------------------------------------------------

// intakeChart plots what arrived per day: the scarce Captains Mode drafts stacked
// on the rest.
//
// Stacked rather than side-by-side because CM drafts are a SUBSET of the day's
// matches — two bars would invite reading them as separate populations that sum to
// more than the day's total. Empty days are drawn as gaps rather than skipped: a
// day the collector was not running is exactly the thing worth seeing.
function intakeChart(daily, width) {
  // Authored at the container's real width for the same reason barsChart is: a
  // stretched viewBox scales the type along with the marks.
  const W = width || 720, H = 190, padL = 46, padR = 10, padT = 10, padB = 26;
  const root = svg('svg', {
    viewBox: `0 0 ${W} ${H}`, width: W, height: H, class: 'viz', role: 'img',
    'aria-label': 'Приток матчей по дням',
  });
  if (!daily || !daily.length) return root;

  const plotW = W - padL - padR, plotH = H - padT - padB;
  const max = Math.max(1, ...daily.map((d) => d.matches));
  const step = plotW / daily.length;
  const barW = Math.min(24, Math.max(2, step - 3)); // never fill the slot: the rest is air

  // Gridlines and ticks: hairline, solid, one step off the surface, and rounded to
  // numbers a person would say out loud.
  const ticks = niceTicks(max, 3);
  ticks.forEach((t) => {
    const y = padT + plotH - (t / max) * plotH;
    root.append(svg('line', { x1: padL, y1: y, x2: W - padR, y2: y, class: 'viz-grid' }));
    const label = svg('text', { x: padL - 8, y: y + 4, class: 'viz-tick', 'text-anchor': 'end' });
    label.textContent = num(t);
    root.append(label);
  });

  daily.forEach((d, i) => {
    const x = padL + i * step + (step - barW) / 2;
    const total = d.matches || 0;
    if (!total) return;
    const cm = Math.min(d.cmDrafts || 0, total);
    const rest = total - cm;
    const totalH = (total / max) * plotH;
    const cmH = (cm / max) * plotH;
    // A 2px surface gap separates the two segments — the spacer does the dividing,
    // never a stroke around the mark.
    const gap = cm > 0 && rest > 0 ? 2 : 0;
    const restH = Math.max(0, totalH - cmH - gap);

    const lines = [dayLabel(d.day), `матчей: ${num(total)}`, `из них CM-драфтов: ${num(cm)}`];
    if (restH > 0) {
      root.append(hoverable(svg('path', {
        d: barPath(x, padT + plotH - restH, barW, restH, 'up'),
        fill: VIZ.muted, class: 'viz-mark',
      }), lines));
    }
    if (cmH > 0) {
      const y = padT + plotH - totalH;
      root.append(hoverable(svg('path', {
        d: barPath(x, y, barW, cmH, 'up'), fill: VIZ.cat[0], class: 'viz-mark',
      }), lines));
    }
  });

  // Baseline, plus the first and last dates — the axis carries the span without a
  // label under every column.
  root.append(svg('line', {
    x1: padL, y1: padT + plotH, x2: W - padR, y2: padT + plotH, class: 'viz-axis',
  }));
  const first = svg('text', { x: padL, y: H - 8, class: 'viz-tick' });
  first.textContent = dayLabel(daily[0].day);
  root.append(first);
  const last = svg('text', { x: W - padR, y: H - 8, class: 'viz-tick', 'text-anchor': 'end' });
  last.textContent = dayLabel(daily[daily.length - 1].day);
  root.append(last);
  return root;
}

// barsChart draws horizontal bars: one row per category, value at the tip.
// colorOf picks the fill per row, so the same routine serves the categorical
// (sources) and ordinal (rank tiers) charts without a ramp ever landing on
// nominal categories.
function barsChart(rows, colorOf, opts) {
  // The width is measured, not assumed, and the viewBox is authored at exactly
  // that many units — so one SVG unit is one CSS pixel and a 12px label renders
  // at 12px in every panel.
  //
  // Before this, the viewBox was a fixed 720 stretched to the container: on a
  // 1900px screen that is a 2.6x scale, so a 12px label came out at ~31px and a
  // ten-row chart stood 700px tall. The bars grew, and the type grew with them.
  const o = opts || {};
  const W = o.width || 720, rowH = o.rowH || 26, padL = o.padL || 130;
  const padT = 6;
  // The right pad has to fit the longest value label ("12 345 · 24%"), which the
  // caller cannot know in advance. Measured from the strings themselves; the
  // fallback keeps a caller-supplied value honoured.
  const padR = o.padR || valueGutter(rows);
  const H = padT * 2 + rows.length * rowH;
  const root = svg('svg', {
    viewBox: `0 0 ${W} ${H}`, width: W, height: H, class: 'viz', role: 'img',
    'aria-label': 'Распределение',
  });
  if (!rows.length) return root;

  const plotW = W - padL - padR;
  const max = Math.max(1, ...rows.map((r) => r.value));
  const barH = Math.min(18, rowH - 8);

  rows.forEach((r, i) => {
    const y = padT + i * rowH + (rowH - barH) / 2;
    const w = Math.max(1, (r.value / max) * plotW);

    const label = svg('text', { x: padL - 10, y: y + barH - 4, class: 'viz-label', 'text-anchor': 'end' });
    label.textContent = r.label;
    root.append(label);

    root.append(hoverable(svg('path', {
      d: barPath(padL, y, w, barH, 'right'),
      fill: colorOf(r, i), class: 'viz-mark',
    }), r.tip || [`${r.label}: ${num(r.value)}`]));

    // Bars get their value at the tip; that is the selective labelling the form
    // allows, and it keeps every number readable without hovering.
    const val = svg('text', { x: padL + w + 8, y: y + barH - 4, class: 'viz-value' });
    val.textContent = r.valueLabel || num(r.value);
    root.append(val);
  });
  return root;
}

// heatmapChart lays a year of collection out the way a contribution graph does:
// one square per day, weeks running left to right, weekdays top to bottom.
//
// Why this and not another bar chart: the day histograms answer "how much on this
// day", one day at a time. Laid out as a calendar the same numbers answer a
// different question at a glance — which stretches are covered, where the gaps
// are, whether collection has a weekly rhythm. That is a shape, and a shape is
// what a row of 300 thin bars cannot show.
//
// Intensity is a sequential ramp on the SAME hue as the rest of the ordinal
// scales: volume is ordered data, so the encoding is lightness, not hue.
function heatmapChart(daily, width, opts) {
  const o = opts || {};
  const cell = o.cell || 13, gap = 3, padL = 30, padT = 18, padB = 6;
  const step = cell + gap;

  const byDay = new Map();
  let max = 0;
  for (const d of daily || []) {
    const value = o.pick ? o.pick(d) : d.matches;
    byDay.set(dayKey(d.day), value);
    if (value > max) max = value;
  }

  // The grid spans whole weeks: a calendar that starts mid-column reads as a bug.
  const days = (daily || []).map((d) => d.day).sort((a, b) => a - b);
  const W = width || 720;
  const root = svg('svg', {
    viewBox: `0 0 ${W} 10`, width: W, height: 10, class: 'viz', role: 'img',
    'aria-label': 'Календарь сбора',
  });
  if (!days.length) return root;

  const first = startOfWeek(new Date(days[0] * 1000));
  const last = new Date(days[days.length - 1] * 1000);
  const weeks = Math.max(1, Math.ceil((last - first) / (7 * 86400 * 1000)) + 1);
  const H = padT + 7 * step + padB;
  root.setAttribute('viewBox', `0 0 ${W} ${H}`);
  root.setAttribute('height', H);

  const dayNames = ['пн', '', 'ср', '', 'пт', '', 'вс'];
  for (let r = 0; r < 7; r++) {
    if (!dayNames[r]) continue;
    const t = svg('text', {
      x: padL - 6, y: padT + r * step + cell - 2, class: 'viz-tick', 'text-anchor': 'end',
    });
    t.textContent = dayNames[r];
    root.append(t);
  }

  let lastMonth = -1;
  for (let w = 0; w < weeks; w++) {
    for (let r = 0; r < 7; r++) {
      const d = new Date(first.getTime() + (w * 7 + r) * 86400 * 1000);
      if (d > last) continue;
      const key = dayKey(Math.floor(d.getTime() / 1000));
      const v = byDay.get(key);
      const x = padL + w * step, y = padT + r * step;

      if (r === 0 && d.getMonth() !== lastMonth) {
        lastMonth = d.getMonth();
        const label = svg('text', { x, y: padT - 6, class: 'viz-tick' });
        label.textContent = d.toLocaleDateString('ru-RU', { month: 'short' });
        root.append(label);
      }

      // A day with no data and a day with zero are drawn the same on purpose:
      // both mean "nothing arrived", and inventing a third state for the
      // difference would be a distinction the collector does not record.
      const rect = svg('rect', {
        x, y, width: cell, height: cell, rx: 3,
        fill: heatColor(v || 0, max), class: 'viz-mark',
      });
      const dateLabel = d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' });
      root.append(hoverable(rect, v ? [dateLabel, `${o.unit || 'матчей'}: ${num(v)}`]
        : [dateLabel, 'ничего не собрано']));
    }
  }
  return root;
}

// heatColor maps a value onto the ordinal ramp, with a distinct "nothing" step so
// an empty day is visibly empty rather than merely pale.
function heatColor(v, max) {
  if (!v) return VIZ.empty;
  const idx = Math.min(VIZ.ramp.length - 1,
    Math.max(0, Math.round((v / Math.max(1, max)) * (VIZ.ramp.length - 1))));
  return VIZ.ramp[idx];
}

function dayKey(unix) {
  const d = new Date(unix * 1000);
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}

// startOfWeek returns the Monday of the week containing d, at local midnight.
function startOfWeek(d) {
  const out = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const shift = (out.getDay() + 6) % 7; // Monday-first, unlike JS's Sunday-first
  out.setDate(out.getDate() - shift);
  return out;
}

// heatLegend is the "меньше → больше" strip a calendar needs: the squares carry
// no axis, so the scale has to be stated somewhere.
function heatLegend() {
  const box = el('div', 'viz-legend');
  box.append(el('span', 'viz-legend-item', 'меньше'));
  const strip = svg('svg', { width: (VIZ.ramp.length + 1) * 14, height: 12, class: 'viz-swatch-strip' });
  strip.append(svg('rect', { x: 0, y: 1, width: 11, height: 11, rx: 3, fill: VIZ.empty }));
  VIZ.ramp.forEach((c, i) => {
    strip.append(svg('rect', { x: (i + 1) * 14, y: 1, width: 11, height: 11, rx: 3, fill: c }));
  });
  box.append(strip);
  box.append(el('span', 'viz-legend-item', 'больше'));
  return box;
}

// niceTicks returns round gridline values up to max, aiming for about `count`
// of them.
//
// Picking the first nice step >= max/count (the obvious version) rounds the
// spacing UP and can leave a single gridline: at max 8 700 and count 3 it lands on
// 5 000 and stops. So every nice step is scored by how close it lands to the
// requested tick count, and the best is used.
function niceTicks(max, count) {
  const raw = max / count;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const candidates = [];
  [mag / 10, mag, mag * 10].forEach((m) => {
    [1, 2, 2.5, 5].forEach((f) => {
      const step = m * f;
      // Everything plotted here is a count of matches, so a fractional gridline
      // ("0.25 матча") is nonsense on a small chart.
      if (step >= 1 && step <= max && Number.isInteger(step)) candidates.push(step);
    });
  });
  if (!candidates.length) return [];

  let best = candidates[0];
  let bestScore = Infinity;
  candidates.forEach((step) => {
    const ticks = Math.floor(max / step);
    if (ticks < 2 || ticks > 6) return; // too bare, or a picket fence
    const score = Math.abs(ticks - count);
    if (score < bestScore) {
      bestScore = score;
      best = step;
    }
  });

  const out = [];
  for (let v = best; v <= max; v += best) out.push(v);
  return out;
}

function dayLabel(unix) {
  const d = new Date(unix * 1000);
  return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' });
}

// legend builds the identity channel that never relies on colour-matching alone.
function legend(items) {
  const box = el('div', 'viz-legend');
  items.forEach(({ color, label }) => {
    const item = el('span', 'viz-legend-item');
    const dot = svg('svg', { width: 10, height: 10, class: 'viz-swatch' });
    dot.append(svg('rect', { x: 0, y: 0, width: 10, height: 10, rx: 2, fill: color }));
    item.append(dot, el('span', null, label));
    box.append(item);
  });
  return box;
}

// tableView is every chart's WCAG-clean twin: the same numbers as text, so nothing
// is encoded in colour or position alone.
function tableView(caption, head, rows) {
  const box = el('details', 'viz-table');
  box.append(el('summary', null, caption));
  const table = el('table');
  const thead = el('thead');
  const hr = el('tr');
  head.forEach((h) => hr.append(el('th', null, h)));
  thead.append(hr);
  const tbody = el('tbody');
  rows.forEach((r) => {
    const tr = el('tr');
    r.forEach((c) => tr.append(el('td', null, String(c))));
    tbody.append(tr);
  });
  table.append(thead, tbody);
  box.append(table);
  return box;
}

// --- rank tiers -------------------------------------------------------------

// Valve's medal tiers, one per decade of rank_tier.
const TIER_NAMES = {
  '-1': 'ранг неизвестен',
  0: 'без калибровки', 10: 'Herald', 20: 'Guardian', 30: 'Crusader',
  40: 'Archon', 50: 'Legend', 60: 'Ancient', 70: 'Divine', 80: 'Immortal',
};

function tierLabel(tier) {
  return TIER_NAMES[String(tier)] || ('tier ' + tier);
}

// --- screen -----------------------------------------------------------------

function statTile(label, value, why) {
  const box = el('div', 'stat');
  box.append(el('div', 'stat-label', label));
  box.append(el('div', 'stat-value', value));
  if (why) box.append(el('div', 'stat-why', why));
  return box;
}

// lastCorpus keeps the payload so a resize can repaint without refetching. The
// charts are authored at a measured pixel width, so a window resize genuinely
// needs new geometry rather than a CSS rescale.
let lastCorpus = null;

// renderCorpus paints the whole "what we collected" section.
function renderCorpus(data) {
  const host = $('corpus');
  lastCorpus = data;
  host.textContent = '';
  // Measured once per repaint: every chart in this section shares the column.
  const fullW = chartWidth(host);
  // Half-width columns on a wide screen, full width when the grid collapses to
  // one column. Measured rather than fixed, so the labels stay the same size as
  // in every other panel.
  const halfW = fullW >= 760 ? Math.floor((fullW - 14) / 2) : fullW;

  if (!data || data.error) {
    host.append(el('div', 'why', 'кэш не прочитан: ' + ((data && data.error) || 'нет данных')));
    return;
  }
  const c = data.corpus;
  if (!c || !c.matches) {
    host.append(el('div', 'why', 'кэш пуст — запусти синхронизацию, и здесь появится картина сбора'));
    return;
  }

  // --- headline numbers
  const tiles = el('div', 'stats-row');
  tiles.append(statTile('Матчей в кэше', num(c.matches), spanLabel(c.oldestStart, c.newestStart)));
  tiles.append(statTile('CM-драфтов', num(c.cmDrafts),
    `с бан-фазой — ${pct(c.cmDrafts, c.matches)} кэша; только на них учится предикт врага`));
  tiles.append(statTile('С полными ролями', num(c.withPositions),
    `позиции известны у всех десятерых — ${pct(c.withPositions, c.matches)}; ` +
    `столько идёт в мету по позициям`));
  tiles.append(statTile('Действий в драфтах', num(c.pickBans), `${num(c.heroes)} героев в справочнике`));
  host.append(tiles);

  // --- two histograms that are easy to confuse and must not be
  //
  // "When were these games played" is about the corpus: a thin day is a day the
  // meta is thinly covered. "When did we download them" is about the collector: a
  // gap there is time the app was closed. One sync pass pulls games spread over
  // weeks, so the two charts look nothing alike — labelling either as "intake"
  // would be plain wrong.
  const played = el('div', 'panel');
  played.append(el('h2', null, 'Матчи по дате игры'));
  played.append(el('p', 'why',
    'Чем покрыто окно обучения. Провал — день, по которому в кэше почти нет игр: ' +
    'мета за него известна хуже. Синим — драфты с бан-фазой, их всегда в разы меньше.'));
  played.append(legend([
    { color: VIZ.cat[0], label: 'CM-драфты (с банами)' },
    { color: VIZ.muted, label: 'остальные матчи' },
  ]));
  played.append(intakeChart(c.daily, fullW));
  played.append(tableView('Показать таблицей', ['День игры', 'Матчей', 'CM-драфтов'],
    (c.daily || []).map((d) => [dayLabel(d.day), num(d.matches), num(d.cmDrafts)])));
  host.append(played);

  const fetched = el('div', 'panel');
  fetched.append(el('h2', null, 'Загружено в кэш по дням'));
  fetched.append(el('p', 'why',
    'Когда матчи реально попадали в кэш. Пустой день — в этот день не сохранилось ' +
    'ничего нового: advisor мог быть не запущен, сбор выключен, или источник не отдал ' +
    'ничего, чего ещё нет.'));
  fetched.append(legend([
    { color: VIZ.cat[0], label: 'CM-драфты (с банами)' },
    { color: VIZ.muted, label: 'остальные матчи' },
  ]));
  fetched.append(intakeChart(c.fetched, fullW));
  fetched.append(tableView('Показать таблицей', ['День загрузки', 'Матчей', 'CM-драфтов'],
    (c.fetched || []).map((d) => [dayLabel(d.day), num(d.matches), num(d.cmDrafts)])));
  host.append(fetched);

  // --- the corpus as a shape rather than a row of bars
  const calendar = el('div', 'panel');
  calendar.append(el('h2', null, 'Календарь сбора'));
  calendar.append(el('p', 'why',
    'Те же дни, что и на гистограммах выше, но разложенные по неделям: сразу видно ' +
    'сплошные периоды, дыры и ритм. Насыщенность клетки — сколько матчей за этот день ' +
    'попало в кэш.'));
  calendar.append(heatLegend());
  const calGrid = el('div', 'grid-2');

  const calPlayed = el('div');
  calPlayed.append(el('h3', null, 'По дате игры'));
  calPlayed.append(heatmapChart(c.daily, halfW));
  calGrid.append(calPlayed);

  const calFetched = el('div');
  calFetched.append(el('h3', null, 'По дате загрузки'));
  calFetched.append(heatmapChart(c.fetched, halfW));
  calGrid.append(calFetched);

  calendar.append(calGrid);
  host.append(calendar);

  // --- sources
  const bySource = el('div', 'panel');
  bySource.append(el('h2', null, 'Откуда данные'));
  bySource.append(el('p', 'why',
    'bulk — публичные матчи без банов (объём для win-prob и матриц). ' +
    'public-cm — публичные Captains Mode нашего ранга, ради них и идёт долгий сбор. ' +
    'pro — про-сцена: другая игра, держим отдельно.'));
  const srcRows = (c.sources || []).map((s) => ({
    label: s.source,
    value: s.matches,
    valueLabel: `${num(s.matches)} · ${pct(s.matches, c.matches)}`,
    tip: [s.source, `матчей: ${num(s.matches)}`, `из них CM-драфтов: ${num(s.cmDrafts)}`],
  }));
  // Colour follows the SOURCE, not its position in the list: the rows are ordered
  // by size, so an index-based palette would repaint bulk the day public-cm
  // overtakes it — and a reader who learned "green is public-cm" would be misled.
  bySource.append(barsChart(srcRows, (r) => SOURCE_COLORS[r.label] || VIZ.muted, { width: fullW }));
  bySource.append(tableView('Показать таблицей', ['Источник', 'Матчей', 'CM-драфтов'],
    (c.sources || []).map((s) => [s.source, num(s.matches), num(s.cmDrafts)])));
  host.append(bySource);

  // --- rank mix
  const byRank = el('div', 'panel');
  byRank.append(el('h2', null, 'Ранги собранного'));
  byRank.append(el('p', 'why',
    'Сбор идёт широко, а обучение фильтрует по рангу (train-min-rank-tier в конфиге). ' +
    'Матчи с неизвестным рангом фильтр не выбрасывает — иначе от корпуса осталась бы малая часть.'));
  const rankRows = (c.ranks || []).map((r) => ({
    label: tierLabel(r.tier),
    value: r.matches,
    valueLabel: `${num(r.matches)} · ${pct(r.matches, c.matches)}`,
    tip: [tierLabel(r.tier), `матчей: ${num(r.matches)}`],
    tier: r.tier,
  }));
  // The ramp is ordered by TIER, not by bar length: rank is an ordered scale, and
  // colouring by size would double-encode the length the bar already shows.
  const known = rankRows.filter((r) => r.tier >= 0).sort((a, b) => a.tier - b.tier);
  byRank.append(barsChart(rankRows, (r) => {
    if (r.tier < 0) return VIZ.muted;
    // Indexed by the tier's own place on the medal scale, not by its position in
    // this cache's list: a bracket nobody collected must not shift the colour of
    // the ones around it.
    const idx = Math.max(0, Math.min(VIZ.ramp.length - 1, Math.floor(r.tier / 10) - 1));
    return VIZ.ramp[idx];
  }, { width: fullW }));
  byRank.append(tableView('Показать таблицей', ['Ранг', 'Матчей'],
    rankRows.map((r) => [r.label, num(r.value)])));
  host.append(byRank);

  // --- meta as this cache sees it
  const meta = el('div', 'panel');
  meta.append(el('h2', null, 'Мета в собранных данных'));
  meta.append(el('p', 'why',
    'Что чаще всего берут и что чаще всего банят в кэше. Это не рекомендация модели, ' +
    'а сырая частота — полезно как проверка, что собралось то, что ожидалось.'));
  const grid = el('div', 'grid-2');

  const half = { width: halfW, padL: 108, rowH: 22 };

  const picks = el('div');
  picks.append(el('h3', null, 'Чаще всего пикают'));
  picks.append(barsChart((c.topPicks || []).map((h) => ({
    label: h.name, value: h.count, tip: [h.name, `пиков: ${num(h.count)}`],
  })), () => VIZ.cat[0], half));
  grid.append(picks);

  const bans = el('div');
  bans.append(el('h3', null, 'Чаще всего банят'));
  bans.append(barsChart((c.topBans || []).map((h) => ({
    label: h.name, value: h.count, tip: [h.name, `банов: ${num(h.count)}`],
  })), () => VIZ.cat[1], half));
  grid.append(bans);

  meta.append(grid);
  meta.append(tableView('Показать таблицей', ['Герой', 'Пиков', 'Банов'], heroTableRows(c)));
  host.append(meta);
}

// heroTableRows merges the two top lists into one table, so the twin view holds
// both without repeating the hero column.
//
// A hero present in one list and absent from the other gets "—", not 0: these are
// top-12 lists, so absence means "not in the top twelve", and printing a zero
// would state something false (Lion is banned plenty; he just is not a top-12 ban).
function heroTableRows(c) {
  const byId = new Map();
  const put = (h, key) => {
    const row = byId.get(h.heroId) || { name: h.name, picks: null, bans: null };
    row[key] = h.count;
    byId.set(h.heroId, row);
  };
  (c.topPicks || []).forEach((h) => put(h, 'picks'));
  (c.topBans || []).forEach((h) => put(h, 'bans'));
  const rank = (r) => (r.picks || 0) + (r.bans || 0);
  return [...byId.values()]
    .sort((a, b) => rank(b) - rank(a))
    .map((r) => [r.name, r.picks === null ? '—' : num(r.picks), r.bans === null ? '—' : num(r.bans)]);
}

// spanLabel describes the range the cache covers, inclusive of both ends.
//
// Counted in calendar days rather than by dividing seconds: elapsed time is not
// the same as days on a calendar once a daylight saving change lands inside the
// range, and the label sits right next to the two dates it claims to span.
function spanLabel(from, to) {
  if (!from || !to) return '';
  const midnight = (unix) => {
    const d = new Date(unix * 1000);
    return new Date(d.getFullYear(), d.getMonth(), d.getDate());
  };
  const a = midnight(from), b = midnight(to);
  const days = Math.round((b - a) / 86400000) + 1; // inclusive of both ends
  return `${dayLabel(from)} — ${dayLabel(to)} · ${days} дн.`;
}

// --- loading ----------------------------------------------------------------

let corpusLoading = false;

// trafficChart draws one source's recent request rate, one bar per second.
//
// Stacked rather than three lines: rate-limited and failed responses are a SUBSET
// of the second's traffic, and drawing them as separate series invites reading a
// second with 10 requests and 10 failures as 20 events. Empty seconds are drawn
// as empty slots, never skipped — a pause is the thing worth seeing, and a chart
// that closes the gap makes a stalled source look busy.
function trafficChart(samples, width) {
  const W = width || 720, H = 46, padT = 2, padB = 10;
  const root = svg('svg', {
    viewBox: `0 0 ${W} ${H}`, width: W, height: H, class: 'viz', role: 'img',
    'aria-label': 'Запросы по секундам',
  });
  if (!samples || !samples.length) return root;

  const plotH = H - padT - padB;
  const slot = W / samples.length;
  // Never thinner than a hairline, never wider than the slot minus its gap: at
  // five minutes of seconds the bars are sub-pixel on a narrow panel, and a
  // rounded-up width would overlap the neighbour and read as solid fill.
  const barW = Math.max(1, Math.min(slot - 0.5, slot * 0.8));
  const max = Math.max(1, ...samples.map((s) => s.requests));

  // A baseline, so an all-empty window still reads as a chart rather than a void.
  root.append(svg('line', {
    x1: 0, y1: padT + plotH + 0.5, x2: W, y2: padT + plotH + 0.5, class: 'viz-grid',
  }));

  samples.forEach((s, i) => {
    if (!s.requests && !s.failures && !s.rateLimited) return;
    const x = i * slot;
    const h = Math.max(1, (s.requests / max) * plotH);
    const y = padT + plotH - h;

    // Trouble is drawn at the BOTTOM of its own bar, so a bad second reads as a
    // bar with a red foot rather than as a separate mark somewhere else.
    const bad = Math.min(s.requests, s.failures + s.rateLimited);
    const badH = bad ? Math.max(1, (bad / max) * plotH) : 0;

    const tip = [
      secLabel(s.sec),
      'запросов: ' + num(s.requests),
    ];
    if (s.rateLimited) tip.push('429: ' + num(s.rateLimited));
    if (s.failures) tip.push('ошибок: ' + num(s.failures));
    if (s.slowestMs) tip.push('самый долгий: ' + fmtMs(s.slowestMs));

    root.append(hoverable(svg('rect', {
      x, y, width: barW, height: h, fill: VIZ.cat[0], class: 'viz-mark',
    }), tip));
    if (badH > 0) {
      root.append(svg('rect', {
        x, y: padT + plotH - badH, width: barW, height: badH,
        fill: s.rateLimited ? '#e0b562' : '#e06a6a',
      }));
    }
  });
  return root;
}

// fmtMs renders a duration the way this panel talks about latency.
function fmtMs(ms) {
  if (ms < 1000) return ms + ' мс';
  return (ms / 1000).toFixed(1) + ' с';
}

// secLabel is a wall-clock time for one bucket.
function secLabel(sec) {
  const d = new Date(sec * 1000);
  return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

// trafficSummary is the sentence under the chart: the numbers a reader would
// otherwise have to estimate from bar heights.
function trafficSummary(samples) {
  if (!samples || !samples.length) return 'нет данных за последние минуты';
  let total = 0, peak = 0, bad = 0, slowest = 0, active = 0;
  for (const s of samples) {
    total += s.requests;
    bad += s.failures + s.rateLimited;
    if (s.requests > peak) peak = s.requests;
    if (s.slowestMs > slowest) slowest = s.slowestMs;
    if (s.requests) active++;
  }
  if (!total) return 'за последние ' + Math.round(samples.length / 60) + ' мин запросов не было';
  const parts = [
    'всего ' + num(total),
    'пик ' + num(peak) + '/с',
    'в среднем ' + (total / samples.length).toFixed(1) + '/с',
    'секунд с активностью ' + num(active) + ' из ' + num(samples.length),
  ];
  if (slowest) parts.push('самый долгий ' + fmtMs(slowest));
  if (bad) parts.push('проблемных ответов ' + num(bad));
  return parts.join(' · ');
}

// --- what our own corpus says ------------------------------------------------
//
// The bundle carries two answers to "what is strong at position 3": STRATZ's
// ranked all-pick meta, and the same question asked of the Captains Mode corpus
// this bundle was trained on. They describe different populations and disagree,
// so both are shown and each says which it is.

async function loadCorpusMeta() {
  const host = $('corpus-meta');
  if (!host) return;
  try {
    const res = await fetch('/api/corpus-meta');
    if (!res.ok) {
      host.textContent = '';
      host.append(el('div', 'why', 'агрегаты недоступны: сервер ответил ' + res.status));
      return;
    }
    renderCorpusMeta(await res.json());
  } catch (e) {
    host.textContent = '';
    host.append(el('div', 'why', String(e)));
  }
}

const POS_TITLE = {
  1: 'Позиция 1 — керри',
  2: 'Позиция 2 — мид',
  3: 'Позиция 3 — оффлейн',
  4: 'Позиция 4 — роумер',
  5: 'Позиция 5 — хардсаппорт',
};

function renderCorpusMeta(data) {
  const host = $('corpus-meta');
  host.textContent = '';
  if (!data || data.error) {
    host.append(el('div', 'why', 'нет данных: ' + ((data && data.error) || '')));
    return;
  }
  if (!data.positions || !data.positions.length) {
    host.append(el('div', 'why', 'бандл собран до появления этих агрегатов — они появятся после следующего обучения'));
    return;
  }

  const head = el('div', 'panel');
  head.append(el('h2', null, 'Позиции и лайны по нашим данным'));
  head.append(el('p', 'why', 'Посчитано по тому корпусу, на котором обучен бандл, а не по рейтинговому олпику из STRATZ — это разные популяции, поэтому цифры расходятся, и панель «Мета по позициям» на экране драфта показывает вторую. Винрейт сглажен к среднему по позиции: герой с тремя играми не должен обгонять реальный пик. Бандл '
    + (data.bundleVersion || '-').slice(0, 12)
    + (data.patchTag ? ', патч ' + data.patchTag : '') + '.'));
  host.append(head);

  const posPanel = el('div', 'panel');
  posPanel.append(el('h2', null, 'Герои по позициям'));
  const grid = el('div', 'grid-2');
  const byPos = new Map();
  for (const r of data.positions) {
    if (!byPos.has(r.position)) byPos.set(r.position, []);
    byPos.get(r.position).push(r);
  }
  const order = Array.from(byPos.keys()).sort((a, b) => a - b);
  for (const pos of order) {
    const col = el('div');
    col.append(el('h3', null, POS_TITLE[pos] || ('Позиция ' + pos)));
    const box = el('div', 'items');
    byPos.get(pos).forEach((r, i) => {
      const row = el('div', 'item');
      row.append(el('span', 'rank', String(i + 1)));
      const mid = el('span', 'hero-cell');
      mid.append(heroIcon(r.heroId));
      mid.append(el('b', null, r.name));
      mid.append(document.createTextNode(' '));
      mid.append(el('span', 'why', num(r.matches) + ' игр · ' + pctOf(r.pickRate) + ' драфтов'));
      row.append(mid);
      row.append(el('span', 'val', (r.winRate * 100).toFixed(1) + '%'));
      box.append(row);
    });
    col.append(box);
    grid.append(col);
  }
  posPanel.append(grid);
  host.append(posPanel);

  if (data.lanes && data.lanes.length) {
    const lanePanel = el('div', 'panel');
    lanePanel.append(el('h2', null, 'Связки на лайнах'));
    lanePanel.append(el('p', 'why', 'Лёгкая — 1+5, сложная — 3+4, мид — соло. «Лифт» — насколько связка лучше среднего лайна: сравнивать стоит именно его, а не голый винрейт.'));
    const box = el('div', 'items');
    for (const r of data.lanes) {
      const row = el('div', 'item');
      row.append(el('span', 'rank', ''));
      const mid = el('span');
      mid.append(el('b', null, r.heroes.join(' + ')));
      mid.append(document.createTextNode(' '));
      mid.append(el('span', 'why', r.lane + ', ' + num(r.matches) + ' игр'));
      row.append(mid);
      const lift = (r.lift >= 0 ? '+' : '') + (r.lift * 100).toFixed(1) + ' п.п.';
      row.append(el('span', 'val', lift));
      box.append(row);
    }
    lanePanel.append(box);
    host.append(lanePanel);
  }

  if (data.bans && data.bans.length) {
    const banPanel = el('div', 'panel');
    banPanel.append(el('h2', null, 'Кого банят'));
    banPanel.append(el('p', 'why', 'Позиции у бана нет: баны идут до того, как появляются лайны.'));
    banPanel.append(barsChart(data.bans.map((b) => ({
      label: b.name,
      value: b.bans,
      valueLabel: num(b.bans) + ' - ' + pctOf(b.banRate),
      tip: [b.name, 'банов: ' + num(b.bans), pctOf(b.banRate)],
    })), () => VIZ.cat[1], { width: chartWidth($('corpus-meta')), padL: 130, rowH: 22 }));
    host.append(banPanel);
  }
}

// pctOf formats a 0..1 share the way the rest of the screen formats percentages.
function pctOf(share) {
  const v = (share || 0) * 100;
  return (v < 1 && v > 0 ? v.toFixed(1) : Math.round(v)) + '%';
}

// Charts are drawn at a measured pixel width, so a resize needs new geometry
// rather than a CSS rescale. Debounced: a drag fires this continuously, and
// repainting the whole section per frame is wasteful for something nobody reads
// mid-drag.
let corpusResizeTimer = null;
window.addEventListener('resize', () => {
  if (!lastCorpus) return;
  if (corpusResizeTimer) clearTimeout(corpusResizeTimer);
  corpusResizeTimer = setTimeout(() => {
    corpusResizeTimer = null;
    const host = document.getElementById('corpus');
    if (host && host.clientWidth) renderCorpus(lastCorpus);
  }, 150);
});

async function loadCorpus() {
  if (corpusLoading) return;
  corpusLoading = true;
  try {
    const res = await fetch('/api/corpus');
    if (!res.ok) {
      renderCorpus({ error: 'сервер ответил ' + res.status });
      return;
    }
    renderCorpus(await res.json());
  } catch (e) {
    renderCorpus({ error: String(e) });
  } finally {
    corpusLoading = false;
  }
}
