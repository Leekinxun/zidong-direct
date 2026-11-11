const { app, BrowserWindow, Menu, dialog, ipcMain, safeStorage, session } = require('electron');
const { execFile } = require('node:child_process');
const { randomUUID } = require('node:crypto');
const fs = require('node:fs/promises');
const path = require('node:path');
const { launchBackend, tokenHeader } = require('./backend.cjs');
const { openPreferences, openWindowsMasterKey } = require('./profile.cjs');

app.setName(app.isPackaged ? 'Zidong Direct' : 'Zidong Direct Dev');
app.setAppUserModelId('io.github.leekinxun.zidongdirect');

let activeWindow;
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
      || (url?.protocol === 'blob:' && url.origin === origin)
      || (url?.protocol === 'data:' && details.resourceType === 'image');
    const otherWindow = details.webContentsId !== undefined && details.webContentsId !== webContentsId;
    callback({ cancel: !allowed || otherWindow });
  });
  workbenchSession.webRequest.onBeforeSendHeaders((details, callback) => {
    if (isWorkbenchPage(details.url, origin)) {
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

async function openWindow(origin, token) {
  const workbenchSession = session.fromPartition(`zidong-${randomUUID()}`);
  await workbenchSession.setProxy({ mode: 'direct' });
  if (quitting) return;
  const win = new BrowserWindow({
    title: '紫东直连',
    show: false,
    width: 1240,
    height: 820,
    minWidth: 860,
    minHeight: 580,
    backgroundColor: '#161618',
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
  activeWindow = { win, origin };
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
    if (input.type === 'keyDown' && input.control && input.shift && !input.alt && !input.meta
      && !input.isAutoRepeat && input.key.toLowerCase() === 'v') {
      event.preventDefault();
      win.webContents.paste();
    }
  });
  win.on('close', (event) => {
    if (!quitReady) {
      event.preventDefault();
      app.quit();
    }
  });
  const rendererGone = new Promise((resolve) => {
    win.webContents.once('render-process-gone', (_event, { reason }) => resolve(`工作台窗口已退出（${reason}）`));
  });
  await win.loadURL(origin);
  if (!quitting) win.show();
  return { rendererGone };
}

function registerPreferences() {
  function fromWorkbench(handler) {
    return (event, value) => {
      const frame = event.senderFrame;
      if (quitting || !activeWindow || !frame || event.sender !== activeWindow.win.webContents
        || frame !== event.sender.mainFrame || !isWorkbenchPage(frame.url, activeWindow.origin)) {
        throw new Error('请求来源无效');
      }
      return handler(value);
    };
  }
  ipcMain.handle('preferences:get', fromWorkbench(() => preferences.get()));
  ipcMain.handle('preferences:sidebar-width', fromWorkbench((width) => preferences.setSidebarWidth(width)));
  ipcMain.handle('preferences:sql-history', fromWorkbench((history) => preferences.setSqlHistory(history)));
}

function setMenu() {
  const fileItems = [];
  if (process.platform === 'win32' && app.isPackaged) {
    fileItems.push({
      label: '打开卸载面板…',
      click: () => {
        const controlPanel = path.join(process.env.SystemRoot || 'C:\\Windows', 'System32', 'control.exe');
        execFile(controlPanel, ['appwiz.cpl'], { windowsHide: true }, (error) => {
          if (error) dialog.showErrorBox('无法打开卸载面板', '请从 Windows 控制面板的“程序和功能”卸载紫东直连。');
        });
      },
    }, { type: 'separator' });
  }
  fileItems.push({ label: '退出', role: 'quit' });
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
  registerPreferences();
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
    activeWindow?.win.destroy();
    activeWindow = undefined;
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
    Promise.allSettled([activeBackend?.stop(), preferences?.flush()]).then(() => {
      quitReady = true;
      app.quit();
    });
  });
  app.whenReady().then(runWorkbench).catch((error) => {
    dialog.showErrorBox('紫东直连', error.message);
    app.quit();
  });
}
