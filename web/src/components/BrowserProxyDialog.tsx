import { useRef, useState } from 'react';
import { X } from 'lucide-react';

export interface BrowserProxyHost {
  host: string;
  port: number;
  username: string;
}

export function BrowserProxyDialog({ host, onClose }: { host: BrowserProxyHost; onClose: () => void }) {
  const commandRef = useRef<HTMLTextAreaElement>(null);
  const [copied, setCopied] = useState(false);
  const safeHost = /^(?:[a-z0-9][a-z0-9._-]*|[a-f0-9:]*:[a-f0-9:.]+)$/i.test(host.host);
  const safeUser = /^[a-z0-9_][a-z0-9_.-]*$/i.test(host.username);
  const port = host.port || 22;
  const command = safeHost && safeUser && Number.isInteger(port) && port >= 1 && port <= 65535
    ? `ssh -N -D 127.0.0.1:1080 -p ${port} -l ${host.username} ${host.host}`
    : '';

  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
    } catch {
      commandRef.current?.focus();
      commandRef.current?.select();
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4"
      onKeyDown={(event) => {
        if (event.key === 'Escape') onClose();
      }}>
      <section role="dialog" aria-modal="true" aria-labelledby="browser-proxy-title"
        className="max-h-full w-full max-w-xl overflow-auto rounded-xl border border-interaction/10 bg-surface-toolbar p-4 text-sm shadow-2xl">
        <header className="mb-3 flex items-center justify-between gap-2">
          <h2 id="browser-proxy-title" className="font-semibold">通过 SSH 访问网页</h2>
          <button type="button" aria-label="关闭浏览器代理设置" onClick={onClose} autoFocus className="rounded p-1 text-slate-400 hover:text-strong">
            <X className="h-4 w-4" />
          </button>
        </header>
        <p className="mb-3 text-slate-300">在能连接此 SSH 主机的电脑上打开本地终端，启动 SOCKS5 转发。</p>
        {command ? (
          <>
            <textarea ref={commandRef} aria-label="本机 SSH 转发命令" readOnly value={command} rows={2}
              className="w-full select-text resize-none rounded border border-slate-700 bg-slate-950 p-2 font-mono text-xs" />
            <button type="button" className="apple-btn-secondary mt-2" onClick={() => void copy()}>
              {copied ? '已复制' : '复制 SSH 命令'}
            </button>
          </>
        ) : (
          <p className="select-text break-words font-mono text-xs text-slate-300">
            主机 {host.host} · 端口 {port} · 用户 {host.username}。在本地 SSH 客户端中添加动态转发，监听 127.0.0.1:1080。
          </p>
        )}
        <p className="mt-4 text-slate-300">使用独立 Chrome 配置，启动时指定以下参数。认证由本地 SSH 客户端完成。</p>
        <pre className="mt-2 select-text whitespace-pre-wrap break-words rounded border border-slate-700 bg-slate-950 p-2 text-xs">{'--proxy-server=socks5://127.0.0.1:1080\n--user-data-dir="<独立配置目录>"'}</pre>
        <p className="mt-3 text-xs text-slate-400">如果目标网页只监听 SSH 主机的 127.0.0.1，可在独立 Chrome 配置中追加 <code>{'--proxy-bypass-list="<-loopback>"'}</code>。关闭本地 SSH 转发后，代理窗口会失去连接。</p>
      </section>
    </div>
  );
}
