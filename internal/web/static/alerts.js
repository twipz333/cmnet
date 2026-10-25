(() => {
  'use strict';

  let view = null;
  let cache = [];
  let live = [];
  const $ = (id) => document.getElementById(id);
  const text = (tag, className, value) => {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (value != null) node.textContent = value;
    return node;
  };
  const input = (type, value, className) => {
    const node = document.createElement('input');
    node.type = type;
    node.value = value == null ? '' : String(value);
    if (className) node.className = className;
    return node;
  };
  const number = (value, className) => {
    const node = input('number', value, className);
    node.step = '1';
    return node;
  };
  const asNumber = (value, fallback) => {
    const n = Number(value);
    return Number.isFinite(n) ? Math.trunc(n) : fallback;
  };
  const clock = (seconds) => {
    const n = Math.max(0, asNumber(seconds, 0));
    return Math.floor(n / 60) + ':' + String(n % 60).padStart(2, '0');
  };
  const parseClock = (value) => {
    const source = String(value || '').trim();
    if (/^\d+$/.test(source)) return Number(source);
    const match = source.match(/^(\d+):(\d{1,2})$/);
    if (!match || Number(match[2]) > 59) return null;
    return Number(match[1]) * 60 + Number(match[2]);
  };
  const setStatus = (message, bad) => {
    const node = $('alerts-status');
    if (!node) return;
    node.textContent = message || '';
    node.className = bad ? 'off' : 'why';
  };

  function field(label, node, hint) {
    const wrap = text('label', 'alert-field');
    wrap.append(text('span', null, label), node);
    if (hint) wrap.append(text('small', 'why', hint));
    return wrap;
  }

  function renderKinds() {
    const host = $('alerts-kinds');
    if (!host || !view) return;
    host.textContent = '';
    for (const kind of view.kinds || []) {
      const row = text('article', 'alert-kind');
      row.dataset.kind = kind.kind;
      const heading = text('div', 'alert-kind-name');
      heading.append(text('strong', null, kind.label || kind.kind));
      heading.append(text('span', 'mono', kind.kind));
      const timing = [];
      if (kind.firstSpawn != null) timing.push('первый: ' + clock(kind.firstSpawn));
      if (kind.intervalSec) timing.push('каждые ' + kind.intervalSec + ' сек.');
      if (timing.length) heading.append(text('small', 'why', timing.join(' · ')));
      const fields = text('div', 'alert-kind-fields');
      fields.append(field('Lead', number(kind.lead, 'kind-lead'), '0 = общий, −1 = выкл'));
      fields.append(field('From', number(kind.from, 'kind-from'), '−1 = без границы'));
      fields.append(field('Until', number(kind.until, 'kind-until'), '0 = без границы'));
      fields.append(field('TTS', input('text', kind.tts, 'kind-tts'), 'пусто = общий шаблон'));
      row.append(heading, fields);
      host.append(row);
    }
  }

  function customRow(item) {
    const row = text('div', 'alert-custom-row');
    row.dataset.id = item.id || ('custom-' + Date.now().toString(36));
    const at = input('text', clock(item.at), 'custom-at');
    at.placeholder = '12:30';
    const label = input('text', item.label, 'custom-label');
    label.placeholder = 'Название';
    const tts = input('text', item.tts, 'custom-tts');
    tts.placeholder = 'Шаблон TTS';
    const lead = number(item.lead, 'custom-lead');
    const enabled = input('checkbox', '', 'custom-enabled');
    enabled.checked = item.enabled !== false;
    const remove = text('button', 'danger small', 'Удалить');
    remove.type = 'button';
    remove.onclick = () => row.remove();
    row.append(field('Время', at), field('Метка', label), field('TTS', tts), field('Lead', lead), field('Вкл', enabled), remove);
    return row;
  }

  function renderCustom() {
    const host = $('alerts-custom');
    if (!host || !view) return;
    host.textContent = '';
    for (const item of view.custom || []) host.append(customRow(item));
    if (!host.children.length) host.append(text('p', 'empty-state', 'Пока нет своих напоминаний.'));
  }

  function renderLive() {
    const host = $('alerts-live');
    if (!host) return;
    host.textContent = '';
    if (!live.length) {
      host.append(text('p', 'empty-state', 'Нет запланированных алертов.'));
      return;
    }
    for (const item of live) {
      const row = text('div', 'live-alert');
      const at = item.at ?? item.nextAt ?? item.when ?? item.time;
      row.append(text('strong', 'mono', typeof at === 'number' ? clock(at) : String(at || '—')));
      row.append(text('span', null, item.label || item.kind || item.text || 'Алерт'));
      if (item.lead != null) row.append(text('small', 'why', 'за ' + item.lead + ' сек.'));
      host.append(row);
    }
  }

  function renderCache() {
    const host = $('alerts-cache');
    if (!host) return;
    host.textContent = '';
    if (!cache.length) {
      host.append(text('p', 'empty-state', 'Кэш озвучки пуст.'));
      return;
    }
    for (const item of cache) {
      const row = text('div', 'cache-entry');
      const info = text('div', 'cache-entry-info');
      info.append(text('strong', null, item.text || item.key || 'Без текста'));
      info.append(text('small', 'why', [item.speaker, item.effect, item.ext, item.bytes ? Math.ceil(item.bytes / 1024) + ' КБ' : ''].filter(Boolean).join(' · ')));
      const remove = text('button', 'danger small', 'Удалить');
      remove.type = 'button';
      remove.onclick = () => deleteCache(item.key);
      row.append(info, remove);
      host.append(row);
    }
  }

  function render() {
    if (!view) return;
    $('alerts-default-lead').value = view.defaultLead == null ? 0 : view.defaultLead;
    $('alerts-default-tts').value = view.defaultTts || '';
    renderKinds();
    renderCustom();
    renderLive();
    renderCache();
  }

  async function load() {
    try {
      const response = await fetch('/api/alerts');
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.error || 'Ошибка загрузки алертов');
      view = data.alerts || { kinds: [], custom: [] };
      cache = data.cache || [];
      live = data.live || [];
      render();
      setStatus(view.path || '');
    } catch (err) {
      setStatus(err.message || 'Не удалось загрузить алерты', true);
    }
  }

  function collectCustom() {
    return Array.from(document.querySelectorAll('#alerts-custom .alert-custom-row')).map((row) => {
      const at = parseClock(row.querySelector('.custom-at').value);
      if (at == null) throw new Error('Время алерта укажите как mm:ss или число секунд.');
      return {
        id: row.dataset.id,
        at,
        label: row.querySelector('.custom-label').value.trim(),
        tts: row.querySelector('.custom-tts').value.trim(),
        lead: asNumber(row.querySelector('.custom-lead').value, 0),
        enabled: row.querySelector('.custom-enabled').checked,
      };
    });
  }

  async function save() {
    try {
      const kinds = {};
      document.querySelectorAll('#alerts-kinds .alert-kind').forEach((row) => {
        kinds[row.dataset.kind] = {
          lead: asNumber(row.querySelector('.kind-lead').value, 0),
          from: asNumber(row.querySelector('.kind-from').value, -1),
          until: asNumber(row.querySelector('.kind-until').value, 0),
          tts: row.querySelector('.kind-tts').value.trim(),
        };
      });
      const body = {
        defaultLead: asNumber($('alerts-default-lead').value, 0),
        defaultTts: $('alerts-default-tts').value.trim(),
        kinds,
        custom: collectCustom(),
      };
      setStatus('сохраняю…');
      const response = await fetch('/api/alerts/save', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || data.ok === false) throw new Error(data.error || ('Ошибка HTTP ' + response.status));
      setStatus(data.status || 'Сохранено');
      await load();
    } catch (err) {
      setStatus(err.message || 'Не удалось сохранить', true);
    }
  }

  async function deleteCache(key) {
    if (!key) return;
    const response = await fetch('/api/tts-cache?key=' + encodeURIComponent(key), { method: 'DELETE' });
    if (!response.ok) { setStatus('Не удалось удалить запись кэша', true); return; }
    cache = cache.filter((item) => item.key !== key);
    renderCache();
  }

  async function clearCache() {
    const response = await fetch('/api/tts-cache?key=*', { method: 'DELETE' });
    if (!response.ok) { setStatus('Не удалось очистить кэш', true); return; }
    cache = [];
    renderCache();
  }

  function wire() {
    $('alerts-save')?.addEventListener('click', save);
    $('alerts-add-custom')?.addEventListener('click', () => {
      const host = $('alerts-custom');
      host.querySelector('.empty-state')?.remove();
      host.append(customRow({ id: 'custom-' + Date.now().toString(36), at: 0, enabled: true }));
    });
    $('alerts-cache-clear')?.addEventListener('click', clearCache);
  }

  document.addEventListener('DOMContentLoaded', wire);
  window.AlertsUI = { show: () => $('screen-alerts')?.removeAttribute('hidden'), load };
})();
