const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('zidongDesktop', {
  getPreferences: () => ipcRenderer.invoke('preferences:get'),
  setSidebarWidth: (width) => ipcRenderer.invoke('preferences:sidebar-width', width),
  setSqlHistory: (history) => ipcRenderer.invoke('preferences:sql-history', history),
});
