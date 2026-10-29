// Entry point: navigation between pages, and routing every state update to the
// page on screen.

import { $, el, store, onState, connect, button } from './core.js';
import { initDraft, renderDraft, trackDraft } from './draft.js';
import { enterData, leaveData, renderData } from './data.js';
import { enterScout, renderScout, scoutStateChanged } from './scout.js';
import { enterSettings, renderSettings, setUpdaterStatus } from './settings.js';
import { enterShare, renderShare } from './share.js';

const PAGES = {
  draft: { enter: () => renderDraft(), render: renderDraft },
  data: { enter: enterData, leave: leaveData, render: renderData },
  scout: { enter: enterScout, render: renderScout },
  share: { enter: enterShare, render: renderShare },
  settings: { enter: (sub) => enterSettings(sub), render: renderSettings },
};

let page = '';

function route() {
  const [name, sub] = (location.hash.slice(1) || 'draft').split('/');
  let next = PAGES[name] ? name : 'draft';
  // The guest app has no data or scouting of its own.
  if (isGuest() && (next === 'data' || next === 'scout')) next = 'share';
  if (page && page !== next) PAGES[page].leave?.();
  const changed = page !== next;
  page = next;
  document.body.dataset.page = page;
  document.querySelectorAll('.nav a').forEach((a) => {
    const on = a.dataset.page === page;
    a.classList.toggle('on', on);
    if (on) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  });
  document.title = { draft: 'cmnet', data: 'Данные · cmnet', scout: 'Разведка · cmnet', share: `${isGuest() ? 'Хост' : 'Друзья'} · cmnet`, settings: 'Настройки · cmnet' }[page];
  if (store.state) PAGES[page].enter(sub);
  if (changed && page !== 'draft') $(`page-${page}`)?.focus({ preventScroll: true });
}

const isGuest = () => !!store.state?.share?.guest;

// Guest: the nav says "Хост", and with nothing received yet the app opens on
// the page that asks for the code.
function go(hash) {
  if (location.hash === hash) route();
  else location.hash = hash; // hashchange routes
}

function applyEdition(s, prev) {
  const guest = !!s.share?.guest;
  document.body.classList.toggle('guest', guest);
  const nav = $('nav-share');
  nav.textContent = guest ? 'Хост' : 'Друзья';
  $('share-title').textContent = guest ? 'Хост' : 'Друзья';
  if (guest && !prev && !s.meta.draftReady && !location.hash.startsWith('#settings')) {
    go('#share');
    return true;
  }
  if (guest && prev && !prev.meta.draftReady && s.meta.draftReady && page === 'share') {
    go('#draft'); // the model just arrived: go draft
    return true;
  }
  if (guest && (page === 'data' || page === 'scout')) { route(); return true; }
  return false;
}

onState((next, prev) => {
  document.body.classList.remove('connecting');
  const moved = applyEdition(next, prev);
  trackDraft(next, prev);
  scoutStateChanged(next);
  renderNavBadges(next);
  if (moved) return; // hashchange renders the new page
  if (!prev) PAGES[page].enter(location.hash.split('/')[1]);
  else PAGES[page].render();
});

// The nav marks a page that has something running, so a sync started on the data
// page stays visible from the draft.
function renderNavBadges(s) {
  const kinds = (s.ops?.active || []).map((j) => j.kind);
  const busy = { data: kinds.some((k) => k === 'sync' || k === 'train') || ['sync', 'train'].includes(s.auto?.phase),
    scout: kinds.some((k) => k.startsWith('scout')) };
  document.querySelectorAll('.nav a').forEach((a) => a.classList.toggle('busy', !!busy[a.dataset.page]));
}

// Desktop shell: update notices. Absent when the UI is opened in a browser.
function wireDesktop() {
  const d = window.cmnetDesktop;
  if (!d?.onUpdate) return;
  let dismissed = false;
  const show = (st) => {
    if (!st) return;
    setUpdaterStatus(st);
    const bar = $('update-bar');
    if (st.state !== 'ready' || dismissed) return;
    bar.textContent = '';
    bar.append(el('span', null, `Готово обновление ${st.version || ''}. Оно установится при выходе или сейчас, за пару секунд.`),
      button('Перезапустить сейчас', 'btn', () => d.installUpdate()),
      button('Потом', 'link-btn', () => { dismissed = true; bar.hidden = true; }));
    bar.hidden = false;
  };
  d.onUpdate(show);
  d.updateStatus?.().then(show).catch(() => {});
}

window.addEventListener('hashchange', route);
initDraft();
wireDesktop();
route();
connect();
