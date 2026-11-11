const { execFileSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const desktop = path.resolve(__dirname, '..');
const repo = path.dirname(desktop);
const unpacked = process.argv[2] === '--dir';
const go = process.env.ZIDONG_WIN7_GO || 'go';

if (process.argv.length > (unpacked ? 3 : 2)) {
  throw new Error('Usage: npm run package:win7:x64 -- [--dir]');
}

const goVersion = execFileSync(go, ['version'], { encoding: 'utf8' }).trim();
if (!/^go version go1\.20\.14\b/.test(goVersion)) {
  throw new Error(`Win7 builds require Go 1.20.14. Set ZIDONG_WIN7_GO to its executable. Found: ${goVersion}`);
}

const npmCLI = process.env.npm_execpath;
if (!npmCLI) throw new Error('Run this build via npm run package:win7:x64.');
execFileSync(process.execPath, [npmCLI, '--prefix', 'web', 'run', 'build:desktop'], {
  cwd: repo,
  stdio: 'inherit',
});

const staging = path.join(desktop, '.staging');
fs.mkdirSync(staging, { recursive: true });
execFileSync(go, [
  'build', '-mod=readonly', '-modfile=go.win7.mod', '-trimpath', '-buildvcs=false',
  '-ldflags=-H=windowsgui',
  '-o', path.join(staging, 'zidong-backend.exe'), './cmd/server',
], {
  cwd: path.join(repo, 'backend'),
  env: { ...process.env, GOOS: 'windows', GOARCH: 'amd64', CGO_ENABLED: '0' },
  stdio: 'inherit',
});

const args = [
  require.resolve('electron-builder/cli.js'),
  '--win', '--x64', '--publish', 'never',
];
if (unpacked) args.push('--dir');
execFileSync(process.execPath, args, { cwd: desktop, stdio: 'inherit' });
