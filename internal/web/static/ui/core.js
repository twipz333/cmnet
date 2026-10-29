// Shared plumbing for every page: the live state from the Go core, requests,
// and the small rendering helpers all pages use. Nothing here knows about any
// particular screen.

export const ROLE = { 1: 'Керри', 2: 'Мид', 3: 'Оффлейн', 4: 'Четвёрка', 5: 'Пятёрка' };
export const ROLE_LOWER = { 1: 'керри', 2: 'мид', 3: 'оффлейн', 4: 'четвёрка', 5: 'пятёрка' };
export const ROLE_OF = { 1: 'керри', 2: 'мида', 3: 'оффлейна', 4: 'четвёрки', 5: 'пятёрки' };

export const $ = (id) => document.getElementById(id);
export function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text != null) n.textContent = text;
  return n;
}

export const store = {
  state: null,
  catalog: [],      // [{id, name, localized, roles: [pos…]}]
  catalogFor: '',
  connected: false,
};

const listeners = [];
export function onState(fn) { listeners.push(fn); }

let lastStatus = '';
export function setState(next) {
  const prev = store.state;
  store.state = next;
  if (next.meta.draftReady && next.meta.bundleVersion !== store.catalogFor) loadCatalog();
  if (next.status && next.status !== lastStatus) toast(humanStatus(next.status));
  lastStatus = next.status || '';
  listeners.forEach((fn) => fn(next, prev));
}

async function loadCatalog() {
  store.catalogFor = store.state?.meta?.bundleVersion || '';
  try {
    const res = await fetch('/api/catalog');
    const data = await res.json();
    store.catalog = data.heroes || [];
    if (store.state) listeners.forEach((fn) => fn(store.state, store.state));
  } catch { /* hero names still come from state.names */ }
}

export function connect() {
  const es = new EventSource('/api/events');
  es.onmessage = (ev) => {
    store.connected = true;
    setState(JSON.parse(ev.data));
  };
  es.onerror = () => {
    store.connected = false;
    document.body.classList.add('offline');
  };
  es.onopen = () => document.body.classList.remove('offline');
}

// post sends a state-changing request; the core answers with the new state.
export async function post(path, body) {
  try {
    const res = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (!res.ok) {
      toast(await res.text());
      return null;
    }
    const st = await res.json();
    setState(st);
    return st;
  } catch {
    toast('Ядро не отвечает. Проверьте, что cmnet запущен.');
    return null;
  }
}

// postJSON is for endpoints that answer with their own payload, not the state.
export async function postJSON(path, body, method = 'POST') {
  try {
    const res = await fetch(path, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    });
    const text = await res.text();
    try { return JSON.parse(text); } catch { return { ok: res.ok, error: res.ok ? '' : text }; }
  } catch {
    return { ok: false, error: 'Ядро не отвечает' };
  }
}

export async function getJSON(path) {
  try {
    const res = await fetch(path);
    if (!res.ok) return { error: (await res.text()) || `HTTP ${res.status}`, unavailable: res.status === 404 };
    return await res.json();
  } catch {
    return { error: 'Ядро не отвечает' };
  }
}

// ---------- heroes ----------

export function nameOf(id) {
  return store.state?.names?.[id] || store.catalog.find((h) => h.id === id)?.localized || `#${id}`;
}
export const pct = (p) => `${Math.round(p * 100)}%`;
export const pct1 = (p) => `${(p * 100).toFixed(1)}%`;

export function initials(name) {
  return name.split(/[\s-]+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase();
}

// Portrait images are checked once per hero and remembered, so a redraw paints
// an already-loaded portrait instantly instead of flashing its initials.
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

export function paint(node, id, withInitials = true) {
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

export function portrait(id, cls = 'portrait') {
  const box = el('div', cls);
  box.setAttribute('aria-hidden', 'true');
  if (id) paint(box, id);
  return box;
}

// ---------- feedback ----------

export function humanStatus(s) {
  if (/^our side → /.test(s)) return 'Мы за ' + s.split('→ ')[1];
  if (/^first pick → /.test(s)) return 'Первыми выбирают: ' + s.split('→ ')[1];
  if (s === 'new draft') return 'Доска очищена, новый драфт';
  if (/^sync queued/.test(s)) return 'Сбор матчей запущен';
  if (/^train queued/.test(s)) return 'Обучение запущено';
  if (/^scout(-allies)? queued/.test(s)) return 'Разведка запущена';
  if (/^(applied|undo|redo|lineup updated)/.test(s)) return '';
  return s;
}

let toastTimer = 0;
export function toast(msg) {
  if (!msg) return;
  const t = $('toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), 3500);
}

// ---------- formatting ----------

export function ago(unixSec) {
  if (!unixSec) return 'никогда';
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unixSec));
  if (s < 60) return 'только что';
  if (s < 3600) return `${Math.floor(s / 60)} мин назад`;
  if (s < 86400) return `${Math.floor(s / 3600)} ч назад`;
  return `${Math.floor(s / 86400)} дн назад`;
}

export function duration(ms) {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s} с`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} мин ${s % 60} с`;
  return `${Math.floor(m / 60)} ч ${m % 60} мин`;
}

export function num(n) {
  return new Intl.NumberFormat('ru-RU').format(n);
}

export function mmss(sec) {
  const neg = sec < 0;
  const s = Math.abs(sec);
  return `${neg ? '−' : ''}${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

// button builds a <button type=button> wired to fn.
export function button(text, cls, fn) {
  const b = el('button', cls, text);
  b.type = 'button';
  if (fn) b.onclick = fn;
  return b;
}

// confirmable turns a button into a two-click confirm for destructive actions.
export function confirmable(btn, prompt, fn) {
  const label = btn.textContent;
  let armed = false;
  btn.onclick = () => {
    if (!armed) {
      armed = true;
      btn.textContent = prompt;
      btn.classList.add('armed');
      setTimeout(() => { armed = false; btn.textContent = label; btn.classList.remove('armed'); }, 3000);
      return;
    }
    armed = false;
    btn.textContent = label;
    btn.classList.remove('armed');
    fn();
  };
  return btn;
}

const JOB_NAMES = {
  sync: 'Сбор матчей', train: 'Обучение', scout: 'Разведка соперников', 'scout-allies': 'Разведка союзников', profiles: 'Проверка профилей',
  assets: 'Картинки героев', backfill: 'Дозагрузка статистики', rankfill: 'Дозагрузка рангов', eval: 'Оценка модели',
};
export const jobName = (kind) => JOB_NAMES[kind] || kind;
