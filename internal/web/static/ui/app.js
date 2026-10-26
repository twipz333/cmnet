// cmnet draft screen. Renders whatever the Go core sends over /api/events; it
// computes nothing about the draft itself.

const ROLE = { 1: 'Керри', 2: 'Мид', 3: 'Оффлейн', 4: 'Четвёрка', 5: 'Пятёрка' };
const ROLE_LOWER = { 1: 'керри', 2: 'мид', 3: 'оффлейн', 4: 'четвёрка', 5: 'пятёрка' };
const ROLE_OF = { 1: 'керри', 2: 'мида', 3: 'оффлейна', 4: 'четвёрки', 5: 'пятёрки' };

const $ = (id) => document.getElementById(id);
const el = (tag, cls, text) => {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text != null) n.textContent = text;
  return n;
};

let state = null;
let catalog = [];          // [{id, name, localized, roles: [pos…]}]
let catalogFor = '';       // bundle version the catalog was loaded for
let roleFilter = 0;        // 0 = all
let lastStatus = '';
let armed = '';            // a destructive action waiting for its second click
let busy = false;          // a draft action is in flight; further clicks are ignored
let prevStrength = null;   // our win chance before the last change on the board
let strengthDelta = null;  // { value, until }
let seenFilled = new Set();// sequence steps already drawn filled (to animate only new ones)

// ---------- core I/O ----------

async function post(path, body) {
  try {
    const res = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (res.ok) setState(await res.json());
    else toast(await res.text());
  } catch {
    toast('Ядро не отвечает. Проверьте, что cmnet запущен.');
  }
}

// draftAction is a post that changes the board. One at a time: a double click
// on a card must not record the same choice for the next step as well.
async function draftAction(path, body) {
  if (busy) return;
  busy = true;
  document.body.classList.add('busy');
  try {
    await post(path, body);
  } finally {
    busy = false;
    document.body.classList.remove('busy');
  }
}

async function loadCatalog() {
  try {
    const res = await fetch('/api/catalog');
    const data = await res.json();
    catalog = data.heroes || [];
    catalogFor = state?.meta?.bundleVersion || '';
    render();
  } catch { /* the grid falls back to the available list */ }
}

function connect() {
  const es = new EventSource('/api/events');
  es.onmessage = (ev) => setState(JSON.parse(ev.data));
  es.onerror = () => {
    $('source').textContent = 'Нет связи с ядром, переподключаюсь…';
  };
}

function setState(next) {
  const before = state?.result?.strength?.ourProb;
  const boardBefore = boardSize(state);
  state = next;
  const after = state.result?.strength?.ourProb;
  // Show how the last pick moved our chances, for a few seconds.
  if (before != null && after != null && boardSize(state) !== boardBefore && Math.abs(after - before) >= 0.005) {
    strengthDelta = { value: after - before, until: Date.now() + 6000 };
    setTimeout(() => render(), 6100);
  }
  prevStrength = after;
  if (state.meta.draftReady && state.meta.bundleVersion !== catalogFor) loadCatalog();
  if (state.status && state.status !== lastStatus) toast(humanStatus(state.status));
  lastStatus = state.status || '';
  render();
}

function boardSize(s) {
  const b = s?.board;
  return b ? b.radiantPicks.length + b.radiantBans.length + b.direPicks.length + b.direBans.length : -1;
}

// ---------- helpers ----------

const nameOf = (id) => state?.names?.[id] || catalog.find((h) => h.id === id)?.localized || `#${id}`;
const pct = (p) => `${Math.round(p * 100)}%`;
const ourSide = () => (state?.meta?.ourTeam || 'Radiant').toLowerCase();

function initials(name) {
  return name.split(/[\s-]+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase();
}

// Portrait images are checked once per hero and remembered, so a state update
// redraws an already-loaded portrait instantly instead of flashing its initials.
const imgState = new Map(); // id -> 'ok' | 'missing' | Promise
function heroImage(id) {
  if (!imgState.has(id)) {
    imgState.set(id, new Promise((resolve) => {
      const img = new Image();
      img.onload = () => { imgState.set(id, 'ok'); resolve('ok'); };
      img.onerror = () => { imgState.set(id, 'missing'); resolve('missing'); };
      img.src = `/assets/heroes/${id}.png`;
    }));
  }
  return imgState.get(id);
}

// paint sets a hero portrait as a background, with the hero's initials as the
// fallback when no image is cached on disk.
function paint(node, id, withInitials = true) {
  const st = heroImage(id);
  const apply = (s) => {
    if (s === 'ok') {
      node.style.backgroundImage = `url(/assets/heroes/${id}.png)`;
      if (withInitials) node.textContent = '';
    } else if (withInitials && !node.textContent) {
      node.textContent = initials(nameOf(id));
    }
  };
  if (typeof st === 'string') apply(st);
  else {
    if (withInitials) node.textContent = initials(nameOf(id));
    st.then(apply);
  }
  return node;
}

function portrait(id, cls = 'portrait') {
  const box = el('div', cls);
  box.setAttribute('aria-hidden', 'true');
  if (id) paint(box, id);
  return box;
}

function humanStatus(s) {
  if (/^our side → /.test(s)) return 'Мы за ' + s.split('→ ')[1];
  if (/^first pick → /.test(s)) return 'Первыми выбирают: ' + s.split('→ ')[1];
  if (s === 'new draft') return 'Доска очищена, новый драфт';
  if (/^(applied|undo|redo|lineup updated)/.test(s)) return '';
  return s;
}

let toastTimer = 0;
function toast(msg) {
  if (!msg) return;
  const t = $('toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), 3500);
}

// The core explains each recommendation as " · "-joined fragments. Turn them
// into separate plain-language lines; the number and the role are shown on the
// card on their own, so they are dropped here.
function reasons(why) {
  const out = [];
  let enemyPct = null;
  for (const part of (why || '').split(' · ')) {
    const p = part.trim();
    if (!p) continue;
    let m;
    if (/^вин ~/.test(p) || Object.values(ROLE_LOWER).includes(p)) continue;
    if ((m = p.match(/^враг ~(\d+)%/))) { enemyPct = +m[1]; continue; }
    if (p === 'контрит нас') out.push({ text: 'бьёт по нашим героям', hot: true });
    else if (p === 'синергия врага') out.push({ text: 'хорошо ложится в их драфт', hot: true });
    else if (/^дыра \d+/.test(p)) out.push({ text: 'равной замены у них нет', hot: true });
    else if (p === 'из наших') out.push({ text: 'наш комфортный герой' });
    else if ((m = p.match(/^исполнимость (\d+)%/))) out.push({ text: `сыграем его: ${m[1]}%` });
    else out.push({ text: p });
  }
  return { list: out, enemyPct };
}

// metaLine says where a hero stands in the patch meta for a position, when the
// baked per-position meta lists it.
function metaLine(heroId, pos) {
  const pm = (state.result?.positionMeta || []).find((p) => p.position === pos);
  if (!pm) return null;
  const i = pm.heroes.findIndex((h) => h.heroId === heroId);
  if (i < 0) return null;
  const wr = pm.heroes[i].winRate;
  return `№${i + 1} в мете ${ROLE_OF[pos]}` + (wr ? `, побед ${(wr * 100).toFixed(1)}%` : '');
}

// enemyWants is how likely the enemy is to pick this hero with their next pick,
// when the core's enemy prediction is about a pick.
function enemyWants(heroId) {
  const res = state.result || {};
  if (!res.enemyStep?.isPick) return 0;
  return (res.enemyPred || []).find((e) => e.heroId === heroId)?.prob || 0;
}

function openRoles(slots) {
  return (slots || []).filter((s) => !s.heroId).map((s) => s.position);
}

function playerName(list, accountId) {
  if (!accountId) return '';
  return (list || []).find((p) => p.accountId === accountId)?.name || '';
}

// ---------- render ----------

function render() {
  if (!state) return;
  const m = state.meta;
  document.body.dataset.our = ourSide();
  document.body.classList.remove('connecting');

  renderBar(m);
  renderRibbon();
  renderTeams();
  renderFocus();
  renderPicker();
}

function renderBar(m) {
  document.querySelectorAll('.side-btn').forEach((b) => {
    const on = b.dataset.side === ourSide();
    b.classList.toggle('on', on);
    b.setAttribute('aria-pressed', String(on));
  });
  const fp = $('first-pick');
  const weFirst = m.firstPick === m.ourTeam;
  fp.textContent = armed === 'first'
    ? 'Доска очистится. Нажмите ещё раз'
    : (weFirst ? 'первыми выбираем мы' : 'первыми выбирают они');
  fp.disabled = m.gsiMode;

  const src = $('source');
  src.textContent = '';
  if (m.gsiMode) {
    const s = el('span', m.gsiSeen ? 'live' : 'wait');
    s.append(el('i', 'dot'), document.createTextNode(m.gsiSeen ? 'Драфт читается из игры' : 'Жду данные из Dota'));
    src.append(s);
  }

  const st = state.result?.strength;
  $('strength').hidden = !st;
  if (st) {
    $('strength-num').textContent = pct(st.ourProb);
    $('strength-fill').style.width = `${st.ourProb * 100}%`;
    $('strength').setAttribute('aria-label', `Шанс на победу по драфту ${pct(st.ourProb)}`);
    const d = $('strength-delta');
    if (strengthDelta && Date.now() < strengthDelta.until) {
      const v = Math.round(strengthDelta.value * 100);
      d.textContent = v === 0 ? '' : (v > 0 ? `+${v}` : `−${-v}`);
      d.className = 'strength-delta ' + (v > 0 ? 'up' : 'down');
      d.title = 'Изменение после последнего хода';
    } else {
      d.textContent = '';
    }
  }

  const a = state.auto;
  const busyAuto = a && (a.phase === 'sync' || a.phase === 'train');
  $('auto').textContent = busyAuto ? (a.line || (a.phase === 'train' ? 'Переобучаю модель' : 'Собираю матчи')) : '';
  $('auto').title = busyAuto ? (a.line || '') : '';

  $('undo').disabled = !m.canUndo;
  $('redo').disabled = !m.canRedo;
  document.querySelector('.history').hidden = m.gsiMode;
}

function renderRibbon() {
  const host = $('ribbon');
  host.textContent = '';
  const seq = state.sequence || [];
  if (!seq.length) return;

  const key = el('div', 'lane-key');
  key.setAttribute('aria-hidden', 'true');
  key.append(el('div', 'k-ours', 'Мы'), el('div', 'k-theirs', 'Они'));
  host.append(key);

  let phase = null;
  let picks = 0;
  let bans = 0;
  const filledNow = new Set();
  seq.forEach((s, i) => {
    if (!phase || seq[i - 1].action !== s.action) {
      phase = el('div', 'phase');
      const steps = el('div', 'phase-steps');
      const isPick = s.action === 'Pick';
      if (isPick) picks++; else bans++;
      phase.append(steps, el('div', 'phase-name', isPick ? `Пики ${picks}` : `Баны ${bans}`));
      host.append(phase);
    }
    const isPick = s.action === 'Pick';
    const node = el('div', `step ${isPick ? 'pick' : 'ban'} ${s.ours ? 'ours' : 'theirs'}`);
    node.style.setProperty('--lane', s.ours ? 1 : 2);
    const what = `${i + 1}. ${s.ours ? 'Наш' : 'Их'} ${isPick ? 'пик' : 'бан'}`;
    if (s.heroId) {
      paint(node, s.heroId, false);
      node.title = `${what}: ${nameOf(s.heroId)}`;
      filledNow.add(i);
      if (!seenFilled.has(i) && seenFilled.size) node.classList.add('fresh');
    } else {
      node.classList.add('empty');
      node.title = what;
    }
    if (s.current) {
      node.classList.add('now');
      node.title = `${what}, сейчас`;
      node.append(el('span', 'now-tag', 'сейчас'));
    }
    node.setAttribute('role', 'img');
    node.setAttribute('aria-label', node.title);
    phase.firstChild.append(node);
  });
  seenFilled = filledNow;
}

function renderTeams() {
  const lu = state.lineup;
  const b = state.board;
  const m = state.meta;
  const ours = ourSide() === 'radiant';
  const sides = [
    { host: $('team-ours'), name: m.ourTeam, note: 'наша команда', slots: lu?.our, players: lu?.allies,
      bans: ours ? b.radiantBans : b.direBans, mine: true },
    { host: $('team-theirs'), name: ours ? 'Dire' : 'Radiant', note: 'соперник, роли угаданы по героям', slots: lu?.enemy,
      players: lu?.enemies, bans: ours ? b.direBans : b.radiantBans, mine: false },
  ];
  for (const s of sides) {
    s.host.textContent = '';
    s.host.append(el('h2', 'team-name', s.name), el('div', 'team-note', s.note));
    for (const slot of s.slots || []) {
      const row = el('div', 'seatrow');
      row.append(el('div', 'role', ROLE[slot.position]));
      const who = el('div', 'who');
      const player = playerName(s.players, slot.accountId);
      if (slot.heroId) {
        const text = el('div', 'who-text');
        text.append(el('span', 'who-hero', nameOf(slot.heroId)));
        if (player) text.append(el('span', 'who-player', player));
        who.append(portrait(slot.heroId), text);
      } else {
        row.classList.add('open');
        if (s.mine) row.classList.add('ours-open');
        const text = el('div', 'who-text');
        text.append(el('span', 'who-hero', s.mine ? 'нужен' : 'ещё нет'));
        if (player) text.append(el('span', 'who-player', player));
        who.append(text);
      }
      row.append(who);
      s.host.append(row);
    }
    const bansBox = el('div', 'bans');
    bansBox.append(el('div', 'bans-title', s.bans?.length ? `Баны: ${s.bans.length} из 7` : 'Банов пока нет'));
    if (s.bans?.length) {
      const list = el('ul', 'bans-list');
      s.bans.forEach((id) => {
        const li = el('li', 'ban');
        li.append(portrait(id, 'mini'), el('span', null, nameOf(id)));
        list.append(li);
      });
      bansBox.append(list);
    }
    s.host.append(bansBox);
  }
}

function renderFocus() {
  const m = state.meta;
  const title = $('focus-title');
  const sub = $('focus-sub');
  const cards = $('cards');
  const rows = $('aside-rows');
  cards.textContent = '';
  rows.textContent = '';
  title.className = 'focus-title';
  sub.textContent = '';

  if (!m.draftReady) {
    title.textContent = 'Модель ещё не обучена';
    sub.textContent = m.draftError || 'Советнику нужен бандл: соберите матчи и обучите модель.';
    const act = el('div', 'actions');
    if (m.canTrain) {
      const go = el('button', 'btn', 'Обучить модель');
      go.type = 'button';
      go.onclick = () => { go.disabled = true; go.textContent = 'Обучение запущено'; post('/api/ops/train'); };
      act.append(go);
    }
    const data = el('a', 'btn quiet', 'Открыть данные');
    data.href = '/classic.html#data';
    act.append(data);
    cards.append(act);
    return;
  }

  const step = state.step;
  const res = state.result || {};
  if (!step) {
    title.textContent = 'Драфт завершён';
    const st = res.strength;
    if (st) sub.innerHTML = `Шанс на победу по драфту: <b>${pct(st.ourProb)}</b>`;
    if (!m.gsiMode) {
      const again = el('button', 'btn', 'Новый драфт');
      again.type = 'button';
      again.onclick = () => draftAction('/api/new-draft');
      rows.append(again);
    }
    return;
  }

  const isPick = step.action === 'Pick';
  if (step.isOurs) {
    title.classList.add('ours');
    title.textContent = isPick ? 'Ваш пик' : 'Ваш бан';
    if (isPick) {
      const need = openRoles(state.lineup?.our);
      if (need.length) sub.innerHTML = 'Нужны: ' + need.map((p) => `<b>${ROLE_LOWER[p]}</b>`).join(', ');
    } else {
      sub.textContent = 'Убираем то, что они скорее всего возьмут и что сильнее всего ударит по нам.';
    }
    const recs = (res.recs || []).slice(0, 5);
    recs.forEach((r, i) => cards.append(recCard(r, i, isPick)));
    if (!recs.length) cards.append(el('p', 'empty-note', 'Для этого шага подходящих героев не нашлось. Выберите героя в списке ниже.'));
  } else {
    title.textContent = isPick ? 'Пик соперника' : 'Бан соперника';
    sub.textContent = m.gsiMode
      ? 'Ждём их выбор в игре. Самые вероятные варианты:'
      : 'Отметьте, кого они взяли: нажмите карточку или найдите героя внизу.';
    const preds = (res.enemyPred || []).slice(0, 5);
    preds.forEach((e, i) => cards.append(enemyCard(e, i, !m.gsiMode)));
    if (!preds.length) cards.append(el('p', 'empty-note', 'Предсказать их ход не из чего. Отметьте выбор в списке ниже.'));
  }

  if (step.isOurs) {
    const es = res.enemyStep;
    if (es && (res.enemyPred || []).length) {
      rows.append(chipRow(
        es.isPick ? 'Их следующий пик' : 'Их следующий бан',
        'вероятность',
        res.enemyPred.slice(0, 6),
      ));
    }
    if (!isPick && (res.safeToLet || []).length) {
      rows.append(chipRow('Можно не банить', 'возьмут, но нам не страшны', res.safeToLet.slice(0, 6)));
    }
    if ((res.poolAlert || []).length) {
      rows.append(chipRow('Наши герои под угрозой', 'их могут забрать следующим ходом', res.poolAlert.slice(0, 6)));
    }
  }
}

function recCard(r, i, isPick) {
  const card = el('button', 'card' + (i === 0 ? ' first' : ''));
  card.type = 'button';
  const art = portrait(r.heroId, 'card-art');
  const key = el('span', 'card-key', String(i + 1));
  key.setAttribute('aria-hidden', 'true');
  art.append(key);
  if (i === 0) art.append(el('span', 'card-best', 'лучший вариант'));
  card.append(art);
  const body = el('div', 'card-body');
  body.append(el('div', 'card-name', nameOf(r.heroId)));
  const why = reasons(r.why);
  const line = el('div', 'card-line');
  if (isPick) {
    line.append(el('span', 'card-role', r.role ? ROLE[r.role] : ''));
    const metric = el('span', 'card-metric', pct(r.score));
    metric.append(el('small', null, 'победа'));
    line.append(metric);
  } else {
    line.append(el('span', 'card-role', 'возьмут'));
    line.append(el('span', 'card-metric', why.enemyPct != null ? `${why.enemyPct}%` : ''));
  }
  body.append(line);
  if (r.player) body.append(el('div', 'card-player', `играет ${r.player}`));

  const ul = el('ul', 'card-why');
  const lines = [...why.list];
  if (isPick) {
    const wants = enemyWants(r.heroId);
    if (wants >= 0.05) lines.unshift({ text: `заберём у них: хотят с шансом ${pct(wants)}`, hot: true });
    const meta = r.role ? metaLine(r.heroId, r.role) : null;
    if (meta) lines.push({ text: meta });
  }
  lines.slice(0, 3).forEach((w) => ul.append(el('li', w.hot ? 'hot' : '', w.text)));
  body.append(ul);
  card.append(body);
  card.setAttribute('aria-label', `${i + 1}. ${nameOf(r.heroId)}` + (isPick ? `, ${r.role ? ROLE_LOWER[r.role] + ', ' : ''}победа ${pct(r.score)}` : ''));
  card.onclick = () => draftAction('/api/apply', { heroId: r.heroId });
  return card;
}

function sourceTag(e) {
  if (e.source === 'event') return 'брали в этом турнире';
  if (e.source === 'player') return 'из пула их игроков';
  return '';
}

function enemyCard(e, i, clickable) {
  const card = el(clickable ? 'button' : 'div', 'card enemy');
  if (clickable) card.type = 'button';
  const art = portrait(e.heroId, 'card-art');
  if (clickable) {
    const key = el('span', 'card-key', String(i + 1));
    key.setAttribute('aria-hidden', 'true');
    art.append(key);
  }
  card.append(art);
  const body = el('div', 'card-body');
  body.append(el('div', 'card-name', nameOf(e.heroId)));
  const line = el('div', 'card-line');
  line.append(el('span', 'card-role', 'вероятность'));
  line.append(el('span', 'card-metric', pct(e.prob)));
  body.append(line);
  const ul = el('ul', 'card-why');
  const tag = sourceTag(e);
  if (tag) ul.append(el('li', 'hot', tag));
  if (e.threat > 0.02) ul.append(el('li', 'hot', 'бьёт по нашим героям'));
  if (e.fit > 0.02) ul.append(el('li', null, 'хорошо ложится в их драфт'));
  body.append(ul);
  card.append(body);
  const bar = el('div', 'card-bar');
  bar.setAttribute('aria-hidden', 'true');
  const fill = el('div');
  fill.style.width = `${Math.min(100, e.prob * 100 * 2)}%`;
  bar.append(fill);
  card.append(bar);
  if (clickable) {
    card.setAttribute('aria-label', `${i + 1}. Они взяли ${nameOf(e.heroId)} (вероятность ${pct(e.prob)})`);
    card.onclick = () => draftAction('/api/apply', { heroId: e.heroId });
  }
  return card;
}

function chipRow(heading, note, list) {
  const box = el('section', 'chips-row');
  const h = el('h2', null, heading);
  if (note) h.append(el('span', null, note));
  box.append(h);
  const chips = el('ul', 'chips');
  list.forEach((e) => {
    const c = el('li', 'chip');
    c.append(portrait(e.heroId), el('span', 'chip-name', nameOf(e.heroId)), el('span', 'pct', pct(e.prob)));
    const tag = sourceTag(e);
    if (tag) c.append(el('span', 'tag', tag));
    chips.append(c);
  });
  box.append(chips);
  return box;
}

// ---------- picker ----------

// Typing in the wrong keyboard layout is the common mistake mid-draft; "фь" is
// "am" on a Russian layout. Matching both costs nothing.
const RU = 'йцукенгшщзхъфывапролджэячсмитьбю';
const EN = 'qwertyuiop[]asdfghjkl;\'zxcvbnm,.';
const fromRu = (s) => [...s].map((c) => { const i = RU.indexOf(c); return i >= 0 ? EN[i] : c; }).join('');

function heroList() {
  if (catalog.length) return catalog;
  return (state.heroes || []).map((h) => ({ ...h, roles: [] }));
}

function matches(h, q) {
  if (!q) return true;
  const label = (h.localized || h.name || '').toLowerCase();
  const internal = (h.name || '').toLowerCase().replace('npc_dota_hero_', '').replace(/_/g, ' ');
  const abbr = label.split(/[\s-]+/).map((w) => w[0]).join('');
  return [q, fromRu(q)].some((x) => label.includes(x) || internal.includes(x) || abbr.startsWith(x));
}

function visibleHeroes() {
  const q = $('search').value.trim().toLowerCase();
  const avail = new Set((state.heroes || []).map((h) => h.id));
  return heroList()
    .filter((h) => matches(h, q) && (!roleFilter || (h.roles || []).includes(roleFilter)))
    .map((h) => ({ ...h, taken: !avail.has(h.id) }));
}

function renderPicker() {
  const m = state.meta;
  const picker = $('picker');
  const readonly = m.gsiMode || !m.draftReady || !state.step;
  picker.classList.toggle('readonly', readonly);
  $('picker-hint').textContent = m.gsiMode
    ? 'Доска заполняется из игры'
    : (!state.step ? '' : 'Печатайте в любом месте. Enter берёт первого найденного, 1–5 выбирают карточку, Ctrl+Z отменяет');

  const rf = $('role-filter');
  if (!rf.childElementCount) {
    [[0, 'Все'], [1, ROLE[1]], [2, ROLE[2]], [3, ROLE[3]], [4, ROLE[4]], [5, ROLE[5]]].forEach(([pos, label]) => {
      const b = el('button', 'role-btn', label);
      b.type = 'button';
      b.dataset.pos = pos;
      b.onclick = () => { roleFilter = pos; renderPicker(); $('search').focus(); };
      rf.append(b);
    });
  }
  rf.querySelectorAll('.role-btn').forEach((b) => {
    const on = +b.dataset.pos === roleFilter;
    b.classList.toggle('on', on);
    b.setAttribute('aria-pressed', String(on));
  });
  if (readonly) return;

  const grid = $('grid');
  grid.textContent = '';
  const list = visibleHeroes();
  const q = $('search').value.trim();
  const firstFree = list.find((h) => !h.taken);
  if (!list.length) {
    grid.append(el('p', 'empty-note', q ? `Никого не нашли по «${q}». Esc очищает поиск.` : 'В этой роли героев нет.'));
    return;
  }
  list.forEach((h) => {
    const label = h.localized || h.name;
    const b = el('button', 'hero' + (h.taken ? ' taken' : '') + (q && h === firstFree ? ' match' : ''));
    b.type = 'button';
    b.title = h.taken ? `${label}: уже на доске` : label;
    b.disabled = h.taken;
    b.append(portrait(h.id), el('span', null, label));
    if (!h.taken) b.onclick = () => applyHero(h.id);
    grid.append(b);
  });
}

function applyHero(id) {
  $('search').value = '';
  draftAction('/api/apply', { heroId: id });
}

// ---------- wiring ----------

document.querySelectorAll('.side-btn').forEach((b) => {
  b.onclick = () => post('/api/side', { team: b.dataset.side });
});

$('first-pick').onclick = () => {
  const empty = boardSize(state) === 0;
  if (!empty && armed !== 'first') {
    armed = 'first';
    render();
    setTimeout(() => { if (armed === 'first') { armed = ''; render(); } }, 3000);
    return;
  }
  armed = '';
  draftAction('/api/first-pick');
};

$('undo').onclick = () => draftAction('/api/undo');
$('redo').onclick = () => draftAction('/api/redo');

const menu = $('menu');
const menuBtn = $('menu-btn');
const closeMenu = () => { menu.hidden = true; menuBtn.setAttribute('aria-expanded', 'false'); };
menuBtn.onclick = (e) => {
  e.stopPropagation();
  menu.hidden = !menu.hidden;
  menuBtn.setAttribute('aria-expanded', String(!menu.hidden));
  if (!menu.hidden) menu.querySelector('button, a')?.focus();
};
document.addEventListener('click', (e) => {
  if (!menu.hidden && !menu.contains(e.target)) closeMenu();
});
menu.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') { closeMenu(); menuBtn.focus(); }
});
menu.querySelector('[data-cmd="new"]').onclick = (e) => {
  const btn = e.currentTarget;
  if (btn.dataset.armed !== '1') {
    btn.dataset.armed = '1';
    btn.textContent = 'Очистить доску? Нажмите ещё раз';
    setTimeout(() => { btn.dataset.armed = ''; btn.textContent = 'Новый драфт'; }, 3000);
    e.stopPropagation();
    return;
  }
  btn.dataset.armed = '';
  btn.textContent = 'Новый драфт';
  closeMenu();
  draftAction('/api/new-draft');
};

const search = $('search');
search.addEventListener('input', renderPicker);
search.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    const first = visibleHeroes().find((h) => !h.taken);
    if (first) applyHero(first.id);
    e.preventDefault();
  } else if (e.key === 'Escape') {
    search.value = '';
    renderPicker();
  }
});

document.addEventListener('keydown', (e) => {
  if (!state) return;
  const inInput = e.target instanceof HTMLInputElement;
  const mod = e.ctrlKey || e.metaKey;
  if (mod && e.key.toLowerCase() === 'z') {
    e.preventDefault();
    draftAction(e.shiftKey ? '/api/redo' : '/api/undo');
    return;
  }
  if (mod && e.key.toLowerCase() === 'y') { e.preventDefault(); draftAction('/api/redo'); return; }
  if (mod || e.altKey || inInput) return;
  if (/^[1-5]$/.test(e.key)) {
    const card = $('cards').querySelectorAll('button.card')[+e.key - 1];
    if (card) { card.click(); e.preventDefault(); }
    return;
  }
  // Any letter starts a search, wherever focus is. The key is written into the
  // field by hand: moving focus mid-keystroke does not carry the character over.
  if (e.key.length === 1 && /\S/.test(e.key) && !$('picker').classList.contains('readonly')) {
    e.preventDefault();
    search.focus();
    search.value += e.key;
    renderPicker();
  }
});

connect();
