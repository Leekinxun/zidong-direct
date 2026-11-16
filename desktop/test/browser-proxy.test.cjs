const assert = require('node:assert/strict');
const { once } = require('node:events');
const fs = require('node:fs/promises');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { test } = require('node:test');
const { setTimeout: delay } = require('node:timers/promises');
const { closeProxiedChromes, launchProxiedChrome } = require('../browser-proxy.cjs');
const { tokenHeader } = require('../backend.cjs');

test('proxy Chrome receives an isolated profile and never receives SSH credentials', {
  skip: process.platform === 'win32',
}, async (t) => {
  const folder = await fs.mkdtemp(path.join(os.tmpdir(), 'zidong-proxy-fixture-'));
  const chrome = path.join(folder, 'fixture-chrome');
  const argsFile = path.join(folder, 'args');
  const requestTarget = { ssh: { host: 'fixture.test', username: 'user', password: 'fixture-password' } };
  const id = 'a'.repeat(32);
  const proxyPort = 53123;
  const token = 'local-backend-token';
  const seen = [];
  const backend = http.createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    seen.push({
      method: request.method,
      url: request.url,
      token: request.headers[tokenHeader.toLowerCase()],
      body: Buffer.concat(chunks).toString(),
    });
    if (request.method === 'POST') {
      response.writeHead(201, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ proxyId: id, port: proxyPort }));
    } else {
      response.writeHead(204).end();
    }
  });
  backend.listen(0, '127.0.0.1');
  await once(backend, 'listening');
  const origin = `http://127.0.0.1:${backend.address().port}`;
  const previousChrome = process.env.ZIDONG_CHROME_PATH;
  const previousArgs = process.env.ZIDONG_FIXTURE_CHROME_ARGS;
  process.env.ZIDONG_CHROME_PATH = chrome;
  process.env.ZIDONG_FIXTURE_CHROME_ARGS = argsFile;
  t.after(async () => {
    await closeProxiedChromes();
    await new Promise((resolve) => backend.close(resolve));
    if (previousChrome === undefined) delete process.env.ZIDONG_CHROME_PATH;
    else process.env.ZIDONG_CHROME_PATH = previousChrome;
    if (previousArgs === undefined) delete process.env.ZIDONG_FIXTURE_CHROME_ARGS;
    else process.env.ZIDONG_FIXTURE_CHROME_ARGS = previousArgs;
    await fs.rm(folder, { recursive: true, force: true });
  });

  await assert.rejects(launchProxiedChrome({
    request: { target: requestTarget, url: 'file:///tmp/test' }, origin, token,
  }), /HTTP 或 HTTPS/);
  assert.deepEqual(seen, []);

  await fs.writeFile(chrome, '#!/bin/sh\nprintf "%s\\n" "$@" > "$ZIDONG_FIXTURE_CHROME_ARGS"\nexec /bin/sleep 20\n', { mode: 0o700 });
  // URL loopback also uses the proxy in this separate Chrome profile.
  await launchProxiedChrome({ request: { target: requestTarget, url: 'http://127.0.0.1:8080/' }, origin, token });
  let argv;
  for (let attempt = 0; attempt < 100; attempt++) {
    try { argv = await fs.readFile(argsFile, 'utf8'); } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
    if (argv) break;
    await delay(10);
  }
  assert.ok(argv);
  assert.match(argv, new RegExp(`--proxy-server=socks5://127\\.0\\.0\\.1:${proxyPort}\\n`));
  assert.match(argv, /--proxy-bypass-list=<-loopback>/);
  assert.match(argv, /http:\/\/127\.0\.0\.1:8080\/\n$/);
  assert.doesNotMatch(argv, /fixture-password|local-backend-token/);
  const profile = argv.match(/^--user-data-dir=(.+)$/m)[1];
  await fs.access(profile);

  await closeProxiedChromes();
  assert.deepEqual(seen, [
    { method: 'POST', url: '/api/browser-proxies', token, body: JSON.stringify(requestTarget) },
    { method: 'DELETE', url: `/api/browser-proxies/${id}`, token, body: '' },
  ]);
  await assert.rejects(fs.access(profile), { code: 'ENOENT' });
});
