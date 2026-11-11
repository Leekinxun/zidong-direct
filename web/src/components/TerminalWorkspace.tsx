import { useEffect, useRef, useState } from 'react';
import { Clipboard, ClipboardPaste, Eraser, Loader2, TerminalSquare, X } from 'lucide-react';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';
import type { ConnectionView } from '../types/db';
import { TerminalSession } from './TerminalSession';

interface Props {
  connections: ConnectionView[];
  initialConnectionId?: string;
  onClose: () => void;
}

const maxPasteBytes = 64 * 1024;
const encoder = new TextEncoder();

function copyTextFallback(text: string): boolean {
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    return ok;
  } catch {
    return false;
  }
}

export function TerminalWorkspace({ connections, initialConnectionId, onClose }: Props) {
  const sshConnections = connections.filter((connection) => connection.ssh?.enabled);
  const initialConn =
    connections.find((c) => c.id === initialConnectionId) || sshConnections[0] || connections[0];

  const [mode, setMode] = useState<'saved' | 'direct'>(() =>
    initialConn?.ssh?.enabled || sshConnections.length > 0 ? 'saved' : 'direct',
  );
  const [connectionId, setConnectionId] = useState(() =>
    sshConnections.some((c) => c.id === initialConnectionId)
      ? initialConnectionId!
      : sshConnections[0]?.id ?? '',
  );

  // Direct / Xshell custom SSH host state (pre-populated from current DB connection if available)
  const [sshHost, setSshHost] = useState(
    () => initialConn?.ssh?.host || initialConn?.host || '127.0.0.1',
  );
  const [sshPort, setSshPort] = useState(() => initialConn?.ssh?.port || 22);
  const [sshUser, setSshUser] = useState(
    () => initialConn?.ssh?.username || initialConn?.username || 'root',
  );
  const [authType, setAuthType] = useState<'password' | 'privateKey'>(
    () => (initialConn?.ssh?.authType === 'privateKey' ? 'privateKey' : 'password'),
  );
  const [sshPassword, setSshPassword] = useState('');
  const [sshPrivateKey, setSshPrivateKey] = useState('');
  const [sshPassphrase, setSshPassphrase] = useState('');
  const [ignoreHostKey, setIgnoreHostKey] = useState(
    () => initialConn?.ssh?.insecureIgnoreHostKey ?? true,
  );
  const [hostFingerprint, setHostFingerprint] = useState(
    () => initialConn?.ssh?.hostKeyFingerprint || '',
  );

  const [status, setStatus] = useState<'idle' | 'connecting' | 'connected'>('idle');
  const [notice, setNotice] = useState('');
  const [pendingPaste, setPendingPaste] = useState<string | null>(null);
  const viewport = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const sessionRef = useRef<TerminalSession | null>(null);
  const lastPasteAtRef = useRef<number>(0);

  const selected = sshConnections.find((connection) => connection.id === connectionId);

  const handleCopySelection = async () => {
    const terminal = terminalRef.current;
    if (!terminal) return;
    const selection = terminal.getSelection();
    if (!selection) {
      setNotice('请先在终端中用鼠标选中要复制的文本');
      return;
    }
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(selection);
        setNotice('已复制选中内容 (Ctrl+Shift+C)');
        return;
      }
    } catch {
      // Fall through to execCommand fallback
    }
    if (copyTextFallback(selection)) {
      setNotice('已复制选中内容 (Ctrl+Shift+C)');
    } else {
      setNotice('无法复制到系统剪贴板');
    }
  };

  const applyPasteData = (data: string) => {
    const terminal = terminalRef.current;
    if (!terminal || !sessionRef.current || terminal.options.disableStdin || !data) return;
    const now = Date.now();
    if (now - lastPasteAtRef.current < 80) return;
    lastPasteAtRef.current = now;

    if (encoder.encode(data).length > maxPasteBytes) {
      setNotice('粘贴内容超过 64 KiB，请分段粘贴');
      return;
    }
    if (/[\r\n\x00-\x08\x0b-\x1f\x7f]/.test(data)) {
      setPendingPaste(data);
    } else {
      terminal.paste(data);
      setNotice('已粘贴剪贴板内容 (Ctrl+Shift+V)');
    }
  };

  const handleClipboardPaste = async () => {
    try {
      if (!navigator.clipboard?.readText) {
        setNotice('请直接在终端窗口按 Ctrl+Shift+V 或右键粘贴');
        return;
      }
      const data = await navigator.clipboard.readText();
      if (data) applyPasteData(data);
    } catch {
      setNotice('浏览器未授予剪贴板读取权限，请直接按 Ctrl+V / Ctrl+Shift+V 粘贴');
    }
  };

  useEffect(() => {
    const element = viewport.current;
    if (!element) return;
    const terminal = new Terminal({
      convertEol: false,
      cursorBlink: true,
      disableStdin: true,
      fontFamily: 'Consolas, Menlo, monospace',
      fontSize: 14,
      linkHandler: { activate: () => {} },
      scrollback: 3000,
      theme: {
        background: '#10141c',
        foreground: '#d9e3f4',
        cursor: '#a3bfff',
        selectionBackground: '#344775',
      },
    });
    terminalRef.current = terminal;
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(element);
    const fitViewport = () => {
      const size = fit.proposeDimensions();
      if (size && element.clientHeight > 0 && element.clientWidth > 0) {
        terminal.resize(Math.min(size.cols, 512), Math.min(size.rows, 512));
      }
    };
    const observer = new ResizeObserver(fitViewport);
    observer.observe(element);
    fitViewport();
    const resize = terminal.onResize((size) => sessionRef.current?.resize(size));
    const input = terminal.onData((data) => sessionRef.current?.send(encoder.encode(data)));
    const binary = terminal.onBinary((data) => {
      sessionRef.current?.send(Uint8Array.from(data, (character) => character.charCodeAt(0)));
    });

    terminal.attachCustomKeyEventHandler((event) => {
      if (
        event.type !== 'keydown' ||
        (!event.ctrlKey && !event.metaKey) ||
        !event.shiftKey ||
        event.altKey
      ) {
        return true;
      }
      const key = event.key.toLowerCase();
      if (key === 'c') {
        event.preventDefault();
        void handleCopySelection();
        return false;
      }
      if (key === 'v') {
        // Trigger clipboard read fallback if the browser doesn't emit a native paste event on Ctrl+Shift+V
        window.setTimeout(() => {
          if (Date.now() - lastPasteAtRef.current >= 75 && navigator.clipboard?.readText) {
            void navigator.clipboard
              .readText()
              .then((text) => {
                if (text) applyPasteData(text);
              })
              .catch(() => {});
          }
        }, 35);
        return false;
      }
      return true;
    });

    const paste = (event: ClipboardEvent) => {
      event.preventDefault();
      event.stopPropagation();
      if (!event.isTrusted || !sessionRef.current || terminal.options.disableStdin) return;
      const data = event.clipboardData?.getData('text/plain');
      if (data) applyPasteData(data);
    };
    element.addEventListener('paste', paste, true);

    return () => {
      sessionRef.current?.close();
      sessionRef.current = null;
      terminalRef.current = null;
      element.removeEventListener('paste', paste, true);
      observer.disconnect();
      resize.dispose();
      input.dispose();
      binary.dispose();
      terminal.dispose();
    };
  }, []);

  const connect = () => {
    const terminal = terminalRef.current;
    if (!terminal || sessionRef.current) return;
    if (mode === 'saved' && !selected) return;
    if (mode === 'direct' && (!sshHost.trim() || !sshUser.trim())) {
      setNotice('请输入 SSH 主机地址与用户名');
      return;
    }

    terminal.reset();
    terminal.options.disableStdin = true;
    setStatus('connecting');
    setNotice('');
    setPendingPaste(null);
    const session = new TerminalSession((error) => {
      if (sessionRef.current !== session) return;
      sessionRef.current = null;
      terminal.options.disableStdin = true;
      setStatus('idle');
      setPendingPaste(null);
      setNotice(error || 'SSH 会话已结束');
    });
    sessionRef.current = session;

    const target =
      mode === 'saved'
        ? selected!.id
        : {
            connectionId: initialConn?.id,
            ssh: {
              host: sshHost.trim(),
              port: Number(sshPort) || 22,
              username: sshUser.trim(),
              authType,
              password: sshPassword,
              privateKey: sshPrivateKey,
              passphrase: sshPassphrase,
              hostKeyFingerprint: ignoreHostKey ? '' : hostFingerprint.trim(),
              insecureIgnoreHostKey: ignoreHostKey,
            },
          };

    void session.connect(target, terminal, () => {
      terminal.options.disableStdin = false;
      session.resize({ rows: terminal.rows, cols: terminal.cols });
      setStatus('connected');
      terminal.focus();
    });
  };

  const disconnect = () => {
    sessionRef.current?.close();
    sessionRef.current = null;
    if (terminalRef.current) terminalRef.current.options.disableStdin = true;
    setStatus('idle');
    setPendingPaste(null);
    setNotice('已断开 SSH 会话');
  };

  const finishPaste = (accepted: boolean) => {
    const terminal = terminalRef.current;
    if (accepted && terminal && pendingPaste && status === 'connected') {
      terminal.paste(pendingPaste);
      setNotice('已粘贴多行内容');
    }
    setPendingPaste(null);
    terminal?.focus();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-4">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="ssh-terminal-title"
        className="flex h-[min(84vh,800px)] w-full max-w-6xl flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
      >
        <header className="flex items-center gap-2 border-b border-slate-800 px-4 py-2.5">
          <TerminalSquare className="h-4 w-4 text-sky-400" aria-hidden="true" />
          <h2 id="ssh-terminal-title" className="flex-1 text-sm font-semibold text-slate-100">
            SSH 终端工作台 (Xshell 交互式终端)
          </h2>

          <div className="flex items-center gap-1.5 text-xs">
            <button
              type="button"
              disabled={status !== 'connected'}
              onClick={() => void handleCopySelection()}
              className="inline-flex items-center gap-1 rounded border border-slate-700 bg-slate-800 px-2.5 py-1 text-slate-200 hover:bg-slate-700 disabled:opacity-40"
              title="复制终端选中文本 (Ctrl+Shift+C)"
            >
              <Clipboard className="h-3.5 w-3.5 text-sky-300" />
              复制 (Ctrl+Shift+C)
            </button>
            <button
              type="button"
              disabled={status !== 'connected'}
              onClick={() => void handleClipboardPaste()}
              className="inline-flex items-center gap-1 rounded border border-slate-700 bg-slate-800 px-2.5 py-1 text-slate-200 hover:bg-slate-700 disabled:opacity-40"
              title="粘贴剪贴板文本 (Ctrl+Shift+V)"
            >
              <ClipboardPaste className="h-3.5 w-3.5 text-emerald-300" />
              粘贴 (Ctrl+Shift+V)
            </button>
            <button
              type="button"
              onClick={() => {
                terminalRef.current?.clear();
                terminalRef.current?.focus();
              }}
              className="inline-flex items-center gap-1 rounded border border-slate-700 bg-slate-800 px-2 py-1 text-slate-300 hover:bg-slate-700"
              title="清空屏幕显示"
            >
              <Eraser className="h-3.5 w-3.5" />
              清屏
            </button>
          </div>

          <button
            type="button"
            onClick={onClose}
            aria-label="关闭 SSH 终端"
            className="rounded p-1 text-slate-400 hover:bg-slate-800 hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="space-y-2 border-b border-slate-800 bg-slate-950/60 px-4 py-2.5 text-xs">
          <div className="flex flex-wrap items-center gap-2">
            <div className="apple-segmented">
              <button
                type="button"
                disabled={status !== 'idle'}
                onClick={() => setMode('saved')}
                className={`apple-segmented-item ${mode === 'saved' ? 'apple-segmented-item-active' : ''}`}
              >
                已存连接 SSH ({sshConnections.length})
              </button>
              <button
                type="button"
                disabled={status !== 'idle'}
                onClick={() => setMode('direct')}
                className={`apple-segmented-item ${mode === 'direct' ? 'apple-segmented-item-active' : ''}`}
              >
                自定义 SSH 直连 (Xshell)
              </button>
            </div>

            {mode === 'saved' ? (
              <select
                aria-label="SSH 连接"
                value={connectionId}
                disabled={status !== 'idle' || sshConnections.length === 0}
                onChange={(event) => setConnectionId(event.target.value)}
                className="max-w-sm rounded border border-slate-700 bg-slate-800 px-2.5 py-1.5 text-slate-100"
              >
                {sshConnections.length === 0 && (
                  <option value="">暂无已启用 SSH 的连接，可切换到「自定义 SSH 直连」</option>
                )}
                {sshConnections.map((connection) => (
                  <option key={connection.id} value={connection.id}>
                    {connection.name} · {connection.ssh.username}@{connection.ssh.host}:
                    {connection.ssh.port || 22}
                  </option>
                ))}
              </select>
            ) : (
              connections.length > 0 && (
                <select
                  aria-label="从现有数据库连接填入主机 IP"
                  disabled={status !== 'idle'}
                  defaultValue={initialConn?.id || ''}
                  onChange={(e) => {
                    const picked = connections.find((c) => c.id === e.target.value);
                    if (picked) {
                      setSshHost(picked.ssh?.host || picked.host || '127.0.0.1');
                      setSshPort(picked.ssh?.port || 22);
                      if (picked.ssh?.username) setSshUser(picked.ssh.username);
                    }
                  }}
                  className="rounded border border-slate-700 bg-slate-800 px-2 py-1.5 text-slate-300"
                  title="快速带入某个数据库连接的主机 IP"
                >
                  <option value="" disabled>
                    从现有连接带入 IP...
                  </option>
                  {connections.map((c) => (
                    <option key={c.id} value={c.id}>
                      带入: {c.name} ({c.ssh?.host || c.host || '本地'})
                    </option>
                  ))}
                </select>
              )
            )}

            {status === 'idle' ? (
              <button
                type="button"
                disabled={mode === 'saved' ? !selected : !sshHost.trim() || !sshUser.trim()}
                onClick={connect}
                className="rounded bg-indigo-600 px-3.5 py-1.5 font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              >
                连接终端
              </button>
            ) : (
              <button
                type="button"
                onClick={disconnect}
                className="rounded bg-rose-700/80 px-3.5 py-1.5 font-medium text-white hover:bg-rose-600"
              >
                {status === 'connecting' ? '取消连接' : '断开终端'}
              </button>
            )}

            {status === 'connecting' && (
              <Loader2 aria-label="正在连接" className="h-4 w-4 animate-spin text-indigo-300" />
            )}
            {mode === 'saved' &&
              selected?.ssh.insecureIgnoreHostKey &&
              !selected.ssh.hostKeyFingerprint && (
                <span className="text-amber-300">此连接跳过主机指纹验证</span>
              )}
          </div>

          {mode === 'direct' && status === 'idle' && (
            <div className="grid grid-cols-2 gap-2 pt-1 sm:grid-cols-6">
              <label className="space-y-0.5 sm:col-span-2">
                <span className="text-[11px] text-slate-400">SSH 主机 / IP</span>
                <input
                  type="text"
                  value={sshHost}
                  onChange={(e) => setSshHost(e.target.value)}
                  placeholder="192.168.1.100"
                  className="w-full rounded border border-slate-700 bg-slate-800 px-2 py-1 font-mono text-xs text-slate-100 outline-none focus:border-indigo-500"
                />
              </label>
              <label className="space-y-0.5">
                <span className="text-[11px] text-slate-400">端口</span>
                <input
                  type="number"
                  value={sshPort}
                  onChange={(e) => setSshPort(Number(e.target.value) || 22)}
                  className="w-full rounded border border-slate-700 bg-slate-800 px-2 py-1 font-mono text-xs text-slate-100 outline-none focus:border-indigo-500"
                />
              </label>
              <label className="space-y-0.5">
                <span className="text-[11px] text-slate-400">用户名</span>
                <input
                  type="text"
                  value={sshUser}
                  onChange={(e) => setSshUser(e.target.value)}
                  placeholder="root"
                  className="w-full rounded border border-slate-700 bg-slate-800 px-2 py-1 font-mono text-xs text-slate-100 outline-none focus:border-indigo-500"
                />
              </label>
              <label className="space-y-0.5">
                <span className="text-[11px] text-slate-400">认证方式</span>
                <select
                  value={authType}
                  onChange={(e) => setAuthType(e.target.value as 'password' | 'privateKey')}
                  className="w-full rounded border border-slate-700 bg-slate-800 px-2 py-1 text-xs text-slate-100 outline-none focus:border-indigo-500"
                >
                  <option value="password">密码认证</option>
                  <option value="privateKey">私钥认证</option>
                </select>
              </label>
              {authType === 'password' ? (
                <label className="space-y-0.5">
                  <span className="text-[11px] text-slate-400">SSH 密码</span>
                  <input
                    type="password"
                    value={sshPassword}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') connect();
                    }}
                    onChange={(e) => setSshPassword(e.target.value)}
                    placeholder="输入密码回车连接"
                    className="w-full rounded border border-slate-700 bg-slate-800 px-2 py-1 text-xs text-slate-100 outline-none focus:border-indigo-500"
                  />
                </label>
              ) : (
                <label className="space-y-0.5">
                  <span className="text-[11px] text-slate-400">私钥口令 (可选)</span>
                  <input
                    type="password"
                    value={sshPassphrase}
                    onChange={(e) => setSshPassphrase(e.target.value)}
                    className="w-full rounded border border-slate-700 bg-slate-800 px-2 py-1 text-xs text-slate-100 outline-none focus:border-indigo-500"
                  />
                </label>
              )}
              {authType === 'privateKey' && (
                <label className="space-y-0.5 sm:col-span-4">
                  <span className="text-[11px] text-slate-400">PEM / OpenSSH 私钥内容</span>
                  <textarea
                    rows={2}
                    value={sshPrivateKey}
                    onChange={(e) => setSshPrivateKey(e.target.value)}
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                    className="w-full resize-y rounded border border-slate-700 bg-slate-800 px-2 py-1 font-mono text-xs text-slate-100 outline-none focus:border-indigo-500"
                  />
                </label>
              )}
              <div className="flex items-center gap-3 pt-1 sm:col-span-6">
                <label className="flex items-center gap-1.5 text-[11px] text-slate-300">
                  <input
                    type="checkbox"
                    checked={ignoreHostKey}
                    onChange={(e) => setIgnoreHostKey(e.target.checked)}
                  />
                  自动接受主机指纹 (内网测试推荐)
                </label>
                {!ignoreHostKey && (
                  <input
                    type="text"
                    value={hostFingerprint}
                    onChange={(e) => setHostFingerprint(e.target.value)}
                    placeholder="SHA256:xxxx 主机指纹"
                    className="w-72 rounded border border-slate-700 bg-slate-800 px-2 py-1 font-mono text-[11px] text-slate-100 outline-none focus:border-indigo-500"
                  />
                )}
              </div>
            </div>
          )}
        </div>

        <div className="min-h-0 flex-1 overflow-hidden bg-[#10141c] p-3">
          <div ref={viewport} className="h-full w-full" aria-label="终端输入与输出" />
        </div>

        {pendingPaste !== null && (
          <div
            role="alert"
            className="flex flex-wrap items-center gap-2 border-t border-amber-700/60 bg-amber-950/50 px-4 py-2 text-xs text-amber-100"
          >
            <span className="flex-1">剪贴板内容含换行或控制字符，粘贴可能执行多条命令。</span>
            <button
              type="button"
              onClick={() => finishPaste(false)}
              className="rounded bg-slate-700 px-2 py-1 text-white"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => finishPaste(true)}
              className="rounded bg-amber-600 px-2 py-1 font-medium text-white"
            >
              继续粘贴
            </button>
          </div>
        )}

        <footer className="flex flex-wrap justify-between gap-2 border-t border-slate-800 px-4 py-2 text-xs text-slate-400">
          <span role="status">
            {notice ||
              (status === 'connected'
                ? '已连接 SSH 终端'
                : status === 'connecting'
                  ? '正在连接...'
                  : '未连接')}
          </span>
          <span>快捷键：选中后按 Ctrl+Shift+C 复制 · Ctrl+Shift+V 粘贴 · Ctrl+C 发送中断信号</span>
        </footer>
      </section>
    </div>
  );
}
