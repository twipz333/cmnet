// The draft page: ribbon, team columns, what to do now, hero picker.

import {
  ROLE, ROLE_LOWER, ROLE_OF, $, el, store, post, getJSON, nameOf, pct, portrait, paint,
  toast, button, mmss,
} from './core.js';

let roleFilter = 0;
let armed = '';
let busy = false;
let strengthDelta = null;
let seenFilled = new Set();
let outcomeSentFor = '';

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

const ourSide = () => (store.state?.meta?.ourTeam || 'Radiant').toLowerCase();

export function boardSize(s) {
  const b = s?.board;
  return b ? b.radiantPicks.length + b.radiantBans.length + b.direPicks.length + b.direBans.length : -1;
}

function boardKey(s) {
  const b = s?.board;
  return b ? [b.radiantPicks, b.direPicks].map((x) => x.join(',')).join('|') : '';
}

// Called for every state update, before rendering.
export function trackDraft(next, prev) {
  const before = prev?.result?.strength?.ourProb;
  const after = next.result?.strength?.ourProb;
  if (before != null && after != null && boardSize(next) !== boardSize(prev) && Math.abs(after - before) >= 0.005) {
    strengthDelta = { value: after - before, until: Date.now() + 6000 };
    setTimeout(() => { if (document.body.dataset.page === 'draft') renderDraft(); }, 6100);
  }
}

// ---------- reasons ----------

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

function metaLine(heroId, pos) {
  const pm = (store.state.result?.positionMeta || []).find((p) => p.position === pos);
  if (!pm) return null;
  const i = pm.heroes.findIndex((h) => h.heroId === heroId);
  if (i < 0) return null;
  const wr = pm.heroes[i].winRate;
  return `№${i + 1} в мете ${ROLE_OF[pos]}` + (wr ? `, побед ${(wr * 100).toFixed(1)}%` : '');
}

function enemyWants(heroId) {
  const res = store.state.result || {};
  if (!res.enemyStep?.isPick) return 0;
  return (res.enemyPred || []).find((e) => e.heroId === heroId)?.prob || 0;
}

const openRoles = (slots) => (slots || []).filter((s) => !s.heroId).map((s) => s.position);
const playerName = (list, id) => (id ? (list || []).find((p) => p.accountId === id)?.name || '' : '');

// ---------- render ----------

export function renderDraft() {
  const s = store.state;
  if (!s) return;
  document.body.dataset.our = ourSide();
  renderControls(s.meta);
  renderRibbon(s);
  renderTeams(s);
  renderFocus(s);
  renderPicker(s);
}

function renderControls(m) {
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
    const sp = el('span', m.gsiSeen ? 'live' : 'wait');
    sp.append(el('i', 'dot'), document.createTextNode(m.gsiSeen ? 'Драфт читается из игры' : 'Жду данные из Dota'));
    src.append(sp);
  }
  const live = store.state.live;
  if (live && live.clockTime != null && live.gameState && /GAME_IN_PROGRESS|PRE_GAME/.test(live.gameState)) {
    src.append(el('span', 'clock', `игра ${mmss(live.clockTime)}`));
  }

  const st = store.state.result?.strength;
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

  $('undo').disabled = !m.canUndo;
  $('redo').disabled = !m.canRedo;
  document.querySelector('.history').hidden = m.gsiMode;
}

function renderRibbon(s) {
  const host = $('ribbon');
  host.textContent = '';
  const seq = s.sequence || [];
  if (!seq.length) return;

  const key = el('div', 'lane-key');
  key.setAttribute('aria-hidden', 'true');
  key.append(el('div', 'k-ours', 'Мы'), el('div', 'k-theirs', 'Они'));
  host.append(key);

  let phase = null;
  let picks = 0;
  let bans = 0;
  const filledNow = new Set();
  seq.forEach((st, i) => {
    if (!phase || seq[i - 1].action !== st.action) {
      phase = el('div', 'phase');
      const isPick = st.action === 'Pick';
      if (isPick) picks++; else bans++;
      phase.append(el('div', 'phase-steps'), el('div', 'phase-name', isPick ? `Пики ${picks}` : `Баны ${bans}`));
      host.append(phase);
    }
    const isPick = st.action === 'Pick';
    const node = el('div', `step ${isPick ? 'pick' : 'ban'} ${st.ours ? 'ours' : 'theirs'}`);
    node.style.setProperty('--lane', st.ours ? 1 : 2);
    const what = `${i + 1}. ${st.ours ? 'Наш' : 'Их'} ${isPick ? 'пик' : 'бан'}`;
    if (st.heroId) {
      paint(node, st.heroId, false);
      node.title = `${what}: ${nameOf(st.heroId)}`;
      filledNow.add(i);
      if (!seenFilled.has(i) && seenFilled.size) node.classList.add('fresh');
    } else {
      node.classList.add('empty');
      node.title = what;
    }
    if (st.current) {
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

function renderTeams(s) {
  const lu = s.lineup;
  const b = s.board;
  const ours = ourSide() === 'radiant';
  const sides = [
    { host: $('team-ours'), side: 'our', name: s.meta.ourTeam, note: 'наша команда', slots: lu?.our, players: lu?.allies,
      bans: ours ? b.radiantBans : b.direBans, mine: true },
    { host: $('team-theirs'), side: 'enemy', name: ours ? 'Dire' : 'Radiant', note: 'соперник, роли угаданы по героям',
      slots: lu?.enemy, players: lu?.enemies, bans: ours ? b.direBans : b.radiantBans, mine: false },
  ];
  for (const sd of sides) {
    sd.host.textContent = '';
    sd.host.append(el('h2', 'team-name', sd.name), el('div', 'team-note', sd.note));
    for (const slot of sd.slots || []) {
      const row = el('div', 'seatrow');
      row.append(el('div', 'role', ROLE[slot.position]));
      const who = el('div', 'who');
      const player = playerName(sd.players, slot.accountId);
      if (slot.heroId) {
        const hero = button('', 'who-btn', () => openMatchups(slot.heroId));
        hero.title = `${nameOf(slot.heroId)}: нажмите, чтобы увидеть, с кем он хорош и кого боится`;
        const text = el('div', 'who-text');
        text.append(el('span', 'who-hero', nameOf(slot.heroId)));
        if (player) text.append(el('span', 'who-player', player));
        hero.append(portrait(slot.heroId), text);
        who.append(hero, moveSelect(sd.side, slot));
      } else {
        row.classList.add('open');
        if (sd.mine) row.classList.add('ours-open');
        const text = el('div', 'who-text');
        text.append(el('span', 'who-hero', sd.mine ? 'нужен' : 'ещё нет'));
        if (player) text.append(el('span', 'who-player', player));
        who.append(text);
      }
      row.append(who);
      sd.host.append(row);
    }
    const bansBox = el('div', 'bans');
    bansBox.append(el('div', 'bans-title', sd.bans?.length ? `Баны: ${sd.bans.length} из 7` : 'Банов пока нет'));
    if (sd.bans?.length) {
      const list = el('ul', 'bans-list');
      sd.bans.forEach((id) => {
        const li = el('li', 'ban');
        li.append(portrait(id, 'mini'), el('span', null, nameOf(id)));
        list.append(li);
      });
      bansBox.append(list);
    }
    sd.host.append(bansBox);
  }
}

// moveSelect lets the captain correct a guessed position: the role guess drives
// which positions count as open, so a wrong guess skews every recommendation.
function moveSelect(side, slot) {
  const sel = el('select', 'move');
  sel.title = 'Переставить на другую позицию';
  sel.setAttribute('aria-label', `Позиция героя ${nameOf(slot.heroId)}`);
  for (let p = 1; p <= 5; p++) {
    const o = el('option', null, ROLE[p]);
    o.value = String(p);
    if (p === slot.position) o.selected = true;
    sel.append(o);
  }
  sel.onchange = () => draftAction('/api/lineup', { side, position: +sel.value, heroId: slot.heroId });
  return sel;
}

function renderFocus(s) {
  const m = s.meta;
  const title = $('focus-title');
  const sub = $('focus-sub');
  const cards = $('cards');
  const rows = $('aside-rows');
  cards.textContent = '';
  rows.textContent = '';
  title.className = 'focus-title';
  sub.textContent = '';

  if (!m.draftReady) {
    const guest = !!s.share?.guest;
    title.textContent = guest ? 'Модели пока нет' : 'Модель ещё не обучена';
    sub.textContent = guest
      ? 'Попросите у хоста код и введите его на странице «Хост».'
      : (m.draftError || 'Советнику нужен бандл: соберите матчи и обучите модель.');
    const act = el('div', 'actions');
    const data = el('a', 'btn', guest ? 'Ввести код хоста' : 'Открыть «Данные»');
    data.href = guest ? '#share' : '#data';
    act.append(data);
    cards.append(act);
    return;
  }

  const step = s.step;
  const res = s.result || {};
  if (!step) {
    title.textContent = 'Драфт завершён';
    const st = res.strength;
    if (st) sub.innerHTML = `Шанс на победу по драфту: <b>${pct(st.ourProb)}</b>`;
    renderAfterDraft(rows, s);
    return;
  }

  const isPick = step.action === 'Pick';
  if (step.isOurs) {
    title.classList.add('ours');
    title.textContent = isPick ? 'Ваш пик' : 'Ваш бан';
    if (isPick) {
      const need = openRoles(s.lineup?.our);
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
      rows.append(chipRow(es.isPick ? 'Их следующий пик' : 'Их следующий бан', 'вероятность', res.enemyPred.slice(0, 6)));
    }
    if (!isPick && (res.safeToLet || []).length) {
      rows.append(chipRow('Можно не банить', 'возьмут, но нам не страшны', res.safeToLet.slice(0, 6)));
    }
    if ((res.poolAlert || []).length) {
      rows.append(chipRow('Наши герои под угрозой', 'их могут забрать следующим ходом', res.poolAlert.slice(0, 6)));
    }
  }
  if ((res.itemAdvice || []).length) rows.append(adviceRow(res.itemAdvice));
}

// After the draft: the objective timers once the game runs, item advice, and the
// result, which is what the decision log needs to say whether the advice helped.
function renderAfterDraft(rows, s) {
  const alerts = s.alerts || [];
  if (alerts.length) {
    const box = el('section', 'chips-row');
    box.append(el('h2', null, 'Ближайшие объекты'));
    const list = el('ul', 'timers');
    alerts.slice(0, 8).forEach((a) => {
      const li = el('li', 'timer' + (a.due ? ' due' : ''));
      li.append(el('span', 'timer-in', mmss(a.nextIn)), el('span', null, a.label));
      list.append(li);
    });
    box.append(list);
    rows.append(box);
  }
  if ((s.result?.itemAdvice || []).length) rows.append(adviceRow(s.result.itemAdvice));

  const key = boardKey(s);
  if (s.meta.logging && outcomeSentFor !== key) {
    const box = el('section', 'outcome');
    box.append(el('h2', null, 'Как сыграли?'),
      el('p', 'muted', 'Итог нужен, чтобы понять, помогают ли советы. Из игры он обычно приходит сам.'));
    const act = el('div', 'actions');
    const send = (win) => async () => {
      if (await post('/api/outcome', { win })) { outcomeSentFor = key; toast(win ? 'Записали победу' : 'Записали поражение'); renderDraft(); }
    };
    act.append(button('Выиграли', 'btn', send(true)), button('Проиграли', 'btn quiet', send(false)));
    box.append(act);
    rows.append(box);
  }
  if (!s.meta.gsiMode) rows.append(button('Новый драфт', 'btn quiet', () => draftAction('/api/new-draft')));
}

function adviceRow(lines) {
  const box = el('section', 'chips-row');
  box.append(el('h2', null, 'Советы по предметам'));
  const ul = el('ul', 'advice');
  lines.forEach((l) => ul.append(el('li', null, l)));
  box.append(ul);
  return box;
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
  line.append(el('span', 'card-role', 'вероятность'), el('span', 'card-metric', pct(e.prob)));
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

// ---------- matchups ----------

async function openMatchups(heroId) {
  const dlg = $('matchups');
  const body = $('matchups-body');
  $('matchups-title').textContent = nameOf(heroId);
  body.textContent = 'Загружаю…';
  if (!dlg.open) dlg.showModal();
  const data = await getJSON(`/api/matchups?hero=${heroId}`);
  body.textContent = '';
  if (data.error || !data.found) {
    body.append(el('p', 'muted', 'Парных данных для этого героя пока нет: в кэше слишком мало игр на каждую пару. Они появятся по мере сбора матчей.'));
    return;
  }
  const col = (heading, list, note, bad) => {
    const sec = el('section', 'mu-col');
    sec.append(el('h3', null, heading), el('p', 'muted', note));
    const ul = el('ul', 'mu-list');
    (list || []).slice(0, 8).forEach((h) => {
      const li = el('li');
      const lift = Math.round((h.value - 0.5) * 1000) / 10;
      li.append(portrait(h.heroId), el('span', null, nameOf(h.heroId)),
        el('span', 'mu-lift ' + ((lift >= 0) !== !!bad ? 'up' : 'down'), `${lift >= 0 ? '+' : '−'}${Math.abs(lift)}`));
      ul.append(li);
    });
    if (!(list || []).length) ul.append(el('li', 'muted', 'нет данных'));
    sec.append(ul);
    return sec;
  };
  body.append(
    col('Хорош вместе с', data.synergy, 'прибавка к винрейту, когда они в одной команде, п.п.'),
    col('Сильнее, чем', data.counters, 'его прибавка, когда они стоят против него, п.п.'),
    col('Боится', data.counteredBy, 'их прибавка, когда они стоят против него, п.п.', true),
  );
}

// ---------- picker ----------

const RU = 'йцукенгшщзхъфывапролджэячсмитьбю';
const EN = 'qwertyuiop[]asdfghjkl;\'zxcvbnm,.';
const fromRu = (s) => [...s].map((c) => { const i = RU.indexOf(c); return i >= 0 ? EN[i] : c; }).join('');

function heroList() {
  if (store.catalog.length) return store.catalog;
  return (store.state.heroes || []).map((h) => ({ ...h, roles: [] }));
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
  const avail = new Set((store.state.heroes || []).map((h) => h.id));
  return heroList()
    .filter((h) => matches(h, q) && (!roleFilter || (h.roles || []).includes(roleFilter)))
    .map((h) => ({ ...h, taken: !avail.has(h.id) }));
}

function renderPicker(s) {
  const m = s.meta;
  const picker = $('picker');
  const readonly = m.gsiMode || !m.draftReady || !s.step;
  picker.classList.toggle('readonly', readonly);
  $('picker-hint').textContent = m.gsiMode
    ? 'Доска заполняется из игры'
    : (!s.step ? '' : 'Печатайте в любом месте. Enter берёт первого найденного, 1–5 выбирают карточку, Ctrl+Z отменяет');

  const rf = $('role-filter');
  if (!rf.childElementCount) {
    [[0, 'Все'], [1, ROLE[1]], [2, ROLE[2]], [3, ROLE[3]], [4, ROLE[4]], [5, ROLE[5]]].forEach(([pos, label]) => {
      const b = button(label, 'role-btn', () => { roleFilter = pos; renderPicker(store.state); $('search').focus(); });
      b.dataset.pos = pos;
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

export function initDraft() {
  document.querySelectorAll('.side-btn').forEach((b) => {
    b.onclick = () => post('/api/side', { team: b.dataset.side });
  });
  $('first-pick').onclick = () => {
    if (boardSize(store.state) !== 0 && armed !== 'first') {
      armed = 'first';
      renderDraft();
      setTimeout(() => { if (armed === 'first') { armed = ''; renderDraft(); } }, 3000);
      return;
    }
    armed = '';
    draftAction('/api/first-pick');
  };
  $('undo').onclick = () => draftAction('/api/undo');
  $('redo').onclick = () => draftAction('/api/redo');
  const nd = $('new-draft');
  let ndArmed = false;
  nd.onclick = () => {
    if (boardSize(store.state) > 0 && !ndArmed) {
      ndArmed = true;
      nd.textContent = 'Очистить? Ещё раз';
      setTimeout(() => { ndArmed = false; nd.textContent = 'Новый драфт'; }, 3000);
      return;
    }
    ndArmed = false;
    nd.textContent = 'Новый драфт';
    draftAction('/api/new-draft');
  };

  $('matchups-close').onclick = () => $('matchups').close();

  const search = $('search');
  search.addEventListener('input', () => renderPicker(store.state));
  search.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      const first = visibleHeroes().find((h) => !h.taken);
      if (first) applyHero(first.id);
      e.preventDefault();
    } else if (e.key === 'Escape') {
      search.value = '';
      renderPicker(store.state);
    }
  });

  document.addEventListener('keydown', (e) => {
    if (!store.state || document.body.dataset.page !== 'draft' || $('matchups').open) return;
    const t = e.target;
    const inField = t instanceof HTMLInputElement || t instanceof HTMLSelectElement || t instanceof HTMLTextAreaElement;
    const mod = e.ctrlKey || e.metaKey;
    if (mod && e.key.toLowerCase() === 'z' && !inField) {
      e.preventDefault();
      draftAction(e.shiftKey ? '/api/redo' : '/api/undo');
      return;
    }
    if (mod && e.key.toLowerCase() === 'y' && !inField) { e.preventDefault(); draftAction('/api/redo'); return; }
    if (mod || e.altKey || inField) return;
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
      renderPicker(store.state);
    }
  });
}
