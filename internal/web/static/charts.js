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
  ramp: ['#9ec5f4', '#6da7ec', '#3987e5', '#256abf', '#184f95'],
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
function intakeChart(daily) {
  const W = 720, H = 190, padL = 46, padR = 10, padT = 10, padB = 26;
  const root = svg('svg', {
    viewBox: `0 0 ${W} ${H}`, class: 'viz', role: 'img',
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
  // The viewBox scales to the container, so a chart rendered in a half-width
  // column must be authored narrow — otherwise every label is scaled down with it
  // and the text ends up unreadably small next to the full-width charts.
  const o = opts || {};
  const W = o.width || 720, rowH = o.rowH || 26, padL = o.padL || 130;
  const padR = o.padR || 56, padT = 6;
  const H = padT * 2 + rows.length * rowH;
  const root = svg('svg', {
    viewBox: `0 0 ${W} ${H}`, class: 'viz', role: 'img',
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

// renderCorpus paints the whole "what we collected" section.
function renderCorpus(data) {
  const host = $('corpus');
  host.textContent = '';

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
  played.append(intakeChart(c.daily));
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
  fetched.append(intakeChart(c.fetched));
  fetched.append(tableView('Показать таблицей', ['День загрузки', 'Матчей', 'CM-драфтов'],
    (c.fetched || []).map((d) => [dayLabel(d.day), num(d.matches), num(d.cmDrafts)])));
  host.append(fetched);

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
  bySource.append(barsChart(srcRows, (r) => SOURCE_COLORS[r.label] || VIZ.muted));
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
    const idx = known.findIndex((k) => k.tier === r.tier);
    return VIZ.ramp[Math.min(VIZ.ramp.length - 1, Math.max(0, idx))];
  }));
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

  const half = { width: 360, padL: 108, padR: 52, rowH: 22 };

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
