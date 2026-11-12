import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';
import type { ConnectionView, SSHHostSaveInput, SSHHostView } from '../types/db';
import { DirectSSHForm } from './DirectSSHForm';
import { TerminalSession, type TerminalSSHSettings, type TerminalTarget } from './TerminalSession';

interface Props {
  active: boolean;
  connections: ConnectionView[];
  sshHosts: SSHHostView[];
  initialSource?: 'direct' | 'saved';
  initialConnectionId?: string;
  initialHostId?: string;
  initialSettings?: TerminalSSHSettings;
  autoConnect?: boolean;
  onDestinationChange?: (label: string) => void;
  onSaveHost?: (input: SSHHostSaveInput) => Promise<SSHHostView>;
}

const maxPasteBytes = 64 * 1024;
const encoder = new TextEncoder();

function savedOption(
  key: string,
  name: string,
  ssh: Pick<TerminalSSHSettings, 'host' | 'port' | 'username' | 'insecureIgnoreHostKey' | 'hostKeyFingerprint'>,
  target: TerminalTarget,
) {
  return {
    key,
    name,
    target,
    label: `${ssh.username}@${ssh.host}:${ssh.port || 22}`,
    unverified: ssh.insecureIgnoreHostKey && !ssh.hostKeyFingerprint,
  };
}

export function TerminalWorkspace({
  active,
  connections,
  sshHosts,
  initialSource,
  initialConnectionId,
  initialHostId,
  initialSettings,
  autoConnect = false,
  onDestinationChange,
  onSaveHost,
}: Props) {
  const saved = useMemo(() => ({
    hosts: sshHosts.map((host) => savedOption(`host:${host.id}`, host.name, host, { sshHostId: host.id })),
    tunnels: connections.filter((connection) => connection.ssh?.enabled).map((connection) =>
      savedOption(`connection:${connection.id}`, connection.name, connection.ssh, { connectionId: connection.id }),
    ),
  }), [connections, sshHosts]);
  const initialSavedKey = initialHostId ? `host:${initialHostId}` : initialConnectionId ? `connection:${initialConnectionId}` : '';
  const [savedKey, setSavedKey] = useState(initialSavedKey);
  const [source, setSource] = useState(initialSource ?? (initialHostId ? 'saved' : 'direct'));
  const [status, setStatus] = useState<'idle' | 'connecting' | 'connected'>('idle');
  const [destination, setDestination] = useState<{ label: string; unverified: boolean } | null>(null);
  const [notice, setNotice] = useState('');
  const [connectionError, setConnectionError] = useState('');
  const [showLastOutput, setShowLastOutput] = useState(false);
  const [pendingPaste, setPendingPaste] = useState<string | null>(null);
  const viewport = useRef<HTMLDivElement | null>(null);
  const activeRef = useRef(active);
  const fitViewportRef = useRef<(() => void) | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const sessionRef = useRef<TerminalSession | null>(null);
  const autoConnectHandled = useRef(false);
  const selected = saved.hosts.find((item) => item.key === savedKey)
    ?? saved.tunnels.find((item) => item.key === savedKey)
    ?? saved.hosts[0]
    ?? saved.tunnels[0];

  useEffect(() => {
    const element = viewport.current;
    if (!element) return;
    const initialize = () => {
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
        if (element.clientHeight === 0 || element.clientWidth === 0) return;
        const size = fit.proposeDimensions();
        if (size) {
          terminal.resize(Math.min(size.cols, 512), Math.min(size.rows, 512));
        }
      };
      fitViewportRef.current = fitViewport;
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
        if (!event.isTrusted || !activeRef.current || !sessionRef.current || terminal.options.disableStdin) return;
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
        fitViewportRef.current = null;
        element.removeEventListener('paste', paste, true);
        observer.disconnect();
        resize.dispose();
        input.dispose();
        binary.dispose();
        terminal.dispose();
      };
    };
    let dispose: (() => void) | undefined;
    // xterm schedules viewport work when opened. Skip the discarded StrictMode mount.
    const frame = window.requestAnimationFrame(() => {
      dispose = initialize();
    });
    return () => {
      window.cancelAnimationFrame(frame);
      dispose?.();
    };
  }, []);

  useEffect(() => {
    activeRef.current = active;
    if (!active) return;
    const frame = window.requestAnimationFrame(() => {
      fitViewportRef.current?.();
      const terminal = terminalRef.current;
      if (terminal && !terminal.options.disableStdin) terminal.focus();
    });
    return () => window.cancelAnimationFrame(frame);
  }, [active]);

  const connect = useCallback((target: TerminalTarget, label: string, unverified: boolean) => {
    const terminal = terminalRef.current;
    if (!terminal || sessionRef.current) return;
    autoConnectHandled.current = true;
    terminal.reset();
    terminal.options.disableStdin = true;
    setStatus('connecting');
    setDestination({ label, unverified });
    onDestinationChange?.(label);
    setNotice('');
    setConnectionError('');
    setShowLastOutput(false);
    setPendingPaste(null);
    const session = new TerminalSession((error) => {
      if (sessionRef.current !== session) return;
      sessionRef.current = null;
      terminal.options.disableStdin = true;
      setStatus('idle');
      setPendingPaste(null);
      setConnectionError(error || '');
      setNotice(error ? '' : 'SSH 会话已结束');
    });
    sessionRef.current = session;
    void session.connect(target, terminal, () => {
      terminal.options.disableStdin = false;
      session.resize({ rows: terminal.rows, cols: terminal.cols });
      setShowLastOutput(true);
      setStatus('connected');
      if (activeRef.current) terminal.focus();
    });
  }, [onDestinationChange]);

  useEffect(() => {
    if (!autoConnect || autoConnectHandled.current || source !== 'saved' || !selected || selected.key !== initialSavedKey) return;
    // A deferred start can be cancelled by StrictMode's first effect cleanup.
    const frame = window.requestAnimationFrame(() => {
      if (!autoConnectHandled.current) connect(selected.target, selected.label, selected.unverified);
    });
    return () => window.cancelAnimationFrame(frame);
  }, [autoConnect, connect, initialSavedKey, selected, source]);

  const disconnect = () => {
    sessionRef.current?.close();
    sessionRef.current = null;
    if (terminalRef.current) terminalRef.current.options.disableStdin = true;
    setStatus('idle');
    setPendingPaste(null);
    setConnectionError('');
    setNotice('已断开 SSH 会话');
  };

  const finishPaste = (accepted: boolean) => {
    const terminal = terminalRef.current;
    if (accepted && terminal && pendingPaste && status === 'connected') terminal.paste(pendingPaste);
    setPendingPaste(null);
    if (activeRef.current) terminal?.focus();
  };

  const directFormVisible = source === 'direct' && status === 'idle' && !showLastOutput;
  const showVerificationWarning = status === 'idle'
    ? source === 'saved' && selected?.unverified
    : destination?.unverified;

  return (
    <section aria-label="SSH 终端" className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-[#161618]">
      <header className="flex shrink-0 flex-wrap items-center gap-2 border-b border-white/[0.08] bg-[#1e1e21] px-4 py-2 text-xs">
        <select
          aria-label="SSH 连接方式"
          value={source}
          disabled={status !== 'idle'}
          onChange={(event) => {
            autoConnectHandled.current = true;
            setSource(event.target.value === 'saved' ? 'saved' : 'direct');
            setNotice('');
            setConnectionError('');
            setShowLastOutput(false);
          }}
          className="max-w-sm rounded border border-slate-700 bg-slate-800 px-2 py-1.5 text-slate-100"
        >
          <option value="direct">直接连接服务器</option>
          <option value="saved" disabled={!selected}>使用已保存的 SSH 设置</option>
        </select>
        {status === 'idle' && source === 'saved' && (
          <>
            <select
              aria-label="已保存的 SSH 连接"
              value={selected?.key ?? ''}
              disabled={!selected}
              onChange={(event) => {
                autoConnectHandled.current = true;
                setSavedKey(event.target.value);
              }}
              className="max-w-sm rounded border border-slate-700 bg-slate-800 px-2 py-1.5 text-slate-100"
            >
              {!selected && <option value="">暂无已保存的 SSH 设置</option>}
              {saved.hosts.length > 0 && (
                <optgroup label="SSH 主机">
                  {saved.hosts.map((item) => <option key={item.key} value={item.key}>{item.name} · {item.label}</option>)}
                </optgroup>
              )}
              {saved.tunnels.length > 0 && (
                <optgroup label="数据库 SSH 隧道">
                  {saved.tunnels.map((item) => <option key={item.key} value={item.key}>{item.name} · {item.label}</option>)}
                </optgroup>
              )}
            </select>
            <button
              type="button"
              disabled={!selected}
              onClick={() => {
                if (selected) connect(selected.target, selected.label, selected.unverified);
              }}
              className="rounded bg-indigo-600 px-3 py-1.5 font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              连接
            </button>
          </>
        )}
        {status !== 'idle' && (
          <button type="button" onClick={disconnect} className="rounded bg-slate-700 px-3 py-1.5 font-medium text-white hover:bg-slate-600">
            {status === 'connecting' ? '取消' : '断开'}
          </button>
        )}
        {status === 'idle' && source === 'direct' && showLastOutput && (
          <button type="button" onClick={() => setShowLastOutput(false)} className="rounded bg-slate-700 px-3 py-1.5 font-medium text-white hover:bg-slate-600">
            返回连接设置
          </button>
        )}
        {status !== 'idle' && <span className="max-w-xs truncate text-slate-300" title={destination?.label}>{destination?.label}</span>}
        {status === 'connecting' && <Loader2 aria-label="正在连接" className="h-4 w-4 animate-spin text-indigo-300" />}
        {showVerificationWarning && (
          <span className="text-amber-300">此连接跳过主机指纹验证</span>
        )}
      </header>
      {connectionError && (
        <p role="alert" className="max-h-24 overflow-y-auto break-words border-b border-red-900/70 bg-red-950/40 px-4 py-2 text-xs text-red-200">
          {connectionError}
        </p>
      )}
      <DirectSSHForm
        visible={directFormVisible}
        initialSettings={initialSettings}
        onSaveHost={onSaveHost}
        onConnect={(ssh) => connect(
          { ssh },
          `${ssh.username}@${ssh.host}:${ssh.port || 22}`,
          ssh.insecureIgnoreHostKey && !ssh.hostKeyFingerprint,
        )}
      />
      <div className={directFormVisible ? 'hidden' : 'min-h-0 min-w-0 flex-1 overflow-hidden bg-[#10141c] p-3'}>
        <div ref={viewport} className="h-full w-full" aria-label="终端输入与输出" />
      </div>
      {pendingPaste !== null && (
        <div role="alert" className="flex flex-wrap items-center gap-2 border-t border-amber-700/60 bg-amber-950/50 px-4 py-2 text-xs text-amber-100">
          <span className="flex-1">剪贴板内容含换行或控制字符，粘贴可能执行多条命令。</span>
          <button type="button" onClick={() => finishPaste(false)} className="rounded bg-slate-700 px-2 py-1 text-white">取消</button>
          <button type="button" onClick={() => finishPaste(true)} className="rounded bg-amber-600 px-2 py-1 font-medium text-white">继续粘贴</button>
        </div>
      )}
      <footer className="flex shrink-0 flex-wrap justify-between gap-2 border-t border-white/[0.08] bg-[#1e1e21] px-4 py-2 text-xs text-slate-400">
        <span role="status">{notice || (status === 'connected' ? '已连接' : status === 'connecting' ? '正在连接' : '未连接')}</span>
        <span>Ctrl+Shift+C 复制 · Ctrl+Shift+V 粘贴</span>
      </footer>
    </section>
  );
}
