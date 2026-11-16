const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('zidongDesktop', {
  getPreferences: () => ipcRenderer.invoke('preferences:get'),
  setSidebarWidth: (width) => ipcRenderer.invoke('preferences:sidebar-width', width),
  setSqlHistory: (history) => ipcRenderer.invoke('preferences:sql-history', history),
  setFontSize: (preference, size) => ipcRenderer.invoke('preferences:font-size', { preference, size }),
  openTerminalWindow: () => ipcRenderer.invoke('terminal:open-window'),
  pasteTerminal: () => ipcRenderer.invoke('terminal:paste'),
  openProxiedChrome: (request) => ipcRenderer.invoke('terminal:open-proxied-chrome', request),
});
