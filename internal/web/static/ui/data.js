// The data page: what the model knows, collecting and training, the jobs that
// do it, the APIs they talk to, and what the cache holds.

import {
  $, el, store, post, getJSON, button, num, duration, jobName, ago, nameOf, portrait, pct1, confirmable,
} from './core.js';

let corpus = null;
let corpusMeta = null;
let sources = [];
let sourcesTimer = 0;
let logJob = 0;
let logLines = [];
let logTimer = 0;

const RANK = { '-1': 'не известен', 0: 'без калибровки', 10: 'Herald', 20: 'Guardian', 30: 'Crusader', 40: 'Archon', 50: 'Legend', 60: 'Ancient', 70: 'Divine', 80: 'Immortal' };
const SOURCE = { bulk: 'Публичные all-pick', 'public-cm': 'Публичные CM', pro: 'Про-матчи', '': 'Без пометки' };

export function enterData() {
  loadCorpus();
  pollSources();
  renderData();
}

export function leaveData() {
  clearTimeout(sourcesTimer);
  clearTimeout(logTimer);
  sourcesTimer = 0;
}

async function loadCorpus() {
  const [c, m] = await Promise.all([getJSON('/api/corpus'), getJSON('/api/corpus-meta')]);
  corpus = c;
  corpusMeta = m;
  renderData();
}

async function pollSources() {
  const data = await getJSON('/api/sources');
  sources = data.sources || [];
  renderSources();
  if (document.body.dataset.page === 'data') sourcesTimer = setTimeout(pollSources, 2000);
}

export function renderData() {
  const s = store.state;
  if (!s) return;
  renderModel(s);
  renderJobs(s);
  renderCorpus();
}

function activeOf(s, kind) {
  return (s.ops?.active || []).find((j) => j.kind === kind);
}

function renderModel(s) {
  const host = $('data-model');
  host.textContent = '';
  const m = s.meta;
  const sec = el('section', 'doc-sec');
  sec.append(el('h2', null, 'Модель'));
  const facts = el('dl', 'facts');
  const fact = (k, v) => { facts.append(el('dt', null, k), el('dd', null, v)); };
  if (m.draftReady) {
    fact('Патч', m.patchTag || 'не указан');
    fact('Версия', (m.bundleVersion || '').slice(0, 12));
    fact('Парные данные', m.hasMatrices ? 'есть' : 'пока нет');
  } else {
    fact('Состояние', m.draftError || 'не обучена');
  }
  sec.append(facts);

  const act = el('div', 'actions');
  const sync = activeOf(s, 'sync');
  const train = activeOf(s, 'train');
  if (m.canSync) {
    const b = button(sync ? 'Сбор матчей идёт' : 'Собрать матчи', 'btn quiet', () => post('/api/ops/sync'));
    b.disabled = !!sync;
    act.append(b);
  }
  if (m.canTrain) {
    const b = button(train ? 'Обучение идёт' : 'Обучить модель', 'btn', () => post('/api/ops/train'));
    b.disabled = !!train;
    act.append(b);
  }
  sec.append(act);
  sec.append(el('p', 'muted', 'Сбор пополняет кэш матчей, модель от этого не меняется. Обучение пересобирает модель из кэша; драфт подхватит её сам, без перезапуска.'));

  const a = s.auto;
  if (a && a.phase && a.phase !== 'off') {
    const auto = el('div', 'auto-line');
    const phase = { idle: 'Фоновый сбор ждёт', sync: 'Фоновый сбор идёт', train: 'Фоновое переобучение', paused: 'Фоновый сбор на паузе' }[a.phase] || a.phase;
    auto.append(el('b', null, phase));
    const tail = a.line || a.last || '';
    if (tail) auto.append(document.createTextNode(`: ${tail}`));
    if (a.phase === 'idle' && a.nextInSeconds) auto.append(el('span', 'muted', ` Следующий проход через ${duration(a.nextInSeconds * 1000)}.`));
    if (a.error) auto.append(el('div', 'err', a.error));
    sec.append(auto);
  }
  host.append(sec);
}

function renderJobs(s) {
  const host = $('data-jobs');
  host.textContent = '';
  const active = s.ops?.active || [];
  const recent = s.ops?.recent || [];

  const sec = el('section', 'doc-sec');
  sec.append(el('h2', null, 'Задачи'));
  if (!active.length) sec.append(el('p', 'muted', 'Сейчас ничего не выполняется.'));
  active.forEach((j) => sec.append(jobRow(j, true)));
  if (recent.length) {
    sec.append(el('h3', null, 'Недавние'));
    const list = el('ul', 'job-history');
    recent.slice(0, 8).forEach((j) => list.append(jobRow(j, false, 'li')));
    sec.append(list);
  }
  host.append(sec);

  if (logJob) {
    const box = el('section', 'doc-sec log');
    const head = el('div', 'log-head');
    head.append(el('h3', null, `Журнал: ${jobName((active.concat(recent).find((j) => j.id === logJob) || {}).kind || '')}`),
      button('Закрыть', 'ghost-btn', () => { logJob = 0; clearTimeout(logTimer); renderData(); }));
    box.append(head);
    const pre = el('ol', 'log-lines');
    logLines.slice(-300).forEach((l) => {
      const li = el('li', 'lvl-' + (l.level || 'info'));
      li.append(el('time', null, new Date(l.at).toLocaleTimeString('ru-RU')), el('span', null, l.text));
      pre.append(li);
    });
    box.append(pre);
    host.append(box);
    requestAnimationFrame(() => { pre.scrollTop = pre.scrollHeight; });
  }
}

function jobRow(j, live, tag = 'div') {
  const row = el(tag, 'job ' + (live ? 'live' : j.state));
  const top = el('div', 'job-top');
  top.append(el('b', null, jobName(j.kind)));
  const state = { pending: 'в очереди', running: 'выполняется', done: 'готово', failed: 'ошибка', cancelled: 'отменено' }[j.state] || j.state;
  top.append(el('span', 'job-state', state));
  if (j.runningMs) top.append(el('span', 'muted', duration(j.runningMs)));
  row.append(top);
  if (live) {
    if (j.phase) row.append(el('div', 'job-phase', j.phase));
    if (j.lastLine) row.append(el('div', 'job-line', j.lastLine));
    if (j.waitingOn) row.append(el('div', 'job-wait', `ждём ответа: ${j.waitingOn}`));
  } else if (j.error) {
    row.append(el('div', 'err', j.error));
  } else if (j.result) {
    row.append(el('div', 'job-line', j.result));
  }
  const act = el('div', 'job-actions');
  act.append(button('Журнал', 'link-btn', () => openLog(j.id)));
  if (live) act.append(confirmable(button('Остановить', 'link-btn danger'), 'Точно остановить?', () => post('/api/ops/cancel', { id: j.id })));
  row.append(act);
  return row;
}

async function openLog(id) {
  logJob = id;
  logLines = [];
  clearTimeout(logTimer);
  await pullLog();
}

async function pullLog() {
  if (!logJob) return;
  const after = logLines.length ? logLines[logLines.length - 1].seq : 0;
  const data = await getJSON(`/api/jobs/log?id=${logJob}&after=${after}&limit=400`);
  if (data.lines) logLines = logLines.concat(data.lines);
  renderData();
  const running = data.job && (data.job.state === 'running' || data.job.state === 'pending');
  if (running && document.body.dataset.page === 'data') logTimer = setTimeout(pullLog, 1500);
}

function renderSources() {
  const host = $('data-sources');
  if (!host) return;
  host.textContent = '';
  const sec = el('section', 'doc-sec');
  sec.append(el('h2', null, 'Источники данных'));
  if (!sources.length) {
    sec.append(el('p', 'muted', 'К API ещё не обращались с момента запуска.'));
    host.append(sec);
    return;
  }
  const table = el('table', 'tbl');
  const head = el('tr');
  ['Источник', 'Сейчас', 'Запросов', 'Повторы', 'Лимит 429', 'Ошибки'].forEach((h) => head.append(el('th', null, h)));
  const thead = el('thead');
  thead.append(head);
  table.append(thead);
  const body = el('tbody');
  sources.forEach((src) => {
    const tr = el('tr');
    let now = 'тихо';
    if (src.waitingUntilMs > 0) now = `пауза ${duration(src.waitingUntilMs)}`;
    else if (src.inFlight > 0) now = `${src.inFlight} в полёте` + (src.oldestMs > 3000 ? `, ждём ${duration(src.oldestMs)}` : '');
    tr.append(el('td', null, src.source), el('td', src.waitingUntilMs > 0 ? 'warn' : '', now),
      el('td', 'n', num(src.requests)), el('td', 'n', num(src.retries)),
      el('td', 'n', num(src.rateLimited)), el('td', 'n' + (src.failures ? ' warn' : ''), num(src.failures)));
    body.append(tr);
    if (src.lastErr) {
      const er = el('tr');
      const td = el('td', 'err', `последняя ошибка: ${src.lastErr}`);
      td.colSpan = 6;
      er.append(td);
      body.append(er);
    }
  });
  table.append(body);
  sec.append(table);
  host.append(sec);
}

function renderCorpus() {
  const host = $('data-corpus');
  host.textContent = '';
  const sec = el('section', 'doc-sec');
  const head = el('div', 'sec-head');
  head.append(el('h2', null, 'Что собрано'), button('Обновить', 'ghost-btn', loadCorpus));
  sec.append(head);
  if (!corpus) { sec.append(el('p', 'muted', 'Считаю…')); host.append(sec); return; }
  if (corpus.error || !corpus.corpus) {
    sec.append(el('p', 'err', corpus.unavailable ? 'Статистика кэша недоступна в этой сборке.' : (corpus.error || 'Кэш пуст.')));
    host.append(sec);
    return;
  }
  const c = corpus.corpus;
  const tiles = el('div', 'tiles');
  const tile = (n, label) => { const t = el('div', 'tile'); t.append(el('b', null, n), el('span', null, label)); tiles.append(t); };
  tile(num(c.matches), 'матчей в окне');
  tile(num(c.cmDrafts), 'драфтов Captains Mode');
  tile(num(c.withPositions), 'с известными позициями');
  if (c.oldestStart) tile(`${new Date(c.oldestStart * 1000).toLocaleDateString('ru-RU')} — ${new Date(c.newestStart * 1000).toLocaleDateString('ru-RU')}`, 'период');
  sec.append(tiles);

  if ((c.daily || []).length) sec.append(dailyChart(c.daily));

  const split = el('div', 'two-col');
  const srcBox = el('div');
  srcBox.append(el('h3', null, 'Откуда'));
  const srcList = el('ul', 'bars');
  const maxSrc = Math.max(...(c.sources || []).map((x) => x.matches), 1);
  (c.sources || []).forEach((x) => srcList.append(barRow(SOURCE[x.source] || x.source, x.matches, maxSrc, x.cmDrafts ? `${num(x.cmDrafts)} CM` : '')));
  srcBox.append(srcList);
  const rankBox = el('div');
  rankBox.append(el('h3', null, 'Ранги'));
  const rankList = el('ul', 'bars');
  const maxRank = Math.max(...(c.ranks || []).map((x) => x.matches), 1);
  (c.ranks || []).forEach((x) => rankList.append(barRow(RANK[x.tier] || x.tier, x.matches, maxRank)));
  rankBox.append(rankList);
  split.append(srcBox, rankBox);
  sec.append(split);

  const meta = el('div', 'two-col');
  meta.append(heroTop('Чаще всего пикают', c.topPicks), heroTop('Чаще всего банят', c.topBans));
  sec.append(meta);
  host.append(sec);

  if (corpusMeta && !corpusMeta.error && (corpusMeta.positions || []).length) host.append(positionMeta(corpusMeta));
}

function barRow(label, value, max, note) {
  const li = el('li');
  li.append(el('span', 'bar-label', label));
  const track = el('span', 'bar-track');
  const fill = el('span', 'bar-fill');
  fill.style.width = `${Math.max(1, (value / max) * 100)}%`;
  track.append(fill);
  li.append(track, el('span', 'bar-num', num(value) + (note ? `, ${note}` : '')));
  return li;
}

function heroTop(title, list) {
  const box = el('div');
  box.append(el('h3', null, title));
  const ul = el('ol', 'hero-top');
  (list || []).slice(0, 10).forEach((h) => {
    const li = el('li');
    li.append(portrait(h.heroId, 'mini'), el('span', null, h.name || nameOf(h.heroId)), el('span', 'bar-num', num(h.count)));
    ul.append(li);
  });
  box.append(ul);
  return box;
}

// dailyChart is an inline bar chart of matches per played day, with CM drafts as
// the darker part of each bar. A table of the same numbers sits behind it for
// screen readers.
function dailyChart(days) {
  const recent = days.slice(-45);
  const max = Math.max(...recent.map((d) => d.matches), 1);
  const fig = el('figure', 'daily');
  fig.append(el('figcaption', null, 'Матчи по дню игры: светлое — все, тёмное — Captains Mode'));
  const chart = el('div', 'daily-bars');
  chart.setAttribute('aria-hidden', 'true');
  recent.forEach((d) => {
    const col = el('div', 'daily-col');
    col.title = `${new Date(d.day * 1000).toLocaleDateString('ru-RU')}: ${num(d.matches)} матчей, ${num(d.cmDrafts)} CM`;
    const all = el('div', 'daily-all');
    all.style.height = `${(d.matches / max) * 100}%`;
    const cm = el('div', 'daily-cm');
    cm.style.height = `${d.matches ? (d.cmDrafts / d.matches) * 100 : 0}%`;
    all.append(cm);
    col.append(all);
    chart.append(col);
  });
  fig.append(chart);
  const axis = el('div', 'daily-axis');
  axis.append(el('span', null, new Date(recent[0].day * 1000).toLocaleDateString('ru-RU')),
    el('span', null, new Date(recent[recent.length - 1].day * 1000).toLocaleDateString('ru-RU')));
  fig.append(axis);
  const table = el('table', 'sr-only');
  recent.forEach((d) => {
    const tr = el('tr');
    tr.append(el('td', null, new Date(d.day * 1000).toLocaleDateString('ru-RU')), el('td', null, String(d.matches)), el('td', null, String(d.cmDrafts)));
    table.append(tr);
  });
  fig.append(table);
  return fig;
}

function positionMeta(cm) {
  const sec = el('section', 'doc-sec');
  sec.append(el('h2', null, 'Мета по позициям в нашем кэше'));
  sec.append(el('p', 'muted', `Посчитано при обучении${cm.patchTag ? `, патч ${cm.patchTag}` : ''}: самые результативные герои на каждой позиции.`));
  const grid = el('div', 'pos-grid');
  const names = { 1: 'Керри', 2: 'Мид', 3: 'Оффлейн', 4: 'Четвёрка', 5: 'Пятёрка' };
  for (let p = 1; p <= 5; p++) {
    const rows = cm.positions.filter((r) => r.position === p).sort((a, b) => b.matches - a.matches).slice(0, 6);
    const col = el('div', 'pos-col');
    col.append(el('h3', null, names[p]));
    const ul = el('ul', 'hero-top');
    rows.forEach((r) => {
      const li = el('li');
      li.append(portrait(r.heroId, 'mini'), el('span', null, r.name || nameOf(r.heroId)), el('span', 'bar-num', pct1(r.winRate)));
      li.title = `${num(r.matches)} матчей, пикрейт ${pct1(r.pickRate)}`;
      ul.append(li);
    });
    col.append(ul);
    grid.append(col);
  }
  sec.append(grid);
  return sec;
}

export { ago };
