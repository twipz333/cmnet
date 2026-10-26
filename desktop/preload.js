// Nothing is exposed to the page yet: the UI talks to the Go core over HTTP like
// any browser would. This file exists so a bridge can be added without turning
// on nodeIntegration.
const { contextBridge } = require('electron');
contextBridge.exposeInMainWorld('cmnetDesktop', { version: process.versions.electron });
