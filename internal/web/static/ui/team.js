// Team builder: our five and their five as role cards, like a game lobby.
// A player is put on a position by dragging, or by clicking a free card and
// choosing from the list. The sheet lives in the core (team.json) and pins the
// seats of every draft; scouting fills in each player's heroes.

import { $, el, store, post, button, confirmable, nameOf, portrait, pct, num, initials, ROLE, jobName } from './core.js';

const SIDES = {
  our: { title: 'Наша команда', ally: true, scoutPath: '/api/ops/scout-allies', can: 'canScoutAllies' },
  enemy: { title: 'Соперники', ally: false, scoutPath: '/api/ops/scout', can: 'canScout' },
};

const ROLE_HINT = { 1: 'лёгкая линия', 2: 'центр', 3: 'сложная линия', 4: 'роумер', 5: 'полная поддержка' };

let rerender = () => {};
let profiles = {}; // id -> {persona, fetchedAt, avatar}
// Ids sent to scouting and not yet seen in the results, per side. Seating players
// one by one becomes one scout of all of them: the requests are gathered for a
// moment and the core folds them into whatever is already queued.
const inflight = { our: new Set(), enemy: new Set() };
const batch = { our: new Set(), enemy: new Set() };
const batchTimer = {};

function queueScout(side, ids) {
  const s = store.state;
  if (!s?.meta?.[SIDES[side].can]) return;
  ids.forEach((id) => { if (id && !inflight[side].has(id)) { batch[side].add(id); inflight[side].add(id); } });
  if (!batch[side].size) return;
  clearTimeout(batchTimer[side]);
  batchTimer[side + 'Armed'] = true; // until the job is in the queue, it is still ours
  batchTimer[side] = setTimeout(async () => {
    const list = [...batch[side]];
    batch[side].clear();
    await post(SIDES[side].scoutPath, { players: list.join(',') });
    batchTimer[side + 'Armed'] = false;
  }, 1200);
  rerender();
}

function nameFor(id, p, known) {
  return profiles[id]?.persona || p?.nickname || known?.persona || `id ${id}`;
}
let choosing = null; // {side, pos}: the free card whose player list is open
let picked = null; // {side, accountId}: chosen on the bench, waiting for a card
const pending = {}; // side -> {pos, before:Set}: a profile link being scouted for a seat

// ctx: {stats, players, reload()} from the scout page.
export function renderTeams(host, ctx) {
  rerender = ctx.rerender;
  profiles = ctx.profiles || {};
  const kindOf = { our: 'scout-allies', enemy: 'scout' };
  ['our', 'enemy'].forEach((side) => {
    const list = side === 'our' ? ctx.stats?.stats?.allyPlayers : ctx.stats?.stats?.enemyPlayers;
    (list || []).forEach((p) => inflight[side].delete(p.accountId));
    // Nothing of this kind left in the queue: whatever did not come back failed.
    const busy = (store.state?.ops?.active || []).some((j) => j.kind === kindOf[side]);
    if (!busy && !batch[side].size && !batchTimer[side + 'Armed']) inflight[side].clear();
  });
  host.textContent = '';
  const s = store.state;
  const st = ctx.stats?.stats;
  ['our', 'enemy'].forEach((side) => host.append(sideSection(side, s, st, ctx)));
}

// After a scout finishes: put the newly scouted player on the seat it was meant for.
export function placePending(stats) {
  const st = stats?.stats;
  if (!st) return;
  Object.entries(pending).forEach(([side, p]) => {
    const list = side === 'our' ? st.allyPlayers : st.enemyPlayers;
    const fresh = (list || []).map((x) => x.accountId).filter((id) => !p.before.has(id));
    delete pending[side];
    if (fresh.length && !seatOf(side, fresh[0])) seat(side, p.pos, fresh[0]);
  });
}

function sheet(side) {
  const t = store.state?.team;
  return (t && t[side]) || [0, 0, 0, 0, 0];
}

function seatOf(side, id) {
  const i = sheet(side).indexOf(id);
  return i >= 0 ? i + 1 : 0;
}

async function seat(side, pos, accountId) {
  choosing = null;
  picked = null;
  await post('/api/team/seat', { side, position: pos, accountId });
}

function scoutedList(side, st) {
  return (side === 'our' ? st?.allyPlayers : st?.enemyPlayers) || [];
}

function sideSection(side, s, st, ctx) {
  const cfg = SIDES[side];
  const sec = el('section', `doc-sec team-sec ${side}`);
  const head = el('div', 'sec-head');
  head.append(el('h2', null, cfg.title));
  const scouted = scoutedList(side, st);
  if (scouted.length && s.meta.canStats !== false && st) {
    head.append(confirmable(button('Очистить разведку', 'link-btn danger'), 'Убрать всех разведанных?', async () => {
      await post('/api/stats/clear-scout', { ally: cfg.ally });
      ctx.reload();
    }));
  }
  sec.append(head);

  const byId = new Map(scouted.map((p) => [p.accountId, p]));
  const book = new Map(((ctx.players && ctx.players.players) || []).map((p) => [p.accountId, p]));
  const seats = sheet(side);
  const canEdit = !!s.team;

  const acts = el('div', 'team-acts');
  const unscouted = seats.filter((id) => id && !byId.has(id) && !inflight[side].has(id));
  if (unscouted.length && s.meta[cfg.can]) {
    acts.append(button(unscouted.length === 1 ? 'Разведать игрока' : `Разведать всех (${unscouted.length})`, 'btn small', () => queueScout(side, unscouted)));
  }
  const faces = [...new Set([...seats.filter(Boolean), ...scouted.map((p) => p.accountId)])];
  if (faces.length) {
    acts.append(button('Обновить ники и аватарки', 'link-btn', () => post('/api/profiles/refresh', { ids: faces, force: true })));
  }
  if (acts.childElementCount) head.insertBefore(acts, head.children[1] || null);

  const row = el('div', 'seats');
  for (let pos = 1; pos <= 5; pos++) {
    row.append(seatCard(side, pos, seats[pos - 1], byId, book, canEdit, s, ctx));
  }
  sec.append(row);

  // Scouted but not on a position yet.
  const bench = scouted.filter((p) => !seats.includes(p.accountId));
  if (bench.length) {
    const b = el('div', 'bench');
    b.append(el('span', 'bench-cap', picked?.side === side ? 'Теперь нажмите на позицию' : 'Без позиции'));
    bench.forEach((p) => b.append(benchChip(side, p)));
    sec.append(b);
  }

  if (side === 'enemy' && st) {
    const l3 = el('div', 'layer3');
    l3.append(el('span', null, st.layer3Drafts
      ? `Их драфтов в этом турнире: ${st.layer3Drafts}. Это сильнее всего влияет на предсказание их ходов.`
      : 'Их драфтов в этом турнире пока нет. Достаточно одного открытого профиля соперника: из его матчей поднимутся драфты всей команды.'));
    if (st.layer3Drafts && s.meta.canStats !== false) {
      l3.append(confirmable(button('Забыть драфты турнира', 'link-btn danger'), 'Точно забыть?', async () => { await post('/api/stats/clear-layer3'); ctx.reload(); }));
    }
    sec.append(l3);
  }

  const jobsOf = (s.ops?.active || []).filter((j) => j.kind === (side === 'our' ? 'scout-allies' : 'scout'));
  if (jobsOf.length) {
    const run = jobsOf.find((j) => j.state === 'running') || jobsOf[0];
    const line = el('div', 'job live');
    const more = jobsOf.length - 1;
    line.append(el('b', null, jobName(run.kind) + (more > 0 ? ` · ещё в очереди ${more}` : '')),
      el('div', 'job-line', run.state === 'running' ? (run.lastLine || 'начинаю…') : 'ждёт очереди'));
    sec.append(line);
  }

  if (s.meta[cfg.can]) sec.append(addForm(side, cfg, scouted));
  return sec;
}

// avatar: the Steam picture when one is stored, initials otherwise. The file's
// time is in the URL, so a refreshed avatar replaces the browser's cached copy.
function avatar(accountId, name) {
  const a = el('div', 'avatar');
  a.textContent = name && !/^id \d+$/.test(name) ? initials(name) : '?';
  const pr = profiles[accountId];
  if (accountId && (!pr || pr.avatar)) {
    const img = new Image();
    img.alt = '';
    img.onload = () => { a.textContent = ''; a.append(img); };
    img.src = `/assets/avatars/${accountId}.jpg${pr?.avatar ? `?v=${pr.avatar}` : ''}`;
  }
  return a;
}

function seatCard(side, pos, acct, byId, book, canEdit, s, ctx) {
  const card = el('div', `seat-card${acct ? ' filled' : ''}`);
  card.dataset.pos = pos;
  const head = el('div', 'seat-head');
  head.append(el('span', 'seat-num', String(pos)), el('span', 'seat-role', ROLE[pos]));
  head.append(el('span', 'seat-hint', ROLE_HINT[pos]));
  card.append(head);

  // Drop target: a player dragged from another card or from the bench.
  if (canEdit) {
    card.ondragover = (e) => {
      if (e.dataTransfer.types.includes(`cmnet/${side}`)) { e.preventDefault(); card.classList.add('drop'); }
    };
    card.ondragleave = () => card.classList.remove('drop');
    card.ondrop = (e) => {
      card.classList.remove('drop');
      const id = Number(e.dataTransfer.getData(`cmnet/${side}`));
      if (id) { e.preventDefault(); seat(side, pos, id); }
    };
  }

  if (!acct) {
    const open = choosing && choosing.side === side && choosing.pos === pos;
    if (open) {
      card.classList.add('choosing');
      card.append(chooser(side, pos, byId, book, s, ctx));
      return card;
    }
    const empty = button('', 'seat-empty', () => {
      if (!canEdit) return;
      if (picked && picked.side === side) { seat(side, pos, picked.accountId); return; }
      choosing = { side, pos };
      rerender();
    });
    empty.append(el('span', 'seat-plus', '+'), el('span', null, picked?.side === side ? 'Поставить сюда' : 'Поставить игрока'));
    empty.setAttribute('aria-label', `${ROLE[pos]}: поставить игрока`);
    empty.disabled = !canEdit;
    card.append(empty);
    return card;
  }

  const p = byId.get(acct);
  const known = book.get(acct);
  const name = nameFor(acct, p, known);
  card.draggable = canEdit;
  card.ondragstart = (e) => { e.dataTransfer.setData(`cmnet/${side}`, String(acct)); e.dataTransfer.effectAllowed = 'move'; card.classList.add('dragging'); };
  card.ondragend = () => card.classList.remove('dragging');
  if (picked && picked.side === side && canEdit) {
    card.classList.add('target');
    card.onclick = (e) => { if (e.target.closest('button,select')) return; seat(side, pos, picked.accountId); };
  }

  const who = el('div', 'seat-who');
  who.append(avatar(acct, name));
  const nm = el('div', 'seat-name');
  nm.append(el('b', null, name));
  const tags = el('div', 'seat-tags');
  if (!p && inflight[side].has(acct)) tags.append(el('span', 'tag', 'разведка…'));
  else if (!p) tags.append(el('span', 'tag', 'не разведан'));
  else if (!p.resolved) tags.append(el('span', 'tag warn', 'профиль скрыт'));
  if (p?.smurf && p.smurf.level !== 'low') {
    const t = el('span', 'tag ' + (p.smurf.level === 'high' ? 'danger' : 'warn'), p.smurf.label || 'возможен смурф');
    t.title = (p.smurf.reasons || []).join('\n');
    tags.append(t);
  }
  nm.append(tags);
  who.append(nm);
  card.append(who);

  const heroes = el('ul', 'seat-heroes');
  (p?.topHeroes || []).slice(0, 4).forEach((h) => {
    const li = el('li');
    li.title = `${h.name || nameOf(h.id)}: ${num(h.games)} игр, побед ${pct(h.winRate)}`;
    li.append(portrait(h.id, 'seat-hero'), el('span', 'seat-games', `${h.games}`));
    heroes.append(li);
  });
  if (p?.topHeroes?.length) card.append(heroes);
  else if (!p && !inflight[side].has(acct) && s.meta[SIDES[side].can]) {
    card.append(button('Разведать', 'btn quiet small', () => queueScout(side, [acct])));
  }

  if (canEdit) {
    const act = el('div', 'seat-actions');
    const move = el('select', 'seat-move');
    move.setAttribute('aria-label', `Перевести ${name} на другую позицию`);
    const o0 = el('option', null, 'Позиция…');
    o0.value = '';
    move.append(o0);
    for (let q = 1; q <= 5; q++) {
      if (q === pos) continue;
      const o = el('option', null, `${q} · ${ROLE[q]}`);
      o.value = String(q);
      move.append(o);
    }
    move.onchange = () => { if (move.value) seat(side, Number(move.value), acct); };
    act.append(move, button('Снять', 'link-btn', () => seat(side, pos, 0)));
    card.append(act);
  }
  return card;
}

// chooser: the open free card lists who can sit here, then a profile link.
function chooser(side, pos, byId, book, s, ctx) {
  const box = el('div', 'seat-choose');
  const seats = sheet(side);
  const cands = [];
  byId.forEach((p) => { if (!seats.includes(p.accountId)) cands.push({ id: p.accountId, name: nameFor(p.accountId, p, book.get(p.accountId)), note: 'разведан' }); });
  const role = side === 'our' ? 'ally' : 'enemy';
  book.forEach((p) => {
    if (seats.includes(p.accountId) || byId.has(p.accountId)) return;
    if (p.role && p.role !== role) return;
    cands.push({ id: p.accountId, name: nameFor(p.accountId, null, p), note: p.pinned ? 'закреплён' : 'из записной книжки', pinned: p.pinned });
  });
  cands.sort((a, b) => (b.note === 'разведан') - (a.note === 'разведан') || (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0));

  const ul = el('ul', 'choose-list');
  cands.slice(0, 7).forEach((c) => {
    const li = el('li');
    const b = button('', 'choose-btn', async () => {
      await seat(side, pos, c.id);
      if (!byId.has(c.id)) queueScout(side, [c.id]);
    });
    b.append(avatar(c.id, c.name), el('span', 'choose-name', c.name), el('span', 'choose-note', c.note));
    li.append(b);
    ul.append(li);
  });
  if (!cands.length) box.append(el('p', 'muted small', 'Пока некого поставить. Вставьте ссылку на профиль.'));
  box.append(ul);

  if (s.meta[SIDES[side].can]) {
    const f = el('form', 'choose-form');
    const input = el('input');
    input.type = 'text';
    input.placeholder = 'ссылка на профиль или id';
    input.autocomplete = 'off';
    input.spellcheck = false;
    input.setAttribute('aria-label', `Профиль игрока на позицию ${ROLE[pos]}`);
    const go = el('button', 'btn small', 'Разведать');
    go.type = 'submit';
    f.append(input, go);
    f.onsubmit = async (e) => {
      e.preventDefault();
      const ref = input.value.trim();
      if (!ref) { input.focus(); return; }
      if (/^\d+$/.test(ref) && Number(ref) > 0 && Number(ref) < 2 ** 32) {
        await seat(side, pos, Number(ref)); // a bare account id: seat it now
        queueScout(side, [Number(ref)]);
        return;
      }
      pending[side] = { pos, before: new Set([...byId.keys()]) };
      choosing = null;
      post(SIDES[side].scoutPath, { players: ref });
    };
    box.append(f);
    setTimeout(() => input.focus(), 0);
  }
  box.append(button('Отмена', 'link-btn', () => { choosing = null; rerender(); }));
  return box;
}

function benchChip(side, p) {
  const chip = el('button', `bench-chip${picked?.side === side && picked.accountId === p.accountId ? ' on' : ''}`);
  chip.type = 'button';
  chip.draggable = true;
  chip.ondragstart = (e) => { e.dataTransfer.setData(`cmnet/${side}`, String(p.accountId)); e.dataTransfer.effectAllowed = 'move'; };
  const name = nameFor(p.accountId, p, null);
  chip.append(avatar(p.accountId, name), el('span', null, name));
  (p.topHeroes || []).slice(0, 3).forEach((h) => chip.append(portrait(h.id, 'bench-hero')));
  chip.setAttribute('aria-pressed', picked?.accountId === p.accountId ? 'true' : 'false');
  chip.title = 'Перетащите на позицию или нажмите, затем выберите позицию';
  chip.onclick = () => {
    picked = picked?.accountId === p.accountId ? null : { side, accountId: p.accountId };
    choosing = null;
    rerender();
  };
  return chip;
}

function addForm(side, cfg, scouted) {
  const f = el('form', 'inline-form team-add');
  const id = `team-add-${side}`;
  const lab = el('label', null, side === 'our' ? 'Добавить своего' : 'Добавить соперника');
  lab.htmlFor = id;
  const input = el('input');
  input.id = id;
  input.type = 'text';
  input.placeholder = 'ссылки на профили или id через запятую';
  input.autocomplete = 'off';
  input.spellcheck = false;
  const go = el('button', 'btn quiet', 'Разведать');
  go.type = 'submit';
  f.append(lab, input, go);
  f.onsubmit = async (e) => {
    e.preventDefault();
    if (!input.value.trim()) { input.focus(); return; }
    if (await post(cfg.scoutPath, { players: input.value })) input.value = '';
  };
  return f;
}
