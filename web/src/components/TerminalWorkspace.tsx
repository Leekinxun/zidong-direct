import { useEffect, useRef, useState } from 'react';
import { Loader2, TerminalSquare, X } from 'lucide-react';
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

export function TerminalWorkspace({ connections, initialConnectionId, onClose }: Props) {
  const sshConnections = connections.filter((connection) => connection.ssh?.enabled);
  const [connectionId, setConnectionId] = useState(() => sshConnections.some(
    (connection) => connection.id === initialConnectionId,
  ) ? initialConnectionId! : sshConnections[0]?.id ?? '');
  const [status, setStatus] = useState<'idle' | 'connecting' | 'connected'>('idle');
  const [notice, setNotice] = useState('');
  const [pendingPaste, setPendingPaste] = useState<string | null>(null);
  const viewport = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const sessionRef = useRef<TerminalSession | null>(null);
  const selected = sshConnections.find((connection) => connection.id === connectionId);

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
      theme: { background: '#10141c', foreground: '#d9e3f4', cursor: '#a3bfff', selectionBackground: '#344775' },
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
      if (event.type !== 'keydown' || (!event.ctrlKey && !event.metaKey) || !event.shiftKey || event.altKey) return true;
      if (event.key.toLowerCase() === 'c') {
        event.preventDefault();
        const selection = terminal.getSelection();
        if (selection) {
          if (navigator.clipboard) {
            void navigator.clipboard.writeText(selection).catch(() => setNotice('无法复制到系统剪贴板'));
          } else {
            setNotice('无法复制到系统剪贴板');
          }
        }
        return false;
      }
      return event.key.toLowerCase() !== 'v';
    });

    const paste = (event: ClipboardEvent) => {
      event.preventDefault();
      event.stopPropagation();
      if (!event.isTrusted || !sessionRef.current || terminal.options.disableStdin) return;
      const data = event.clipboardData?.getData('text/plain');
      if (!data) return;
      if (encoder.encode(data).length > maxPasteBytes) {
        setNotice('粘贴内容超过 64 KiB，请分段粘贴');
        return;
      }
      if (/[\r\n\x00-\x08\x0b-\x1f\x7f]/.test(data)) {
        setPendingPaste(data);
      } else {
        terminal.paste(data);
      }
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
    if (!selected || !terminal || sessionRef.current) return;
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
    void session.connect(selected.id, terminal, () => {
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
    if (accepted && terminal && pendingPaste && status === 'connected') terminal.paste(pendingPaste);
    setPendingPaste(null);
    terminal?.focus();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="ssh-terminal-title"
        className="flex h-[min(80vh,760px)] w-full max-w-6xl flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
      >
        <header className="flex items-center gap-2 border-b border-slate-800 px-4 py-3">
          <TerminalSquare className="h-4 w-4 text-indigo-300" aria-hidden="true" />
          <h2 id="ssh-terminal-title" className="flex-1 text-sm font-semibold text-slate-100">SSH 终端</h2>
          <button type="button" onClick={onClose} aria-label="关闭 SSH 终端" className="rounded p-1 text-slate-400 hover:bg-slate-800 hover:text-white">
            <X className="h-4 w-4" />
          </button>
        </header>
        <div className="flex flex-wrap items-center gap-2 border-b border-slate-800 px-4 py-2 text-xs">
          <select
            aria-label="SSH 连接"
            value={connectionId}
            disabled={status !== 'idle' || sshConnections.length === 0}
            onChange={(event) => setConnectionId(event.target.value)}
            className="max-w-sm rounded border border-slate-700 bg-slate-800 px-2 py-1.5 text-slate-100"
          >
            {sshConnections.length === 0 && <option value="">请先保存一个启用 SSH 的数据库连接</option>}
            {sshConnections.map((connection) => (
              <option key={connection.id} value={connection.id}>
                {connection.name} · {connection.ssh.username}@{connection.ssh.host}:{connection.ssh.port || 22}
              </option>
            ))}
          </select>
          {status === 'idle' ? (
            <button type="button" disabled={!selected} onClick={connect} className="rounded bg-indigo-600 px-3 py-1.5 font-medium text-white hover:bg-indigo-500 disabled:opacity-50">
              连接
            </button>
          ) : (
            <button type="button" onClick={disconnect} className="rounded bg-slate-700 px-3 py-1.5 font-medium text-white hover:bg-slate-600">
              {status === 'connecting' ? '取消' : '断开'}
            </button>
          )}
          {status === 'connecting' && <Loader2 aria-label="正在连接" className="h-4 w-4 animate-spin text-indigo-300" />}
          {selected?.ssh.insecureIgnoreHostKey && !selected.ssh.hostKeyFingerprint && (
            <span className="text-amber-300">此连接跳过主机指纹验证</span>
          )}
        </div>
        <div className="min-h-0 flex-1 overflow-hidden bg-[#10141c] p-3">
          <div ref={viewport} className="h-full w-full" aria-label="终端输入与输出" />
        </div>
        {pendingPaste !== null && (
          <div role="alert" className="flex flex-wrap items-center gap-2 border-t border-amber-700/60 bg-amber-950/50 px-4 py-2 text-xs text-amber-100">
            <span className="flex-1">剪贴板内容含换行或控制字符，粘贴可能执行多条命令。</span>
            <button type="button" onClick={() => finishPaste(false)} className="rounded bg-slate-700 px-2 py-1 text-white">取消</button>
            <button type="button" onClick={() => finishPaste(true)} className="rounded bg-amber-600 px-2 py-1 font-medium text-white">继续粘贴</button>
          </div>
        )}
        <footer className="flex flex-wrap justify-between gap-2 border-t border-slate-800 px-4 py-2 text-xs text-slate-400">
          <span role="status">{notice || (status === 'connected' ? '已连接' : status === 'connecting' ? '正在连接' : '未连接')}</span>
          <span>Ctrl+Shift+C 复制 · Ctrl+Shift+V 粘贴</span>
        </footer>
      </section>
    </div>
  );
}
