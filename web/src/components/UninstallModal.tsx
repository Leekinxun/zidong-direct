import React, { useState } from 'react';
import { AlertTriangle, Check, Copy, Loader2, Terminal, Trash2, X } from 'lucide-react';
import { api, ApiError } from '../api/client';
import { clearBrowserPreferences } from '../preferences';

interface Props {
  onClose: () => void;
  onResetCompleted: () => void;
}

export const UninstallModal: React.FC<Props> = ({ onClose, onResetCompleted }) => {
  const [clearConnections, setClearConnections] = useState(true);
  const [clearSqliteFiles, setClearSqliteFiles] = useState(false);
  const [clearBrowserCache, setClearBrowserCache] = useState(true);
  const [confirmed, setConfirmed] = useState(false);
  const [working, setWorking] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [copiedCmd, setCopiedCmd] = useState<string | null>(null);

  const copyCommand = (id: string, cmd: string) => {
    void navigator.clipboard?.writeText(cmd);
    setCopiedCmd(id);
    window.setTimeout(() => setCopiedCmd(null), 1500);
  };

  const handleResetWorkspace = async () => {
    setError('');
    setNotice('');
    setWorking(true);
    try {
      let msgParts: string[] = [];
      if (clearConnections || clearSqliteFiles) {
        const res = await api.resetSystem({
          clearConnections,
          clearSqliteFiles,
          confirmed,
        });
        msgParts.push(`已清理 ${res.clearedConnections} 个连接配置`);
        if (res.clearedSshHosts) {
          msgParts.push(`已清理 ${res.clearedSshHosts} 个独立 SSH 主机`);
        }
        if (clearSqliteFiles) {
          msgParts.push(`已删除 ${res.clearedSqliteFiles} 个 SQLite 文件`);
        }
      }
      if (clearBrowserCache) {
        clearBrowserPreferences();
        msgParts.push('已清空浏览器本地偏好与 SQL 历史缓存');
      }
      setNotice(msgParts.join(' · ') || '清理完成');
      setConfirmed(false);
      onResetCompleted();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '执行清理失败');
    } finally {
      setWorking(false);
    }
  };

  const commands = [
    {
      id: 'script-keep',
      label: 'Linux / macOS 一键卸载容器与镜像（保留 ./data 数据目录）',
      cmd: './uninstall.sh',
    },
    {
      id: 'script-purge',
      label: 'Linux / macOS 彻底卸载（含容器、镜像及 ./data 全部持久化数据）',
      cmd: './uninstall.sh --purge-data --yes',
    },
    {
      id: 'win-script',
      label: 'Windows 一键卸载脚本（CMD / PowerShell）',
      cmd: '.\\uninstall.bat --purge-data',
    },
    {
      id: 'docker-raw',
      label: 'Docker Compose 原生命令卸载（移除容器、网络与本地构建镜像）',
      cmd: 'docker compose down --rmi all --volumes --remove-orphans',
    },
  ];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-label="卸载与清理工作台"
        className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
      >
        <header className="flex items-center justify-between border-b border-slate-800 px-4 py-3">
          <div className="flex items-center gap-2">
            <Trash2 className="h-4 w-4 text-rose-400" />
            <h3 className="text-sm font-semibold text-slate-100">卸载与重置工作台 (Uninstall & Reset)</h3>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭"
            className="rounded p-1 text-slate-400 hover:bg-slate-800 hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="min-h-0 space-y-5 overflow-y-auto p-4 text-xs text-slate-300">
          {/* Section 1: Online Workspace Reset */}
          <section className="space-y-3 rounded-lg border border-slate-800 bg-slate-950/60 p-3.5">
            <div className="flex items-center gap-2 font-semibold text-slate-100">
              <AlertTriangle className="h-4 w-4 text-amber-400" />
              <span>方式一：在线清空工作台数据（恢复出厂初始状态）</span>
            </div>
            <p className="text-slate-400">
              无需登录服务器终端，直接在网页端断开所有活跃数据库连接池与 SSH 会话，并抹除保存的连接配置与本地缓存。
            </p>

            <div className="space-y-2 pt-1">
              <label className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={clearConnections}
                  onChange={(e) => setClearConnections(e.target.checked)}
                />
                <span>清空所有已保存的数据库连接及加密凭据 (`data/meta/connections.json`)</span>
              </label>
              <label className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={clearSqliteFiles}
                  onChange={(e) => setClearSqliteFiles(e.target.checked)}
                />
                <span>同时删除受管目录中的本地 SQLite 数据库文件 (`data/sqlite/*.db`)</span>
              </label>
              <label className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={clearBrowserCache}
                  onChange={(e) => setClearBrowserCache(e.target.checked)}
                />
                <span>清空当前浏览器的 SQL 历史记录与界面布局缓存 (`localStorage`)</span>
              </label>
              <label className="flex items-center gap-2 pt-1 font-medium text-amber-300">
                <input
                  type="checkbox"
                  checked={confirmed}
                  onChange={(e) => setConfirmed(e.target.checked)}
                />
                <span>我已确认清理上述选中数据（操作不可撤销）</span>
              </label>
            </div>

            {notice && (
              <div className="rounded border border-emerald-800 bg-emerald-950/60 px-3 py-2 text-emerald-200">
                {notice}
              </div>
            )}
            {error && (
              <div className="rounded border border-rose-800 bg-rose-950/70 px-3 py-2 text-rose-200">
                {error}
              </div>
            )}

            <div className="flex justify-end pt-1">
              <button
                type="button"
                disabled={
                  working ||
                  !confirmed ||
                  (!clearConnections && !clearSqliteFiles && !clearBrowserCache)
                }
                onClick={() => void handleResetWorkspace()}
                className="inline-flex items-center gap-1.5 rounded bg-rose-600 px-3.5 py-1.5 font-medium text-white hover:bg-rose-500 disabled:opacity-40"
              >
                {working ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Trash2 className="h-3.5 w-3.5" />
                )}
                立即清理并重置工作台
              </button>
            </div>
          </section>

          {/* Section 2: Server / Docker Uninstallation Scripts */}
          <section className="space-y-3 rounded-lg border border-slate-800 bg-slate-950/60 p-3.5">
            <div className="flex items-center gap-2 font-semibold text-slate-100">
              <Terminal className="h-4 w-4 text-sky-400" />
              <span>方式二：卸载容器与服务端程序（Docker / 原生部署）</span>
            </div>
            <p className="text-slate-400">
              项目根目录内置了跨平台一键卸载脚本 `uninstall.sh` 与 `uninstall.bat`，可在宿主机终端一键停止服务、移除容器与镜像：
            </p>

            <div className="space-y-2.5">
              {commands.map((item) => (
                <div key={item.id} className="space-y-1">
                  <div className="text-[11px] text-slate-400">{item.label}</div>
                  <div className="flex items-center justify-between gap-2 rounded border border-slate-800 bg-slate-900 px-3 py-1.5">
                    <code className="truncate font-mono text-xs text-sky-200 select-all">
                      {item.cmd}
                    </code>
                    <button
                      type="button"
                      onClick={() => copyCommand(item.id, item.cmd)}
                      className="inline-flex shrink-0 items-center gap-1 rounded border border-slate-700 bg-slate-800 px-2 py-1 text-[11px] text-slate-200 hover:bg-slate-700"
                    >
                      {copiedCmd === item.id ? (
                        <>
                          <Check className="h-3 w-3 text-emerald-400" />
                          已复制
                        </>
                      ) : (
                        <>
                          <Copy className="h-3 w-3" />
                          复制
                        </>
                      )}
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </section>
        </div>

        <footer className="flex justify-end border-t border-slate-800 px-4 py-3">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-slate-700 px-3 py-1.5 text-xs text-slate-300 hover:bg-slate-800"
          >
            关闭
          </button>
        </footer>
      </div>
    </div>
  );
};
