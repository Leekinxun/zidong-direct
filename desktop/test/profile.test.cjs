const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const { test } = require('node:test');
const { openPreferences, openWindowsMasterKey } = require('../profile.cjs');

async function createProfile(t) {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'zidong-profile-'));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  return dir;
}

const testSafeStorage = {
  isEncryptionAvailable: () => true,
  encryptString: (value) => Buffer.from(`wrapped:${value}`),
  decryptString: (value) => {
    const text = value.toString();
    if (!text.startsWith('wrapped:')) throw new Error('Invalid wrapped key');
    return text.slice('wrapped:'.length);
  },
};

test('serializes overlapping preference writes and reopens them', async (t) => {
  const dir = await createProfile(t);
  const preferences = await openPreferences(dir);
  const history = [{ sql: 'SELECT 1', connName: '测试', database: 'demo', time: '2026-01-01' }];
  await Promise.all([
    preferences.setSidebarWidth(460),
    preferences.setSqlHistory(history),
    preferences.setSidebarWidth(470),
  ]);

  const reopened = await openPreferences(dir);
  assert.deepEqual(reopened.get(), { sidebarWidth: 470, sqlHistory: history });
  assert.throws(() => reopened.setSidebarWidth(681), RangeError);
  assert.throws(() => reopened.setSqlHistory([{ sql: 'SELECT 1' }]), TypeError);
  assert.throws(() => reopened.setSqlHistory([{ ...history[0], sql: '表'.repeat(800000) }]), RangeError);
});

test('preserves the imported key until backend readiness is confirmed', async (t) => {
  const dir = await createProfile(t);
  const metaDir = path.join(dir, 'meta');
  const oldFile = path.join(metaDir, '.master.key');
  const protectedFile = path.join(metaDir, '.master.key.dpapi');
  const key = 'a'.repeat(64);
  await fs.mkdir(metaDir);
  await fs.writeFile(oldFile, key);

  const opened = await openWindowsMasterKey(dir, testSafeStorage);
  assert.equal(opened.key, key);
  assert.equal(await fs.readFile(oldFile, 'utf8'), key);
  await assert.rejects(fs.access(protectedFile), { code: 'ENOENT' });
  await opened.finishImport();

  await assert.rejects(fs.access(oldFile), { code: 'ENOENT' });
  assert.equal((await openWindowsMasterKey(dir, testSafeStorage)).key, key);
});

for (const dataFile of ['connections.json', 'ssh_hosts.json']) {
  test(`refuses to create a replacement key for ${dataFile} without its key`, async (t) => {
    const dir = await createProfile(t);
    const metaDir = path.join(dir, 'meta');
    await fs.mkdir(metaDir);
    await fs.writeFile(path.join(metaDir, dataFile), '[]');

    await assert.rejects(openWindowsMasterKey(dir, testSafeStorage), /缺少主密钥/);
    await assert.rejects(fs.access(path.join(metaDir, '.master.key.dpapi')), { code: 'ENOENT' });
  });
}
