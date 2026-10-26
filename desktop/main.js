// cmnet desktop shell.
//
// The window is a thin host: the Go core (advisor.exe) runs as a sidecar process
// and serves the UI on a loopback port; this process only starts it, waits for
// it, shows its page, and stops it on exit. All draft logic stays in Go.
//
// Data (cache, bundle, config, snapshots) does NOT live next to the program: the
// install folder may be read-only (Program Files) and is replaced on update. It
// lives in a data folder — by default <userData>/data, changeable from the menu,
// which is how an existing cmnet folder with a large cmnet.db is reused.

const { app, BrowserWindow, Menu, dialog, shell } = require('electron');
const { spawn } = require('node:child_process');
const fs = require('node:fs');
const net = require('node:net');
const path = require('node:path');
const http = require('node:http');

const settingsFile = () => path.join(app.getPath('userData'), 'desktop.json');

function loadSettings() {
  try {
    return JSON.parse(fs.readFileSync(settingsFile(), 'utf8'));
  } catch {
    return {};
  }
}

function saveSettings(s) {
  fs.mkdirSync(path.dirname(settingsFile()), { recursive: true });
  fs.writeFileSync(settingsFile(), JSON.stringify(s, null, 2));
}

function dataDir() {
  const dir = loadSettings().dataDir || path.join(app.getPath('userData'), 'data');
  fs.mkdirSync(dir, { recursive: true });
  return dir;
}

// The packaged core sits in resources/; in development it is built next to us.
function corePath() {
  const packaged = path.join(process.resourcesPath, 'advisor.exe');
  if (app.isPackaged) return packaged;
  const dev = path.join(__dirname, 'resources', process.platform === 'win32' ? 'advisor.exe' : 'advisor');
  return dev;
}

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

function waitForHTTP(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve, reject) => {
    const attempt = () => {
      const req = http.get(url, (res) => {
        res.resume();
        resolve();
      });
      req.on('error', () => {
        if (Date.now() > deadline) reject(new Error('core did not start'));
        else setTimeout(attempt, 250);
      });
      req.setTimeout(1000, () => req.destroy());
    };
    attempt();
  });
}

let core = null;
let coreLog = [];
let win = null;
let quitting = false;

async function startCore() {
  const port = await freePort();
  const addr = `127.0.0.1:${port}`;
  const cwd = dataDir();
  // Relative defaults (cmnet.db, bundle.json, config.yaml, snapshot.json) resolve
  // against the working directory, so pointing cwd at the data folder is all the
  // configuration the core needs.
  const args = ['--web-addr', addr, '--open=false', '--discord=false'];
  coreLog = [];
  core = spawn(corePath(), args, { cwd, windowsHide: true });
  const keep = (buf) => {
    coreLog.push(buf.toString());
    if (coreLog.length > 200) coreLog.shift();
  };
  core.stdout.on('data', keep);
  core.stderr.on('data', keep);
  core.on('exit', (code) => {
    core = null;
    if (!quitting) {
      dialog.showErrorBox('cmnet', `Ядро остановилось (код ${code}).\n\n${coreLog.join('').slice(-2000)}`);
      app.quit();
    }
  });
  const url = `http://${addr}/`;
  await waitForHTTP(url, 30000);
  return url;
}

function stopCore() {
  if (!core) return Promise.resolve();
  return new Promise((resolve) => {
    const c = core;
    const t = setTimeout(() => { try { c.kill('SIGKILL'); } catch {} resolve(); }, 3000);
    c.once('exit', () => { clearTimeout(t); resolve(); });
    try { c.kill(); } catch { resolve(); }
  });
}

async function chooseDataDir() {
  const res = await dialog.showOpenDialog(win, {
    title: 'Папка данных cmnet (кэш матчей, бандл, конфиг)',
    defaultPath: dataDir(),
    properties: ['openDirectory', 'createDirectory'],
  });
  if (res.canceled || !res.filePaths[0]) return;
  saveSettings({ ...loadSettings(), dataDir: res.filePaths[0] });
  quitting = true;
  await stopCore();
  quitting = false;
  await boot();
}

function buildMenu() {
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    {
      label: 'cmnet',
      submenu: [
        { label: 'Папка данных…', click: chooseDataDir },
        { label: 'Открыть папку данных', click: () => shell.openPath(dataDir()) },
        { type: 'separator' },
        { role: 'reload', label: 'Обновить' },
        { role: 'toggleDevTools', label: 'Инструменты разработчика' },
        { type: 'separator' },
        { role: 'quit', label: 'Выход' },
      ],
    },
  ]));
}

async function boot() {
  win.loadFile(path.join(__dirname, 'splash.html'));
  try {
    const url = await startCore();
    win.loadURL(url);
  } catch (err) {
    dialog.showErrorBox('cmnet', `Не удалось запустить ядро: ${err.message}\n\n${coreLog.join('').slice(-2000)}`);
    app.quit();
  }
}

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', () => {
    if (win) {
      if (win.isMinimized()) win.restore();
      win.focus();
    }
  });

  app.whenReady().then(() => {
    buildMenu();
    win = new BrowserWindow({
      width: 1440,
      height: 900,
      minWidth: 1024,
      minHeight: 640,
      backgroundColor: '#0f1115',
      title: 'cmnet',
      webPreferences: {
        preload: path.join(__dirname, 'preload.js'),
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
      },
    });
    // Links out of the app open in the real browser, never inside the shell.
    win.webContents.setWindowOpenHandler(({ url }) => {
      shell.openExternal(url);
      return { action: 'deny' };
    });
    boot();
  });

  app.on('before-quit', async (e) => {
    if (quitting || !core) return;
    e.preventDefault();
    quitting = true;
    await stopCore();
    app.quit();
  });

  app.on('window-all-closed', () => app.quit());
}
