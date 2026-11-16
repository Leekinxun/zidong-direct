const { app, BrowserWindow, Menu, dialog, ipcMain, safeStorage, session } = require('electron');
const { execFile } = require('node:child_process');
const { randomUUID } = require('node:crypto');
const fs = require('node:fs/promises');
const path = require('node:path');
const { launchBackend, tokenHeader } = require('./backend.cjs');
const { launchProxiedChrome, closeProxiedChromes } = require('./browser-proxy.cjs');
const { openPreferences, openWindowsMasterKey } = require('./profile.cjs');

app.setName(app.isPackaged ? 'Zidong Direct' : 'Zidong Direct Dev');
app.setAppUserModelId('io.github.leekinxun.zidongdirect');

let activeWindow;
const workbenchWindows = new Map();
let activeBackend;
let preferences;
let quitting = false;
let quitReady = false;

function parseURL(value) {
  try {
    return new URL(value);
  } catch {
    return null;
  }
}

function isWorkbenchPage(value, origin) {
  const url = parseURL(value);
  return url?.protocol === 'http:' && url.origin === origin;
}

function isSqliteDownload(url, origin) {
  return url?.protocol === 'http:' && url.origin === origin && url.pathname === '/api/sqlite/download';
}

function isTerminalStream(url, origin) {
  return url?.protocol === 'ws:' && `http://${url.host}` === origin
    && /^\/api\/terminal\/sessions\/[a-f0-9]{32}\/stream$/.test(url.pathname) && !url.search;
}

function themeBackground(theme) {
  return theme === 'light' ? '#f5f6f8' : '#161618';
}

function downloadFilename(value) {
  let name = value.replace(/[<>:"/\\|?*\x00-\x1f]/g, '_').slice(0, 160).replace(/[ .]+$/g, '');
  if (!name) return 'export';
  if (/^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:\.|$)/i.test(name)) name = `_${name}`;
  return name;
}

function restrictSession(workbenchSession, win, origin, token) {
  const webContentsId = win.webContents.id;
  const ownWindow = (contents) => contents?.id === webContentsId;
  workbenchSession.webRequest.onBeforeRequest((details, callback) => {
    const url = parseURL(details.url);
    const allowed = isWorkbenchPage(details.url, origin)
      || (details.resourceType === 'webSocket' && isTerminalStream(url, origin))
      || (url?.protocol === 'blob:' && url.origin === origin)
      || (url?.protocol === 'data:' && details.resourceType === 'image');
    const otherWindow = details.webContentsId !== undefined && details.webContentsId !== webContentsId;
    callback({ cancel: !allowed || otherWindow });
  });
  workbenchSession.webRequest.onBeforeSendHeaders((details, callback) => {
    if (isWorkbenchPage(details.url, origin)
      || (details.resourceType === 'webSocket' && isTerminalStream(parseURL(details.url), origin))) {
      for (const name of Object.keys(details.requestHeaders)) {
        if (name.toLowerCase() === tokenHeader.toLowerCase()) delete details.requestHeaders[name];
      }
      details.requestHeaders[tokenHeader] = token;
    }
    callback({ requestHeaders: details.requestHeaders });
  });
  workbenchSession.setPermissionCheckHandler((contents, permission, requestingOrigin, details) => (
    permission === 'clipboard-sanitized-write' && ownWindow(contents)
      && details.isMainFrame && isWorkbenchPage(requestingOrigin, origin)
  ));
  workbenchSession.setPermissionRequestHandler((contents, permission, callback, details) => {
    callback(permission === 'clipboard-sanitized-write' && ownWindow(contents)
      && details.isMainFrame && isWorkbenchPage(details.requestingUrl, origin));
  });
  function saveDownload(event, item, contents) {
    const allowed = item.getURLChain().every((value) => {
      const url = parseURL(value);
      return url?.origin === origin && (
        url.protocol === 'blob:' || (isSqliteDownload(url, origin) && item.getMimeType() === 'application/octet-stream')
      );
    });
    if (!ownWindow(contents) || !allowed) {
      event.preventDefault();
      return;
    }
    item.setSaveDialogOptions({
      defaultPath: path.join(app.getPath('downloads'), downloadFilename(item.getFilename())),
    });
    item.once('done', (_event, state) => {
      if (state === 'interrupted' && !quitting && !win.isDestroyed()) {
        void dialog.showMessageBox(win, { type: 'error', message: '文件下载中断' });
      }
    });
  }
  workbenchSession.on('will-download', saveDownload);
  win.once('closed', () => {
    workbenchSession.removeListener('will-download', saveDownload);
    workbenchSession.webRequest.onBeforeRequest(null);
    workbenchSession.webRequest.onBeforeSendHeaders(null);
    workbenchSession.setPermissionCheckHandler(null);
    workbenchSession.setPermissionRequestHandler(null);
  });
}

async function openWindow(origin, token, terminalOnly = false) {
  const workspace = activeWindow;
  if (terminalOnly && (!workspace || workspace.origin !== origin || workspace.token !== token)) {
    throw new Error('本地工作台尚未就绪');
  }
  const workbenchSession = session.fromPartition(`zidong-${randomUUID()}`);
  await workbenchSession.setProxy({ mode: 'direct' });
  if (quitting) return;
  if (terminalOnly && activeWindow !== workspace) throw new Error('本地工作台已关闭');
  const win = new BrowserWindow({
    title: terminalOnly ? 'SSH 终端 · 紫东直连' : '紫东直连',
    show: false,
    width: terminalOnly ? 1024 : 1240,
    height: terminalOnly ? 700 : 820,
    minWidth: terminalOnly ? 520 : 860,
    minHeight: terminalOnly ? 400 : 580,
    backgroundColor: themeBackground(preferences?.get().theme),
    webPreferences: {
      session: workbenchSession,
      preload: path.join(__dirname, 'preload.cjs'),
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
      devTools: !app.isPackaged,
    },
  });
  const record = { win, origin, token };
  const contentsId = win.webContents.id;
  workbenchWindows.set(contentsId, record);
  win.once('closed', () => workbenchWindows.delete(contentsId));
  if (terminalOnly) {
    win.on('page-title-updated', (event) => event.preventDefault());
  } else {
    activeWindow = record;
  }
  restrictSession(workbenchSession, win, origin, token);
  win.webContents.setWindowOpenHandler(() => ({ action: 'deny' }));
  const allowNavigation = (event, url) => {
    if (!isWorkbenchPage(url, origin)) event.preventDefault();
  };
  win.webContents.on('will-navigate', (event, url) => {
    if (isSqliteDownload(parseURL(url), origin)) {
      event.preventDefault();
      win.webContents.downloadURL(url);
    } else {
      allowNavigation(event, url);
    }
  });
  win.webContents.on('will-redirect', allowNavigation);
  win.webContents.on('will-attach-webview', (event) => event.preventDefault());
  win.webContents.on('before-input-event', (event, input) => {
    if (input.type !== 'keyDown' || !input.shift || input.alt || input.isAutoRepeat) return;
    const key = input.key.toLowerCase();
    const pasteModifier = (input.control && !input.meta)
      || (process.platform === 'darwin' && input.meta && !input.control);
    if (pasteModifier && key === 'v') {
      event.preventDefault();
      win.webContents.paste();
    } else if (process.platform === 'darwin' && input.control && !input.meta && key === 'n') {
      event.preventDefault();
      void openTerminalWindowFromMenu();
    }
  });
  if (!terminalOnly) {
    win.on('close', (event) => {
      if (!quitReady) {
        event.preventDefault();
        app.quit();
      }
    });
  }
  const rendererGone = new Promise((resolve) => {
    win.webContents.once('render-process-gone', (_event, { reason }) => {
      if (terminalOnly && !win.isDestroyed()) win.destroy();
      resolve(`工作台窗口已退出（${reason}）`);
    });
  });
  const url = new URL('/', origin);
  if (terminalOnly) url.searchParams.set('workspace', 'terminal');
  try {
    await win.loadURL(url.href);
    if (!quitting && !win.isDestroyed()) win.show();
  } catch (error) {
    if (!win.isDestroyed()) win.destroy();
    throw error;
  }
  return { rendererGone };
}

async function openTerminalWindow() {
  if (quitting) return;
  if (!activeWindow || activeWindow.win.isDestroyed()) throw new Error('本地工作台尚未就绪');
  const { origin, token } = activeWindow;
  await openWindow(origin, token, true);
}

async function openTerminalWindowFromMenu() {
  try {
    await openTerminalWindow();
  } catch (error) {
    if (!quitting) dialog.showErrorBox('无法打开 SSH 终端窗口', error.message);
  }
}

function registerWorkbenchHandlers() {
  function fromWorkbench(handler) {
    return (event, value) => {
      const frame = event.senderFrame;
      const workspace = workbenchWindows.get(event.sender.id);
      if (quitting || !activeWindow || !workspace || workspace.origin !== activeWindow.origin
        || workspace.token !== activeWindow.token || !frame || event.sender !== workspace.win.webContents
        || frame !== event.sender.mainFrame || !isWorkbenchPage(frame.url, workspace.origin)) {
        throw new Error('请求来源无效');
      }
      return handler(value, workspace);
    };
  }
  ipcMain.handle('preferences:get', fromWorkbench(() => preferences.get()));
  ipcMain.handle('preferences:sidebar-width', fromWorkbench((width) => preferences.setSidebarWidth(width)));
  ipcMain.handle('preferences:sql-history', fromWorkbench((history) => preferences.setSqlHistory(history)));
  ipcMain.handle('preferences:font-size', fromWorkbench(({ preference, size } = {}) => preferences.setFontSize(preference, size)));
  ipcMain.handle('preferences:theme', fromWorkbench(async (theme) => {
    await preferences.setTheme(theme);
    for (const { win } of workbenchWindows.values()) {
      if (win.isDestroyed() || win.webContents.isDestroyed()) continue;
      win.setBackgroundColor(themeBackground(theme));
      win.webContents.send('preferences:theme-changed', theme);
    }
  }));
  ipcMain.handle('terminal:open-window', fromWorkbench(openTerminalWindow));
  ipcMain.handle('terminal:paste', fromWorkbench((_value, { win }) => {
    if (!win.isFocused()) throw new Error('请先聚焦终端窗口');
    win.webContents.paste();
  }));
  ipcMain.handle('terminal:open-proxied-chrome', fromWorkbench((request, { origin, token }) =>
    launchProxiedChrome({ request, origin, token }),
  ));
}

function setMenu() {
  const fileItems = [{
    label: '新终端窗口',
    accelerator: 'CommandOrControl+Shift+N',
    click: () => void openTerminalWindowFromMenu(),
  }];
  if (process.platform === 'win32' && app.isPackaged) {
    fileItems.push({ type: 'separator' }, {
      label: '打开卸载面板…',
      click: () => {
        const controlPanel = path.join(process.env.SystemRoot || 'C:\\Windows', 'System32', 'control.exe');
        execFile(controlPanel, ['appwiz.cpl'], { windowsHide: true }, (error) => {
          if (error) dialog.showErrorBox('无法打开卸载面板', '请从 Windows 控制面板的“程序和功能”卸载紫东直连。');
        });
      },
    });
  }
  fileItems.push({ type: 'separator' }, { label: '退出', role: 'quit' });
  const editingItems = [
    { label: '撤销', role: 'undo' },
    { label: '重做', role: 'redo' },
    { type: 'separator' },
    { label: '剪切', role: 'cut' },
    { label: '复制', role: 'copy' },
    { label: '粘贴', role: 'paste' },
    { label: '全选', role: 'selectAll' },
  ];
  if (process.platform === 'win32') {
    // Let the focused editor or PTY handle Ctrl key combinations.
    for (const item of editingItems) {
      if (item.role) item.registerAccelerator = false;
    }
  }
  const menu = [
    { label: '文件', submenu: fileItems },
    { label: '编辑', submenu: editingItems },
    { label: '窗口', submenu: [
      { label: '最小化', role: 'minimize' },
      { label: '切换全屏', role: 'togglefullscreen' },
    ] },
  ];
  Menu.setApplicationMenu(Menu.buildFromTemplate(menu));
}

async function runWorkbench() {
  setMenu();
  registerWorkbenchHandlers();
  const profileDir = path.join(app.getPath('userData'), 'workspace');
  const dataDir = path.join(profileDir, 'data');
  const extension = process.platform === 'win32' ? '.exe' : '';
  const executable = app.isPackaged
    ? path.join(process.resourcesPath, `zidong-backend${extension}`)
    : path.resolve(process.env.ZIDONG_DESKTOP_BACKEND || path.join(__dirname, '.staging', `zidong-backend${extension}`));
  const staticDir = app.isPackaged
    ? path.join(process.resourcesPath, 'web')
    : path.join(__dirname, '..', 'web', 'dist-desktop');

  while (!quitting) {
    let failure;
    try {
      await fs.mkdir(profileDir, { recursive: true, mode: 0o700 });
      preferences ??= await openPreferences(profileDir);
      const storage = process.platform === 'win32' ? await openWindowsMasterKey(dataDir, safeStorage) : undefined;
      await fs.access(path.join(staticDir, 'index.html'));
      if (quitting) return;
      activeBackend = launchBackend({ executable, dataDir, staticDir, masterKey: storage?.key });
      const origin = await activeBackend.ready;
      if (quitting) return;
      await storage?.finishImport();
      if (quitting) return;
      const window = await openWindow(origin, activeBackend.token);
      if (quitting) return;
      failure = await Promise.race([activeBackend.exited, window.rendererGone]);
    } catch (error) {
      failure = error.message;
    }

    if (quitting) return;
    activeWindow = undefined;
    for (const { win } of workbenchWindows.values()) {
      if (!win.isDestroyed()) win.destroy();
    }
    await closeProxiedChromes();
    await activeBackend?.stop();
    activeBackend = undefined;
    if (quitting) return;
    const { response } = await dialog.showMessageBox({
      type: 'error',
      title: '紫东直连',
      message: '无法连接本地工作台',
      detail: failure,
      buttons: ['重试', '退出'],
      cancelId: 1,
    });
    if (response !== 0) app.quit();
  }
}

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', () => {
    const win = activeWindow?.win;
    if (!win || win.isDestroyed()) return;
    if (win.isMinimized()) win.restore();
    win.focus();
  });
  app.on('window-all-closed', () => {});
  app.on('before-quit', (event) => {
    if (quitReady) return;
    event.preventDefault();
    if (quitting) return;
    quitting = true;
    Promise.allSettled([
      closeProxiedChromes().finally(() => activeBackend?.stop()),
      preferences?.flush(),
    ]).then(() => {
      quitReady = true;
      app.quit();
    });
  });
  app.whenReady().then(runWorkbench).catch((error) => {
    dialog.showErrorBox('紫东直连', error.message);
    app.quit();
  });
}
