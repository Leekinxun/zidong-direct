const { execFileSync, spawn } = require('node:child_process');
const { existsSync, mkdirSync } = require('node:fs');
const { constants } = require('node:os');
const path = require('node:path');

const desktop = path.resolve(__dirname, '..');
const repo = path.dirname(desktop);
const staging = path.join(desktop, '.staging');
const developmentDir = path.join(staging, 'dev');
const windows = process.platform === 'win32';
const extension = windows ? '.exe' : '';
const executable = path.join(developmentDir, `zidong-backend${extension}`);
const localGo = path.join(staging, 'go', 'bin', `go${extension}`);
const goOverride = process.env.ZIDONG_DEV_GO;
const go = goOverride ? path.resolve(repo, goOverride) : (existsSync(localGo) ? localGo : 'go');

function start() {
  const npm = process.env.npm_execpath;
  if (!npm) throw new Error('请从仓库根目录运行 npm run dev:electron');

  let electron;
  try {
    electron = require('electron');
    if (!existsSync(electron)) throw new Error('Electron binary missing');
  } catch {
    throw new Error('Electron 依赖未安装完成，请运行 npm ci --prefix desktop');
  }

  try {
    execFileSync(go, ['version'], { stdio: 'ignore' });
  } catch {
    throw new Error('无法运行 Go。请安装 Go 1.20.14 或更新版本，或设置 ZIDONG_DEV_GO 指向 Go 可执行文件。');
  }

  execFileSync(process.execPath, [npm, '--prefix', 'web', 'run', 'build:desktop'], {
    cwd: repo,
    stdio: 'inherit',
  });
  console.log('正在构建本机 Go 后端…');
  mkdirSync(developmentDir, { recursive: true });
  execFileSync(go, [
    'build', '-mod=readonly', '-modfile=go.win7.mod', '-trimpath', '-o', executable, './cmd/server',
  ], {
    cwd: path.join(repo, 'backend'),
    env: {
      ...process.env,
      GOOS: windows ? 'windows' : process.platform,
      GOARCH: process.arch === 'x64' ? 'amd64' : process.arch,
      CGO_ENABLED: '0',
    },
    stdio: 'inherit',
  });

  const child = spawn(electron, [desktop, ...process.argv.slice(2)], {
    cwd: desktop,
    env: { ...process.env, ZIDONG_DESKTOP_BACKEND: executable },
    stdio: 'inherit',
  });
  process.once('SIGINT', () => child.kill('SIGINT'));
  process.once('SIGTERM', () => child.kill('SIGTERM'));
  child.once('error', (error) => {
    console.error(`无法启动 Electron：${error.message}`);
    process.exitCode = 1;
  });
  child.once('exit', (code, signal) => {
    process.exitCode = code ?? 128 + (constants.signals[signal] ?? 0);
  });
}

try {
  start();
} catch (error) {
  console.error(error.message);
  process.exitCode = error.status || 1;
}
