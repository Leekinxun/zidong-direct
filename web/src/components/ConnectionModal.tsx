import React, { useEffect, useState } from 'react';
import { CheckCircle2, Download, FolderOpen, KeyRound, Loader2, Plus, Shield, Upload, X } from 'lucide-react';
import { api, ApiError } from '../api/client';
import type { ConnectionInput, ConnectionView, EngineType, SQLiteFileEntry } from '../types/db';

interface Props {
  editing: ConnectionView | null;
  initialEngine?: EngineType;
  onClose: () => void;
  onSaved: (conn: ConnectionView) => void;
}

const DEFAULT_PORTS: Record<EngineType, number> = {
  mysql: 3306,
  postgres: 5432,
  sqlite: 0,
  sqlserver: 1433,
  oracle: 1521,
  dameng: 5236,
};

const ENGINE_LABELS: Record<EngineType, string> = {
  mysql: 'MySQL / MariaDB',
  postgres: 'PostgreSQL',
  sqlite: 'SQLite',
  sqlserver: 'SQL Server',
  oracle: 'Oracle Database',
  dameng: '达梦数据库 (DM8)',
};

export const ConnectionModal: React.FC<Props> = ({ editing, initialEngine, onClose, onSaved }) => {
  const defaultEngine = editing?.engine || initialEngine || 'mysql';
  const [tab, setTab] = useState<'general' | 'ssh'>('general');
  const [engine, setEngine] = useState<EngineType>(defaultEngine);
  const [name, setName] = useState(editing?.name || '');
  const [host, setHost] = useState(editing?.host || '127.0.0.1');
  const [port, setPort] = useState<number>(editing?.port || DEFAULT_PORTS[defaultEngine]);
  const [database, setDatabase] = useState(editing?.database || '');
  const [username, setUsername] = useState(editing?.username || '');
  const [password, setPassword] = useState('');
  const [sslMode, setSslMode] = useState(editing?.sslMode || 'disable');
  const [trustServerCert, setTrustServerCert] = useState(editing?.trustServerCert ?? true);
  const [oracleMode, setOracleMode] = useState(editing?.oracleMode || 'service');
  const [sqlitePath, setSqlitePath] = useState(editing?.sqlitePath || '');

  // SSH Tunnel
  const [sshEnabled, setSshEnabled] = useState(editing?.ssh.enabled || false);
  const [sshHost, setSshHost] = useState(editing?.ssh.host || '');
  const [sshPort, setSshPort] = useState<number>(editing?.ssh.port || 22);
  const [sshUsername, setSshUsername] = useState(editing?.ssh.username || 'root');
  const [sshAuthType, setSshAuthType] = useState<'password' | 'privateKey'>(editing?.ssh.authType || 'password');
  const [sshPassword, setSshPassword] = useState('');
  const [sshPrivateKey, setSshPrivateKey] = useState('');
  const [sshPassphrase, setSshPassphrase] = useState('');
  const [sshFingerprint, setSshFingerprint] = useState(editing?.ssh.hostKeyFingerprint || '');
  const [sshIgnoreHostKey, setSshIgnoreHostKey] = useState(editing?.ssh.insecureIgnoreHostKey ?? true);

  // SQLite file helper
  const [sqliteFiles, setSqliteFiles] = useState<SQLiteFileEntry[]>([]);
  const [newSqliteName, setNewSqliteName] = useState('');

  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
  const [error, setError] = useState('');

  const loadSqliteFiles = async () => {
    try {
      const res = await api.listSqliteFiles();
      setSqliteFiles(res.files || []);
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    if (engine === 'sqlite') {
      void loadSqliteFiles();
    }
  }, [engine]);

  const handleEngineChange = (nextEngine: EngineType) => {
    setEngine(nextEngine);
    if (!editing) {
      setPort(DEFAULT_PORTS[nextEngine]);
      if (nextEngine === 'sqlite') {
        setSshEnabled(false);
      }
    }
  };

  const buildInput = (): ConnectionInput => ({
    id: editing?.id,
    name: name.trim() || `${ENGINE_LABELS[engine]} (${engine === 'sqlite' ? sqlitePath : host})`,
    engine,
    host: host.trim(),
    port: Number(port) || DEFAULT_PORTS[engine],
    database: database.trim(),
    username: username.trim(),
    password: password || undefined,
    sslMode,
    trustServerCert,
    oracleMode,
    sqlitePath: sqlitePath.trim(),
    ssh: {
      enabled: engine === 'sqlite' ? false : sshEnabled,
      host: sshHost.trim(),
      port: Number(sshPort) || 22,
      username: sshUsername.trim(),
      authType: sshAuthType,
      password: sshPassword || undefined,
      privateKey: sshPrivateKey || undefined,
      passphrase: sshPassphrase || undefined,
      hostKeyFingerprint: sshFingerprint.trim(),
      insecureIgnoreHostKey: sshIgnoreHostKey,
    },
  });

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    setError('');
    try {
      const res = await api.testConnection(buildInput());
      setTestResult({ ok: true, msg: `连接成功！延迟 ${res.latencyMs} ms` });
    } catch (err) {
      setTestResult({ ok: false, msg: err instanceof ApiError ? err.message : '连接失败' });
    } finally {
      setTesting(false);
    }
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError('');
    try {
      const payload = buildInput();
      const res = editing
        ? await api.updateConnection(editing.id, payload)
        : await api.createConnection(payload);
      onSaved(res.connection);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const handleCreateSqlite = async () => {
    if (!newSqliteName.trim()) return;
    try {
      const res = await api.createSqliteFile(newSqliteName.trim());
      setSqlitePath(res.relativePath);
      setNewSqliteName('');
      await loadSqliteFiles();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '创建 SQLite 文件失败');
    }
  };

  const handleUploadSqlite = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      const res = await api.uploadSqliteFile(file);
      setSqlitePath(res.relativePath);
      await loadSqliteFiles();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '上传 SQLite 文件失败');
    }
    e.target.value = '';
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4">
      <div className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-lg border border-slate-700 bg-slate-900 shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-slate-100">
            {editing ? `编辑连接：${editing.name}` : '新建数据库连接'}
          </h2>
          <button type="button" onClick={onClose} className="text-slate-400 hover:text-slate-200">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex border-b border-slate-800 px-5">
          <button
            type="button"
            onClick={() => setTab('general')}
            className={`border-b-2 px-4 py-2 text-xs font-medium ${
              tab === 'general'
                ? 'border-indigo-500 text-indigo-400'
                : 'border-transparent text-slate-400 hover:text-slate-200'
            }`}
          >
            常规连接参数
          </button>
          {engine !== 'sqlite' && (
            <button
              type="button"
              onClick={() => setTab('ssh')}
              className={`flex items-center gap-1.5 border-b-2 px-4 py-2 text-xs font-medium ${
                tab === 'ssh'
                  ? 'border-indigo-500 text-indigo-400'
                  : 'border-transparent text-slate-400 hover:text-slate-200'
              }`}
            >
              <KeyRound className="h-3.5 w-3.5" />
              SSH 隧道 {sshEnabled ? '(已启用)' : ''}
            </button>
          )}
        </div>

        <form onSubmit={handleSave} className="flex-1 overflow-y-auto p-5">
          {tab === 'general' ? (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="mb-1 block text-xs text-slate-300">数据库类型</label>
                  <select
                    value={engine}
                    onChange={(e) => handleEngineChange(e.target.value as EngineType)}
                    className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                  >
                    {(Object.keys(ENGINE_LABELS) as EngineType[]).map((key) => (
                      <option key={key} value={key}>
                        {ENGINE_LABELS[key]}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="mb-1 block text-xs text-slate-300">连接名称</label>
                  <input
                    type="text"
                    placeholder="例如：生产只读库 / 本地测试"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                    required
                  />
                </div>
              </div>

              {engine === 'sqlite' ? (
                <div className="space-y-3 rounded border border-slate-800 bg-slate-950/60 p-3">
                  <div>
                    <label className="mb-1 block text-xs text-slate-300">
                      SQLite 文件路径（容器 SQLITE_DIR 内相对路径）
                    </label>
                    <div className="flex gap-2">
                      <input
                        type="text"
                        placeholder="例如：demo.db 或 uploads/app.sqlite"
                        value={sqlitePath}
                        onChange={(e) => setSqlitePath(e.target.value)}
                        className="flex-1 rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                        required
                      />
                      {sqlitePath && (
                        <a
                          href={api.downloadSqliteUrl(sqlitePath)}
                          className="flex items-center gap-1 rounded border border-slate-700 bg-slate-800 px-2.5 py-1.5 text-xs text-slate-200 hover:bg-slate-700"
                          title="下载此 SQLite 文件"
                        >
                          <Download className="h-3.5 w-3.5" />
                          下载
                        </a>
                      )}
                    </div>
                  </div>

                  <div className="flex flex-wrap items-center gap-2">
                    <div className="flex flex-1 items-center gap-1.5">
                      <input
                        type="text"
                        placeholder="新建空白库名，如 mydata.db"
                        value={newSqliteName}
                        onChange={(e) => setNewSqliteName(e.target.value)}
                        className="flex-1 rounded border border-slate-700 bg-slate-800 px-2.5 py-1 text-xs text-slate-100 outline-none focus:border-indigo-500"
                      />
                      <button
                        type="button"
                        onClick={handleCreateSqlite}
                        className="flex items-center gap-1 rounded border border-slate-700 bg-slate-800 px-2.5 py-1 text-xs text-slate-200 hover:bg-slate-700"
                      >
                        <Plus className="h-3.5 w-3.5" /> 新建库
                      </button>
                    </div>
                    <label className="flex cursor-pointer items-center gap-1 rounded border border-indigo-700 bg-indigo-950/60 px-2.5 py-1 text-xs text-indigo-300 hover:bg-indigo-900/60">
                      <Upload className="h-3.5 w-3.5" />
                      上传 .db 文件
                      <input type="file" accept=".db,.sqlite,.sqlite3" className="hidden" onChange={handleUploadSqlite} />
                    </label>
                  </div>

                  {sqliteFiles.length > 0 && (
                    <div>
                      <div className="mb-1 flex items-center gap-1 text-xs text-slate-400">
                        <FolderOpen className="h-3.5 w-3.5" /> 容器目录已有文件（点击选用）：
                      </div>
                      <div className="max-h-28 overflow-y-auto rounded border border-slate-800 bg-slate-900 p-1">
                        {sqliteFiles.map((f) => (
                          <button
                            key={f.relativePath}
                            type="button"
                            onClick={() => setSqlitePath(f.relativePath)}
                            className={`flex w-full items-center justify-between rounded px-2 py-1 text-left text-xs hover:bg-slate-800 ${
                              sqlitePath === f.relativePath ? 'bg-indigo-950 text-indigo-300' : 'text-slate-300'
                            }`}
                          >
                            <span>{f.relativePath}</span>
                            <span className="text-slate-500">{(f.size / 1024).toFixed(1)} KB</span>
                          </button>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              ) : (
                <>
                  <div className="grid grid-cols-3 gap-3">
                    <div className="col-span-2">
                      <label className="mb-1 block text-xs text-slate-300">主机地址 / IP</label>
                      <input
                        type="text"
                        value={host}
                        onChange={(e) => setHost(e.target.value)}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                        required
                      />
                    </div>
                    <div>
                      <label className="mb-1 block text-xs text-slate-300">端口</label>
                      <input
                        type="number"
                        value={port}
                        onChange={(e) => setPort(Number(e.target.value))}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                        required
                      />
                    </div>
                  </div>

                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="mb-1 block text-xs text-slate-300">
                        {engine === 'oracle'
                          ? oracleMode === 'sid'
                            ? 'Oracle SID'
                            : 'Oracle Service Name'
                          : engine === 'dameng'
                          ? '默认模式 / Schema（可选）'
                          : '默认数据库（可选）'}
                      </label>
                      <input
                        type="text"
                        placeholder={
                          engine === 'oracle'
                            ? '例如：ORCLPDB1 / ORCL'
                            : engine === 'dameng'
                            ? '例如：SYSDBA（留空则按用户名默认模式）'
                            : '留空则连接后浏览全库'
                        }
                        value={database}
                        onChange={(e) => setDatabase(e.target.value)}
                        required={engine === 'oracle'}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                      />
                    </div>

                    {engine === 'oracle' ? (
                      <div>
                        <label className="mb-1 block text-xs text-slate-300">Oracle 连接模式</label>
                        <select
                          value={oracleMode}
                          onChange={(e) => setOracleMode(e.target.value)}
                          className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                        >
                          <option value="service">Service Name</option>
                          <option value="sid">SID</option>
                        </select>
                      </div>
                    ) : (
                      <div>
                        <label className="mb-1 block text-xs text-slate-300">TLS / SSL 模式</label>
                        <select
                          value={sslMode}
                          onChange={(e) => setSslMode(e.target.value)}
                          className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                        >
                          <option value="disable">禁用 (disable)</option>
                          <option value="require">加密 (require)</option>
                          <option value="verify-full">严格校验证书 (verify-full)</option>
                        </select>
                      </div>
                    )}
                  </div>

                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="mb-1 block text-xs text-slate-300">用户名</label>
                      <input
                        type="text"
                        value={username}
                        onChange={(e) => setUsername(e.target.value)}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                      />
                    </div>
                    <div>
                      <label className="mb-1 block text-xs text-slate-300">
                        密码 {editing?.hasPassword ? '（留空则保留原密码）' : ''}
                      </label>
                      <input
                        type="password"
                        placeholder={editing?.hasPassword ? '••••••••' : ''}
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                      />
                    </div>
                  </div>

                  {(engine === 'sqlserver' || engine === 'oracle') && (
                    <label className="flex items-center gap-2 text-xs text-slate-300">
                      <input
                        type="checkbox"
                        checked={trustServerCert}
                        onChange={(e) => setTrustServerCert(e.target.checked)}
                        className="rounded border-slate-600 bg-slate-800"
                      />
                      信任自签名服务器证书 (TrustServerCertificate)
                    </label>
                  )}
                </>
              )}
            </div>
          ) : (
            <div className="space-y-4">
              <label className="flex items-center gap-2 text-sm font-medium text-slate-200">
                <input
                  type="checkbox"
                  checked={sshEnabled}
                  onChange={(e) => setSshEnabled(e.target.checked)}
                  className="rounded border-slate-600 bg-slate-800"
                />
                启用 SSH 隧道转发（通过跳板机连接内网数据库）
              </label>

              {sshEnabled && (
                <div className="space-y-3 rounded border border-slate-800 bg-slate-950/60 p-4">
                  <div className="grid grid-cols-3 gap-3">
                    <div className="col-span-2">
                      <label className="mb-1 block text-xs text-slate-300">SSH 主机</label>
                      <input
                        type="text"
                        value={sshHost}
                        onChange={(e) => setSshHost(e.target.value)}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                        required
                      />
                    </div>
                    <div>
                      <label className="mb-1 block text-xs text-slate-300">SSH 端口</label>
                      <input
                        type="number"
                        value={sshPort}
                        onChange={(e) => setSshPort(Number(e.target.value))}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                        required
                      />
                    </div>
                  </div>

                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="mb-1 block text-xs text-slate-300">SSH 用户名</label>
                      <input
                        type="text"
                        value={sshUsername}
                        onChange={(e) => setSshUsername(e.target.value)}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                        required
                      />
                    </div>
                    <div>
                      <label className="mb-1 block text-xs text-slate-300">认证方式</label>
                      <select
                        value={sshAuthType}
                        onChange={(e) => setSshAuthType(e.target.value as 'password' | 'privateKey')}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                      >
                        <option value="password">密码认证</option>
                        <option value="privateKey">私钥认证 (PEM / OpenSSH)</option>
                      </select>
                    </div>
                  </div>

                  {sshAuthType === 'password' ? (
                    <div>
                      <label className="mb-1 block text-xs text-slate-300">
                        SSH 密码 {editing?.ssh.hasPassword ? '（留空则保留原密码）' : ''}
                      </label>
                      <input
                        type="password"
                        placeholder={editing?.ssh.hasPassword ? '••••••••' : ''}
                        value={sshPassword}
                        onChange={(e) => setSshPassword(e.target.value)}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                      />
                    </div>
                  ) : (
                    <>
                      <div>
                        <label className="mb-1 block text-xs text-slate-300">
                          私钥内容 {editing?.ssh.hasPrivateKey ? '（留空则保留原私钥）' : ''}
                        </label>
                        <textarea
                          rows={4}
                          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                          value={sshPrivateKey}
                          onChange={(e) => setSshPrivateKey(e.target.value)}
                          className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 font-mono text-xs text-slate-100 outline-none focus:border-indigo-500"
                        />
                      </div>
                      <div>
                        <label className="mb-1 block text-xs text-slate-300">
                          私钥口令 Passphrase（若无则留空）
                        </label>
                        <input
                          type="password"
                          placeholder={editing?.ssh.hasPassphrase ? '••••••••' : ''}
                          value={sshPassphrase}
                          onChange={(e) => setSshPassphrase(e.target.value)}
                          className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                        />
                      </div>
                    </>
                  )}

                  <div className="space-y-2 border-t border-slate-800 pt-2">
                    <div>
                      <label className="mb-1 flex items-center gap-1 text-xs text-slate-300">
                        <Shield className="h-3.5 w-3.5 text-slate-400" />
                        SSH 主机公钥 SHA256 指纹（可选，如 SHA256:...）
                      </label>
                      <input
                        type="text"
                        placeholder="留空则根据下方选项决定是否跳过校验"
                        value={sshFingerprint}
                        onChange={(e) => setSshFingerprint(e.target.value)}
                        className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 font-mono text-xs text-slate-100 outline-none focus:border-indigo-500"
                      />
                    </div>
                    <label className="flex items-center gap-2 text-xs text-slate-400">
                      <input
                        type="checkbox"
                        checked={sshIgnoreHostKey}
                        onChange={(e) => setSshIgnoreHostKey(e.target.checked)}
                        className="rounded border-slate-600 bg-slate-800"
                      />
                      未填写指纹时跳过主机公钥校验（仅建议用于受信任内网）
                    </label>
                  </div>
                </div>
              )}
            </div>
          )}

          {testResult && (
            <div
              className={`mt-4 flex items-center gap-2 rounded border px-3 py-2 text-xs ${
                testResult.ok
                  ? 'border-emerald-800 bg-emerald-950/70 text-emerald-200'
                  : 'border-rose-800 bg-rose-950/70 text-rose-200'
              }`}
            >
              {testResult.ok && <CheckCircle2 className="h-4 w-4 shrink-0" />}
              <span>{testResult.msg}</span>
            </div>
          )}

          {error && (
            <div className="mt-4 rounded border border-rose-800 bg-rose-950/70 px-3 py-2 text-xs text-rose-200">
              {error}
            </div>
          )}

          <div className="mt-5 flex items-center justify-between border-t border-white/[0.08] pt-4">
            <button
              type="button"
              onClick={handleTest}
              disabled={testing}
              className="apple-btn-secondary px-3.5 py-1.5"
            >
              {testing && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              测试连接
            </button>

            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={onClose}
                className="apple-btn-secondary px-4 py-1.5"
              >
                取消
              </button>
              <button
                type="submit"
                disabled={saving}
                className="apple-btn-primary px-4 py-1.5"
              >
                {saving ? '保存中...' : '保存连接'}
              </button>
            </div>
          </div>
        </form>
      </div>
    </div>
  );
};
