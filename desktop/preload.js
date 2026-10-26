// The only bridge between pages and the shell: the first-run data-folder
// screen asks the shell to scan, browse and choose. The draft UI itself talks to
// the Go core over HTTP and gets nothing from here.
const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('cmnetSetup', {
  scan: () => ipcRenderer.invoke('setup:scan'),
  choose: (dir) => ipcRenderer.invoke('setup:choose', dir),
  browse: () => ipcRenderer.invoke('setup:browse'),
  fresh: () => ipcRenderer.invoke('setup:fresh'),
});
