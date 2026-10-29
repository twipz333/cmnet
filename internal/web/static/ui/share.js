// Friends page. On the host: open a code that hands the model and scouting to
// friends. On the guest: type the host's code to get them.

import { $, el, store, post, button, toast } from './core.js';

const GUEST_DOWNLOAD = 'https://github.com/twipz333/cmnet/releases/latest';

let codeDraft = '';

export function enterShare() { renderShare(); }

function when(t) {
  if (!t) return '';
  const d = new Date(t);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return '';
  return d.toLocaleString('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });
}

async function copy(text, what) {
  try {
    await navigator.clipboard.writeText(text);
    toast(`${what} скопирован`);
  } catch {
    toast('Не получилось скопировать, выделите и скопируйте вручную');
  }
}

export function renderShare() {
  const s = store.state;
  const host = $('share-body');
  if (!s || !host) return;
  host.textContent = '';
  const sh = s.share || {};
  if (sh.guest) renderGuest(host, s, sh);
  else renderHost(host, s, sh);
}

function renderHost(host, s, sh) {
  const h = sh.host || {};
  const sec = el('section', 'doc-sec');
  sec.append(el('h2', null, 'Дать другу cmnet'));
  sec.append(el('p', 'muted', 'Друг ставит гостевую версию cmnet и вводит код. Ему придут твоя модель, разведка соперников и портреты героев. Сам он ничего не собирает и не обучает.'));

  if (h.active) {
    const box = el('div', 'share-code');
    const code = el('div', 'share-code-num', h.code);
    code.setAttribute('aria-label', `Код ${h.code.split('').join(' ')}`);
    box.append(el('div', 'share-code-cap', 'Код для друга'), code);
    const act = el('div', 'actions');
    act.append(button('Скопировать код', 'btn', () => copy(h.code, 'Код')));
    act.append(button('Закрыть раздачу', 'btn quiet', () => post('/api/share/stop', {})));
    box.append(act);
    sec.append(box);
    const facts = el('dl', 'facts');
    const fact = (k, v) => facts.append(el('dt', null, k), el('dd', null, v));
    fact('Открыта', when(h.since) || 'только что');
    fact('Получили', h.served ? `${h.served} раз, последний ${when(h.lastServed)}${h.lastVia === 'https' ? ' (через HTTPS)' : ''}` : 'пока никто');
    sec.append(facts);
    if (h.error) sec.append(el('p', 'err', `Связь с relay: ${h.error}. Раздача продолжает попытки.`));
    sec.append(el('p', 'muted', 'Код работает, пока открыт cmnet. После новой разведки другу достаточно нажать «Обновить от хоста»: придёт свежая.'));
  } else {
    const act = el('div', 'actions');
    const b = button('Открыть раздачу', 'btn', () => post('/api/share/start', {}));
    if (!s.meta.draftReady) {
      b.disabled = true;
      act.append(b, el('span', 'muted', 'Сначала нужна обученная модель (раздел «Данные»).'));
    } else act.append(b);
    sec.append(act);
  }
  $('share-body').append(sec);

  const dl = el('section', 'doc-sec');
  dl.append(el('h2', null, 'Где взять гостевую версию'));
  dl.append(el('p', 'muted', 'На странице релиза файл cmnet-guest-setup. Ставится рядом с обычным cmnet, не мешает ему и обновляется сам.'));
  const a = el('a', 'link', GUEST_DOWNLOAD);
  a.href = GUEST_DOWNLOAD;
  a.target = '_blank';
  a.rel = 'noopener';
  const act2 = el('div', 'actions');
  act2.append(a, button('Скопировать ссылку', 'btn quiet', () => copy(GUEST_DOWNLOAD, 'Адрес')));
  dl.append(act2);
  $('share-body').append(dl);
}

function renderGuest(host, s, sh) {
  const got = sh.received;
  const sec = el('section', 'doc-sec');
  sec.append(el('h2', null, got ? 'Модель от хоста' : 'Подключиться к хосту'));

  if (got) {
    const m = got.meta || {};
    const facts = el('dl', 'facts');
    const fact = (k, v) => facts.append(el('dt', null, k), el('dd', null, v));
    fact('Модель', `патч ${m.patchTag || '—'}, ${String(m.bundleVersion || '').slice(0, 12)}`);
    fact('Разведка', m.scouted ? `${m.scouted} соперников` : (m.hasSnapshot ? 'есть' : 'нет'));
    fact('Получено', when(got.at));
    sec.append(facts);
    const act = el('div', 'actions');
    const b = button(sh.receiving ? 'Получаю…' : 'Обновить от хоста', 'btn', () => post('/api/share/receive', { code: '' }));
    b.disabled = !!sh.receiving;
    act.append(b);
    if (sh.lastCode) act.append(el('span', 'muted', `код ${sh.lastCode}`));
    sec.append(act);
    sec.append(el('p', 'muted', 'Хост сделал новую разведку? Нажмите «Обновить». Доска драфта при этом очистится.'));
  } else {
    sec.append(el('p', 'muted', 'Попросите у хоста код. Он появляется у него в cmnet: «Друзья», затем «Открыть раздачу».'));
  }

  const form = el('form', 'share-form');
  const input = el('input', 'share-input');
  input.type = 'text';
  input.placeholder = 'XXXX-XXXX-XXXX';
  input.autocomplete = 'off';
  input.spellcheck = false;
  input.setAttribute('aria-label', 'Код хоста');
  input.value = codeDraft;
  input.oninput = () => { codeDraft = input.value; };
  const go = button(sh.receiving ? 'Ищу хоста…' : (got ? 'Получить по новому коду' : 'Получить'), got ? 'btn quiet' : 'btn', null);
  go.type = 'submit';
  go.disabled = !!sh.receiving;
  form.append(input, go);
  form.onsubmit = (e) => {
    e.preventDefault();
    if (!codeDraft.trim()) { input.focus(); return; }
    post('/api/share/receive', { code: codeDraft });
  };
  sec.append(form);
  if (sh.receiving) sec.append(el('p', 'muted', 'Ищу хоста, это занимает до двух минут. У хоста должен быть открыт cmnet с включённой раздачей.'));
  if (sh.error) sec.append(el('p', 'err', sh.error));
  $('share-body').append(sec);
  if (!got && !sh.receiving) setTimeout(() => { if (document.activeElement === document.body) input.focus(); }, 0);
}
