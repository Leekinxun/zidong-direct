const { spawn } = require('node:child_process');
const { randomBytes } = require('node:crypto');
const http = require('node:http');
const path = require('node:path');

const tokenHeader = 'X-Zidong-Desktop-Token';
const startupErrors = {
  bootstrap: '本地服务启动协议无效',
  configuration: '本地服务配置或界面资源无效',
  profile: '无法读取本地档案。请检查目录权限和迁移密钥',
  listen: '无法监听本机回环端口',
};

function waitForPort(child, exited) {
  return new Promise((resolve, reject) => {
    let buffer = '';
    const timeout = setTimeout(() => finish(new Error('本地服务启动超时')), 15000);

    function finish(error, port) {
      clearTimeout(timeout);
      child.stdout.off('data', receive);
      if (error) reject(error);
      else resolve(port);
    }

    function receive(chunk) {
      buffer += chunk;
      if (buffer.length > 4096) {
        finish(new Error('本地服务返回了过大的就绪消息'));
        return;
      }
      const end = buffer.indexOf('\n');
      if (end < 0) return;

      try {
        const { type, port, code } = JSON.parse(buffer.slice(0, end));
        if (type === 'error' && Object.hasOwn(startupErrors, code)) {
          finish(new Error(startupErrors[code]));
          return;
        }
        if (type !== 'ready' || !Number.isInteger(port) || port < 1 || port > 65535) {
          throw new Error('invalid readiness message');
        }
        finish(null, port);
      } catch {
        finish(new Error('本地服务返回了无效的就绪消息'));
      }
    }

    child.stdout.setEncoding('utf8').on('data', receive);
    exited.then((reason) => finish(new Error(reason)));
  });
}

function checkHealth(origin, token) {
  return new Promise((resolve, reject) => {
    const request = http.get(`${origin}/api/health`, {
      agent: false,
      headers: { [tokenHeader]: token },
      timeout: 5000,
    }, (response) => {
      response.resume();
      if (response.statusCode === 200) resolve();
      else reject(new Error(`本地服务健康检查失败（HTTP ${response.statusCode}）`));
    });
    request.on('timeout', () => request.destroy(new Error('本地服务健康检查超时')));
    request.on('error', reject);
  });
}

function launchBackend({ executable, dataDir, staticDir, masterKey }) {
  const token = randomBytes(32).toString('hex');
  const child = spawn(executable, [], {
    cwd: path.dirname(executable),
    env: {
      ...process.env,
      ZIDONG_DESKTOP: '1',
      DATA_DIR: dataDir,
      SQLITE_DIR: path.join(dataDir, 'sqlite'),
      STATIC_DIR: staticDir,
      APP_SECRET_KEY: '',
      APP_ADMIN_PASSWORD: '',
      ALLOW_ARBITRARY_SQLITE_PATHS: 'false',
    },
    stdio: ['pipe', 'pipe', 'pipe'],
    windowsHide: true,
  });

  child.stdin.on('error', () => {});
  child.stderr.resume();

  let ended = false;
  const exited = new Promise((resolve) => {
    let spawnError;
    child.once('error', (error) => {
      spawnError = `无法启动本地服务（${error.code || '启动失败'}）`;
    });
    child.once('close', (code, signal) => {
      ended = true;
      resolve(spawnError ?? `本地服务已退出（${signal || code}）`);
    });
  });

  const ready = waitForPort(child, exited).then(async (port) => {
    const origin = `http://127.0.0.1:${port}`;
    await checkHealth(origin, token);
    return origin;
  });
  child.stdin.write(`${JSON.stringify({ token, masterKey })}\n`);

  let stopping;
  function stop() {
    if (!stopping) {
      stopping = (async () => {
        if (ended) return;
        child.stdin.end('shutdown\n');
        const timeout = setTimeout(() => child.kill(), 12000);
        await exited;
        clearTimeout(timeout);
      })();
    }
    return stopping;
  }

  return { token, ready, exited, stop };
}

module.exports = { launchBackend, tokenHeader };
