const { spawn } = require('node:child_process');
const fs = require('node:fs/promises');
const { constants } = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { tokenHeader } = require('./backend.cjs');

const sessions = new Set();
const proxyRoute = '/api/browser-proxies';

function browserURL(raw) {
  if (raw === undefined || raw === '') return 'about:blank';
  try {
    if (typeof raw !== 'string' || raw.length > 4096) throw new Error();
    const url = new URL(raw);
    if ((url.protocol !== 'http:' && url.protocol !== 'https:') || url.username || url.password) throw new Error();
    return url.href;
  } catch {
    throw new Error('请输入不含账号密码的 HTTP 或 HTTPS 网址');
  }
}

function chromeCandidates(platform, env) {
  if (env.ZIDONG_CHROME_PATH) {
    if (!path.isAbsolute(env.ZIDONG_CHROME_PATH)) throw new Error('ZIDONG_CHROME_PATH 必须是 Chrome 可执行文件的绝对路径');
    return [env.ZIDONG_CHROME_PATH];
  }
  if (platform === 'win32') {
    return [env.LOCALAPPDATA, env.ProgramFiles, env['ProgramFiles(x86)'], env.ProgramW6432]
      .filter(Boolean)
      .map((base) => path.join(base, 'Google', 'Chrome', 'Application', 'chrome.exe'));
  }
  if (platform === 'darwin') {
    return ['/Applications', path.join(os.homedir(), 'Applications')]
      .map((base) => path.join(base, 'Google Chrome.app', 'Contents', 'MacOS', 'Google Chrome'));
  }
  const binaries = ['google-chrome', 'google-chrome-stable', 'chromium', 'chromium-browser'];
  return ['/usr/local/bin', '/usr/bin', '/opt/google/chrome', ...(env.PATH || '').split(path.delimiter)]
    .filter((dir) => path.isAbsolute(dir))
    .flatMap((dir) => binaries.map((binary) => path.join(dir, binary)));
}

async function findChrome() {
  for (const candidate of new Set(chromeCandidates(process.platform, process.env))) {
    try {
      if (!(await fs.stat(candidate)).isFile()) continue;
      await fs.access(candidate, constants.X_OK);
      return candidate;
    } catch {
      // Try the next standard install location.
    }
  }
  throw new Error('找不到 Chrome。请安装 Chrome，或用 ZIDONG_CHROME_PATH 指定可执行文件。');
}

function proxyRequest(origin, token, method, route, payload, signal) {
  return new Promise((resolve, reject) => {
    const body = payload === undefined ? undefined : JSON.stringify(payload);
    if (body !== undefined && Buffer.byteLength(body) > 64 * 1024) {
      reject(new Error('SSH 连接参数过长'));
      return;
    }
    const headers = { [tokenHeader]: token };
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
      headers['Content-Length'] = Buffer.byteLength(body);
    }
    const request = http.request(new URL(route, origin), {
      method, headers, signal, agent: false, timeout: method === 'DELETE' ? 3000 : 20000,
    }, (response) => {
      let data = '';
      response.setEncoding('utf8');
      response.on('data', (chunk) => {
        data += chunk;
        if (data.length > 16 * 1024) request.destroy(new Error('无效的本地代理响应'));
      });
      response.on('error', reject);
      response.on('end', () => {
        let detail;
        try { detail = data ? JSON.parse(data) : undefined; } catch {
          reject(new Error('无效的本地代理响应'));
          return;
        }
        if (response.statusCode >= 200 && response.statusCode < 300) resolve(detail);
        else reject(new Error(detail?.message || '本地代理请求失败'));
      });
    });
    request.on('timeout', () => request.destroy(new Error('本地代理请求超时')));
    request.on('error', reject);
    request.end(body);
  });
}

function proxyChromeArguments(port, profile, url) {
  return [
    `--user-data-dir=${profile}`,
    `--proxy-server=socks5://127.0.0.1:${port}`,
    // Loopback in this isolated browser should refer to the SSH host.
    '--proxy-bypass-list=<-loopback>',
    '--disable-background-networking',
    '--disable-quic',
    '--force-webrtc-ip-handling-policy=disable_non_proxied_udp',
    '--no-first-run',
    '--no-default-browser-check',
    '--new-window',
    url,
  ];
}

async function launchProxiedChrome({ request, origin, token }) {
  const url = browserURL(request?.url);
  const target = request?.target;
  if (!target || typeof target !== 'object' || Array.isArray(target)) throw new Error('缺少 SSH 连接来源');
  if (sessions.size >= 8) throw new Error('浏览器代理已达 8 个，请关闭部分代理 Chrome 窗口');

  const abort = new AbortController();
  let finish;
  const finished = new Promise((resolve) => { finish = resolve; });
  const session = { abort, finished, child: null };
  sessions.add(session);
  let profile;
  let proxyId;
  const cleanup = async () => {
    try {
      if (proxyId) await proxyRequest(origin, token, 'DELETE', `${proxyRoute}/${proxyId}`).catch(() => {});
      if (profile) await fs.rm(profile, { recursive: true, force: true, maxRetries: 2 }).catch(() => {});
    } finally {
      sessions.delete(session);
      finish();
    }
  };

  try {
    const chrome = await findChrome();
    if (abort.signal.aborted) throw new Error('启动代理浏览器已取消');
    profile = await fs.mkdtemp(path.join(os.tmpdir(), 'zidong-chrome-'));
    const proxy = await proxyRequest(origin, token, 'POST', proxyRoute, target, abort.signal);
    if (typeof proxy?.proxyId !== 'string' || !/^[a-f0-9]{32}$/.test(proxy.proxyId)) {
      throw new Error('无效的浏览器代理编号');
    }
    proxyId = proxy.proxyId;
    if (!Number.isInteger(proxy.port) || proxy.port < 1 || proxy.port > 65535) {
      throw new Error('无效的本地代理端口');
    }
    if (abort.signal.aborted) throw new Error('启动代理浏览器已取消');
    const child = spawn(chrome, proxyChromeArguments(proxy.port, profile, url), {
      stdio: 'ignore',
      windowsHide: false,
    });
    session.child = child;
    child.once('close', () => void cleanup());
    await new Promise((resolve, reject) => {
      child.once('spawn', resolve);
      child.on('error', () => reject(new Error('无法启动 Chrome')));
    });
  } catch (error) {
    if (session.child) {
      await finished;
    } else {
      await cleanup();
    }
    throw error;
  }
}

async function closeProxiedChromes() {
  const active = Array.from(sessions);
  for (const session of active) {
    session.abort.abort();
    session.child?.kill();
  }
  const timer = setTimeout(() => {
    for (const session of active) {
      if (sessions.has(session)) session.child?.kill('SIGKILL');
    }
  }, 2500);
  try {
    await Promise.all(active.map(({ finished }) => finished));
  } finally {
    clearTimeout(timer);
  }
}

module.exports = { launchProxiedChrome, closeProxiedChromes };
