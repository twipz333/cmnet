// The scout page: who we draft against (and with) today, the address book of
// players we have met, and our own hero pool.

import {
  $, el, store, post, getJSON, button, confirmable, nameOf, portrait, pct, ago, jobName, num,
} from './core.js';

let stats = null;
let players = null;
let roleFilter = '';
const selected = new Set();

export function enterScout() {
  loadStats();
  loadPlayers();
  renderScout();
}

async function loadStats() {
  stats = await getJSON('/api/stats');
  renderScout();
}

async function loadPlayers() {
  players = await getJSON('/api/players' + (roleFilter ? `?role=${roleFilter}` : ''));
  renderScout();
}

// After a scout finishes the snapshot changes; refresh when a scout job leaves
// the active list.
let wasScouting = false;
export function scoutStateChanged(s) {
  const scouting = (s.ops?.active || []).some((j) => j.kind === 'scout' || j.kind === 'scout-allies');
  if (wasScouting && !scouting) { loadStats(); loadPlayers(); }
  wasScouting = scouting;
}

export function renderScout() {
  const s = store.state;
  if (!s) return;
  renderRun(s);
  renderToday();
  renderPool();
  renderBook();
}

function renderRun(s) {
  const host = $('scout-run');
  host.textContent = '';
  const sec = el('section', 'doc-sec');
  sec.append(el('h2', null, 'Кого разведать'));
  sec.append(el('p', 'muted', 'Вставьте ссылки на профили (Steam, OpenDota, STRATZ) или числовые id через запятую. Достаточно одного открытого профиля соперника: из его матчей поднимется драфт всей команды.'));

  const form = (id, label, placeholder, path, cta) => {
    const f = el('form', 'inline-form');
    const lab = el('label', null, label);
    lab.htmlFor = id;
    const input = el('input');
    input.id = id;
    input.type = 'text';
    input.placeholder = placeholder;
    input.autocomplete = 'off';
    input.spellcheck = false;
    const go = el('button', 'btn', cta);
    go.type = 'submit';
    f.append(lab, input, go);
    f.onsubmit = async (e) => {
      e.preventDefault();
      if (!input.value.trim()) { input.focus(); return; }
      if (await post(path, { players: input.value })) input.value = '';
    };
    return f;
  };
  if (s.meta.canScout) sec.append(form('scout-enemies', 'Соперники', 'steamcommunity.com/id/ник, 123456789', '/api/ops/scout', 'Разведать соперников'));
  if (s.meta.canScoutAllies) sec.append(form('scout-allies', 'Наша команда', 'профили наших игроков', '/api/ops/scout-allies', 'Разведать своих'));
  if (!s.meta.canScout && !s.meta.canScoutAllies) sec.append(el('p', 'err', 'Разведка недоступна в этой сборке.'));

  const running = (s.ops?.active || []).filter((j) => j.kind === 'scout' || j.kind === 'scout-allies');
  running.forEach((j) => {
    const line = el('div', 'job live');
    line.append(el('b', null, jobName(j.kind)), el('div', 'job-line', j.lastLine || 'начинаю…'));
    if (j.waitingOn) line.append(el('div', 'job-wait', `ждём ответа: ${j.waitingOn}`));
    sec.append(line);
  });
  host.append(sec);
}

function renderToday() {
  const host = $('scout-today');
  host.textContent = '';
  const sec = el('section', 'doc-sec');
  const head = el('div', 'sec-head');
  head.append(el('h2', null, 'Сегодня'));
  sec.append(head);
  if (!stats) { sec.append(el('p', 'muted', 'Загружаю…')); host.append(sec); return; }
  if (stats.error || !stats.stats) {
    sec.append(el('p', stats.unavailable ? 'muted' : 'err', stats.unavailable ? 'Снапшот разведки недоступен в этой сборке.' : stats.error));
    host.append(sec);
    return;
  }
  const st = stats.stats;
  const cols = el('div', 'two-col');
  cols.append(playerColumn('Соперники', st.enemyPlayers, false), playerColumn('Наши', st.allyPlayers, true));
  sec.append(cols);

  const l3 = el('div', 'layer3');
  l3.append(el('span', null, st.layer3Drafts
    ? `Драфтов соперника в этом турнире: ${st.layer3Drafts}. Они сильнее всего влияют на предсказание их ходов.`
    : 'Драфтов соперника в этом турнире пока нет.'));
  if (st.layer3Drafts) l3.append(confirmable(button('Забыть драфты турнира', 'link-btn danger'), 'Точно забыть?', async () => { await post('/api/stats/clear-layer3'); loadStats(); }));
  sec.append(l3);
  host.append(sec);
}

function playerColumn(title, list, ally) {
  const box = el('div', 'pcol');
  const head = el('div', 'sec-head');
  head.append(el('h3', null, title));
  if ((list || []).length) {
    head.append(confirmable(button('Очистить', 'link-btn danger'), 'Очистить всех?', async () => {
      await post('/api/stats/clear-scout', { ally });
      loadStats();
    }));
  }
  box.append(head);
  if (!(list || []).length) {
    box.append(el('p', 'muted', ally ? 'Своих не разведывали. Это нужно, чтобы советы учитывали ваши комфортные герои.' : 'Соперников не разведывали.'));
    return box;
  }
  box.append(el('p', 'muted small', 'Под ником — любимые герои и сколько игр на каждом за последний месяц.'));
  const ul = el('ul', 'players');
  list.forEach((p) => {
    const li = el('li', 'player');
    const top = el('div', 'player-top');
    top.append(el('b', null, p.nickname || `id ${p.accountId}`));
    if (!p.resolved) top.append(el('span', 'tag warn', 'профиль скрыт'));
    if (p.smurf && p.smurf.level !== 'low') {
      const t = el('span', 'tag ' + (p.smurf.level === 'high' ? 'danger' : 'warn'), p.smurf.label || 'возможен смурф');
      t.title = (p.smurf.reasons || []).join('\n');
      top.append(t);
    }
    top.append(confirmable(button('Убрать', 'link-btn'), 'Убрать?', async () => {
      await post('/api/stats/remove-player', { accountId: p.accountId, ally });
      loadStats();
      loadPlayers();
    }));
    li.append(top);
    const heroes = el('ul', 'phero');
    (p.topHeroes || []).slice(0, 8).forEach((h) => {
      const hi = el('li');
      hi.title = `${h.name || nameOf(h.id)}: ${num(h.games)} игр, побед ${pct(h.winRate)}`;
      hi.append(portrait(h.id, 'mini'), el('span', null, `${h.games}`));
      heroes.append(hi);
    });
    li.append(heroes);
    ul.append(li);
  });
  box.append(ul);
  return box;
}

function renderPool() {
  const host = $('scout-pool');
  host.textContent = '';
  const s = store.state;
  if (!s.meta.canEditPool) return;
  const sec = el('section', 'doc-sec');
  sec.append(el('h2', null, 'Наш пул героев'));
  sec.append(el('p', 'muted', 'Герои, которых команда хочет играть. Советник предупредит, если соперник может их забрать.'));
  const pool = (stats?.stats?.heroPool || []);
  const chips = el('ul', 'chips');
  pool.forEach((h) => {
    const li = el('li', 'chip');
    li.append(portrait(h.id), el('span', 'chip-name', h.name || nameOf(h.id)));
    const rm = button('', 'chip-x', () => savePool(pool.filter((x) => x.id !== h.id).map((x) => x.name || nameOf(x.id))));
    rm.setAttribute('aria-label', `Убрать ${h.name || nameOf(h.id)} из пула`);
    rm.textContent = '×';
    li.append(rm);
    chips.append(li);
  });
  if (!pool.length) chips.append(el('li', 'muted', 'Пул пуст.'));
  sec.append(chips);

  const f = el('form', 'inline-form');
  const lab = el('label', null, 'Добавить героя');
  lab.htmlFor = 'pool-add';
  const input = el('input');
  input.id = 'pool-add';
  input.setAttribute('list', 'hero-names');
  input.placeholder = 'начните вводить имя';
  input.autocomplete = 'off';
  const add = el('button', 'btn quiet', 'Добавить');
  add.type = 'submit';
  f.append(lab, input, add);
  f.onsubmit = (e) => {
    e.preventDefault();
    const name = input.value.trim();
    if (!name) return;
    savePool(pool.map((x) => x.name || nameOf(x.id)).concat(name));
    input.value = '';
  };
  sec.append(f);
  host.append(sec);
  fillHeroNames();
}

async function savePool(names) {
  await post('/api/stats/hero-pool', { tokens: names.join(', ') });
  loadStats();
}

function fillHeroNames() {
  const dl = $('hero-names');
  if (dl.childElementCount || !store.catalog.length) return;
  store.catalog.forEach((h) => { const o = el('option'); o.value = h.localized || h.name; dl.append(o); });
}

function renderBook() {
  const host = $('scout-book');
  host.textContent = '';
  const sec = el('section', 'doc-sec');
  const head = el('div', 'sec-head');
  head.append(el('h2', null, 'Записная книжка'));
  const filter = el('select');
  filter.setAttribute('aria-label', 'Кого показывать');
  [['', 'Все'], ['ally', 'Свои'], ['enemy', 'Соперники']].forEach(([v, t]) => {
    const o = el('option', null, t);
    o.value = v;
    if (v === roleFilter) o.selected = true;
    filter.append(o);
  });
  filter.onchange = () => { roleFilter = filter.value; loadPlayers(); };
  head.append(filter);
  sec.append(head);
  sec.append(el('p', 'muted', 'Все, с кем играли или кого разведывали. Отметьте нужных и разведайте одним нажатием. Список хранится только на этом компьютере.'));

  const add = el('form', 'inline-form');
  const lab = el('label', null, 'Добавить');
  lab.htmlFor = 'book-add';
  const input = el('input');
  input.id = 'book-add';
  input.placeholder = 'ссылка на профиль или id';
  input.autocomplete = 'off';
  const role = el('select');
  role.setAttribute('aria-label', 'Кто это');
  [['ally', 'свой'], ['enemy', 'соперник']].forEach(([v, t]) => { const o = el('option', null, t); o.value = v; role.append(o); });
  const go = el('button', 'btn quiet', 'Добавить');
  go.type = 'submit';
  add.append(lab, input, role, go);
  add.onsubmit = async (e) => {
    e.preventDefault();
    if (!input.value.trim()) return;
    if (await post('/api/players/add', { ref: input.value.trim(), ally: role.value === 'ally' })) { input.value = ''; loadPlayers(); }
  };
  sec.append(add);

  if (!players) { sec.append(el('p', 'muted', 'Загружаю…')); host.append(sec); return; }
  if (players.error) { sec.append(el('p', players.unavailable ? 'muted' : 'err', players.unavailable ? 'Записная книжка недоступна в этой сборке.' : players.error)); host.append(sec); return; }
  const list = players.players || [];
  if (!list.length) { sec.append(el('p', 'muted', 'Пока никого нет.')); host.append(sec); return; }

  const bulk = el('div', 'bulk');
  const ids = () => [...selected].join(',');
  const b1 = button(`Разведать как соперников (${selected.size})`, 'btn quiet', async () => { await post('/api/ops/scout', { players: ids() }); selected.clear(); renderScout(); });
  const b2 = button(`Разведать как своих (${selected.size})`, 'btn quiet', async () => { await post('/api/ops/scout-allies', { players: ids() }); selected.clear(); renderScout(); });
  b1.disabled = b2.disabled = !selected.size;
  bulk.append(b1, b2);
  if (selected.size) bulk.append(button('Снять выбор', 'link-btn', () => { selected.clear(); renderScout(); }));
  sec.append(bulk);

  const table = el('table', 'tbl book');
  const thead = el('thead');
  const hr = el('tr');
  ['', 'Игрок', 'Кто', 'Героев в кэше', 'Видели', 'Сегодня', ''].forEach((h) => hr.append(el('th', null, h)));
  thead.append(hr);
  table.append(thead);
  const tbody = el('tbody');
  list.forEach((p) => {
    const tr = el('tr');
    const cb = el('input');
    cb.type = 'checkbox';
    cb.checked = selected.has(p.accountId);
    cb.setAttribute('aria-label', `Выбрать ${p.persona || p.accountId}`);
    cb.onchange = () => { if (cb.checked) selected.add(p.accountId); else selected.delete(p.accountId); renderScout(); };
    const tdc = el('td'); tdc.append(cb);
    const name = el('td');
    name.append(el('b', null, p.persona || `id ${p.accountId}`));
    if (p.note) name.append(el('div', 'muted', p.note));
    const today = p.inSnapshot === 'ally' ? 'в нашем составе' : p.inSnapshot === 'enemy' ? 'в составе соперника' : '';
    const acts = el('td', 'acts');
    acts.append(button(p.pinned ? 'Открепить' : 'Закрепить', 'link-btn', async () => { await post('/api/players/pin', { accountId: p.accountId, pinned: !p.pinned }); loadPlayers(); }));
    acts.append(confirmable(button('Забыть', 'link-btn danger'), 'Забыть насовсем?', async () => { await post('/api/players/forget', { accountId: p.accountId }); selected.delete(p.accountId); loadPlayers(); }));
    tr.append(tdc, name, el('td', null, p.role === 'ally' ? 'свой' : p.role === 'enemy' ? 'соперник' : '—'),
      el('td', 'n', num(p.heroes)), el('td', null, ago(p.lastSeen)), el('td', null, today), acts);
    if (p.pinned) tr.classList.add('pinned');
    tbody.append(tr);
  });
  table.append(tbody);
  sec.append(table);
  host.append(sec);
}
