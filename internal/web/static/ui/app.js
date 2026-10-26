// cmnet draft screen. Renders whatever the Go core sends over /api/events; it
// computes nothing about the draft itself.

const ROLE = { 1: 'Керри', 2: 'Мид', 3: 'Оффлейн', 4: 'Четвёрка', 5: 'Пятёрка' };
const ROLE_LOWER = { 1: 'керри', 2: 'мид', 3: 'оффлейн', 4: 'четвёрка', 5: 'пятёрка' };

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
    toast('Ядро не отвечает');
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
  es.onerror = () => $('source').textContent = 'Нет связи с ядром, переподключаюсь';
}

function setState(next) {
  state = next;
  if (state.meta.draftReady && state.meta.bundleVersion !== catalogFor) loadCatalog();
  if (state.status && state.status !== lastStatus) toast(humanStatus(state.status));
  lastStatus = state.status || '';
  render();
}

// ---------- helpers ----------

const nameOf = (id) => state?.names?.[id] || catalog.find((h) => h.id === id)?.localized || `#${id}`;
const pct = (p) => `${Math.round(p * 100)}%`;
const ourSide = () => (state?.meta?.ourTeam || 'Radiant').toLowerCase();

function initials(name) {
  return name.split(/[\s-]+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase();
}

// portrait renders a hero image with the hero's initials underneath, so a
// missing image file degrades to a readable tile rather than an empty box.
function portrait(id, cls = 'portrait') {
  const box = el('div', cls, id ? initials(nameOf(id)) : '');
  if (id) {
    const img = new Image();
    img.alt = '';
    img.loading = 'lazy';
    img.src = `/assets/heroes/${id}.png`;
    img.onload = () => {
      box.textContent = '';
      box.style.backgroundImage = `url(${img.src})`;
    };
  }
  return box;
}

function humanStatus(s) {
  if (/^our side → /.test(s)) return 'Мы за ' + s.split('→ ')[1];
  if (/^first pick → /.test(s)) return 'Первыми выбирают: ' + s.split('→ ')[1];
  if (s === 'new draft') return 'Новый драфт';
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
  toastTimer = setTimeout(() => t.classList.remove('show'), 3200);
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
    else if ((m = p.match(/^дыра (\d+)/))) out.push({ text: 'равной замены у них нет', hot: true });
    else if (p === 'из наших') out.push({ text: 'наш комфортный герой' });
    else if ((m = p.match(/^исполнимость (\d+)%/))) out.push({ text: `сыграем его: ${m[1]}%` });
    else out.push({ text: p });
  }
  return { list: out, enemyPct };
}

function openRoles(slots) {
  return (slots || []).filter((s) => !s.heroId).map((s) => s.position);
}

// ---------- render ----------

function render() {
  if (!state) return;
  const m = state.meta;
  document.body.dataset.our = ourSide();

  renderBar(m);
  renderRibbon();
  renderTeams();
  renderFocus();
  renderPicker();
}

function renderBar(m) {
  document.querySelectorAll('.side-btn').forEach((b) => {
    b.classList.toggle('on', b.dataset.side === ourSide());
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
    const s = el('span', m.gsiSeen ? 'live' : '', m.gsiSeen ? 'Драфт читается из игры' : 'Жду данные из Dota');
    src.append(s);
  }

  const st = state.result?.strength;
  $('strength').hidden = !st;
  if (st) {
    $('strength-num').textContent = pct(st.ourProb);
    $('strength-fill').style.width = `${st.ourProb * 100}%`;
  }

  const a = state.auto;
  $('auto').textContent = a && (a.phase === 'sync' || a.phase === 'train') ? (a.line || '') : '';

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
  key.append(el('div', 'k-ours', 'Мы'), el('div', 'k-theirs', 'Они'));
  host.append(key);

  let phase = null;
  let picks = 0;
  let bans = 0;
  seq.forEach((s, i) => {
    if (!phase || seq[i - 1].action !== s.action) {
      phase = el('div', 'phase');
      const steps = el('div', 'phase-steps');
      const isPick = s.action === 'Pick';
      if (isPick) picks++; else bans++;
      phase.append(steps, el('div', 'phase-name', isPick ? `Пики ${picks}` : `Баны ${bans}`));
      host.append(phase);
    }
    const node = el('div', `step ${s.action === 'Pick' ? 'pick' : 'ban'} ${s.ours ? 'ours' : 'theirs'}`);
    node.style.setProperty('--lane', s.ours ? 1 : 2);
    if (s.heroId) {
      node.title = nameOf(s.heroId);
      node.style.backgroundImage = `url(/assets/heroes/${s.heroId}.png)`;
    } else {
      node.classList.add('empty');
    }
    if (s.current) {
      node.classList.add('now');
      node.title = `Сейчас: ${s.ours ? 'наш' : 'их'} ${s.action === 'Pick' ? 'пик' : 'бан'}`;
    }
    phase.firstChild.append(node);
  });
}

function renderTeams() {
  const lu = state.lineup;
  const b = state.board;
  const m = state.meta;
  const ours = ourSide() === 'radiant';
  const sides = [
    { host: $('team-ours'), name: m.ourTeam, note: 'наша команда', slots: lu?.our, bans: ours ? b.radiantBans : b.direBans, mine: true },
    { host: $('team-theirs'), name: ours ? 'Dire' : 'Radiant', note: 'соперник, роли угаданы по героям', slots: lu?.enemy, bans: ours ? b.direBans : b.radiantBans, mine: false },
  ];
  for (const s of sides) {
    s.host.textContent = '';
    s.host.append(el('div', 'team-name', s.name), el('div', 'team-note', s.note));
    for (const slot of s.slots || []) {
      const row = el('div', 'seatrow');
      row.append(el('div', 'role', ROLE[slot.position]));
      const who = el('div', 'who');
      if (slot.heroId) {
        who.append(portrait(slot.heroId), el('span', null, nameOf(slot.heroId)));
      } else {
        row.classList.add('open');
        if (s.mine) row.classList.add('ours-open');
        who.textContent = s.mine ? 'нужен' : 'ещё нет';
      }
      row.append(who);
      s.host.append(row);
    }
    if (s.bans?.length) {
      const bans = el('div', 'bans');
      s.bans.forEach((id) => {
        const mini = el('div', 'mini');
        mini.title = nameOf(id);
        mini.style.backgroundImage = `url(/assets/heroes/${id}.png)`;
        bans.append(mini);
      });
      s.host.append(bans);
    }
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
    title.textContent = '';
    const n = el('div', 'notice');
    n.append(el('h2', null, 'Модель ещё не обучена'));
    n.append(el('p', null, m.draftError || 'Советнику нужен бандл: соберите матчи и обучите модель.'));
    const act = el('div', 'actions');
    if (m.canTrain) {
      const go = el('button', 'btn', 'Обучить модель');
      go.onclick = () => post('/api/ops/train');
      act.append(go);
    }
    const data = el('a', 'btn quiet', 'Открыть данные');
    data.href = '/classic.html#data';
    act.append(data);
    n.append(act);
    cards.append(n);
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
      again.onclick = () => post('/api/new-draft');
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
    (res.recs || []).slice(0, 5).forEach((r, i) => cards.append(recCard(r, i, isPick)));
    if (!(res.recs || []).length) cards.append(el('p', 'focus-sub', 'Нет подходящих героев для этого шага.'));
  } else {
    title.textContent = isPick ? 'Пик соперника' : 'Бан соперника';
    sub.textContent = m.gsiMode
      ? 'Ждём их выбор в игре. Самые вероятные варианты:'
      : 'Отметьте, кого они взяли: нажмите карточку или найдите героя внизу.';
    (res.enemyPred || []).slice(0, 5).forEach((e, i) => cards.append(enemyCard(e, i, !m.gsiMode)));
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
  art.append(el('span', 'card-key', String(i + 1)));
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
  if (r.player) body.append(el('div', 'card-role', `играет ${r.player}`));
  const ul = el('ul', 'card-why');
  why.list.slice(0, 3).forEach((w) => ul.append(el('li', w.hot ? 'hot' : '', w.text)));
  body.append(ul);
  card.append(body);
  card.onclick = () => post('/api/apply', { heroId: r.heroId });
  return card;
}

function sourceTag(e) {
  if (e.source === 'event') return 'брали в этом турнире';
  if (e.source === 'player') return 'из пула их игроков';
  return '';
}

function enemyCard(e, i, clickable) {
  const card = el(clickable ? 'button' : 'div', 'card');
  if (clickable) card.type = 'button';
  const art = portrait(e.heroId, 'card-art');
  if (clickable) art.append(el('span', 'card-key', String(i + 1)));
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
  body.append(ul);
  card.append(body);
  const bar = el('div', 'card-bar');
  const fill = el('div');
  fill.style.width = `${Math.min(100, e.prob * 100 * 2)}%`;
  bar.append(fill);
  card.append(bar);
  if (clickable) card.onclick = () => post('/api/apply', { heroId: e.heroId });
  return card;
}

function chipRow(heading, note, list) {
  const box = el('div', 'chips-row');
  const h = el('h2', null, heading);
  if (note) h.append(el('span', null, note));
  box.append(h);
  const chips = el('div', 'chips');
  list.forEach((e) => {
    const c = el('div', 'chip');
    c.append(portrait(e.heroId), el('span', null, nameOf(e.heroId)), el('span', 'pct', pct(e.prob)));
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
  const words = label.split(/[\s-]+/);
  const abbr = words.map((w) => w[0]).join('');
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
    : (!state.step ? '' : 'Enter выбирает первого найденного, цифры 1–5 — карточки, Ctrl+Z отменяет');

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
  rf.querySelectorAll('.role-btn').forEach((b) => b.classList.toggle('on', +b.dataset.pos === roleFilter));
  if (readonly) return;

  const grid = $('grid');
  grid.textContent = '';
  const list = visibleHeroes();
  const q = $('search').value.trim();
  const firstFree = list.find((h) => !h.taken);
  list.forEach((h) => {
    const b = el('button', 'hero' + (h.taken ? ' taken' : '') + (q && h === firstFree ? ' match' : ''));
    b.type = 'button';
    b.title = h.localized || h.name;
    b.append(portrait(h.id), el('span', null, h.localized || h.name));
    b.onclick = () => applyHero(h.id);
    grid.append(b);
  });
}

function applyHero(id) {
  $('search').value = '';
  post('/api/apply', { heroId: id });
}

// ---------- wiring ----------

document.querySelectorAll('.side-btn').forEach((b) => {
  b.onclick = () => post('/api/side', { team: b.dataset.side });
});

$('first-pick').onclick = () => {
  const b = state?.board;
  const empty = b && !(b.radiantPicks.length + b.radiantBans.length + b.direPicks.length + b.direBans.length);
  if (!empty && armed !== 'first') {
    armed = 'first';
    render();
    setTimeout(() => { if (armed === 'first') { armed = ''; render(); } }, 3000);
    return;
  }
  armed = '';
  post('/api/first-pick');
};

$('undo').onclick = () => post('/api/undo');
$('redo').onclick = () => post('/api/redo');

const menu = $('menu');
const menuBtn = $('menu-btn');
menuBtn.onclick = (e) => {
  e.stopPropagation();
  menu.hidden = !menu.hidden;
  menuBtn.setAttribute('aria-expanded', String(!menu.hidden));
};
document.addEventListener('click', (e) => {
  if (!menu.hidden && !menu.contains(e.target)) { menu.hidden = true; menuBtn.setAttribute('aria-expanded', 'false'); }
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
  menu.hidden = true;
  post('/api/new-draft');
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
    post(e.shiftKey ? '/api/redo' : '/api/undo');
    return;
  }
  if (mod && e.key.toLowerCase() === 'y') { e.preventDefault(); post('/api/redo'); return; }
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
