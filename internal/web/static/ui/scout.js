// The scout page: who we draft against (and with) today, the address book of
// players we have met, and our own hero pool.

import {
  $, el, store, post, getJSON, button, confirmable, nameOf, portrait, pct, ago, jobName, num,
} from './core.js';
import { renderTeams, placePending, avatar } from './team.js';

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
  placePending(stats);
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
  renderTeams($('scout-teams'), { stats, players, reload: () => { loadStats(); loadPlayers(); }, rerender: renderScout });
  renderPool();
  renderBook();
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

  const refreshIDs = () => {
    const chosen = [...selected];
    return chosen.length ? chosen : list.map((p) => p.accountId).slice(0, 10);
  };
  const refresh = async (ids) => {
    if (!ids.length) return;
    if (await post('/api/players/refresh', { accountIds: ids })) {
      await Promise.all([loadPlayers(), loadStats()]);
    }
  };

  const bulk = el('div', 'bulk');
  const ids = () => [...selected].join(',');
  const b1 = button(`Разведать как соперников (${selected.size})`, 'btn quiet', async () => { await post('/api/ops/scout', { players: ids() }); selected.clear(); renderScout(); });
  const b2 = button(`Разведать как своих (${selected.size})`, 'btn quiet', async () => { await post('/api/ops/scout-allies', { players: ids() }); selected.clear(); renderScout(); });
  b1.disabled = b2.disabled = !selected.size;
  bulk.append(b1, b2, button(
    selected.size ? `Обновить ники и аватарки (${selected.size})` : 'Обновить ники и аватарки (первые 10)',
    'btn quiet', () => refresh(refreshIDs()),
  ));
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
    const identity = el('div', 'book-player');
    identity.append(avatar(p.accountId, p.persona || `id ${p.accountId}`, p.avatarVersion));
    identity.append(el('b', null, p.persona || `id ${p.accountId}`));
    name.append(identity);
    if (p.note) name.append(el('div', 'muted', p.note));
    const today = p.inSnapshot === 'ally' ? 'в нашем составе' : p.inSnapshot === 'enemy' ? 'в составе соперника' : '';
    const acts = el('td', 'acts');
    acts.append(button('Проверить профиль', 'link-btn', () => refresh([p.accountId])));
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
