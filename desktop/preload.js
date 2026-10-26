// The only bridge between pages and the shell.
//  - cmnetSetup: the first-run data-folder screen asks the shell to scan, browse
//    and choose.
//  - cmnetDesktop: the main UI (served by the Go core) learns the app version and
//    data folder and drives auto-update. In a plain browser neither exists and
//    the UI hides what depends on them.
const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('cmnetSetup', {
  scan: () => ipcRenderer.invoke('setup:scan'),
  choose: (dir) => ipcRenderer.invoke('setup:choose', dir),
  browse: () => ipcRenderer.invoke('setup:browse'),
  fresh: () => ipcRenderer.invoke('setup:fresh'),
});

let info = {};
try {
  info = ipcRenderer.sendSync('desktop:info') || {};
} catch {
  info = {};
}

contextBridge.exposeInMainWorld('cmnetDesktop', {
  version: info.version || '',
  dataDir: info.dataDir || '',
  chooseDataDir: () => ipcRenderer.invoke('desktop:choose-data-dir'),
  checkForUpdates: () => ipcRenderer.invoke('update:check'),
  updateStatus: () => ipcRenderer.invoke('update:status'),
  installUpdate: () => ipcRenderer.invoke('update:install'),
  onUpdate: (fn) => {
    ipcRenderer.on('update:status', (_e, st) => fn(st));
  },
});
