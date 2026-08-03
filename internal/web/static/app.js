'use strict';

// The front end renders exactly what the core returned, in the order it returned
// it. It must never sort, re-rank, round or recompute anything: the ranking is
// already rounded and tie-broken by hero id server-side, and re-sorting here would
// reintroduce exactly the cross-machine divergence the bundle rules prevent (§6).

let state = null;
let stats = null;
let activeTab = 'draft';

// --- panel registry ---------------------------------------------------------
//
// Each entry maps one field of the core's Result to a renderer. Adding a field to
// Result means adding one line here — and until you do, the generic fallback below
// already renders it, so a core change is never invisible in the browser.

const PANELS = [
  { key: 'recs', title: 'Рекомендации', render: renderRecs, empty: 'нет ходов' },
  { key: 'enemyPred', title: 'Предикт врага', render: renderEnemyPred },
  { key: 'poolAlert', title: 'Наш пул под угрозой', render: renderEnemyPred },
  { key: 'safeToLet', title: 'Не бойся — пусть берут', render: renderSafeToLet },
  { key: 'positionMeta', title: 'Мета по позициям', render: renderPositionMeta },
  { key: 'itemAdvice', title: 'Советы по предметам', render: renderAdvice },
  // Rendered inside the board panel, not as a standalone panel.
  { key: 'strength', skip: true },
  { key: 'ourTurn', skip: true },
  { key: 'isPick', skip: true },
];

function heroName(id) {
  return (state && state.names && state.names[String(id)]) || ('Hero ' + id);
}

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}

function $(id) { return document.getElementById(id); }

// heroIcon returns a portrait element for a hero, or null when images are not
// cached yet.
//
// The image is served by our own HTTP endpoint from a local directory — the page
// never references a CDN, so it works with no network at all. A portrait that has
// not been downloaded 404s, and the element removes itself: the text label beside
// it was always the real content, and the picture is decoration on top of it.
function heroIcon(id) {
  const img = document.createElement('img');
  img.className = 'hero-icon';
  img.loading = 'lazy';
  img.alt = '';
  img.src = '/assets/heroes/' + id + '.png';
  img.onerror = () => img.remove();
  return img;
}

// heroItem is one clickable row: rank, hero name, a value and an optional note.
function heroItem(rank, id, value, note, extra) {
  const row = el('div', 'item');
  row.append(el('span', 'rank', rank === null ? '' : String(rank)));
  const mid = el('span', 'hero-cell');
  mid.append(heroIcon(id));
  mid.append(el('b', null, heroName(id)));
  if (note) {
    mid.append(document.createTextNode(' '));
    mid.append(el('span', 'why', note));
  }
  row.append(mid);
  const right = el('span', 'val', value);
  if (extra) {
    right.append(document.createTextNode(' '));
    right.append(el('span', 'src', extra));
  }
  row.append(right);
  row.onclick = () => openMatchups(id);
  return row;
}

function renderRecs(recs) {
  const box = el('div', 'items');
  recs.forEach((r, i) => box.append(heroItem(i + 1, r.heroId, r.score.toFixed(4), r.why)));
  return box;
}

// dangerNote describes what a hero would cost us, and only when it is big enough to
// matter — a tag on every row is noise, not information. Both values come from the
// core; this only decides whether to show them.
function dangerNote(e) {
  if (typeof e.danger !== 'number') return null;
  if (e.danger >= 0.005) return 'опасен ' + e.danger.toFixed(2);
  if (e.danger <= -0.005) return 'нам на руку ' + e.danger.toFixed(2);
  return null;
}

function renderEnemyPred(list) {
  const box = el('div', 'items');
  list.forEach((e, i) => {
    box.append(heroItem(i + 1, e.heroId, (e.prob * 100).toFixed(1) + '%', dangerNote(e), e.source));
  });
  return box;
}

// renderSafeToLet shows the "don't fear" list with the full decomposition on every
// row, unlike the prediction panel: this list exists to justify conceding a hero,
// and the numbers ARE the justification. vs-us = counter lift against our picks,
// with-them = synergy with the enemy draft, cost = the two combined (the same
// quantity the ban ranking sorts by).
function renderSafeToLet(list) {
  const box = el('div', 'items');
  list.forEach((e, i) => {
    const note = 'против нас ' + (e.threat * 100).toFixed(1) + '%'
      + ' · с их драфтом ' + (e.fit * 100).toFixed(1) + '%'
      + ' · цена ' + e.danger.toFixed(3);
    box.append(heroItem(i + 1, e.heroId, (e.prob * 100).toFixed(1) + '%', note, e.source));
  });
  return box;
}

function renderPositionMeta(positions) {
  const grid = el('div', 'grid-2');
  positions.forEach((pm) => {
    const col = el('div');
    col.append(el('h2', null, 'Позиция ' + pm.position));
    const box = el('div', 'items');
    pm.heroes.forEach((h, i) => {
      const wr = h.winRate > 0 ? (h.winRate * 100).toFixed(1) + '% wr' : h.score.toFixed(3);
      box.append(heroItem(i + 1, h.heroId, wr));
    });
    col.append(box);
    grid.append(col);
  });
  return grid;
}

function renderAdvice(lines) {
  const ul = el('ul', 'advice');
  lines.forEach((l) => ul.append(el('li', null, l)));
  return ul;
}

// renderUnknown is the fallback for a Result field with no registered renderer:
// core work shows up in the browser immediately, just unstyled.
function renderUnknown(value) {
  return el('pre', 'raw', JSON.stringify(value, null, 2));
}

function isEmpty(v) {
  return v === null || v === undefined || (Array.isArray(v) && v.length === 0);
}

function renderPanels() {
  const host = $('panels');
  host.textContent = '';
  if (!state.meta.draftReady) return;
  const registered = new Set(PANELS.map((p) => p.key));

  const entries = PANELS.filter((p) => !p.skip).map((p) => [p, state.result[p.key]]);
  // Anything the core added that this file has not learned about yet.
  Object.keys(state.result).forEach((k) => {
    if (!registered.has(k)) entries.push([{ key: k, title: k, render: renderUnknown }, state.result[k]]);
  });

  entries.forEach(([panel, value]) => {
    if (isEmpty(value) && !panel.empty) return;
    const box = el('section', 'panel');
    box.append(el('h2', null, panel.title));
    box.append(isEmpty(value) ? el('div', 'why', panel.empty) : panel.render(value));
    host.append(box);
  });

  if (state.alerts && state.alerts.length) {
    const box = el('section', 'panel');
    box.append(el('h2', null, 'Объекты'));
    const items = el('div', 'items');
    state.alerts.forEach((a) => {
      const row = el('div', 'item' + (a.due ? ' alert-due' : ''));
      row.append(el('span', 'rank', a.due ? '●' : ''));
      row.append(el('span', null, a.label));
      row.append(el('span', 'val', mmss(a.nextIn)));
      items.append(row);
    });
    box.append(items);
    host.append(box);
  }
}

function mmss(sec) {
  const s = Math.max(0, sec);
  return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
}

function clockMMSS(sec) {
  const sign = sec < 0 ? '-' : '';
  const s = Math.abs(sec);
  return sign + Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
}

// --- board ------------------------------------------------------------------

function sideRow(side, cls, picks, bans) {
  const wrap = el('div', 'board-side ' + cls);
  const line = (label, ids, isBan) => {
    const row = el('div');
    row.append(el('span', 'label', label));
    const box = el('span', 'board-heroes');
    ids.forEach((id) => {
      const chip = el('span', 'chip' + (isBan ? ' ban' : '') + ' name');
      chip.append(heroIcon(id));
      chip.append(document.createTextNode(heroName(id)));
      chip.onclick = () => openMatchups(id);
      box.append(chip);
    });
    if (!ids.length) box.append(el('span', 'why', '—'));
    row.append(box);
    return row;
  };
  wrap.append(line(side + ' picks', picks, false));
  wrap.append(line('bans', bans, true));
  return wrap;
}

function renderBoard() {
  const host = $('board');
  host.textContent = '';
  const b = state.board;
  host.append(sideRow('Radiant', 'radiant', b.radiantPicks, b.radiantBans));
  host.append(sideRow('Dire', 'dire', b.direPicks, b.direBans));

  const s = state.result.strength;
  if (s) {
    const box = el('div', 'strength');
    box.append(el('span', 'big', (s.ourProb * 100).toFixed(1) + '%'));
    box.append(document.createTextNode(' наша сила драфта  '));
    box.append(el('span', 'why', 'Radiant ' + (s.radiantProb * 100).toFixed(1) + '%'));
    if (s.radiantPicks < 5 || s.direPicks < 5) {
      box.append(el('div', 'note',
        'драфт неполный (' + s.radiantPicks + ' vs ' + s.direPicks + ') — оценка шумная'));
    }
    host.append(box);
  }
}

// --- header / status --------------------------------------------------------

function renderHeader() {
  const m = state.meta;
  $('our-team').textContent = m.ourTeam || '—';
  $('first-pick').textContent = m.firstPick || '—';

  const step = $('step');
  step.textContent = '';
  if (!m.draftReady) {
    step.append(el('span', 'why', 'бандл не загружен'));
  } else if (!state.step) {
    step.append(el('span', 'why', m.gsiMode && !m.gsiSeen ? 'ожидание GSI…' : 'драфт завершён'));
  } else {
    const ours = state.step.isOurs;
    step.append(el('span', ours ? 'ours' : 'theirs',
      (ours ? 'НАШ ход' : 'ход соперника') + ': ' + state.step.action + ' (' + state.step.team + ')'));
  }

  $('draft-missing').hidden = m.draftReady;
  $('draft-missing-msg').textContent = m.draftError || 'бандл ещё не собран';

  // In GSI mode the board is authoritative; manual editing is disabled server-side
  // too, so the buttons only reflect that. Only the controls that actually mutate
  // the draft are gated — scouting an enemy and browsing matchups do not, and
  // disabling them here made the whole screen look broken.
  const manual = m.draftReady && !m.gsiMode;
  // Nothing is greyed out because something else is running any more: work goes
  // into a queue that decides what may overlap, by resource. A second request is
  // queued, not refused, so disabling the buttons would only hide that.
  const enable = (id, on) => { const b = $(id); if (b) b.disabled = !on; };

  document.querySelectorAll('#screen-draft [data-action]').forEach((b) => {
    const a = b.dataset.action;
    if (a === 'undo') b.disabled = !m.canUndo;
    else if (a === 'redo') b.disabled = !m.canRedo;
    else if (a === 'side' || a === 'reload' || a === 'enemy') b.disabled = !m.draftReady;
    else b.disabled = !manual;
  });
  $('enemy-panel').hidden = !m.canAddEnemy;
  // The filter still narrows the list in GSI mode, where clicking a hero opens its
  // matchups instead of picking it.
  $('hero-filter').disabled = !m.draftReady;

  // Say why the board cannot be edited, instead of just greying everything out.
  // The click hint is conditional: openMatchups does nothing without matrices in the
  // bundle, and promising something that silently fails is what this notice exists
  // to prevent. Manual input is turned off by `gsi` in config.yaml as often as by
  // the flag, so name both ways back out of it.
  const gsiNote = $('gsi-note');
  gsiNote.hidden = !m.draftReady || !m.gsiMode;
  const clickHint = m.hasMatrices ? ' Клик по герою открывает матчапы.' : '';
  gsiNote.textContent = (m.gsiSeen
    ? 'Драфт ведёт Dota (GSI) — доска только для чтения.'
    : 'Драфт ведёт Dota (GSI) — ждём первое событие от игры. Доска только для чтения.')
    + clickHint
    + ' Ручной ввод — запусти с --gsi=false (или убери gsi из config.yaml).';

  enable('btn-sync', m.canSync);
  enable('btn-train', m.canTrain);
  enable('btn-scout', m.canScout);
  enable('btn-allies', m.canScoutAllies);
  enable('btn-clear3', true);

  // Tabs whose backing operations this build does not offer stay visible but inert.
  const tabAvailable = {
    draft: true,
    data: m.canSync || m.canTrain,
    scout: m.canScout || m.canScoutAllies,
    stats: m.canStats,
  };
  document.querySelectorAll('#tabs .tab').forEach((t) => {
    t.disabled = !tabAvailable[t.dataset.tab];
    t.classList.toggle('active', t.dataset.tab === activeTab);
  });
}

// A long operation can legitimately go quiet for minutes — a sync whose cache is
// already warm skips match after match without producing a line. The banner has to
// distinguish that from a real stall, so all three facts come from the server
// (runningMs, quietMs, and rev, which unlike log.length is not capped) and are
// ticked forward locally between frames. The one thing measured in the page is how
// long since a frame ARRIVED: the server sends a keepalive every few seconds, so
// silence there means the connection died, not that the work stopped.
let opFrameAt = null;   // when the last frame reached this page
let opRunningMs = 0;    // server-measured, at opFrameAt
let opQuietMs = 0;      // server-measured, at opFrameAt
let opTicker = null;

// staleFrameMs is when a missing keepalive stops being scheduling jitter. The
// server aims for one every 3s.
const staleFrameMs = 10000;

function fmtElapsed(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return s + 'с';
  const m = Math.floor(s / 60);
  return m + 'м ' + String(s % 60).padStart(2, '0') + 'с';
}

// STATE_LABEL names a job state in the UI. The wire keeps the English identifiers.
const STATE_LABEL = {
  pending: 'в очереди',
  running: 'выполняется',
  done: 'готово',
  failed: 'ошибка',
  cancelled: 'отменено',
};

// selectedJob is the job whose log is shown. null follows whatever is running,
// which is what a user watching a sync expects; clicking a row pins it instead.
let selectedJob = null;
let logSeq = 0;
let logLines = [];
let logPending = false;

// renderOpHeading redraws just the banner title, cheaply, once a second.
function renderOpHeading() {
  const o = state && state.ops;
  if (!o) return;
  const active = o.active || [];
  if (!active.length) {
    $('op-name').textContent = 'фоновых задач нет';
    return;
  }
  const running = active.filter((j) => j.state === 'running');
  const queued = active.length - running.length;
  if (!running.length) {
    $('op-name').textContent = 'в очереди: ' + active.length;
    return;
  }
  const sinceFrame = opFrameAt ? Date.now() - opFrameAt : 0;
  const names = running.map((j) => j.kind).join(', ');
  let text = names + ' — выполняется ' + fmtElapsed(opRunningMs + sinceFrame);
  if (queued > 0) text += ' · в очереди ещё ' + queued;

  if (sinceFrame > staleFrameMs) {
    // No keepalive: what the server is doing is unknown from here. Say exactly that
    // rather than reassuring the user about a process we have lost contact with.
    text += ' · нет ответа от сервера ' + fmtElapsed(sinceFrame) + ' — состояние неизвестно';
  } else {
    const quiet = opQuietMs + sinceFrame;
    // The loops emit a heartbeat every few seconds, so silence this long is one slow
    // call (a wide Explorer scan, a stalled API request) rather than skipped work.
    // Report the fact; the user can wait or cancel.
    if (quiet > 15000) {
      text += ' · без новых строк ' + fmtElapsed(quiet) + ' — сервер отвечает, ждём медленный запрос';
    }
  }
  $('op-name').textContent = text + '…';
}

// jobRow renders one job as a clickable row: state, kind, and its latest line.
function jobRow(j) {
  const li = el('li', 'job job-' + j.state);
  if (selectedJob === j.id) li.classList.add('sel');
  li.dataset.jobId = String(j.id);

  li.append(el('span', 'job-state', STATE_LABEL[j.state] || j.state));
  li.append(el('span', 'job-kind', j.kind));
  if (j.state === 'running' && j.runningMs) {
    li.append(el('span', 'job-time', fmtElapsed(j.runningMs)));
  }
  const tail = j.error || j.result || j.lastLine || '';
  if (tail) li.append(el('span', j.error ? 'job-tail off' : 'job-tail', tail));
  if (j.attempts > 1) li.append(el('span', 'job-tail', 'попытка ' + j.attempts));

  li.addEventListener('click', () => {
    // Clicking the pinned row unpins it, so the panel goes back to following the
    // running job instead of being stuck on a finished one.
    selectedJob = selectedJob === j.id ? null : j.id;
    logSeq = 0;
    logLines = [];
    loadJobLog();
    renderOps();
  });
  return li;
}

// followedJob is the job whose log the panel shows: the pinned one, else the first
// running one, else the most recent.
function followedJob() {
  const o = state && state.ops;
  if (!o) return null;
  const all = (o.active || []).concat(o.recent || []);
  if (selectedJob) return all.find((j) => j.id === selectedJob) || null;
  return (o.active || []).find((j) => j.state === 'running') || all[0] || null;
}

// loadJobLog fetches the lines after what is already shown. Incremental by design:
// a long sync's log runs to thousands of lines, and refetching it every second
// would be the heaviest thing the page does.
function loadJobLog() {
  const job = followedJob();
  if (!job || logPending) return;
  logPending = true;
  fetch('/api/jobs/log?id=' + job.id + '&after=' + logSeq)
    .then((r) => (r.ok ? r.json() : null))
    .then((data) => {
      logPending = false;
      if (!data || !data.lines) return;
      for (const l of data.lines) {
        logLines.push(l);
        logSeq = l.seq;
      }
      // Bounded in the page, unbounded in the database: scrolling back further is a
      // request away, but the DOM stays small.
      if (logLines.length > 600) logLines = logLines.slice(-600);
      renderJobLog();
    })
    .catch(() => { logPending = false; });
}

function renderJobLog() {
  const box = $('op-log');
  const level = $('job-level') ? $('job-level').value : '';
  const keep = (l) => {
    if (level === 'warn') return l.level === 'warn' || l.level === 'error';
    if (level === 'error') return l.level === 'error';
    return true;
  };
  const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
  box.textContent = '';
  let lastPhase = null;
  for (const l of logLines) {
    if (!keep(l)) continue;
    if (l.phase && l.phase !== lastPhase) {
      box.append(el('div', 'log-phase', '── ' + l.phase));
      lastPhase = l.phase;
    }
    box.append(el('div', 'log-line log-' + (l.level || 'info'), l.text));
  }
  if (atBottom) box.scrollTop = box.scrollHeight;
}

function renderOps() {
  const o = state.ops || {};
  const active = o.active || [];
  const recent = o.recent || [];
  const banner = $('op-banner');
  banner.hidden = !active.length && !recent.length;

  const running = active.find((j) => j.state === 'running');
  if (running) {
    opFrameAt = Date.now();
    opRunningMs = running.runningMs || 0;
    opQuietMs = running.quietMs || 0;
    if (opTicker === null) opTicker = setInterval(renderOpHeading, 1000);
  } else {
    opFrameAt = null;
    opRunningMs = 0;
    opQuietMs = 0;
    if (opTicker !== null) { clearInterval(opTicker); opTicker = null; }
  }
  renderOpHeading();

  const list = $('job-list');
  list.textContent = '';
  for (const j of active) list.append(jobRow(j));
  for (const j of recent.slice(0, 6)) list.append(jobRow(j));

  // A pinned job that has been trimmed away would leave the panel following
  // nothing at all.
  if (selectedJob && !active.concat(recent).some((j) => j.id === selectedJob)) {
    selectedJob = null;
    logSeq = 0;
    logLines = [];
  }
  loadJobLog();

  const res = $('op-result');
  const last = recent[0];
  if (res) {
    res.textContent = last ? (last.error || last.result || '—') : '—';
    res.className = last && last.error ? 'off' : 'why';
  }
  banner.querySelector('[data-action="ops/cancel"]').disabled = !active.length;
}

function renderStatus() {
  const bar = $('statusbar');
  bar.textContent = '';
  const m = state.meta;
  bar.append(el('span', null, 'bundle ' + (m.bundleVersion || '—').slice(0, 12)));
  bar.append(el('span', null, 'patch ' + (m.patchTag || '—')));
  if (m.draftReady) {
    if (m.gsiMode) {
      bar.append(el('span', m.gsiSeen ? 'ok' : 'off', m.gsiSeen ? 'GSI: подключено' : 'GSI: ждём событие'));
    } else {
      bar.append(el('span', null, 'ручной ввод'));
    }
  } else {
    bar.append(el('span', 'off', 'бандл не загружен'));
  }
  if (state.live) bar.append(el('span', null, 'clock ' + clockMMSS(state.live.clockTime)));
  const auto = autoText(state.auto);
  if (auto) bar.append(el('span', state.auto.error ? 'off' : null, auto));
  if (state.status) bar.append(el('span', 'msg', state.status));
}

// autoText describes the background collector for the status bar. The user gets
// no other sign that data keeps arriving while they do something else, so an
// idle worker still reports when it last ran rather than going silent.
function autoText(a) {
  if (!a || a.phase === 'off') return '';
  if (a.error) return 'фон: ошибка — ' + a.error;
  switch (a.phase) {
    case 'sync': return 'фон: сбор' + (a.line ? ' · ' + a.line : '');
    case 'train': return 'фон: обучение' + (a.line ? ' · ' + a.line : '');
    case 'paused': return 'фон: пауза (ручная операция)';
    case 'idle': {
      const next = a.nextInSeconds > 0 ? 'фон: следующий проход через ' + a.nextInSeconds + ' с' : 'фон: ожидание';
      return a.last ? next + ' · ' + a.last : next;
    }
    default: return '';
  }
}

// --- hero picker ------------------------------------------------------------

function renderHeroes() {
  const list = $('hero-list');
  const q = $('hero-filter').value.trim().toLowerCase();
  list.textContent = '';
  // Order comes from the bundle; the filter only removes rows, never reorders.
  state.heroes
    .filter((h) => !q || (h.localized || h.name || '').toLowerCase().includes(q))
    .forEach((h) => {
      const li = el('li');
      li.append(el('span', null, h.localized || h.name || ('Hero ' + h.id)));
      li.append(el('span', 'id', 'id ' + h.id));
      // With GSI driving the board a click cannot pick, so it does the one useful
      // thing left: show that hero's matchups — and only when the bundle actually
      // has matrices, otherwise the click would do nothing at all.
      if (!state.meta.gsiMode) li.onclick = () => apply(h.id);
      else if (state.meta.hasMatrices) li.onclick = () => openMatchups(h.id);
      list.append(li);
    });
}

// --- statistics screen ------------------------------------------------------

async function loadStats() {
  if (!state || !state.meta.canStats) return;
  const res = await fetch('/api/stats');
  if (!res.ok) return;
  stats = await res.json();
  renderStats();
}

function playerRows(players, ally) {
  const box = el('div', 'items');
  if (!players || !players.length) {
    box.append(el('div', 'why', '(нет)'));
    return box;
  }
  players.forEach((p) => {
    const row = el('div', 'item player');
    const name = p.nickname || ('account ' + p.accountId);
    const mid = el('span');
    mid.append(el('b', null, name));
    if (!p.resolved) {
      mid.append(document.createTextNode(' '));
      mid.append(el('span', 'off', 'не резолвится'));
    } else {
      // Comfort heroes, ordered by the server; shown as-is.
      const top = (p.topHeroes || []).slice(0, 5)
        .map((h) => `${h.name} (${h.games}g ${(h.winRate * 100).toFixed(0)}%)`).join(', ');
      mid.append(document.createTextNode(' '));
      mid.append(el('span', 'why', top || '(нет данных)'));
    }
    row.append(mid);
    const del = el('button', 'danger small', 'удалить');
    del.onclick = () => post('/api/stats/remove-player', { accountId: p.accountId, ally }).then(loadStats);
    row.append(del);
    box.append(row);
  });
  return box;
}

function renderStats() {
  if (!stats || !stats.stats) return;
  const s = stats.stats;
  $('stats-path').textContent = s.snapshotPath || '—';

  const enemies = $('stats-enemies');
  enemies.textContent = '';
  enemies.append(playerRows(s.enemyPlayers, false));
  if (s.layer3Drafts > 0) {
    enemies.append(el('div', 'why', `+ ${s.layer3Drafts} драфтов команды в этом ивенте (слой 3)`));
  }

  $('stats-allies').textContent = '';
  $('stats-allies').append(playerRows(s.allyPlayers, true));

  // Only overwrite the editors when the user is not typing in them.
  const pool = $('pool-input');
  if (document.activeElement !== pool) {
    pool.value = (s.heroPool || []).map((h) => h.name).join(', ');
  }
  const rules = $('rules-input');
  if (document.activeElement !== rules) rules.value = s.itemRules || '';
}

// --- matchups modal ---------------------------------------------------------

async function openMatchups(heroId) {
  if (!state.meta.hasMatrices) return;
  const res = await fetch('/api/matchups?hero=' + heroId);
  const data = await res.json();
  const body = $('matchups-body');
  body.textContent = '';
  $('matchups-title').textContent = 'Матчапы: ' + heroName(heroId);
  const empty = !data.found ||
    !((data.synergy || []).length + (data.counters || []).length + (data.counteredBy || []).length);
  if (empty) {
    body.append(el('div', 'why',
      'нет данных: матрицы synergy/counter в текущем бандле пусты — нужен Train на кэше с парами'));
  } else {
    const grid = el('div', 'grid-2');
    [['Синергия', data.synergy], ['Контрит', data.counters], ['Контрят его', data.counteredBy]]
      .forEach(([title, rows]) => {
        const col = el('div');
        col.append(el('h2', null, title));
        const box = el('div', 'items');
        (rows || []).forEach((r, i) => box.append(heroItem(i + 1, r.heroId, r.value.toFixed(4))));
        col.append(box);
        grid.append(col);
      });
    body.append(grid);
  }
  $('matchups').hidden = false;
}

// --- transport --------------------------------------------------------------

async function post(path, payload) {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload || {}),
  });
  if (res.ok) render(await res.json());
  return res.ok;
}

function apply(heroId) { post('/api/apply', { heroId }); }

// Rebuilding the hero list and every panel means creating a few hundred DOM nodes.
// Under GSI the state arrives several times a second while only the clock moves, so
// the expensive parts are rebuilt only when their input actually changed. The cheap,
// always-changing parts (status bar, operation log, header) are re-rendered every
// frame. Keys are derived from what each section reads — never from a timestamp.
let lastBoardKey = null;
let lastResultKey = null;

function render(next) {
  state = next;
  renderHeader();
  renderOps();

  const resultKey = JSON.stringify(state.result) + '|' + JSON.stringify(state.alerts);
  const resultChanged = resultKey !== lastResultKey;

  if (state.meta.draftReady) {
    const boardKey = JSON.stringify(state.board) + '|' + JSON.stringify(state.step) +
      '|' + state.meta.gsiMode + '|' + state.heroes.length;
    if (boardKey !== lastBoardKey) {
      lastBoardKey = boardKey;
      renderBoard();
      renderHeroes(); // keeps whatever the user typed in the filter
    } else if (resultChanged) {
      renderBoard(); // draft strength is drawn inside the board panel
    }
  } else {
    lastBoardKey = null;
  }

  if (resultChanged) {
    lastResultKey = resultKey;
    renderPanels();
  }

  renderStatus();
  if (next.ring) beep();
}

// beep sounds an objective alert. Browsers block audio until the user has
// interacted with the page, so a silent first alert is expected, not a bug.
function beep() {
  try {
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.frequency.value = 880;
    gain.gain.value = 0.08;
    osc.connect(gain).connect(ctx.destination);
    osc.start();
    osc.stop(ctx.currentTime + 0.18);
  } catch (_) { /* no audio available: the panel still highlights the alert */ }
}

function connect() {
  const es = new EventSource('/api/events');
  es.onmessage = (ev) => {
    const prevRunning = state && state.ops && state.ops.running;
    render(JSON.parse(ev.data));
    // An operation that just finished may have rewritten the snapshot, and a sync
    // certainly changed what the Data screen is showing.
    if (prevRunning && !(state.ops && state.ops.running)) {
      loadStats();
      if (activeTab === 'data') loadCorpus();
    }
  };
  // EventSource reconnects on its own; this only surfaces the gap in the status bar.
  es.onerror = () => {
    const bar = $('statusbar');
    if (bar.lastChild && bar.lastChild.className === 'off') return;
    bar.append(el('span', 'off', 'соединение потеряно, переподключаюсь…'));
  };
}

// --- known players ----------------------------------------------------------
//
// The local address book. Clicking a row appends the account to whichever scout
// field matches its role, so the regular five are five clicks rather than five
// profile URLs — which is the whole complaint this answers.

async function loadKnownPlayers() {
  const host = $('known-players');
  if (!host) return;
  const role = $('players-role') ? $('players-role').value : '';
  try {
    const res = await fetch('/api/players' + (role ? '?role=' + role : ''));
    if (!res.ok) {
      host.textContent = '';
      host.append(el('li', 'why', 'список недоступен: сервер ответил ' + res.status));
      return;
    }
    const data = await res.json();
    renderKnownPlayers(data.players || [], data.error);
  } catch (e) {
    host.textContent = '';
    host.append(el('li', 'why', 'список недоступен: ' + e));
  }
}

function renderKnownPlayers(players, error) {
  const host = $('known-players');
  host.textContent = '';
  if (error) {
    host.append(el('li', 'why', error));
    return;
  }
  if (!players.length) {
    host.append(el('li', 'why',
      'пока пусто — после первой разведки игроки появятся здесь автоматически'));
    return;
  }
  for (const p of players) {
    const li = el('li', 'known');
    if (p.pinned) li.classList.add('pinned');

    const name = el('span', 'known-name', p.persona || String(p.accountId));
    li.append(name);
    li.append(el('span', 'known-role', p.role === 'ally' ? 'союзник' : 'враг'));
    if (p.heroes) li.append(el('span', 'known-cached', 'в кэше: ' + p.heroes + ' героев'));
    if (p.lastSeen) li.append(el('span', 'known-seen', ago(p.lastSeen)));

    // Clicking the row fills the field; the buttons are separate so a stray click
    // never deletes anybody.
    li.addEventListener('click', () => addToScoutField(p));

    const pin = el('button', 'small', p.pinned ? 'открепить' : 'закрепить');
    pin.title = 'Закреплённые не удаляются автоматической чисткой';
    pin.onclick = async (e) => {
      e.stopPropagation();
      await post('/api/players/pin', { accountId: p.accountId, pinned: !p.pinned });
      loadKnownPlayers();
    };
    li.append(pin);

    const forget = el('button', 'small danger', 'забыть');
    forget.title = 'Убрать из списка и удалить кэш по этому игроку';
    forget.onclick = async (e) => {
      e.stopPropagation();
      await post('/api/players/forget', { accountId: p.accountId });
      loadKnownPlayers();
    };
    li.append(forget);

    host.append(li);
  }
}

// addToScoutField appends an account to the enemy or ally input, without
// duplicating one that is already there.
function addToScoutField(p) {
  const field = $(p.role === 'ally' ? 'allies-players' : 'scout-players');
  if (!field) return;
  const parts = field.value.split(/[\s,]+/).filter(Boolean);
  const id = String(p.accountId);
  if (!parts.includes(id)) parts.push(id);
  field.value = parts.join(', ');
  field.focus();
}

// ago renders a unix timestamp as a coarse "how long ago", which is all this list
// needs — the exact minute someone was scouted is not a decision input.
function ago(unix) {
  const days = Math.floor((Date.now() / 1000 - unix) / 86400);
  if (days <= 0) return 'сегодня';
  if (days === 1) return 'вчера';
  if (days < 30) return days + ' дн. назад';
  const months = Math.floor(days / 30);
  return months + ' мес. назад';
}

// --- tabs -------------------------------------------------------------------

function showTab(name) {
  activeTab = name;
  ['draft', 'data', 'scout', 'stats'].forEach((t) => {
    $('screen-' + t).hidden = t !== name;
  });
  document.querySelectorAll('#tabs .tab').forEach((t) => {
    t.classList.toggle('active', t.dataset.tab === name);
  });
  if (name === 'stats') loadStats();
  if (name === 'scout') loadKnownPlayers();
  // The corpus aggregates are half a dozen table scans, so they are fetched when
  // the screen is actually looked at rather than on every state poll.
  if (name === 'data') loadCorpus();
}

// --- wiring -----------------------------------------------------------------

// ACTIONS maps a data-action to the request it fires. Anything not listed here is
// posted to /api/<action> with no body.
const ACTIONS = {
  'enemy': () => ['/api/enemy', { url: takeValue('enemy-url') }],
  'ops/scout': () => ['/api/ops/scout', { players: $('scout-players').value }],
  'ops/scout-allies': () => ['/api/ops/scout-allies', { players: $('allies-players').value }],
  'stats/hero-pool': () => ['/api/stats/hero-pool', { tokens: $('pool-input').value }],
  'stats/item-rules': () => ['/api/stats/item-rules', { text: $('rules-input').value }],
};

function takeValue(id) {
  const input = $(id);
  const v = input.value;
  input.value = '';
  return v;
}

document.addEventListener('DOMContentLoaded', () => {
  document.querySelectorAll('#tabs .tab').forEach((t) => {
    t.onclick = () => showTab(t.dataset.tab);
  });

  document.querySelectorAll('[data-action]').forEach((btn) => {
    btn.onclick = async () => {
      const a = btn.dataset.action;
      if (a === 'stats-refresh') { loadStats(); return; }
      if (a === 'players-refresh') { loadKnownPlayers(); return; }
      if (a === 'corpus-refresh') { loadCorpus(); return; }
      const build = ACTIONS[a];
      const [path, payload] = build ? build() : ['/api/' + a, {}];
      await post(path, payload);
      // Snapshot-editing endpoints change what the Statistics screen shows.
      if (a.startsWith('stats/')) loadStats();
    };
  });

  $('hero-filter').oninput = () => renderHeroes();
  $('hero-filter').onkeydown = (e) => {
    // Enter picks the first visible hero, mirroring the TUI's filter+enter flow.
    if (e.key !== 'Enter') return;
    const first = document.querySelector('#hero-list li');
    if (first) first.click();
  };

  // The level filter re-renders what is already loaded rather than refetching: the
  // lines are in memory, and the server has no idea what the user is filtering to.
  const level = $('job-level');
  if (level) level.onchange = () => renderJobLog();

  const playersRole = $('players-role');
  if (playersRole) playersRole.onchange = () => loadKnownPlayers();

  document.querySelector('[data-close]').onclick = () => { $('matchups').hidden = true; };
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') $('matchups').hidden = true;
  });

  connect();
});
