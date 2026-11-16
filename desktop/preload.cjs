const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('zidongDesktop', {
  getPreferences: () => ipcRenderer.invoke('preferences:get'),
  setSidebarWidth: (width) => ipcRenderer.invoke('preferences:sidebar-width', width),
  setSqlHistory: (history) => ipcRenderer.invoke('preferences:sql-history', history),
  setFontSize: (preference, size) => ipcRenderer.invoke('preferences:font-size', { preference, size }),
  setTheme: (theme) => ipcRenderer.invoke('preferences:theme', theme),
  onThemeChanged: (callback) => {
    if (typeof callback !== 'function') throw new TypeError('主题监听器无效');
    const listener = (_event, theme) => {
      if (theme === 'dark' || theme === 'light') callback(theme);
    };
    ipcRenderer.on('preferences:theme-changed', listener);
    return () => ipcRenderer.removeListener('preferences:theme-changed', listener);
  },
  openTerminalWindow: () => ipcRenderer.invoke('terminal:open-window'),
  pasteTerminal: () => ipcRenderer.invoke('terminal:paste'),
  openProxiedChrome: (request) => ipcRenderer.invoke('terminal:open-proxied-chrome', request),
});
