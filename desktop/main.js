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

const { app, BrowserWindow, Menu, dialog, shell, ipcMain } = require('electron');
const { scan, describe } = require('./datadirs');
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

const defaultDataDir = () => path.join(app.getPath('userData'), 'data');

function dataDir() {
  const dir = loadSettings().dataDir || defaultDataDir();
  fs.mkdirSync(dir, { recursive: true });
  return dir;
}

// needsSetup: nobody has chosen a data folder yet and the default one holds no
// trained model. Then the first screen is the data-folder picker, not an empty
// advisor. (Checking for cmnet.db alone is not enough: the core creates an empty
// one the first time it starts.)
function needsSetup() {
  const s = loadSettings();
  if (s.dataDir || s.setupDone) return false;
  return !fs.existsSync(path.join(defaultDataDir(), 'bundle.json'));
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

// useDataDir switches the core to dir: stop it, remember the choice, start again.
async function useDataDir(dir) {
  saveSettings({ ...loadSettings(), dataDir: dir, setupDone: true });
  quitting = true;
  await stopCore();
  quitting = false;
  await boot();
}

function showSetup() {
  win.loadFile(path.join(__dirname, 'welcome.html'));
}

ipcMain.handle('setup:scan', () => scan({ extra: [path.dirname(process.execPath), dataDir()] }));
ipcMain.handle('setup:choose', async (_e, dir) => {
  if (!dir || !(await describe(dir))) return { error: 'В этой папке нет cmnet.db' };
  await useDataDir(dir);
  return {};
});
ipcMain.handle('setup:browse', async () => {
  const res = await dialog.showOpenDialog(win, {
    title: 'Где лежит cmnet.db?',
    properties: ['openFile'],
    filters: [{ name: 'База cmnet', extensions: ['db'] }],
  });
  if (res.canceled || !res.filePaths[0]) return {};
  const file = res.filePaths[0];
  if (path.basename(file).toLowerCase() !== 'cmnet.db') return { error: 'Нужен файл cmnet.db' };
  await useDataDir(path.dirname(file));
  return {};
});
ipcMain.handle('setup:fresh', async () => {
  await useDataDir(defaultDataDir());
  return {};
});

function buildMenu() {
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    {
      label: 'cmnet',
      submenu: [
        { label: 'Папка данных…', click: showSetup },
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
  if (needsSetup()) {
    showSetup();
    return;
  }
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
