import { Plus, Server, TerminalSquare, Trash2 } from 'lucide-react';
import type { ConnectionView, SSHHostView } from '../types/db';

interface Props {
  connections: ConnectionView[];
  hosts: SSHHostView[];
  sessions: { id: string; title: string }[];
  activeTabId: string;
  error?: string;
  onNew: () => void;
  onOpenHost: (host: SSHHostView) => void;
  onDeleteHost: (host: SSHHostView) => void;
  onOpenSaved: (connection: ConnectionView) => void;
  onSelectSession: (id: string) => void;
}

const row = 'flex w-full min-w-0 items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-[12.5px] focus-visible:outline focus-visible:outline-1 focus-visible:outline-[#52a9ff]';

export function SSHSidebar({
  connections, hosts, sessions, activeTabId, error, onNew, onOpenHost, onDeleteHost, onOpenSaved, onSelectSession,
}: Props) {
  const savedConnections = connections.filter((connection) => connection.ssh.enabled);

  return (
    <section aria-label="SSH 导航">
      <header className="sticky top-0 z-10 flex items-center justify-between bg-[#1e1e21] px-3.5 py-2">
        <h2 className="text-[12.5px] font-semibold tracking-wide text-[#b4b4be]">SSH 终端</h2>
        <button
          type="button"
          onClick={onNew}
          aria-label="新建 SSH 终端"
          title="新建 SSH 终端"
          className="apple-btn-secondary px-2.5 py-1 text-[12px]"
        >
          <Plus aria-hidden="true" className="h-3.5 w-3.5 text-[#52a9ff]" />
          新建
        </button>
      </header>
      {error && <p role="alert" className="px-3.5 pb-2 text-xs text-rose-300">{error}</p>}
      {sessions.length > 0 && (
        <ul aria-label="已打开的 SSH 会话" className="space-y-0.5 px-2 pb-2">
          {sessions.map((session) => {
            const active = session.id === activeTabId;
            return (
              <li key={session.id}>
                <button
                  type="button"
                  onClick={() => onSelectSession(session.id)}
                  aria-current={active ? 'true' : undefined}
                  title={session.title}
                  className={`${row} ${active ? 'bg-[#0a84ff]/20 text-white' : 'text-[#b4b4be] hover:bg-white/[0.06] hover:text-white'}`}
                >
                  <TerminalSquare aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-[#64d2ff]" />
                  <span className="truncate">{session.title}</span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
      {hosts.length > 0 && (
        <div className="pb-2">
          <h3 className="px-3.5 pb-1 text-[11px] font-medium text-[#9e9ea9]">已保存的 SSH 主机</h3>
          <ul aria-label="已保存的 SSH 主机" className="space-y-0.5 px-2">
            {hosts.map((host) => (
              <li key={host.id} className="group flex items-center gap-1">
                <button
                  type="button"
                  onClick={() => onOpenHost(host)}
                  title={`${host.username}@${host.host}:${host.port || 22}`}
                  className={`${row} min-w-0 flex-1 text-[#b4b4be] hover:bg-white/[0.06] hover:text-white`}
                >
                  <Server aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-[#64d2ff]" />
                  <span className="min-w-0 flex-1 truncate">{host.name}</span>
                  {host.name !== host.host && (
                    <span className="max-w-[40%] truncate font-mono text-[10.5px] text-[#9e9ea9]">{host.host}</span>
                  )}
                </button>
                <button
                  type="button"
                  onClick={() => onDeleteHost(host)}
                  aria-label={`删除 SSH 主机 ${host.name}`}
                  title="删除 SSH 主机"
                  className="shrink-0 rounded p-1.5 text-[#9e9ea9] opacity-0 hover:bg-rose-500/10 hover:text-rose-300 focus-visible:opacity-100 group-hover:opacity-100"
                >
                  <Trash2 aria-hidden="true" className="h-3.5 w-3.5" />
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      {savedConnections.length > 0 && (
        <div className="pb-2">
          <h3 className="px-3.5 pb-1 text-[11px] font-medium text-[#9e9ea9]">数据库 SSH 设置</h3>
          <ul aria-label="数据库 SSH 设置" className="space-y-0.5 px-2">
            {savedConnections.map((connection) => (
              <li key={connection.id}>
                <button
                  type="button"
                  onClick={() => onOpenSaved(connection)}
                  title={`${connection.ssh.username}@${connection.ssh.host}:${connection.ssh.port || 22}`}
                  className={`${row} text-[#b4b4be] hover:bg-white/[0.06] hover:text-white`}
                >
                  <Server aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-[#9e9ea9]" />
                  <span className="min-w-0 flex-1 truncate">{connection.name}</span>
                  {connection.name !== connection.ssh.host && (
                    <span className="max-w-[45%] truncate font-mono text-[10.5px] text-[#9e9ea9]">{connection.ssh.host}</span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
