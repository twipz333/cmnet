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
  // recs rendered in #primary-action — keep skip so we do not duplicate.
  { key: 'recs', skip: true },
  { key: 'enemyPred', title: 'Предикт врага', render: renderEnemyPred, secondary: true },
  { key: 'poolAlert', title: 'Наш пул под угрозой', render: renderEnemyPred, secondary: true },
  { key: 'safeToLet', title: 'Не бойся — пусть берут', render: renderSafeToLet, secondary: true },
  { key: 'positionMeta', title: 'Мета по позициям', render: renderPositionMeta, secondary: true },
  { key: 'itemAdvice', title: 'Советы по предметам', render: renderAdvice, secondary: true },
  // Rendered inside the board panel, not as a standalone panel.
  { key: 'strength', skip: true },
  { key: 'ourTurn', skip: true },
  { key: 'isPick', skip: true },
];

const PICK_SLOTS = 5;
const BAN_SLOTS = 7; // Captains Mode: 7 bans per team
const STATS_HERO_SHOW = 8;

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

function canApplyHero() {
  const m = state && state.meta;
  return !!(m && m.draftReady && !m.gsiMode && state.step);
}

const ROLE_NAMES = { 1: 'керри', 2: 'мид', 3: 'офф', 4: 'софт', 5: 'хард' };

function roleName(pos) {
  return ROLE_NAMES[pos] || (pos ? ('поз. ' + pos) : '');
}

function recNote(r) {
  let note = r.why || '';
  if (r.player) note = (note ? note + ' · ' : '') + 'для ' + r.player;
  if (r.role) note = (note ? note + ' · ' : '') + roleName(r.role);
  return note;
}

let lastLineupKey = null;

function lineupSelectFocused() {
  const ae = document.activeElement;
  if (!ae || ae.tagName !== 'SELECT') return false;
  const host = $('lineup-panel');
  return !!(host && host.contains(ae));
}

function renderLineup() {
  const host = $('lineup-panel');
  if (!host) return;
  const lu = state && state.lineup;
  if (!state || !state.meta || !state.meta.draftReady || !lu) {
    host.hidden = true;
    lastLineupKey = null;
    return;
  }
  host.hidden = false;
  // GSI pushes state ~4×/s for the clock. Rebuilding the selects every tick
  // closes an open dropdown before the captain can pick — only rebuild when
  // the lineup payload actually changed, and never while a select is focused.
  const key = JSON.stringify(lu);
  if (key === lastLineupKey) return;
  if (lineupSelectFocused()) return;
  lastLineupKey = key;
  fillLineupSide($('lineup-our'), lu.our || [], lu.allies || [], 'our');
  fillLineupSide($('lineup-enemy'), lu.enemy || [], lu.enemies || [], 'enemy');
}

function lineupHeroIds(slots) {
  const seen = new Set();
  const ids = [];
  (slots || []).forEach((s) => {
    if (s.heroId && !seen.has(s.heroId)) {
      seen.add(s.heroId);
      ids.push(s.heroId);
    }
  });
  return ids;
}

function lineupSelectBlurFlush(sel) {
  sel.addEventListener('blur', () => {
    if (lastLineupKey !== JSON.stringify(state && state.lineup)) {
      lastLineupKey = null;
      renderLineup();
    }
  });
}

function fillLineupSide(box, slots, players, side) {
  if (!box) return;
  box.textContent = '';
  const heroIds = lineupHeroIds(slots);
  slots.forEach((slot) => {
    const hasHero = !!slot.heroId;
    const row = el('div', 'lineup-slot' + (hasHero ? ' has-hero' : '') + (slot.locked ? ' locked' : ''));

    const pos = el('div', 'lineup-slot-pos');
    pos.append(el('span', 'pos-num', String(slot.position)));
    pos.append(el('span', 'pos-role', roleName(slot.position) || ''));
    row.append(pos);

    const heroBox = el('div', 'lineup-slot-hero' + (hasHero ? ' filled' : ''));
    if (hasHero) {
      const ic = heroIcon(slot.heroId);
      if (ic) heroBox.append(ic);
      else heroBox.textContent = '#' + slot.heroId;
    } else {
      heroBox.textContent = '—';
    }
    row.append(heroBox);

    const body = el('div', 'lineup-slot-body');
    const meta = el('div', 'lineup-slot-meta');
    if (hasHero) {
      meta.append(el('span', 'hero-label', heroName(slot.heroId)));
    } else {
      meta.append(el('span', 'hero-label empty', 'нет пика'));
    }
    if (side === 'enemy' && !slot.locked && (slot.accountId || hasHero)) {
      meta.append(el('span', 'lineup-tag auto', 'авто'));
    } else if (slot.locked) {
      meta.append(el('span', 'lineup-tag manual', 'вручную'));
    }
    body.append(meta);

    // Hero placement: move any picked hero onto this position (swap if needed).
    const heroSel = document.createElement('select');
    heroSel.className = 'lineup-hero-sel';
    const heroEmpty = document.createElement('option');
    heroEmpty.value = '';
    heroEmpty.textContent = heroIds.length ? 'Герой…' : 'нет пиков';
    heroEmpty.disabled = heroIds.length === 0;
    heroSel.append(heroEmpty);
    heroIds.forEach((hid) => {
      const o = document.createElement('option');
      o.value = String(hid);
      o.textContent = heroName(hid);
      if (slot.heroId && Number(slot.heroId) === Number(hid)) o.selected = true;
      heroSel.append(o);
    });
    heroSel.disabled = heroIds.length === 0;
    heroSel.onchange = () => {
      const heroId = heroSel.value ? Number(heroSel.value) : 0;
      if (!heroId || heroId === Number(slot.heroId || 0)) return;
      post('/api/lineup', {
        side,
        position: slot.position,
        heroId,
        accountId: slot.accountId || 0,
      });
    };
    lineupSelectBlurFlush(heroSel);
    body.append(heroSel);

    const sel = document.createElement('select');
    sel.className = 'lineup-player-sel';
    const empty = document.createElement('option');
    empty.value = '';
    empty.textContent = side === 'our' ? 'Игрок…' : 'Авто / игрок…';
    sel.append(empty);
    players.forEach((p) => {
      const o = document.createElement('option');
      o.value = String(p.accountId);
      o.textContent = p.name || ('игрок ' + p.accountId);
      if (slot.accountId && Number(slot.accountId) === Number(p.accountId)) o.selected = true;
      sel.append(o);
    });
    sel.onchange = () => {
      const accountId = sel.value ? Number(sel.value) : 0;
      if (!accountId) {
        post('/api/lineup', { side, position: slot.position, clear: true });
        return;
      }
      post('/api/lineup', {
        side,
        position: slot.position,
        accountId,
        heroId: slot.heroId || 0,
      });
    };
    lineupSelectBlurFlush(sel);
    body.append(sel);
    row.append(body);
    box.append(row);
  });
}


function renderRecs(recs) {
  const box = el('div', 'items');
  recs.forEach((r, i) => {
    box.append(heroItem(i + 1, r.heroId, r.score.toFixed(4), recNote(r)));
  });
  return box;
}

// Primary action strip: top recommendations as large clickable cards.
function renderPrimaryAction() {
  const host = $('primary-action');
  if (!host) return;
  host.textContent = '';
  if (!state || !state.meta.draftReady) {
    host.hidden = true;
    return;
  }
  const recs = (state.result && state.result.recs) || [];
  if (!recs.length) {
    host.hidden = true;
    return;
  }
  host.hidden = false;
  const step = state.step;
  const actionLabel = step
    ? ((step.isOurs ? 'Сейчас: ' : 'Пока ход врага — на будущее: ')
      + (step.action === 'Pick' || step.action === 'pick' ? 'ПИК' : 'БАН'))
    : 'Рекомендации';
  host.append(el('h2', null, actionLabel));
  if (canApplyHero()) {
    host.append(el('p', 'why', 'Клик по карточке — сделать этот ход. Клик по портрету в списках ниже — матчапы.'));
  } else if (state.meta.gsiMode) {
    host.append(el('p', 'why', 'GSI: доска только для чтения. Клик открывает матчапы.'));
  }

  const grid = el('div', 'rec-grid');
  recs.slice(0, 5).forEach((r, i) => {
    const card = el('button', 'rec-card' + (i === 0 ? ' rec-top' : ''));
    card.type = 'button';
    const head = el('div', 'rec-card-head');
    head.append(el('span', 'rank', String(i + 1)));
    head.append(heroIcon(r.heroId));
    head.append(el('b', null, heroName(r.heroId)));
    card.append(head);
    const note = recNote(r);
    if (note) card.append(el('div', 'why', note));
    card.append(el('div', 'val', r.score.toFixed(4)));
    card.onclick = () => {
      if (canApplyHero()) apply(r.heroId);
      else openMatchups(r.heroId);
    };
    grid.append(card);
  });
  host.append(grid);
}

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
  Object.keys(state.result).forEach((k) => {
    if (!registered.has(k)) entries.push([{ key: k, title: k, render: renderUnknown }, state.result[k]]);
  });

  const primary = [];
  const secondary = [];
  entries.forEach(([panel, value]) => {
    if (isEmpty(value) && !panel.empty) return;
    (panel.secondary ? secondary : primary).push([panel, value]);
  });

  primary.forEach(([panel, value]) => {
    const box = el('section', 'panel');
    box.append(el('h2', null, panel.title));
    box.append(isEmpty(value) ? el('div', 'why', panel.empty) : panel.render(value));
    host.append(box);
  });

  if (secondary.length) {
    const wrap = el('details', 'secondary-panels');
    wrap.open = false;
    wrap.append(el('summary', null, 'Дополнительно (' + secondary.length + ')'));
    secondary.forEach(([panel, value]) => {
      const box = el('section', 'panel');
      box.append(el('h2', null, panel.title));
      box.append(isEmpty(value) ? el('div', 'why', panel.empty) : panel.render(value));
      wrap.append(box);
    });
    host.append(wrap);
  }

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

function slotChip(id, isBan, active) {
  const chip = el('span', 'slot' + (isBan ? ' ban' : ' pick') + (id ? ' filled' : ' empty') + (active ? ' next' : ''));
  if (id) {
    chip.append(heroIcon(id));
    chip.append(el('span', 'slot-name', heroName(id)));
    chip.onclick = () => openMatchups(id);
    chip.title = heroName(id);
  } else {
    chip.append(el('span', 'slot-ph', isBan ? 'бан' : 'пик'));
  }
  return chip;
}

function sideSlots(side, cls, picks, bans, nextKind, nextOursSide) {
  const wrap = el('div', 'board-side ' + cls);
  const ours = state.meta && state.meta.ourTeam === side;
  const title = el('div', 'board-side-title');
  title.append(el('span', 'side-name', side + (ours ? ' (мы)' : '')));
  wrap.append(title);

  const pickRow = el('div', 'slot-row');
  pickRow.append(el('span', 'label', 'Пики'));
  const pickBox = el('span', 'board-heroes');
  for (let i = 0; i < PICK_SLOTS; i++) {
    const id = picks[i] || 0;
    const active = nextOursSide === side && nextKind === 'pick' && !id && i === picks.length;
    pickBox.append(slotChip(id, false, active));
  }
  pickRow.append(pickBox);
  wrap.append(pickRow);

  const banRow = el('div', 'slot-row');
  banRow.append(el('span', 'label', 'Баны'));
  const banBox = el('span', 'board-heroes');
  for (let i = 0; i < BAN_SLOTS; i++) {
    const id = bans[i] || 0;
    const active = nextOursSide === side && nextKind === 'ban' && !id && i === bans.length;
    banBox.append(slotChip(id, true, active));
  }
  banRow.append(banBox);
  wrap.append(banRow);
  return wrap;
}

function nextSlotHint() {
  if (!state.step || !state.step.isOurs) return { kind: '', side: '' };
  const act = String(state.step.action || '').toLowerCase();
  const kind = act.indexOf('pick') >= 0 ? 'pick' : 'ban';
  // Only highlight the next empty slot on OUR turn.
  return { kind: kind, side: state.step.team || '' };
}

function renderPhaseBanner() {
  const host = $('phase-banner');
  if (!host) return;
  host.textContent = '';
  if (!state || !state.meta.draftReady) {
    host.hidden = true;
    return;
  }
  if (!state.step) {
    host.hidden = false;
    host.className = 'phase-banner done';
    host.append(el('div', 'phase-main', 'Драфт завершён'));
    return;
  }
  host.hidden = false;
  const ours = state.step.isOurs;
  const act = String(state.step.action || '');
  const isPick = act.toLowerCase().indexOf('pick') >= 0;
  host.className = 'phase-banner'
    + (ours ? ' ours' : ' theirs')
    + (isPick ? ' is-pick' : ' is-ban');
  const verb = isPick ? 'ПИК' : 'БАН';
  const main = ours ? ('НАШ ' + verb) : ('ХОД ВРАГА · ' + verb);
  host.append(el('div', 'phase-main', main));
  host.append(el('div', 'phase-sub',
    (state.step.team || '') + (ours ? ' — ваш ход' : ' — ждём соперника')));
}

function renderBoard() {
  const host = $('board');
  host.textContent = '';
  const b = state.board;
  const hint = nextSlotHint();
  host.append(sideSlots('Radiant', 'radiant', b.radiantPicks || [], b.radiantBans || [], hint.kind, hint.side));
  host.append(sideSlots('Dire', 'dire', b.direPicks || [], b.direBans || [], hint.kind, hint.side));

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
    config: m.canConfig,
    alerts: m.canAlerts !== false,
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
// Banner starts collapsed; expands while something runs (user can toggle).
let opsCollapsed = true;
let opsUserToggled = false;
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

  const kindRow = el('span', 'job-kind-row');
  kindRow.append(el('span', 'job-kind', KIND_LABEL[j.kind] || j.kind));
  if (j.phase) kindRow.append(el('span', 'job-phase', j.phase));
  li.append(kindRow);

  if (j.state === 'running') {
    const tm = el('span', 'job-time', fmtElapsed(runningFor(j)));
    tm.dataset.jobTime = String(j.id);
    li.append(tm);
  } else {
    li.append(el('span', 'job-time', ''));
  }

  const tail = j.error || j.result || j.lastLine || '';
  let tailText = tail;
  if (j.attempts > 1) {
    tailText = (tailText ? tailText + ' · ' : '') + 'попытка ' + j.attempts;
  }
  if (tailText) li.append(el('span', j.error ? 'job-tail off' : 'job-tail', tailText));

  li.addEventListener('click', () => {
    selectedJob = selectedJob === j.id ? null : j.id;
    // Opening a job expands the panel so the log is visible.
    if (selectedJob !== null) {
      opsCollapsed = false;
      opsUserToggled = true;
      applyOpsCollapsed();
    }
    logSeq = 0;
    logLines = [];
    logPending = false; // allow a fresh fetch after a previous in-flight request
    renderJobLog();
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
  'rankfill': 'дозагрузка рангов',
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
  if (!job) {
    logLines = [];
    logSeq = 0;
    renderJobLog();
    return;
  }
  if (logPending) return;
  const jobId = job.id;
  const after = logSeq;
  logPending = true;
  fetch('/api/jobs/log?id=' + jobId + '&after=' + after)
    .then((r) => r.json().then((data) => ({ ok: r.ok, data })).catch(() => ({ ok: false, data: null })))
    .then(({ ok, data }) => {
      logPending = false;
      // Ignore stale responses after the user pinned another job.
      const current = followedJob();
      if (!current || current.id !== jobId) return;
      if (!ok || !data) {
        renderJobLog();
        return;
      }
      const lines = Array.isArray(data.lines) ? data.lines : [];
      for (const l of lines) {
        logLines.push(l);
        if (l.seq > logSeq) logSeq = l.seq;
      }
      if (logLines.length > 600) logLines = logLines.slice(-600);
      renderJobLog();
    })
    .catch(() => {
      logPending = false;
      renderJobLog();
    });
}

function renderJobLog() {
  const box = $('op-log');
  if (!box) return;
  const level = $('job-level') ? $('job-level').value : '';
  const keep = (l) => {
    if (level === 'warn') return l.level === 'warn' || l.level === 'error';
    if (level === 'error') return l.level === 'error';
    return true;
  };
  const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
  box.textContent = '';
  let lastPhase = null;
  let shown = 0;
  for (const l of logLines) {
    if (!keep(l)) continue;
    const text = (l && l.text != null) ? String(l.text) : '';
    if (!text && !(l && l.phase)) continue;
    shown++;
    if (l.phase && l.phase !== lastPhase) {
      box.append(el('div', 'log-phase', '── ' + l.phase));
      lastPhase = l.phase;
    }
    if (text) box.append(el('div', 'log-line log-' + (l.level || 'info'), text));
  }
  const followed = followedJob();
  if (!shown) {
    box.textContent = followed
      ? ('Нет строк лога для «' + (KIND_LABEL[followed.kind] || followed.kind) + '».')
      : 'Выберите задачу слева — здесь появится её лог.';
  }
  if (atBottom) box.scrollTop = box.scrollHeight;

  const peek = $('op-peek');
  if (peek) {
    const tail = logLines.filter(keep).map((l) => l && l.text).filter(Boolean).slice(-3);
    peek.textContent = tail.length
      ? tail.join(' · ')
      : (followed ? 'Лог пуст' : 'Лог задачи появится здесь.');
  }
}

function applyOpsCollapsed() {
  const banner = $('op-banner');
  if (!banner) return;
  banner.classList.toggle('collapsed', opsCollapsed);
  const btn = $('op-toggle');
  if (btn) {
    btn.textContent = opsCollapsed ? '▸' : '▾';
    btn.title = opsCollapsed ? 'Развернуть задачи и лог' : 'Свернуть';
  }
}

function renderOpsPills(active) {
  const host = $('op-pills');
  if (!host) return;
  host.textContent = '';
  const running = active.filter((j) => j.state === 'running');
  const pending = active.filter((j) => j.state === 'pending');
  const failed = active.filter((j) => j.state === 'failed');
  const add = (j, cls) => {
    const pill = el('span', 'op-pill ' + cls);
    pill.append(el('span', 'pill-kind', KIND_LABEL[j.kind] || j.kind));
    let meta = '';
    if (j.state === 'running') {
      meta = (j.phase ? j.phase + ' · ' : '') + fmtElapsed(runningFor(j));
    } else if (j.state === 'pending') {
      meta = 'ждёт';
    } else if (j.error) {
      meta = j.error;
    }
    if (meta) pill.append(el('span', 'pill-meta', meta));
    host.append(pill);
  };
  running.forEach((j) => add(j, 'running'));
  pending.slice(0, 3).forEach((j) => add(j, 'pending'));
  failed.forEach((j) => add(j, 'failed'));
  if (pending.length > 3) {
    host.append(el('span', 'op-pill pending', '+' + (pending.length - 3) + ' в очереди'));
  }
}

function renderOps() {
  const o = state.ops || {};
  const active = o.active || [];
  const recent = o.recent || [];
  const banner = $('op-banner');
  banner.hidden = !active.length && !recent.length;

  const running = active.filter((j) => j.state === 'running');
  const hasFailed = active.some((j) => j.state === 'failed') || recent.some((j) => j.state === 'failed');
  banner.classList.toggle('has-running', running.length > 0);
  banner.classList.toggle('has-failed', hasFailed && !running.length);

  // When nothing is queued, forget the manual toggle so the next run auto-expands.
  if (!active.length) opsUserToggled = false;

  // Auto-expand while work runs, unless the user explicitly collapsed it.
  if (!opsUserToggled) {
    opsCollapsed = running.length === 0;
  }
  applyOpsCollapsed();

  if (running.length) {
    if (opTicker === null) opTicker = setInterval(tickBanner, 1000);
  } else if (opTicker !== null) {
    clearInterval(opTicker);
    opTicker = null;
  }
  renderOpHeading();
  renderOpsPills(active);

  const listActive = $('job-list-active');
  const listRecent = $('job-list-recent');
  const labActive = $('op-active-label');
  const labRecent = $('op-recent-label');
  if (listActive) {
    listActive.textContent = '';
    active.forEach((j) => listActive.append(jobRow(j)));
  }
  if (labActive) labActive.hidden = !active.length;

  // Deduplicate consecutive identical "готово" noise (e.g. assets ×3).
  const shownRecent = [];
  for (const j of recent) {
    const prev = shownRecent[shownRecent.length - 1];
    if (prev && prev.kind === j.kind && prev.state === j.state && j.state === 'done'
        && (prev.result || '') === (j.result || '')) {
      continue;
    }
    shownRecent.push(j);
    if (shownRecent.length >= 4) break;
  }
  if (listRecent) {
    listRecent.textContent = '';
    shownRecent.forEach((j) => listRecent.append(jobRow(j)));
  }
  if (labRecent) labRecent.hidden = !shownRecent.length;

  if (selectedJob && !active.concat(recent).some((j) => j.id === selectedJob)) {
    selectedJob = null;
    logSeq = 0;
    logLines = [];
  }

  const target = $('op-log-target');
  const followed = followedJob();
  if (target) {
    target.textContent = followed
      ? ('— ' + (KIND_LABEL[followed.kind] || followed.kind))
      : '';
  }

  if (followed) loadJobLog();

  const res = $('op-result');
  const last = recent[0];
  if (res) {
    res.textContent = last ? (last.error || last.result || '—') : '—';
    res.className = last && last.error ? 'off' : 'why';
  }
  const cancel = banner.querySelector('[data-action="ops/cancel"]');
  if (cancel) cancel.disabled = !active.length;
}

function renderStatus() {
  syncTabVisibility();
  const bar = $('statusbar');
  bar.textContent = '';
  const m = state.meta;
  bar.append(el('span', null, 'bundle ' + (m.bundleVersion || '—').slice(0, 12)));
  if (m.patchTag) {
    bar.append(el('span', null, 'patch ' + m.patchTag));
  } else {
    bar.append(el('span', 'off', 'patch — (обучи модель — тег ставится при train)'));
  }
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

function sortHeroesByGames(heroes) {
  return (heroes || []).slice().sort((a, b) => {
    const ga = a.games || 0, gb = b.games || 0;
    if (gb !== ga) return gb - ga;
    return (a.id || 0) - (b.id || 0);
  });
}

function playerCard(p, ally) {
  const card = el('div', 'player-card' + (p.resolved ? '' : ' unresolved'));
  const head = el('div', 'player-card-head');
  head.append(el('b', 'player-name', p.nickname || ('account ' + p.accountId)));
  if (!p.resolved) {
    head.append(el('span', 'off', 'не резолвится'));
  }
  if (p.smurf && p.smurf.label) {
    const badge = el('span', 'smurf-badge smurf-' + (p.smurf.level || 'low'), p.smurf.label);
    if (p.smurf.reasons && p.smurf.reasons.length) {
      badge.title = p.smurf.reasons.join('\n');
    }
    head.append(badge);
  }
  const del = el('button', 'danger small', 'удалить');
  del.onclick = (ev) => {
    ev.stopPropagation();
    post('/api/stats/remove-player', { accountId: p.accountId, ally }).then(loadStats);
  };
  head.append(del);
  card.append(head);

  if (!p.resolved) {
    card.append(el('div', 'why', 'Нет публичных данных по этому аккаунту.'));
    return card;
  }

  const heroes = sortHeroesByGames(p.topHeroes);
  if (!heroes.length) {
    card.append(el('div', 'why', '(нет данных за окно скаута)'));
    return card;
  }

  const grid = el('div', 'hero-stat-grid');
  const shown = heroes.slice(0, STATS_HERO_SHOW);
  shown.forEach((h) => {
    const cell = el('div', 'hero-stat');
    cell.append(heroIcon(h.id));
    const meta = el('div', 'hero-stat-meta');
    meta.append(el('div', 'hero-stat-name', h.name || heroName(h.id)));
    const wr = ((h.winRate || 0) * 100).toFixed(0) + '%';
    let line = (h.games || 0) + ' игр · ' + wr + ' WR';
    if (typeof h.aff === 'number' && h.aff > 0) {
      line += ' · aff ' + h.aff.toFixed(2);
    }
    meta.append(el('div', 'why', line));
    cell.append(meta);
    cell.onclick = () => openMatchups(h.id);
    grid.append(cell);
  });
  card.append(grid);
  if (heroes.length > STATS_HERO_SHOW) {
    card.append(el('div', 'why', '+' + (heroes.length - STATS_HERO_SHOW) + ' героев в пуле'));
  }
  return card;
}

function playerRows(players, ally) {
  const box = el('div', 'player-cards');
  if (!players || !players.length) {
    box.append(el('div', 'why', '(нет)'));
    return box;
  }
  players.forEach((p) => box.append(playerCard(p, ally)));
  return box;
}

function renderStats() {
  if (!stats || !stats.stats) return;
  const s = stats.stats;
  let path = s.snapshotPath || '—';
  if (s.windowDays) path += ' · окно ' + s.windowDays + 'д';
  $('stats-path').textContent = path;

  const enemies = $('stats-enemies');
  enemies.textContent = '';
  enemies.append(playerRows(s.enemyPlayers, false));
  if (s.layer3Drafts > 0) {
    enemies.append(el('div', 'why', '+ ' + s.layer3Drafts + ' драфтов команды в этом ивенте (слой 3)'));
  }

  $('stats-allies').textContent = '';
  $('stats-allies').append(playerRows(s.allyPlayers, true));

  const pool = $('pool-input');
  if (document.activeElement !== pool) {
    pool.value = (s.heroPool || []).map((h) => h.name).join(', ');
  }
  const rules = $('rules-input');
  if (document.activeElement !== rules) rules.value = s.itemRules || '';
}


// --- post-draft survey (§6.1) -----------------------------------------------
//
// During the draft the captain should not fill forms. When the board is full
// (no current step) we open a one-shot questionnaire: why off-rec picks, then
// win/loss. Answers go to the same local decision log as before.
//
// "Тест: После драфта" opens the same UI with sample steps and never POSTs
// to /api/reason|/api/outcome — so you can poke the panel without polluting
// decisions.jsonl.

const SURVEY_CHIPS = [
  'не играет / не в пуле',
  'роль / позиция',
  'свой контр / синергия',
  'бан под следующий ход',
  'тиммейт захотел',
  'другое',
];

const SURVEY_TEST_CHOICES = [
  {
    seq: 0, action: 'ban', chosen: 74, chosenName: 'Invoker', chosenRank: 3,
    topHero: 11, topName: 'Shadow Fiend', topWhy: 'сильный мид в текущей мете',
  },
  {
    seq: 2, action: 'pick', chosen: 8, chosenName: 'Juggernaut', chosenRank: 0,
    topHero: 1, topName: 'Anti-Mage', topWhy: 'сейфлейн фарм + сплэш',
  },
  {
    seq: 5, action: 'pick', chosen: 26, chosenName: 'Lion', chosenRank: 2,
    topHero: 5, topName: 'Crystal Maiden', topWhy: 'аура и сейв на лайн',
  },
];

let surveyDoneKey = null;
let surveyOpenKey = null;
let surveyBusy = false;
let surveyMode = 'live'; // 'live' | 'test'

function boardFingerprint(board) {
  if (!board) return '';
  return JSON.stringify([
    board.radiantPicks || [], board.direPicks || [],
    board.radiantBans || [], board.direBans || [],
  ]);
}

function boardHasHeroes(board) {
  if (!board) return false;
  return (board.radiantPicks || []).length + (board.direPicks || []).length +
    (board.radiantBans || []).length + (board.direBans || []).length > 0;
}

async function maybeShowSurvey() {
  if (!state || !state.meta || !state.meta.logging || !state.meta.draftReady) return;
  if (state.step) return; // draft still in progress
  if (!boardHasHeroes(state.board)) return;
  const key = boardFingerprint(state.board);
  if (!key || key === surveyDoneKey || key === surveyOpenKey) return;
  if ($('survey') && !$('survey').hidden) return;

  let data;
  try {
    const res = await fetch('/api/declog/review');
    data = await res.json();
  } catch (_) {
    return;
  }
  if (!data || !data.ready || !data.logging) return;
  const draftKey = data.draftKey || key;
  if (draftKey === surveyDoneKey) return;
  // No off-rec steps → nothing to ask; win/loss comes from GSI later.
  if (!(data.choices || []).length) {
    surveyDoneKey = draftKey;
    return;
  }
  surveyMode = 'live';
  openSurvey(data, draftKey);
}

function openSurveyTest() {
  surveyMode = 'test';
  const key = 'test|' + Date.now();
  openSurvey({ choices: SURVEY_TEST_CHOICES, draftKey: key }, key);
}

function setSurveyModeUI() {
  const dock = $('survey');
  const badge = $('survey-mode-badge');
  const saveBtn = $('survey-save');
  const test = surveyMode === 'test';
  if (dock) dock.classList.toggle('test-mode', test);
  if (badge) badge.hidden = !test;
  if (saveBtn) {
    saveBtn.textContent = test ? 'Закрыть тест' : 'Сохранить причины';
  }
}

function openSurvey(data, draftKey) {
  surveyOpenKey = draftKey;
  setSurveyModeUI();
  const box = $('survey-steps');
  box.textContent = '';
  const choices = data.choices || [];
  if (surveyMode === 'test') {
    $('survey-intro').textContent =
      'ТЕСТ: три примера шагов, где отошли от #1. Чипы/текст можно понажимать — в decisions.jsonl ничего не пишется.';
  } else {
    $('survey-intro').textContent = choices.length
      ? ('Отошли от #1 на ' + choices.length + ' шаг(ах). По каждому — почему. Панель не блокирует доску; итог матча потом из GSI.')
      : 'Все ваши шаги совпали с #1 — причин нет. Итог матча запишется из GSI (win_team), когда игра закончится.';
  }

  choices.forEach((c, i) => {
    const step = el('div', 'survey-step');
    step.dataset.seq = String(c.seq);
    const act = c.action === 'ban' ? 'бан' : 'пик';
    const rank = c.chosenRank > 0 ? ('#' + c.chosenRank) : 'вне топа';
    const head = el('div', 'survey-head');
    head.append(document.createTextNode((i + 1) + '. Ход ' + (c.seq + 1) + ' · ' + act + ' · взяли '));
    const strong = el('strong', null, c.chosenName || ('Hero ' + c.chosen));
    head.append(strong);
    head.append(document.createTextNode(' (' + rank + ')'));
    step.append(head);
    if (c.topName) {
      const top = el('div', 'survey-top',
        'Совет #1 был: ' + c.topName + (c.topWhy ? (' — ' + c.topWhy) : ''));
      step.append(top);
    }
    const chips = el('div', 'survey-chips');
    SURVEY_CHIPS.forEach((label) => {
      const btn = el('button', 'survey-chip', label);
      btn.type = 'button';
      btn.onclick = () => {
        chips.querySelectorAll('.survey-chip').forEach((b) => b.classList.remove('active'));
        btn.classList.add('active');
        step.dataset.chip = label;
        if (label === 'другое') {
          const inp = step.querySelector('input');
          if (inp) inp.focus();
        }
      };
      chips.append(btn);
    });
    step.append(chips);
    const inp = document.createElement('input');
    inp.type = 'text';
    inp.placeholder = 'своя формулировка (необязательно)';
    step.append(inp);
    box.append(step);
  });

  const saveBtn = $('survey-save');
  if (saveBtn) saveBtn.hidden = choices.length === 0 && surveyMode !== 'test';
  $('survey').hidden = false;
}

function closeSurvey(markDone) {
  $('survey').hidden = true;
  if (markDone && surveyMode === 'live' && surveyOpenKey) {
    surveyDoneKey = surveyOpenKey;
  }
  surveyOpenKey = null;
  surveyMode = 'live';
  setSurveyModeUI();
}

function reasonTextFromStep(stepEl) {
  const chip = stepEl.dataset.chip || '';
  const extra = (stepEl.querySelector('input')?.value || '').trim();
  if (chip && chip !== 'другое' && extra) return chip + '; ' + extra;
  if (chip && chip !== 'другое') return chip;
  return extra;
}

async function submitSurvey(win) {
  if (surveyBusy) return;
  if (surveyMode === 'test') {
    closeSurvey(false);
    return;
  }
  surveyBusy = true;
  try {
    const steps = Array.from(document.querySelectorAll('#survey-steps .survey-step'));
    for (const step of steps) {
      const text = reasonTextFromStep(step);
      if (!text) continue;
      const seq = Number(step.dataset.seq);
      if (!(await post('/api/reason', { seq, text }))) return;
    }
    if (!(await post('/api/outcome', { win }))) return;
    closeSurvey(true);
  } finally {
    surveyBusy = false;
  }
}

async function saveSurveyReasons() {
  if (surveyBusy) return;
  if (surveyMode === 'test') {
    closeSurvey(false);
    return;
  }
  surveyBusy = true;
  try {
    const steps = Array.from(document.querySelectorAll('#survey-steps .survey-step'));
    for (const step of steps) {
      const text = reasonTextFromStep(step);
      if (!text) continue;
      const seq = Number(step.dataset.seq);
      if (!(await post('/api/reason', { seq, text }))) return;
    }
    closeSurvey(true);
  } finally {
    surveyBusy = false;
  }
}

async function dismissSurvey() {
  if (surveyBusy) return;
  if (surveyMode === 'test') {
    closeSurvey(false);
    return;
  }
  // Save whatever was filled, then close. Outcome comes from GSI win_team later.
  surveyBusy = true;
  try {
    const steps = Array.from(document.querySelectorAll('#survey-steps .survey-step'));
    for (const step of steps) {
      const text = reasonTextFromStep(step);
      if (!text) continue;
      const seq = Number(step.dataset.seq);
      await post('/api/reason', { seq, text });
    }
    closeSurvey(true);
  } finally {
    surveyBusy = false;
  }
}

function wireSurvey() {
  const sw = $('survey-skip');
  const sb = $('survey-skip-btn');
  const save = $('survey-save');
  const win = $('survey-win');
  const loss = $('survey-loss');
  const test = $('survey-test');
  if (sw) sw.onclick = () => dismissSurvey();
  if (sb) sb.onclick = () => dismissSurvey();
  if (save) save.onclick = () => saveSurveyReasons();
  if (win) win.onclick = () => submitSurvey(true);
  if (loss) loss.onclick = () => submitSurvey(false);
  if (test) test.onclick = () => openSurveyTest();
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
    const boardChanged = boardKey !== lastBoardKey;
    if (boardChanged) {
      lastBoardKey = boardKey;
      renderPhaseBanner();
      renderBoard();
      renderPrimaryAction();
      renderHeroes(); // keeps whatever the user typed in the filter
    } else if (resultChanged) {
      renderPhaseBanner();
      renderBoard(); // draft strength is drawn inside the board panel
      renderPrimaryAction();
    }
  } else {
    lastBoardKey = null;
    renderPhaseBanner();
    renderPrimaryAction();
  }

  if (resultChanged) {
    lastResultKey = resultKey;
    renderPanels();
  }

  renderStatus();
  renderLineup();
  if (next.ring) beep();
  maybeShowSurvey();
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

    const main = el('div', 'known-main');
    main.append(el('span', 'known-name', p.persona || String(p.accountId)));
    const meta = el('div', 'known-meta');
    meta.append(el('span', 'known-role', p.role === 'ally' ? 'союзник' : 'враг'));
    if (p.inSnapshot) {
      meta.append(el('span', 'known-in', p.inSnapshot === 'ally' ? 'в составе' : 'в разведке'));
    }
    if (p.heroes) meta.append(el('span', 'known-cached', 'в кэше: ' + p.heroes + ' героев'));
    if (p.lastSeen) meta.append(el('span', 'known-seen', ago(p.lastSeen)));
    main.append(meta);

    const actions = el('div', 'known-actions');
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
      actions.append(out);
    } else {
      const into = el('button', 'small', 'в состав');
      into.title = 'Разведать этого игрока и добавить в снапшот';
      into.onclick = async (e) => {
        e.stopPropagation();
        await scoutPlayers([p.accountId], p.role === 'ally');
      };
      actions.append(into);
    }

    const pin = el('button', 'small', p.pinned ? 'открепить' : 'закрепить');
    pin.title = 'Закреплённые не удаляются автоматической чисткой списка';
    pin.onclick = async (e) => {
      e.stopPropagation();
      await post('/api/players/pin', { accountId: p.accountId, pinned: !p.pinned });
      loadKnownPlayers();
    };
    actions.append(pin);

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
    actions.append(forget);

    // Clicking the row toggles the tick — the whole row is a bigger target than
    // the box, and picking five people is the common action here.
    li.addEventListener('click', (e) => {
      if (e.target.tagName === 'BUTTON' || e.target.tagName === 'INPUT') return;
      tick.checked = !tick.checked;
      tick.onchange();
    });

    li.append(tick);
    li.append(main);
    li.append(actions);
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


// --- config editor ----------------------------------------------------------

let configView = null;
let discordConfigView = null;

async function loadConfig() {
  if (!state || !state.meta.canConfig) return;
  const res = await fetch('/api/config');
  if (!res.ok) return;
  const data = await res.json();
  if (data.error) {
    $('config-status').textContent = data.error;
    return;
  }
  configView = data.config;
  renderConfig();
}

function renderConfig() {
  const v = configView;
  if (!v) return;
  let path = v.path || '—';
  if (!v.exists) path += ' (файла ещё нет — сохранится при записи)';
  $('config-path').textContent = path;
  $('config-note').textContent = v.note || '';
  $('config-status').textContent = '';
  const form = $('config-form');
  form.textContent = '';
  (v.sections || []).forEach((sec, si) => {
    const fields = (sec.fields || []).filter((f) => !String(f.key || '').startsWith('alert-'));
    if (!fields.length) return;
    const box = el('div', 'config-section panel');
    box.append(el('h3', null, sec.title || sec.name || 'Общие'));
    fields.forEach((f, fi) => {
      box.append(configFieldRow(f, si, fi));
    });
    form.append(box);
  });
}


function formatLeadHint(sec) {
  const n = Number(sec);
  if (!Number.isFinite(n)) return '';
  if (n < 0) return '= выкл';
  if (n === 0) return '= как alert-lead';
  return '= за ' + n + 'с';
}

function formatClockHint(sec) {
  const n = Number(sec);
  if (!Number.isFinite(n)) return '';
  if (n < 0) return '= без нижней границы';
  const abs = Math.abs(Math.trunc(n));
  const clock = Math.floor(abs / 60) + ':' + String(abs % 60).padStart(2, '0');
  if (n === 0) return '= 0:00 (рог; для until 0 = без верхней границы)';
  return '= ' + clock;
}

function configFieldRow(f, si, fi) {
  const row = el('div', 'config-field' + (f.set ? '' : ' unset'));
  row.dataset.si = String(si);
  row.dataset.fi = String(fi);

  const setLab = el('label');
  const setCb = document.createElement('input');
  setCb.type = 'checkbox';
  setCb.checked = !!f.set;
  setCb.title = 'Переопределить в файле (выкл = дефолт флага)';
  setCb.onchange = () => {
    f.set = setCb.checked;
    row.classList.toggle('unset', !f.set);
  };
  setLab.append(setCb);
  row.append(setLab);

  const key = el('label', 'key', f.key);
  key.title = f.key;
  row.append(key);

  const wrap = el('div', 'value-wrap');
  let input;
  if (f.type === 'bool') {
    input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = !!f.value;
    input.onchange = () => { f.value = input.checked; f.set = true; setCb.checked = true; row.classList.remove('unset'); };
  } else {
    input = document.createElement('input');
    input.type = f.secret ? 'password' : (f.type === 'int' || f.type === 'float' ? 'number' : 'text');
    if (f.type === 'float') input.step = 'any';
    if (f.type === 'int') input.step = '1';
    input.value = f.value == null ? '' : String(f.value);
    input.placeholder = f.default != null ? String(f.default) : '';
    input.autocomplete = 'off';
    input.oninput = () => {
      f.value = input.value;
      if (!f.set) { f.set = true; setCb.checked = true; row.classList.remove('unset'); }
      if (clockHint) {
        clockHint.textContent = /^alert-lead/.test(f.key)
          ? formatLeadHint(input.value)
          : formatClockHint(input.value);
      }
    };
  }
  wrap.append(input);
  let clockHint = null;
  if (/^alert-(from|until)-/.test(f.key)) {
    clockHint = el('div', 'def', formatClockHint(f.value != null ? f.value : f.default));
    wrap.append(clockHint);
  } else if (/^alert-lead/.test(f.key)) {
    clockHint = el('div', 'def', formatLeadHint(f.value != null ? f.value : f.default));
    wrap.append(clockHint);
  }
  if (f.default != null && f.default !== '') {
    wrap.append(el('div', 'def', 'дефолт: ' + f.default));
  }
  row.append(wrap);

  if (f.help) {
    row.append(el('p', 'help', f.help));
  }
  return row;
}

async function saveConfig() {
  if (!configView) return;
  const sections = (configView.sections || []).map((sec) => ({
    name: sec.name || '',
    fields: (sec.fields || []).map((f) => {
      let value = f.value;
      if (f.type === 'int' && typeof value === 'string') value = value === '' ? 0 : parseInt(value, 10);
      if (f.type === 'float' && typeof value === 'string') value = value === '' ? 0 : parseFloat(value);
      if (f.type === 'bool') value = !!value;
      return { key: f.key, set: !!f.set, value };
    }),
  }));
  $('config-status').textContent = 'сохраняю…';
  const ok = await post('/api/config/save', { sections });
  $('config-status').textContent = ok
    ? ((state && state.status) || 'сохранено')
    : 'не удалось сохранить';
  await loadConfig();
}


// --- discord.yaml editor ----------------------------------------------------

async function loadDiscordConfig() {
  const panel = $('discord-config-panel');
  const form = $('discord-config-form');
  if (!panel || !form) return;
  if (!state || !state.meta.canDiscordConfig) {
    panel.hidden = true;
    form.textContent = '';
    discordConfigView = null;
    return;
  }
  panel.hidden = false;
  const res = await fetch('/api/discord-config');
  if (!res.ok) return;
  const data = await res.json();
  if (data.error) {
    $('discord-config-status').textContent = data.error;
    return;
  }
  discordConfigView = data.config;
  renderDiscordConfig();
}

function renderDiscordConfig() {
  const v = discordConfigView;
  if (!v) return;
  let path = v.path || '—';
  if (!v.exists) path += ' (файла ещё нет — создастся при сохранении)';
  $('discord-config-path').textContent = path;
  $('discord-config-note').textContent = v.note || '';
  $('discord-config-status').textContent = '';
  const form = $('discord-config-form');
  form.textContent = '';
  (v.sections || []).forEach((sec, si) => {
    const box = el('div', 'config-section panel');
    box.append(el('h3', null, sec.title || sec.name || 'Discord'));
    (sec.fields || []).forEach((f, fi) => {
      // Always-on fields: no "override" checkbox (unlike advisor flags).
      box.append(discordFieldRow(f, si, fi));
    });
    form.append(box);
  });
}

function discordFieldRow(f, si, fi) {
  f.set = true;
  const row = el('div', 'config-field config-field-fixed');
  row.dataset.si = String(si);
  row.dataset.fi = String(fi);
  row.append(el('span', 'key', f.key));
  const wrap = el('div', 'value-wrap');
  const input = document.createElement('input');
  input.type = f.secret ? 'password' : 'text';
  input.spellcheck = false;
  input.value = f.value == null ? '' : String(f.value);
  input.placeholder = f.default != null ? String(f.default) : '';
  input.autocomplete = 'off';
  const sync = () => { f.value = input.value; f.set = true; };
  input.oninput = sync;
  input.onchange = sync;
  input.onblur = sync;
  wrap.append(input);
  if (f.default != null && f.default !== '') {
    wrap.append(el('div', 'def', 'дефолт: ' + f.default));
  }
  row.append(wrap);
  if (f.help) row.append(el('p', 'help', f.help));
  return row;
}

async function saveDiscordConfig() {
  if (!discordConfigView) return;
  // Read from DOM — paste/autofill often skips input events, so f.value can be stale.
  const form = $('discord-config-form');
  if (form) {
    form.querySelectorAll('.config-field').forEach((row) => {
      const si = Number(row.dataset.si);
      const fi = Number(row.dataset.fi);
      const input = row.querySelector('input');
      if (!input || !discordConfigView.sections[si] || !discordConfigView.sections[si].fields[fi]) return;
      discordConfigView.sections[si].fields[fi].value = input.value;
      discordConfigView.sections[si].fields[fi].set = true;
    });
  }
  const sections = (discordConfigView.sections || []).map((sec) => ({
    name: sec.name || '',
    fields: (sec.fields || []).map((f) => ({
      key: f.key,
      set: true,
      value: f.value == null ? '' : String(f.value),
    })),
  }));
  $('discord-config-status').textContent = 'сохраняю…';
  const res = await fetch('/api/discord-config/save', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sections }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok || data.ok === false) {
    $('discord-config-status').textContent = data.error || ('ошибка HTTP ' + res.status);
    return;
  }
  $('discord-config-status').textContent = data.status || 'сохранено';
  await loadDiscordConfig();
}

// --- tabs -------------------------------------------------------------------

function syncTabVisibility() {
  const alertsTab = document.getElementById('tab-alerts');
  if (alertsTab && state && state.meta) {
    alertsTab.hidden = state.meta.canAlerts === false;
  }
}

function showTab(name) {
  activeTab = name;
  ['draft', 'data', 'scout', 'stats', 'alerts', 'config'].forEach((t) => {
    $('screen-' + t).hidden = t !== name;
  });
  document.querySelectorAll('#tabs .tab').forEach((t) => {
    t.classList.toggle('active', t.dataset.tab === name);
  });
  if (name === 'stats') loadStats();
  if (name === 'scout') loadKnownPlayers();
  if (name === 'config') { loadConfig(); loadDiscordConfig(); }
  syncTabVisibility();
  if (name === 'alerts' && window.AlertsUI) AlertsUI.load();
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
  wireSurvey();
  document.querySelectorAll('#tabs .tab').forEach((t) => {
    t.onclick = () => showTab(t.dataset.tab);
  });
  const cfgRefresh = $('config-refresh');
  if (cfgRefresh) cfgRefresh.onclick = () => loadConfig();
  const cfgSave = $('config-save');
  if (cfgSave) cfgSave.onclick = () => saveConfig();
  const dRefresh = $('discord-config-refresh');
  if (dRefresh) dRefresh.onclick = () => loadDiscordConfig();
  const dSave = $('discord-config-save');
  if (dSave) dSave.onclick = () => saveDiscordConfig();

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

  const toggle = $('op-toggle');
  if (toggle) {
    toggle.onclick = () => {
      opsCollapsed = !opsCollapsed;
      opsUserToggled = true;
      applyOpsCollapsed();
      if (!opsCollapsed) loadJobLog();
    };
  }
  const opName = $('op-name');
  if (opName) {
    opName.style.cursor = 'pointer';
    opName.title = 'Клик — развернуть / свернуть';
    opName.onclick = () => { if (toggle) toggle.click(); };
  }

  const playersRole = $('players-role');
  if (playersRole) playersRole.onchange = () => loadKnownPlayers();

  document.querySelector('[data-close]').onclick = () => { $('matchups').hidden = true; };
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') $('matchups').hidden = true;
  });

  loadStartup();
  connect();
});
