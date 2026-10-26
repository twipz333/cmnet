// The settings page: app settings (config.yaml), objective alerts, item advice
// rules, the Discord bot, and "about" with updates.

import { $, el, store, post, postJSON, getJSON, button, confirmable, toast, mmss } from './core.js';

const TABS = [
  ['app', 'Приложение'],
  ['alerts', 'Таймеры и озвучка'],
  ['rules', 'Советы по предметам'],
  ['discord', 'Discord'],
  ['about', 'О программе'],
];

let tab = 'app';
let config = null;
let alerts = null;
let rulesText = null;
let discord = null;
let updater = null; // desktop shell status, when running inside the app

export function enterSettings(sub) {
  if (sub && TABS.some(([k]) => k === sub)) tab = sub;
  load();
  renderSettings();
}

async function load() {
  if (tab === 'app') config = await getJSON('/api/config');
  if (tab === 'alerts') alerts = await getJSON('/api/alerts');
  if (tab === 'rules') { const st = await getJSON('/api/stats'); rulesText = st.stats ? st.stats.itemRules || '' : null; }
  if (tab === 'discord') discord = await getJSON('/api/discord-config');
  if (tab === 'about' && window.cmnetDesktop?.updateStatus) updater = await window.cmnetDesktop.updateStatus();
  renderSettings();
}

export function renderSettings() {
  const s = store.state;
  if (!s) return;
  const nav = $('settings-tabs');
  nav.textContent = '';
  TABS.forEach(([key, label]) => {
    if (key === 'discord' && !s.meta.canDiscordConfig) return;
    if (key === 'alerts' && !s.meta.canAlerts) return;
    if (key === 'rules' && !s.meta.canEditRules) return;
    if (key === 'app' && !s.meta.canConfig) return;
    const a = el('a', 'subtab' + (tab === key ? ' on' : ''), label);
    a.href = `#settings/${key}`;
    if (tab === key) a.setAttribute('aria-current', 'page');
    nav.append(a);
  });
  const host = $('settings-body');
  host.textContent = '';
  ({ app: renderApp, alerts: renderAlerts, rules: renderRules, discord: renderDiscord, about: renderAbout })[tab](host);
}

// ---------- config.yaml ----------

const HIDDEN_KEYS = /^alert-/; // edited on the alerts tab

function renderApp(host) {
  if (!config) { host.append(el('p', 'muted', 'Загружаю…')); return; }
  if (config.error || !config.config) { host.append(el('p', 'err', config.error || 'Настройки недоступны.')); return; }
  const v = config.config;
  host.append(el('p', 'muted', `Файл: ${v.path}${v.exists ? '' : ' (появится при сохранении)'}. Включённый переключатель слева — значение записано в файл; выключенный — работает значение по умолчанию.`));
  if (v.note) host.append(el('p', 'muted', v.note));
  (v.sections || []).forEach((sec) => {
    const fields = (sec.fields || []).filter((f) => !HIDDEN_KEYS.test(f.key || ''));
    if (!fields.length) return;
    const box = el('section', 'doc-sec');
    box.append(el('h2', null, sec.title || sec.name || 'Общие'));
    const list = el('div', 'fields');
    fields.forEach((f) => list.append(fieldRow(f, true)));
    box.append(list);
    host.append(box);
  });
  const act = el('div', 'actions sticky-actions');
  act.append(button('Сохранить', 'btn', saveApp), button('Отменить изменения', 'btn quiet', async () => { config = await getJSON('/api/config'); renderSettings(); }));
  host.append(act);
}

let fieldSeq = 0;
// fieldRow renders one setting. With overridable, a checkbox says whether the
// value is written to the file or left at the default.
function fieldRow(f, overridable) {
  const id = `f-${fieldSeq++}`;
  const row = el('div', 'field' + (overridable && !f.set ? ' unset' : ''));
  if (overridable) {
    const on = el('input');
    on.type = 'checkbox';
    on.checked = !!f.set;
    on.className = 'field-set';
    on.setAttribute('aria-label', `Записать ${f.key} в файл`);
    on.onchange = () => { f.set = on.checked; row.classList.toggle('unset', !f.set); };
    row.append(on);
  }
  const lab = el('label', 'field-key', f.key);
  lab.htmlFor = id;
  row.append(lab);
  let input;
  if (f.type === 'bool') {
    input = el('input');
    input.type = 'checkbox';
    input.checked = !!f.value;
    input.onchange = () => { f.value = input.checked; mark(); };
  } else {
    input = el('input');
    input.type = f.secret ? 'password' : (f.type === 'int' || f.type === 'float' ? 'number' : 'text');
    if (f.type === 'float') input.step = 'any';
    if (f.type === 'int') input.step = '1';
    input.value = f.value == null ? '' : String(f.value);
    input.placeholder = f.default != null ? String(f.default) : '';
    input.autocomplete = 'off';
    input.spellcheck = false;
    input.oninput = () => { f.value = input.value; mark(); };
  }
  input.id = id;
  function mark() {
    if (overridable && !f.set) {
      f.set = true;
      row.querySelector('.field-set').checked = true;
      row.classList.remove('unset');
    }
  }
  const val = el('div', 'field-val');
  val.append(input);
  if (f.default != null && f.default !== '') val.append(el('span', 'muted', `по умолчанию ${f.default}`));
  row.append(val);
  if (f.help) row.append(el('p', 'field-help', f.help));
  return row;
}

async function saveApp() {
  const v = config.config;
  const sections = (v.sections || []).map((sec) => ({
    name: sec.name || '',
    fields: (sec.fields || []).map((f) => {
      let value = f.value;
      if (f.type === 'int' && typeof value === 'string') value = value === '' ? 0 : parseInt(value, 10);
      if (f.type === 'float' && typeof value === 'string') value = value === '' ? 0 : parseFloat(value);
      if (f.type === 'bool') value = !!value;
      return { key: f.key, set: !!f.set, value };
    }),
  }));
  if (await post('/api/config/save', { sections })) toast('Настройки сохранены. Часть из них подействует после перезапуска.');
  config = await getJSON('/api/config');
  renderSettings();
}

// ---------- alerts ----------

const clockText = (sec) => mmss(Math.max(0, sec | 0));
function parseClock(v) {
  const s = String(v || '').trim();
  if (/^\d+$/.test(s)) return +s;
  const m = s.match(/^(\d+):(\d{1,2})$/);
  return m && +m[2] < 60 ? +m[1] * 60 + +m[2] : null;
}

function renderAlerts(host) {
  if (!alerts) { host.append(el('p', 'muted', 'Загружаю…')); return; }
  if (alerts.error || !alerts.alerts) { host.append(el('p', 'err', alerts.error || 'Таймеры недоступны.')); return; }
  const v = alerts.alerts;
  host.append(el('p', 'muted', 'Во время игры cmnet предупреждает о рунах, лотосах, торменторе и смене дня и ночи по игровым часам из Dota. Здесь задаётся, за сколько секунд предупреждать и что говорить голосом.'));

  const def = el('section', 'doc-sec');
  def.append(el('h2', null, 'По умолчанию'));
  const g = el('div', 'fields');
  const lead = numberField('Предупреждать за, сек', v.defaultLead || 0, 'al-def-lead');
  const tts = textField('Фраза', v.defaultTts || '', 'al-def-tts', '{{label}} через {{nextIn}} секунд');
  g.append(lead, tts);
  def.append(g, el('p', 'muted', 'В фразе можно использовать {{label}} — название, {{nextIn}} — сколько секунд осталось, {{nextAt}} — игровое время.'));
  host.append(def);

  const kinds = el('section', 'doc-sec');
  kinds.append(el('h2', null, 'Объекты карты'));
  const table = el('table', 'tbl alerts-kinds');
  const thead = el('thead');
  const hr = el('tr');
  ['Объект', 'Появляется', 'Предупреждать за, сек', 'С какой минуты', 'До какой минуты', 'Своя фраза'].forEach((h) => hr.append(el('th', null, h)));
  thead.append(hr);
  table.append(thead);
  const tb = el('tbody');
  (v.kinds || []).forEach((k) => {
    const tr = el('tr');
    tr.dataset.kind = k.kind;
    const spawn = [k.firstSpawn != null ? `с ${clockText(k.firstSpawn)}` : '', k.intervalSec ? `каждые ${k.intervalSec} с` : ''].filter(Boolean).join(', ');
    const inp = (cls, val, type = 'number', ph = '') => { const i = el('input', cls); i.type = type; i.value = val; i.placeholder = ph; i.setAttribute('aria-label', `${k.label}: ${cls}`); const td = el('td'); td.append(i); return td; };
    tr.append(el('td', null, k.label || k.kind), el('td', 'muted', spawn),
      inp('k-lead', k.lead ?? 0, 'number', '0 — как обычно, −1 — выкл'),
      inp('k-from', k.from ?? -1), inp('k-until', k.until ?? 0), inp('k-tts', k.tts || '', 'text', 'как по умолчанию'));
    tb.append(tr);
  });
  table.append(tb);
  kinds.append(table, el('p', 'muted', 'Предупреждать за 0 — как по умолчанию, −1 — не предупреждать. «С какой минуты» −1 и «до какой» 0 означают без ограничений; значения в секундах игрового времени.'));
  host.append(kinds);

  const custom = el('section', 'doc-sec');
  const ch = el('div', 'sec-head');
  ch.append(el('h2', null, 'Свои напоминания'));
  const list = el('div', 'custom-list');
  const addRow = (c) => list.append(customRow(c));
  ch.append(button('Добавить', 'ghost-btn', () => addRow({ at: 600, label: '', tts: '', lead: 0, enabled: true })));
  custom.append(ch);
  (v.custom || []).forEach(addRow);
  if (!(v.custom || []).length) list.append(el('p', 'muted empty-custom', 'Своих напоминаний пока нет. Например: «Смоки на 10:00».'));
  custom.append(list);
  host.append(custom);

  if ((alerts.live || []).length) {
    const live = el('section', 'doc-sec');
    live.append(el('h2', null, 'Ближайшие в текущей игре'));
    const ul = el('ul', 'timers');
    alerts.live.forEach((a) => { const li = el('li', 'timer' + (a.due ? ' due' : '')); li.append(el('span', 'timer-in', mmss(a.nextIn ?? 0)), el('span', null, a.label)); ul.append(li); });
    live.append(ul);
    host.append(live);
  }

  const cache = el('section', 'doc-sec');
  const cache_h = el('div', 'sec-head');
  cache_h.append(el('h2', null, 'Озвучка в кэше'));
  if ((alerts.cache || []).length) cache_h.append(confirmable(button('Очистить всё', 'link-btn danger'), 'Удалить все файлы?', async () => {
    const r = await postJSON('/api/tts-cache?key=*', null, 'DELETE');
    toast(r.ok ? 'Кэш озвучки очищен' : r.error);
    alerts = await getJSON('/api/alerts');
    renderSettings();
  }));
  cache.append(cache_h);
  if (!(alerts.cache || []).length) cache.append(el('p', 'muted', 'Готовых аудиофайлов нет: фразы озвучиваются при первом использовании.'));
  else {
    const ul = el('ul', 'cache-list');
    alerts.cache.forEach((c) => {
      const li = el('li');
      li.append(el('span', null, c.text || c.key), el('span', 'muted', `${Math.ceil((c.bytes || 0) / 1024)} КБ`));
      li.append(button('Удалить', 'link-btn danger', async () => {
        const r = await postJSON(`/api/tts-cache?key=${encodeURIComponent(c.key)}`, null, 'DELETE');
        if (!r.ok) toast(r.error);
        alerts = await getJSON('/api/alerts');
        renderSettings();
      }));
      ul.append(li);
    });
    cache.append(ul);
  }
  host.append(cache);

  const act = el('div', 'actions sticky-actions');
  act.append(button('Сохранить таймеры', 'btn', saveAlerts));
  host.append(act);
}

function numberField(label, value, id) {
  const row = el('div', 'field');
  const lab = el('label', 'field-key', label);
  lab.htmlFor = id;
  const i = el('input');
  i.type = 'number';
  i.id = id;
  i.value = value;
  const val = el('div', 'field-val');
  val.append(i);
  row.append(lab, val);
  return row;
}

function textField(label, value, id, ph) {
  const row = el('div', 'field');
  const lab = el('label', 'field-key', label);
  lab.htmlFor = id;
  const i = el('input');
  i.type = 'text';
  i.id = id;
  i.value = value;
  i.placeholder = ph || '';
  const val = el('div', 'field-val');
  val.append(i);
  row.append(lab, val);
  return row;
}

let customSeq = 0;
function customRow(c) {
  document.querySelector('.empty-custom')?.remove();
  const row = el('div', 'custom-row');
  row.dataset.id = c.id || `custom-${Date.now().toString(36)}-${customSeq++}`;
  const f = (label, input) => { const l = el('label', 'mini-field'); l.append(el('span', null, label), input); return l; };
  const at = el('input', 'c-at'); at.value = clockText(c.at || 0); at.placeholder = '10:00';
  const label = el('input', 'c-label'); label.value = c.label || ''; label.placeholder = 'Смоки';
  const tts = el('input', 'c-tts'); tts.value = c.tts || ''; tts.placeholder = 'как по умолчанию';
  const lead = el('input', 'c-lead'); lead.type = 'number'; lead.value = c.lead || 0;
  const on = el('input', 'c-on'); on.type = 'checkbox'; on.checked = c.enabled !== false;
  row.append(f('Время', at), f('Название', label), f('Фраза', tts), f('За, сек', lead), f('Включено', on),
    button('Удалить', 'link-btn danger', () => row.remove()));
  return row;
}

async function saveAlerts() {
  const kinds = {};
  document.querySelectorAll('.alerts-kinds tbody tr').forEach((tr) => {
    const n = (cls, d) => { const x = Number(tr.querySelector(cls).value); return Number.isFinite(x) ? Math.trunc(x) : d; };
    kinds[tr.dataset.kind] = { lead: n('.k-lead', 0), from: n('.k-from', -1), until: n('.k-until', 0), tts: tr.querySelector('.k-tts').value.trim() };
  });
  const custom = [];
  for (const row of document.querySelectorAll('.custom-row')) {
    const at = parseClock(row.querySelector('.c-at').value);
    if (at == null) { toast('Время напоминания пишите как 10:00 или числом секунд'); row.querySelector('.c-at').focus(); return; }
    custom.push({
      id: row.dataset.id, at, label: row.querySelector('.c-label').value.trim(), tts: row.querySelector('.c-tts').value.trim(),
      lead: Math.trunc(Number(row.querySelector('.c-lead').value) || 0), enabled: row.querySelector('.c-on').checked,
    });
  }
  const body = {
    defaultLead: Math.trunc(Number($('al-def-lead').value) || 0),
    defaultTts: $('al-def-tts').value.trim(),
    kinds, custom,
  };
  const r = await postJSON('/api/alerts/save', body);
  toast(r.ok ? 'Таймеры сохранены' : (r.error || 'Не удалось сохранить'));
  alerts = await getJSON('/api/alerts');
  renderSettings();
}

// ---------- item rules ----------

function renderRules(host) {
  if (rulesText == null) { host.append(el('p', 'muted', 'Загружаю…')); return; }
  host.append(el('p', 'muted', 'Подсказки по предметам срабатывают, когда соперник берёт указанных героев. Одно правило на строку: герои через запятую, «|» означает «или», после «=>» текст совета.'));
  const ex = el('pre', 'example', 'axe, lifestealer | ursa => Eul или Lotus Orb\nphantom assassin => Monkey King Bar');
  host.append(ex);
  const ta = el('textarea', 'rules');
  ta.id = 'rules-text';
  ta.rows = 12;
  ta.spellcheck = false;
  ta.value = rulesText;
  ta.setAttribute('aria-label', 'Правила советов');
  host.append(ta);
  const act = el('div', 'actions');
  act.append(button('Сохранить правила', 'btn', async () => {
    if (await post('/api/stats/item-rules', { text: ta.value })) toast('Правила сохранены');
  }));
  host.append(act);
}

// ---------- discord ----------

function renderDiscord(host) {
  if (!discord) { host.append(el('p', 'muted', 'Загружаю…')); return; }
  if (discord.error || !discord.config) { host.append(el('p', 'err', discord.error || 'Настройки бота недоступны.')); return; }
  const v = discord.config;
  host.append(el('p', 'muted', `Бот озвучивает таймеры в голосовом канале. Файл: ${v.path}${v.exists ? '' : ' (появится при сохранении)'}.`));
  if (v.note) host.append(el('p', 'muted', v.note));
  (v.sections || []).forEach((sec) => {
    const box = el('section', 'doc-sec');
    box.append(el('h2', null, sec.title || sec.name || 'Discord'));
    const list = el('div', 'fields');
    (sec.fields || []).forEach((f) => { f.set = true; list.append(fieldRow(f, false)); });
    box.append(list);
    host.append(box);
  });
  const act = el('div', 'actions sticky-actions');
  act.append(button('Сохранить', 'btn', async () => {
    const sections = (v.sections || []).map((sec) => ({
      name: sec.name || '',
      fields: (sec.fields || []).map((f) => ({ key: f.key, set: true, value: f.value == null ? '' : String(f.value) })),
    }));
    const r = await postJSON('/api/discord-config/save', { sections });
    toast(r.ok ? (r.status || 'Сохранено') : (r.error || 'Не удалось сохранить'));
    discord = await getJSON('/api/discord-config');
    renderSettings();
  }));
  host.append(act);
}

// ---------- about / updates ----------

function renderAbout(host) {
  const m = store.state.meta;
  const sec = el('section', 'doc-sec');
  sec.append(el('h2', null, 'cmnet'));
  const facts = el('dl', 'facts');
  const fact = (k, v) => facts.append(el('dt', null, k), el('dd', null, v));
  const d = window.cmnetDesktop;
  if (d?.version) fact('Версия приложения', d.version);
  fact('Модель', m.draftReady ? `патч ${m.patchTag || '—'}, ${String(m.bundleVersion || '').slice(0, 12)}` : 'не обучена');
  if (d?.dataDir) fact('Папка данных', d.dataDir);
  sec.append(facts);
  if (d?.chooseDataDir) sec.append(button('Сменить папку данных', 'btn quiet', () => d.chooseDataDir()));
  host.append(sec);

  const up = el('section', 'doc-sec');
  up.append(el('h2', null, 'Обновления'));
  if (!d?.checkForUpdates) {
    up.append(el('p', 'muted', 'Обновления ставятся автоматически в приложении cmnet для Windows. Сейчас интерфейс открыт в браузере.'));
  } else {
    const st = updater || {};
    const text = {
      idle: 'Проверяю обновления раз в несколько часов и при запуске.',
      checking: 'Проверяю…',
      'up-to-date': 'Установлена последняя версия.',
      available: `Нашлась версия ${st.version || ''}, скачиваю.`,
      downloading: `Скачиваю версию ${st.version || ''}: ${Math.round(st.percent || 0)}%.`,
      ready: `Версия ${st.version || ''} скачана и установится при перезапуске.`,
      error: `Не удалось проверить: ${st.error || 'нет связи'}.`,
      dev: 'Это сборка для разработки, обновления выключены.',
    }[st.state || 'idle'];
    up.append(el('p', null, text));
    const act = el('div', 'actions');
    if (st.state === 'ready') act.append(button('Перезапустить и обновить', 'btn', () => d.installUpdate()));
    else if (!['checking', 'available', 'downloading', 'dev'].includes(st.state)) act.append(button('Проверить сейчас', 'btn quiet', async () => { updater = await d.checkForUpdates(); renderSettings(); }));
    up.append(act);
  }
  host.append(up);
}

export function setUpdaterStatus(st) {
  updater = st;
  if (document.body.dataset.page === 'settings' && tab === 'about') renderSettings();
}
