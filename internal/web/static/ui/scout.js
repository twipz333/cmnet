// The scout page: who we draft against (and with) today, the address book of
// players we have met, and our own hero pool.

import {
  $, el, store, post, getJSON, button, confirmable, nameOf, portrait, pct, ago, jobName, num, stableView,
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

// Refresh as EACH scouting job ends, including when others remain queued.
let activeScoutIds = new Set();
export function scoutStateChanged(s) {
  const now = new Set((s.ops?.active || [])
    .filter((j) => j.kind === 'scout' || j.kind === 'scout-allies')
    .map((j) => j.id));
  const finished = [...activeScoutIds].some((id) => !now.has(id));
  activeScoutIds = now;
  if (finished) { loadStats(); loadPlayers(); }
}

export function renderScout() {
  const s = store.state;
  if (!s) return;
  stableView($('page-scout'), () => {
    renderTeams($('scout-teams'), { stats, players, reload: () => { loadStats(); loadPlayers(); }, rerender: renderScout });
    renderPool();
    renderBook();
  });
}

// Live progress does not require tearing down the entire roster and book.
// Job identity/visibility changes still trigger a full render via app.js.
export function updateScoutProgress(s) {
  const jobs = new Map((s.ops?.active || []).map((j) => [String(j.id), j]));
  document.querySelectorAll('#scout-teams [data-scout-job]').forEach((node) => {
    const j = jobs.get(node.dataset.scoutJob);
    if (!j) return;
    const line = node.querySelector('.job-line');
    if (line) line.textContent = j.lastLine || 'начинаю…';
  });
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
    return chosen.length ? chosen.slice(0, 10) : list.map((p) => p.accountId).slice(0, 10);
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
    selected.size ? `Обновить ники и аватарки (${Math.min(10, selected.size)} из ${selected.size})` : 'Обновить ники и аватарки (первые 10)',
    'btn quiet', () => refresh(refreshIDs()),
  ));
  if (selected.size) bulk.append(button('Снять выбор', 'link-btn', () => { selected.clear(); renderScout(); }));
  sec.append(bulk);

  // Native-app-style cards rather than a wide HTML table. A fixed-width table
  // caused a second horizontal scrollbar whenever the desktop window narrowed.
  const cards = el('div', 'book-list');
  list.forEach((p) => {
    const card = el('article', 'book-entry');
    if (p.pinned) card.classList.add('pinned');

    const header = el('div', 'book-entry-head');
    const cb = el('input');
    cb.type = 'checkbox';
    cb.checked = selected.has(p.accountId);
    cb.setAttribute('aria-label', `Выбрать ${p.persona || p.accountId}`);
    cb.onchange = () => { if (cb.checked) selected.add(p.accountId); else selected.delete(p.accountId); renderScout(); };
    const identity = el('div', 'book-entry-person');
    identity.append(avatar(p.accountId, p.persona || `id ${p.accountId}`, p.avatarVersion));
    const label = el('div', 'book-entry-identity');
    label.append(el('b', null, p.persona || `id ${p.accountId}`));
    label.append(el('span', 'muted', `Steam ID ${p.accountId}`));
    identity.append(label);
    header.append(cb, identity);
    if (p.pinned) header.append(el('span', 'book-pin', 'Закреплён'));
    card.append(header);

    const badges = el('div', 'book-entry-badges');
    badges.append(el('span', 'tag', p.role === 'ally' ? 'Союзник' : p.role === 'enemy' ? 'Соперник' : 'Без роли'));
    if (p.inSnapshot) badges.append(el('span', 'tag', p.inSnapshot === 'ally' ? 'В нашем составе' : 'У соперников'));
    if (p.note) badges.append(el('span', 'book-entry-note', p.note));
    card.append(badges);

    const facts = el('div', 'book-entry-facts');
    [[`Героев ${num(p.heroes)}`, 'cache'], [`Видели: ${ago(p.lastSeen)}`, 'seen'],
     [`Профиль: ${p.profileCheckedAt ? ago(p.profileCheckedAt) : 'не проверен'}`, 'profile']].forEach(([value, kind]) => {
      const item = el('span', 'book-fact', value);
      item.dataset.kind = kind;
      facts.append(item);
    });
    card.append(facts);

    const acts = el('div', 'book-entry-actions');
    acts.append(button('Обновить Steam', 'btn quiet small', () => refresh([p.accountId])));
    acts.append(button(p.pinned ? 'Открепить' : 'Закрепить', 'link-btn', async () => {
      await post('/api/players/pin', { accountId: p.accountId, pinned: !p.pinned });
      loadPlayers();
    }));
    acts.append(confirmable(button('Забыть', 'link-btn danger'), 'Забыть насовсем?', async () => {
      await post('/api/players/forget', { accountId: p.accountId });
      selected.delete(p.accountId);
      loadPlayers();
    }));
    card.append(acts);
    cards.append(card);
  });
  sec.append(cards);
  host.append(sec);
}
