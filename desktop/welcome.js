const api = window.cmnetSetup;
const list = document.getElementById('list');
const err = document.getElementById('err');

function size(bytes) {
  if (bytes > 1e9) return (bytes / 1e9).toFixed(1) + ' ГБ';
  return Math.max(1, Math.round(bytes / 1e6)) + ' МБ';
}

function ago(ms) {
  const days = Math.floor((Date.now() - ms) / 86400000);
  if (days <= 0) return 'обновлено сегодня';
  if (days === 1) return 'обновлено вчера';
  return `обновлено ${days} дн. назад`;
}

async function use(dir) {
  err.textContent = '';
  const res = await api.choose(dir);
  if (res && res.error) err.textContent = res.error;
}

api.scan().then((found) => {
  list.textContent = '';
  if (!found.length) {
    const none = document.createElement('div');
    none.className = 'searching';
    none.textContent = 'Папок с cmnet.db не нашлось. Укажите файл вручную или начните с нуля.';
    list.append(none);
    return;
  }
  found.forEach((f, i) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'pick';
    const dir = document.createElement('span');
    dir.className = 'dir';
    dir.textContent = f.dir;
    const meta = document.createElement('span');
    meta.className = 'meta';
    meta.textContent = `${size(f.dbBytes)}, ${ago(f.modified)}` + (f.hasBundle ? ', модель обучена' : ', модели ещё нет');
    const go = document.createElement('span');
    go.className = 'go';
    go.textContent = 'Использовать';
    b.append(dir, meta, go);
    b.onclick = () => use(f.dir);
    list.append(b);
    if (i === 0) b.focus();
  });
});

document.getElementById('browse').onclick = async () => {
  err.textContent = '';
  const res = await api.browse();
  if (res && res.error) err.textContent = res.error;
};
document.getElementById('fresh').onclick = () => api.fresh();
