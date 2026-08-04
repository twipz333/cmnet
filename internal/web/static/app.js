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
// Clocks are stored as ABSOLUTE local marks, not as "elapsed as of the last
// frame". The delta form had a bug with teeth: any repaint that was not caused by
// a fresh frame — clicking a row, changing the level filter — reset the base to
// "now" while keeping the elapsed value from the previous frame, so the timer
// jumped backwards by however long it had been since that frame.
//
// With a start instant, a repaint reads the same number the tick does, and only a
// genuinely new frame moves anything.
let lastFrameAt = null;       // when a frame last reached this page
const jobClocks = new Map();  // job id -> { startedAt, lastLineAt, seq }
let opTicker = null;

// staleFrameMs is when a missing keepalive stops being scheduling jitter. The
// server aims for one every 3s.
const staleFrameMs = 10000;

// syncJobClocks folds a freshly arrived frame into the local clocks.
//
// Called from render() alone: the server's runningMs/quietMs are measured at the
// moment the frame was built, so converting them to a local instant is only valid
// right then. Doing it on every repaint is what made the timer jump.
function syncJobClocks(ops) {
  const now = Date.now();
  const seen = new Set();
  for (const j of (ops && ops.active) || []) {
    seen.add(j.id);
    const prev = jobClocks.get(j.id);
    // The start instant is fixed for the life of the job, so it is computed once.
    // Recomputing it per frame would make the display jitter by the network delay.
    const startedAt = prev ? prev.startedAt : now - (j.runningMs || 0);
    // The quiet clock DOES move: it restarts whenever a new line lands, which the
    // sequence number is how we detect.
    let lastLineAt = prev ? prev.lastLineAt : now - (j.quietMs || 0);
    if (!prev || j.seq !== prev.seq) lastLineAt = now - (j.quietMs || 0);
    jobClocks.set(j.id, { startedAt, lastLineAt, seq: j.seq });
  }
  // Finished jobs stop ticking; keeping their entries would leak one per job over
  // a long session.
  for (const id of Array.from(jobClocks.keys())) {
    if (!seen.has(id)) jobClocks.delete(id);
  }
}

// runningFor returns how long a job has been running, by the local clock.
function runningFor(job) {
  const c = jobClocks.get(job.id);
  if (!c) return job.runningMs || 0;
  return Date.now() - c.startedAt;
}

// quietFor returns how long a job has been silent, by the local clock.
function quietFor(job) {
  const c = jobClocks.get(job.id);
  if (!c) return job.quietMs || 0;
  return Date.now() - c.lastLineAt;
}

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
  const sinceFrame = lastFrameAt ? Date.now() - lastFrameAt : 0;
  // The longest-running job is the one the headline is about; the rest are named
  // after it. Picking arbitrarily made the headline swap between two jobs on
  // every frame.
  const lead = running.slice().sort((a, b) => runningFor(b) - runningFor(a))[0];
  const others = running.filter((j) => j.id !== lead.id).map((j) => j.kind);

  let text = KIND_LABEL[lead.kind] || lead.kind;
  const phase = lead.phase ? ', ' + lead.phase : '';
  text += phase + ' — ' + fmtElapsed(runningFor(lead));
  if (others.length) text += ' · параллельно: ' + others.join(', ');
  if (queued > 0) text += ' · в очереди ещё ' + queued;

  if (sinceFrame > staleFrameMs) {
    // No keepalive: what the server is doing is unknown from here. Say exactly that
    // rather than reassuring the user about a process we have lost contact with.
    text += ' · нет ответа от сервера ' + fmtElapsed(sinceFrame) + ' — состояние неизвестно';
  } else {
    const quiet = quietFor(lead);
    // The loops emit a heartbeat every few seconds, so silence this long is one slow
    // call (a wide Explorer scan, a stalled API request) rather than skipped work.
    // Report the fact, and name who we are waiting on when the server knows.
    if (quiet > 15000) {
      text += ' · тишина ' + fmtElapsed(quiet) + ' — ждём ответа'
        + (lead.waitingOn ? ' от ' + lead.waitingOn : '');
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
  li.append(el('span', 'job-kind', KIND_LABEL[j.kind] || j.kind));
  if (j.state === 'running') {
    // Same clock as the headline, so the two cannot disagree.
    const t = el('span', 'job-time', fmtElapsed(runningFor(j)));
    t.dataset.jobTime = String(j.id);
    li.append(t);
  }
  // The phase says WHAT the job is doing right now — "which of the five things a
  // sync does am I looking at" was not answerable from a kind alone.
  if (j.phase) li.append(el('span', 'job-phase', j.phase));
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

// KIND_LABEL names a job kind in the UI. The wire keeps the English identifiers,
// which are also what the queue stores.
const KIND_LABEL = {
  'sync': 'сбор матчей',
  'train': 'обучение',
  'scout': 'разведка врагов',
  'scout-allies': 'разведка союзников',
  'backfill': 'дозагрузка статы',
  'assets': 'картинки',
};

// tickBanner advances every visible clock once a second without touching the
// state — no refetch, no re-layout of the list.
function tickBanner() {
  renderOpHeading();
  const active = (state && state.ops && state.ops.active) || [];
  const byID = new Map(active.map((j) => [String(j.id), j]));
  document.querySelectorAll('[data-job-time]').forEach((node) => {
    const j = byID.get(node.dataset.jobTime);
    if (j) node.textContent = fmtElapsed(runningFor(j));
  });
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

  // The ticker is the only thing started here. The clocks themselves are set in
  // render(), so repainting after a click cannot disturb them.
  const running = active.find((j) => j.state === 'running');
  if (running) {
    if (opTicker === null) opTicker = setInterval(tickBanner, 1000);
  } else if (opTicker !== null) {
    clearInterval(opTicker);
    opTicker = null;
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
    const frame = JSON.parse(ev.data);
    // A frame arrived: this is the only moment the server's elapsed values line
    // up with the local clock, so it is the only moment they are converted.
    lastFrameAt = Date.now();
    syncJobClocks(frame.ops);
    render(frame);
    // An operation that just finished may have rewritten the snapshot, and a sync
    // certainly changed what the Data screen is showing.
    if (prevRunning && !(state.ops && state.ops.running)) {
      loadStats();
      if (activeTab === 'data') { loadCorpus(); loadCorpusMeta(); }
    }
  };
  // EventSource reconnects on its own; this only surfaces the gap in the status bar.
  es.onerror = () => {
    const bar = $('statusbar');
    if (bar.lastChild && bar.lastChild.className === 'off') return;
    bar.append(el('span', 'off', 'соединение потеряно, переподключаюсь…'));
  };
}

// --- startup messages -------------------------------------------------------
//
// Loaded once. These are the lines the binary printed before any job existed —
// "background collection is off", "could not install the GSI config" — which went
// to stderr only, where a browser user never saw them. They are usually the answer
// to "why is nothing happening".

async function loadStartup() {
  const host = $('startup');
  if (!host) return;
  try {
    const res = await fetch('/api/startup');
    if (!res.ok) return;
    const data = await res.json();
    const msgs = (data.messages || []).filter((m) => m.level !== 'info');
    host.textContent = '';
    host.hidden = !msgs.length;
    for (const m of msgs) {
      host.append(el('div', 'log-line log-' + (m.level || 'info'), m.text));
    }
  } catch (_) { /* the page works without them */ }
}

// --- sources ----------------------------------------------------------------
//
// "Who is not responding" needs an actual answer. A quiet job could be a slow
// STRATZ, an OpenDota that is rate-limiting us, or a genuinely hung request, and
// from the page all three looked the same: nothing happening.

let sourcesTimer = null;

function startSourcesPolling() {
  if (sourcesTimer !== null) return;
  loadSources();
  // Once a second, matching the resolution of the data: the chart is per-second
  // buckets, so a slower poll would make it advance in visible jerks. The payload
  // is a few hundred integers from a server on this machine, and the timer stops
  // the moment the screen is left.
  sourcesTimer = setInterval(() => {
    if (activeTab === 'data') loadSources(); else stopSourcesPolling();
  }, 1000);
}

function stopSourcesPolling() {
  if (sourcesTimer === null) return;
  clearInterval(sourcesTimer);
  sourcesTimer = null;
}

async function loadSources() {
  const host = $('sources');
  if (!host) return;
  try {
    const res = await fetch('/api/sources');
    if (!res.ok) return;
    renderSources((await res.json()).sources || []);
  } catch (_) { /* the rest of the screen still works */ }
}

function renderSources(list) {
  const host = $('sources');
  // Repainting drops the element the tooltip was anchored to, which would leave
  // it stranded over an empty spot until the next mouse move.
  hideTip();
  host.textContent = '';
  if (!list.length) {
    host.append(el('div', 'why', 'ни одного запроса за этот запуск'));
    return;
  }
  for (const a of list) {
    const row = el('div', 'src-row');
    row.append(el('span', 'src-name', a.source));

    // State first, because that is the question being asked.
    let stateText;
    let stateCls = 'src-state';
    if (a.waitingUntilMs > 0) {
      stateText = 'ждём ' + fmtElapsed(a.waitingUntilMs) + ' (лимит/ошибка)';
      stateCls += ' warnish';
    } else if (a.inFlight > 0) {
      stateText = 'в полёте ' + a.inFlight + ', дольше всего ' + fmtElapsed(a.oldestMs);
      if (a.oldestMs > 15000) stateCls += ' warnish';
    } else {
      stateText = 'простаивает';
    }
    row.append(el('span', stateCls, stateText));

    const counters = 'запросов ' + num(a.requests)
      + ' · ретраев ' + num(a.retries)
      + ' · 429: ' + num(a.rateLimited)
      + ' · ошибок ' + num(a.failures);
    row.append(el('span', 'src-counters', counters));

    if (a.lastErr) row.append(el('span', 'src-err off', a.lastErr));
    else if (a.lastStatus) row.append(el('span', 'src-counters', 'последний ответ: ' + a.lastStatus));

    const card = el('div', 'src-card');
    card.append(row);
    host.append(card);
    // The chart is the same data as the counters, over time: the counters say how
    // much has happened since start-up, this says whether anything is happening
    // now — which is the actual question when a job goes quiet.
    //
    // Drawn after the card is in the page: the card is narrower than the panel it
    // sits in, and a chart authored at the panel's width gets scaled down to fit.
    drawInto(card, (w) => trafficChart(a.recent, w));
    card.append(el('div', 'src-summary why', trafficSummary(a.recent)));
  }
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

// rosterSelection is who is ticked right now. Kept outside the render so a
// refresh — which happens on a timer and after every action — does not clear a
// selection the user is halfway through making.
const rosterSelection = new Set();

function renderKnownPlayers(players, error) {
  const host = $('known-players');
  host.textContent = '';
  if (error) {
    host.append(el('li', 'why', error));
    return;
  }
  if (!players.length) {
    host.append(el('li', 'why',
      'пусто — добавь игрока по id или ссылке выше, либо запусти разведку, и он появится здесь'));
    return;
  }
  // Forget ticks for rows that are no longer shown, otherwise a filter change
  // would silently scout somebody invisible.
  const shown = new Set(players.map((p) => p.accountId));
  for (const id of Array.from(rosterSelection)) {
    if (!shown.has(id)) rosterSelection.delete(id);
  }

  for (const p of players) {
    const li = el('li', 'known');
    if (p.pinned) li.classList.add('pinned');
    if (p.inSnapshot) li.classList.add('in-snapshot');

    const tick = document.createElement('input');
    tick.type = 'checkbox';
    tick.className = 'known-tick';
    tick.checked = rosterSelection.has(p.accountId);
    tick.onchange = () => {
      if (tick.checked) rosterSelection.add(p.accountId);
      else rosterSelection.delete(p.accountId);
      updateRosterTools();
    };
    li.append(tick);

    li.append(el('span', 'known-name', p.persona || String(p.accountId)));
    li.append(el('span', 'known-role', p.role === 'ally' ? 'союзник' : 'враг'));
    if (p.inSnapshot) {
      li.append(el('span', 'known-in', p.inSnapshot === 'ally' ? 'в составе' : 'в разведке'));
    }
    if (p.heroes) li.append(el('span', 'known-cached', 'в кэше: ' + p.heroes + ' героев'));
    if (p.lastSeen) li.append(el('span', 'known-seen', ago(p.lastSeen)));

    // Clicking the row toggles the tick — the whole row is a bigger target than
    // the box, and picking five people is the common action here.
    li.addEventListener('click', (e) => {
      if (e.target.tagName === 'BUTTON' || e.target.tagName === 'INPUT') return;
      tick.checked = !tick.checked;
      tick.onchange();
    });

    if (p.inSnapshot) {
      const out = el('button', 'small', 'убрать из состава');
      out.title = 'Вынуть из снапшота. В списке игрок останется.';
      out.onclick = async (e) => {
        e.stopPropagation();
        await post('/api/stats/remove-player',
          { accountId: p.accountId, ally: p.inSnapshot === 'ally' });
        loadKnownPlayers();
        loadStats();
      };
      li.append(out);
    } else {
      const into = el('button', 'small', 'в состав');
      into.title = 'Разведать этого игрока и добавить в снапшот';
      into.onclick = async (e) => {
        e.stopPropagation();
        await scoutPlayers([p.accountId], p.role === 'ally');
      };
      li.append(into);
    }

    const pin = el('button', 'small', p.pinned ? 'открепить' : 'закрепить');
    pin.title = 'Закреплённые не удаляются автоматической чисткой списка';
    pin.onclick = async (e) => {
      e.stopPropagation();
      await post('/api/players/pin', { accountId: p.accountId, pinned: !p.pinned });
      loadKnownPlayers();
    };
    li.append(pin);

    const forget = el('button', 'small danger', 'удалить');
    forget.title = 'Убрать из списка вместе с накопленной по нему статистикой';
    forget.onclick = async (e) => {
      e.stopPropagation();
      const who = p.persona || p.accountId;
      if (!window.confirm('Удалить ' + who + ' из списка? Накопленная по нему статистика тоже удалится.\n\n'
        + 'Чтобы просто вынуть из текущего состава, есть «убрать из состава».')) return;
      await post('/api/players/forget', { accountId: p.accountId });
      rosterSelection.delete(p.accountId);
      loadKnownPlayers();
    };
    li.append(forget);

    host.append(li);
  }
  updateRosterTools();
}

// updateRosterTools keeps the bulk buttons honest about how many are ticked.
function updateRosterTools() {
  const n = rosterSelection.size;
  const allies = $('btn-roster-allies');
  const enemies = $('btn-roster-enemies');
  if (!allies || !enemies) return;
  allies.disabled = n === 0;
  enemies.disabled = n === 0;
  allies.textContent = n ? 'Скаутить выбранных (' + n + ') как союзников'
    : 'Скаутить выбранных как союзников';
}

// scoutPlayers runs the scout for a set of accounts. One request, so the whole
// stack becomes a single job rather than five.
async function scoutPlayers(ids, ally) {
  if (!ids.length) return;
  await post(ally ? '/api/ops/scout-allies' : '/api/ops/scout',
    { players: ids.join(',') });
  loadKnownPlayers();
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
  if (name === 'data') { loadCorpus(); loadCorpusMeta(); startSourcesPolling(); }
  else stopSourcesPolling();
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
      if (a === 'roster-scout-allies') {
        await scoutPlayers(Array.from(rosterSelection), true);
        return;
      }
      if (a === 'roster-scout-enemies') {
        await scoutPlayers(Array.from(rosterSelection), false);
        return;
      }
      if (a === 'roster-select-none') {
        rosterSelection.clear();
        loadKnownPlayers();
        return;
      }
      if (a === 'player-add') {
        const ref = $('player-ref').value.trim();
        if (!ref) return;
        await post('/api/players/add', { ref, ally: $('player-add-role').value === 'ally' });
        $('player-ref').value = '';
        loadKnownPlayers();
        return;
      }
      if (a === 'corpus-refresh') { loadCorpus(); loadCorpusMeta(); return; }
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

  loadStartup();
  connect();
});
