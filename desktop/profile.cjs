const fs = require('node:fs/promises');
const path = require('node:path');
const { randomBytes } = require('node:crypto');

const maxHistoryBytes = 2 * 1024 * 1024;
const keyPattern = /^[a-f0-9]{64}$/;

async function readOptional(file, encoding) {
  try {
    return await fs.readFile(file, encoding);
  } catch (error) {
    if (error.code === 'ENOENT') return undefined;
    throw error;
  }
}

async function writeAtomic(file, contents) {
  const temporary = `${file}.${randomBytes(8).toString('hex')}.tmp`;
  const handle = await fs.open(temporary, 'wx', 0o600);
  try {
    try {
      await handle.writeFile(contents);
      await handle.sync();
    } finally {
      await handle.close();
    }
    await fs.rename(temporary, file);
  } finally {
    await fs.unlink(temporary).catch((error) => {
      if (error.code !== 'ENOENT') throw error;
    });
  }
}

function validWidth(value) {
  return Number.isInteger(value) && value >= 220 && value <= 680;
}

function validateHistory(value) {
  if (!Array.isArray(value) || value.length > 30) {
    throw new TypeError('查询历史格式无效');
  }
  const history = value.map((entry) => {
    const { sql, connName, database, time } = entry || {};
    if ([sql, connName, database, time].some((field) => typeof field !== 'string')) {
      throw new TypeError('查询历史条目格式无效');
    }
    return { sql, connName, database, time };
  });
  if (Buffer.byteLength(JSON.stringify(history)) > maxHistoryBytes) {
    throw new RangeError('查询历史超出保存限制');
  }
  return history;
}

async function openPreferences(profileDir) {
  const file = path.join(profileDir, 'preferences.json');
  let preferences = {};
  const raw = await readOptional(file, 'utf8');
  if (raw && Buffer.byteLength(raw) <= maxHistoryBytes + 256) {
    try {
      const previous = JSON.parse(raw);
      if (validWidth(previous?.sidebarWidth)) preferences.sidebarWidth = previous.sidebarWidth;
      if (previous?.sqlHistory !== undefined) preferences.sqlHistory = validateHistory(previous.sqlHistory);
    } catch {
      // The workbench can start with default preferences if this file is damaged.
    }
  }

  let pending = Promise.resolve();
  function update(change) {
    const write = pending.then(async () => {
      const next = { ...preferences, ...change };
      await writeAtomic(file, JSON.stringify(next));
      preferences = next;
    });
    pending = write.catch(() => {});
    return write;
  }

  return {
    get: () => preferences,
    flush: () => pending,
    setSidebarWidth: (width) => {
      if (!validWidth(width)) throw new RangeError('连接树宽度无效');
      return update({ sidebarWidth: width });
    },
    setSqlHistory: (history) => update({ sqlHistory: validateHistory(history) }),
  };
}

async function openWindowsMasterKey(dataDir, safeStorage) {
  if (!safeStorage.isEncryptionAvailable()) {
    throw new Error('Windows 凭据保护不可用');
  }

  const metaDir = path.join(dataDir, 'meta');
  await fs.mkdir(metaDir, { recursive: true });
  const keyFile = path.join(metaDir, '.master.key.dpapi');
  const legacyFile = path.join(metaDir, '.master.key');
  const encrypted = await readOptional(keyFile);
  const legacy = (await readOptional(legacyFile, 'utf8'))?.trim().toLowerCase();
  if (legacy !== undefined && !keyPattern.test(legacy)) {
    throw new Error('导入的主密钥格式无效');
  }

  let key;
  let importedKey;
  if (encrypted) {
    key = safeStorage.decryptString(encrypted);
    if (!keyPattern.test(key) || (legacy !== undefined && key !== legacy)) {
      throw new Error('主密钥与此用户档案不匹配');
    }
  } else {
    if (legacy === undefined && await readOptional(path.join(metaDir, 'connections.json'))) {
      throw new Error('现有连接配置缺少主密钥');
    }
    key = legacy ?? randomBytes(32).toString('hex');
    const sealed = safeStorage.encryptString(key);
    if (safeStorage.decryptString(sealed) !== key) throw new Error('无法验证 Windows 凭据保护');
    if (legacy === undefined) await writeAtomic(keyFile, sealed);
    else importedKey = sealed;
  }

  return {
    key,
    finishImport: async () => {
      if (importedKey) await writeAtomic(keyFile, importedKey);
      if (legacy !== undefined) await fs.unlink(legacyFile);
    },
  };
}

module.exports = { openPreferences, openWindowsMasterKey };
