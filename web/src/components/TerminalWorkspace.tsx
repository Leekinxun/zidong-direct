import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  CaseSensitive,
  ChevronDown,
  ChevronUp,
  Clipboard,
  ClipboardPaste,
  Columns2,
  Copy,
  CopyPlus,
  CornerDownLeft,
  Ellipsis,
  Loader2,
  PowerOff,
  ExternalLink,
  Regex,
  RotateCw,
  Rows2,
  Search,
  TerminalSquare,
  Trash2,
  X,
} from 'lucide-react';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';
import type { ConnectionView, SSHHostSaveInput, SSHHostView } from '../types/db';
import { useTerminalFontSize, useWheelFontZoom } from '../useWheelFontZoom';
import { DirectSSHForm } from './DirectSSHForm';
import { TerminalSession, type TerminalEncoding, type TerminalSSHSettings, type TerminalTarget } from './TerminalSession';

interface Props {
  active: boolean;
  connections: ConnectionView[];
  sshHosts: SSHHostView[];
  initialSource?: 'direct' | 'saved';
  initialConnectionId?: string;
  initialHostId?: string;
  initialSettings?: TerminalSSHSettings;
  initialEncoding?: TerminalEncoding;
  autoConnect?: boolean;
  onDestinationChange?: (label: string) => void;
  onSaveHost?: (input: SSHHostSaveInput) => Promise<SSHHostView>;
  onCloneTerminal?: (target: TerminalTarget, label?: string, encoding?: TerminalEncoding) => void;
  onBrowserProxy?: (target: TerminalTarget) => void;
  onSplit?: (direction: 'columns' | 'rows', target?: TerminalTarget, encoding?: TerminalEncoding) => void;
  onClosePane?: () => void;
}

interface ContextMenuState {
  x: number;
  y: number;
  hasSelection: boolean;
  selectedText: string;
}

function terminalMenu(x: number, y: number, selectedText: string): ContextMenuState {
  return { x, y, hasSelection: Boolean(selectedText), selectedText };
}

const isMac = typeof navigator !== 'undefined' && /Mac|iPod|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
const maxPasteBytes = 64 * 1024;
const encoder = new TextEncoder();
const nativePasteShortcut = isMac ? '⌘+V' : 'Ctrl+V';

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Some browsers permit copying a selection even without Clipboard API permission.
  }
  const input = document.createElement('textarea');
  const previous = document.activeElement;
  input.value = text;
  input.style.cssText = 'position:fixed;opacity:0;pointer-events:none';
  document.body.appendChild(input);
  try {
    input.select();
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    input.remove();
    if (previous instanceof HTMLElement) previous.focus();
  }
}

const searchDecorations = {
  matchBackground: '#344775',
  matchBorder: '#4a6baf',
  matchOverviewRuler: '#344775',
  activeMatchBackground: '#e5a50a',
  activeMatchBorder: '#ffd700',
  activeMatchColorOverviewRuler: '#e5a50a',
};

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
  initialEncoding = 'utf-8',
  autoConnect = false,
  onDestinationChange,
  onSaveHost,
  onCloneTerminal,
  onBrowserProxy,
  onSplit,
  onClosePane,
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
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [useRegex, setUseRegex] = useState(false);
  const [searchResultIndex, setSearchResultIndex] = useState(-1);
  const [searchResultCount, setSearchResultCount] = useState(0);
  const [searchError, setSearchError] = useState('');
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const searchAddonRef = useRef<SearchAddon | null>(null);
  const openSearchRef = useRef<(() => void) | null>(null);

  const [commandBarOpen, setCommandBarOpen] = useState(false);
  const [commandText, setCommandText] = useState('');
  const [autoEnter, setAutoEnter] = useState(true);
  const [commandHistory, setCommandHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const commandInputRef = useRef<HTMLInputElement | null>(null);
  const toggleCommandBarRef = useRef<(() => void) | null>(null);

  const [fontSize, adjustFontSize] = useTerminalFontSize();
  const [encoding, setEncoding] = useState<TerminalEncoding>(initialEncoding);
  const paneRef = useRef<HTMLElement>(null);
  const viewport = useRef<HTMLDivElement | null>(null);
  const fontSizeRef = useRef(fontSize);
  const activeRef = useRef(active);
  const fitViewportRef = useRef<(() => void) | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const sessionRef = useRef<TerminalSession | null>(null);
  const lastTargetRef = useRef<{ target: TerminalTarget; label: string; unverified: boolean } | null>(null);
  const handlePasteTextRef = useRef<((data: string) => void) | null>(null);
  const reconnectRef = useRef<(() => void) | null>(null);
  const disconnectRef = useRef<(() => void) | null>(null);
  const reconnecting = useRef(false);
  const mounted = useRef(false);
  const autoConnectHandled = useRef(false);
  const selected = saved.hosts.find((item) => item.key === savedKey)
    ?? saved.tunnels.find((item) => item.key === savedKey)
    ?? saved.hosts[0]
    ?? saved.tunnels[0];

  useWheelFontZoom(viewport, adjustFontSize);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  useEffect(() => {
    if (!active) setContextMenu(null);
  }, [active]);

  useEffect(() => {
    if (!contextMenu) return;
    const close = () => setContextMenu(null);
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setContextMenu(null);
        terminalRef.current?.focus();
      }
    };
    window.addEventListener('click', close);
    window.addEventListener('contextmenu', close);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('click', close);
      window.removeEventListener('contextmenu', close);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [contextMenu]);

  useEffect(() => {
    const element = viewport.current;
    if (!element) return;
    const initialize = () => {
      const terminal = new Terminal({
        // The pinned SearchAddon uses xterm's decoration API for match highlighting.
        allowProposedApi: true,
        convertEol: false,
        cursorBlink: true,
        disableStdin: true,
        fontFamily: 'Consolas, Menlo, monospace',
        fontSize: fontSizeRef.current,
        linkHandler: { activate: () => {} },
        scrollback: 3000,
        theme: { background: '#10141c', foreground: '#d9e3f4', cursor: '#a3bfff', selectionBackground: '#344775' },
      });
      terminalRef.current = terminal;
      const fit = new FitAddon();
      terminal.loadAddon(fit);
      const search = new SearchAddon();
      terminal.loadAddon(search);
      searchAddonRef.current = search;
      const searchResultsSub = search.onDidChangeResults((e) => {
        setSearchResultIndex(e.resultIndex);
        setSearchResultCount(e.resultCount);
      });

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
        sessionRef.current?.sendBinary(Uint8Array.from(data, (character) => character.charCodeAt(0)));
      });

      terminal.attachCustomKeyEventHandler((event) => {
        if (event.type !== 'keydown') return true;

        // Search: Cmd+F on Mac, Ctrl+F on Windows/Linux
        const isFind = (isMac ? event.metaKey : event.ctrlKey) && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 'f';
        if (isFind) {
          event.preventDefault();
          openSearchRef.current?.();
          return false;
        }

        // Command Bar: Option+I on Mac, Alt+I on Windows/Linux
        const isCmdInput = event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey && (event.key.toLowerCase() === 'i' || event.code === 'KeyI');
        if (isCmdInput) {
          event.preventDefault();
          toggleCommandBarRef.current?.();
          return false;
        }

        if ((event.ctrlKey || event.metaKey) && event.shiftKey && !event.altKey) {
          if (event.key.toLowerCase() === 'c') {
            event.preventDefault();
            const selection = terminal.getSelection();
            if (selection) void copyToClipboard(selection).then((copied) => {
              setNotice(copied ? '已复制选中文本' : '无法复制到系统剪贴板');
            });
            return false;
          }
          return event.key.toLowerCase() !== 'v';
        }

        const localShortcut = event.altKey && event.shiftKey && !event.ctrlKey && !event.metaKey;
        if (localShortcut) {
          if (event.code === 'KeyK') {
            event.preventDefault();
            terminal.clear();
            return false;
          }
          if (event.code === 'KeyR') {
            event.preventDefault();
            reconnectRef.current?.();
            return false;
          }
          if (event.code === 'KeyW') {
            event.preventDefault();
            disconnectRef.current?.();
            return false;
          }
        }

        return true;
      });

      const paste = (event: ClipboardEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (!event.isTrusted || !activeRef.current) return;
        const data = event.clipboardData?.getData('text/plain');
        if (data) handlePasteTextRef.current?.(data);
      };
      element.addEventListener('paste', paste, true);

      const handleContextMenu = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        setContextMenu(terminalMenu(event.clientX, event.clientY, terminal.getSelection()));
      };
      element.addEventListener('contextmenu', handleContextMenu);

      return () => {
        searchResultsSub.dispose();
        searchAddonRef.current = null;
        search.dispose();
        sessionRef.current?.close();
        sessionRef.current = null;
        terminalRef.current = null;
        fitViewportRef.current = null;
        element.removeEventListener('paste', paste, true);
        element.removeEventListener('contextmenu', handleContextMenu);
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
    fontSizeRef.current = fontSize;
    const terminal = terminalRef.current;
    // Hidden terminals may be unable to measure glyphs. Apply their pending
    // size when they become visible without replacing the session.
    if (!active || !terminal || terminal.options.fontSize === fontSize) return;
    terminal.options.fontSize = fontSize;
    const frame = window.requestAnimationFrame(() => fitViewportRef.current?.());
    return () => window.cancelAnimationFrame(frame);
  }, [active, fontSize]);

  useEffect(() => {
    activeRef.current = active;
    if (!active) return;
    const frame = window.requestAnimationFrame(() => {
      fitViewportRef.current?.();
      const terminal = terminalRef.current;
      if (terminal && !terminal.options.disableStdin && !paneRef.current?.contains(document.activeElement)) terminal.focus();
    });
    return () => window.cancelAnimationFrame(frame);
  }, [active]);

  const handlePasteText = useCallback((data: string) => {
    const terminal = terminalRef.current;
    if (!activeRef.current || !terminal || !sessionRef.current || terminal.options.disableStdin) return;
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
  }, []);

  const connect = useCallback((target: TerminalTarget, label: string, unverified: boolean) => {
    const terminal = terminalRef.current;
    if (!terminal || sessionRef.current) return;
    autoConnectHandled.current = true;
    lastTargetRef.current = { target, label, unverified };
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
    }, setNotice);
    sessionRef.current = session;
    void session.connect(target, terminal, () => {
      terminal.options.disableStdin = false;
      session.resize({ rows: terminal.rows, cols: terminal.cols });
      setShowLastOutput(true);
      setStatus('connected');
      if (activeRef.current) terminal.focus();
    }, encoding);
  }, [encoding, onDestinationChange]);

  const disconnect = useCallback(() => {
    sessionRef.current?.close();
    sessionRef.current = null;
    if (terminalRef.current) terminalRef.current.options.disableStdin = true;
    setStatus('idle');
    setPendingPaste(null);
    setConnectionError('');
    setNotice('已断开 SSH 会话');
  }, []);

  const reconnect = useCallback(async () => {
    const targetInfo = lastTargetRef.current || (source === 'saved' && selected ? {
      target: selected.target,
      label: selected.label,
      unverified: selected.unverified,
    } : source === 'direct' && initialSettings?.host ? {
      target: { ssh: initialSettings },
      label: `${initialSettings.username || ''}@${initialSettings.host}:${initialSettings.port || 22}`,
      unverified: Boolean(initialSettings.insecureIgnoreHostKey && !initialSettings.hostKeyFingerprint),
    } : null);
    if (!targetInfo || reconnecting.current) return;
    reconnecting.current = true;
    setStatus('connecting');
    if (terminalRef.current) terminalRef.current.options.disableStdin = true;
    try {
      const previous = sessionRef.current;
      await previous?.close();
      if (!mounted.current || (previous && sessionRef.current !== previous)) return;
      sessionRef.current = null;
      connect(targetInfo.target, targetInfo.label, targetInfo.unverified);
    } finally {
      reconnecting.current = false;
    }
  }, [connect, initialSettings, selected, source]);

  useEffect(() => {
    handlePasteTextRef.current = handlePasteText;
    reconnectRef.current = reconnect;
    disconnectRef.current = disconnect;
    openSearchRef.current = openSearch;
    toggleCommandBarRef.current = toggleCommandBar;
  });

  useEffect(() => {
    if (!autoConnect || autoConnectHandled.current) return;
    if (source === 'saved' && selected && selected.key === initialSavedKey) {
      const frame = window.requestAnimationFrame(() => {
        if (!autoConnectHandled.current) connect(selected.target, selected.label, selected.unverified);
      });
      return () => window.cancelAnimationFrame(frame);
    }
    if (source === 'direct' && initialSettings?.host && initialSettings?.username) {
      const frame = window.requestAnimationFrame(() => {
        if (!autoConnectHandled.current) {
          connect(
            { ssh: initialSettings },
            `${initialSettings.username}@${initialSettings.host}:${initialSettings.port || 22}`,
            Boolean(initialSettings.insecureIgnoreHostKey && !initialSettings.hostKeyFingerprint),
          );
        }
      });
      return () => window.cancelAnimationFrame(frame);
    }
  }, [autoConnect, connect, initialSavedKey, initialSettings, selected, source]);

  const finishPaste = (accepted: boolean) => {
    const terminal = terminalRef.current;
    if (accepted && terminal && pendingPaste && status === 'connected') terminal.paste(pendingPaste);
    setPendingPaste(null);
    if (activeRef.current) terminal?.focus();
  };

  const directFormVisible = source === 'direct' && status === 'idle' && !showLastOutput;
  const currentOrSelectedTarget = status !== 'idle' ? lastTargetRef.current : source === 'saved' && selected ? {
    target: selected.target,
    label: selected.label,
    unverified: selected.unverified,
  } : directFormVisible ? null : lastTargetRef.current;

  const hasTarget = Boolean(currentOrSelectedTarget);
  const handleCopySelection = () => {
    const text = contextMenu?.selectedText || terminalRef.current?.getSelection();
    setContextMenu(null);
    if (text) {
      void copyToClipboard(text).then((copied) => setNotice(copied ? '已复制选中文本' : '无法复制到系统剪贴板'));
    }
    terminalRef.current?.focus();
  };

  const handleClipboardPaste = async () => {
    setContextMenu(null);
    terminalRef.current?.focus();
    const session = sessionRef.current;
    if (!session) return;
    if (!navigator.clipboard?.readText) {
      setNotice(`当前页面无法读取系统剪贴板，请在终端按 ${nativePasteShortcut}。`);
      return;
    }
    try {
      const text = await navigator.clipboard.readText();
      if (sessionRef.current === session) handlePasteText(text);
    } catch {
      setNotice(`浏览器未授予剪贴板读取权限，请在终端按 ${nativePasteShortcut}。`);
    }
  };

  const handlePasteSelection = () => {
    const text = contextMenu?.selectedText || terminalRef.current?.getSelection();
    setContextMenu(null);
    if (text) {
      handlePasteText(text);
    }
    terminalRef.current?.focus();
  };

  const handleClearScreen = () => {
    setContextMenu(null);
    terminalRef.current?.clear();
    terminalRef.current?.focus();
  };

  const handleReconnect = () => {
    setContextMenu(null);
    reconnect();
  };

  const handleDisconnect = () => {
    setContextMenu(null);
    disconnect();
  };

  const handleCloneTab = () => {
    setContextMenu(null);
    if (currentOrSelectedTarget && onCloneTerminal) {
      onCloneTerminal(currentOrSelectedTarget.target, currentOrSelectedTarget.label, encoding);
    }
  };

  const performSearch = useCallback((term: string, caseSens: boolean, regex: boolean, incremental = false, previous = false) => {
    const search = searchAddonRef.current;
    if (!search) return;
    setSearchError('');
    if (!term) {
      search.clearDecorations();
      terminalRef.current?.clearSelection();
      setSearchResultIndex(-1);
      setSearchResultCount(0);
      return;
    }
    const clearSearchResult = (message: string) => {
      search.clearDecorations();
      terminalRef.current?.clearSelection();
      setSearchResultIndex(-1);
      setSearchResultCount(0);
      setSearchError(message);
    };
    if (regex) {
      try {
        new RegExp(term, caseSens ? '' : 'i');
      } catch {
        clearSearchResult('正则表达式无效');
        return;
      }
    }
    try {
      const options = { caseSensitive: caseSens, regex, incremental, decorations: searchDecorations };
      if (previous) search.findPrevious(term, options);
      else search.findNext(term, options);
    } catch {
      clearSearchResult('终端搜索失败');
    }
  }, []);

  const openSearch = useCallback((initialQuery?: string) => {
    setContextMenu(null);
    const selection = terminalRef.current?.getSelection();
    const query = initialQuery !== undefined ? initialQuery : (selection ? selection.trim() : searchQuery);
    setSearchQuery(query);
    setSearchOpen(true);
    if (query) {
      performSearch(query, caseSensitive, useRegex, true);
    }
    window.requestAnimationFrame(() => {
      searchInputRef.current?.focus();
      searchInputRef.current?.select();
    });
  }, [caseSensitive, performSearch, searchQuery, useRegex]);

  const closeSearch = useCallback(() => {
    setSearchOpen(false);
    searchAddonRef.current?.clearDecorations();
    terminalRef.current?.clearSelection();
    terminalRef.current?.focus();
  }, []);

  const findNext = useCallback(() => {
    performSearch(searchQuery, caseSensitive, useRegex);
  }, [caseSensitive, performSearch, searchQuery, useRegex]);

  const findPrevious = useCallback(() => {
    performSearch(searchQuery, caseSensitive, useRegex, false, true);
  }, [caseSensitive, performSearch, searchQuery, useRegex]);

  const handleSearchChange = (value: string) => {
    setSearchQuery(value);
    performSearch(value, caseSensitive, useRegex, true);
  };

  const toggleCaseSensitive = () => {
    const next = !caseSensitive;
    setCaseSensitive(next);
    performSearch(searchQuery, next, useRegex, false);
  };

  const toggleRegex = () => {
    const next = !useRegex;
    setUseRegex(next);
    performSearch(searchQuery, caseSensitive, next, false);
  };

  const handleSearchKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeSearch();
    } else if (event.key === 'Enter') {
      event.preventDefault();
      if (event.shiftKey) {
        findPrevious();
      } else {
        findNext();
      }
    } else if (event.altKey && event.key.toLowerCase() === 'c') {
      event.preventDefault();
      toggleCaseSensitive();
    } else if (event.altKey && event.key.toLowerCase() === 'r') {
      event.preventDefault();
      toggleRegex();
    }
  };

  const openCommandBar = useCallback(() => {
    setContextMenu(null);
    setCommandBarOpen(true);
    window.requestAnimationFrame(() => {
      commandInputRef.current?.focus();
      commandInputRef.current?.select();
    });
  }, []);

  const closeCommandBar = useCallback(() => {
    setCommandBarOpen(false);
    terminalRef.current?.focus();
  }, []);

  const toggleCommandBar = useCallback(() => {
    if (commandBarOpen) {
      closeCommandBar();
    } else {
      openCommandBar();
    }
  }, [closeCommandBar, commandBarOpen, openCommandBar]);

  const handleSendCommand = (event?: React.FormEvent) => {
    event?.preventDefault();
    const terminal = terminalRef.current;
    if (!commandText || !sessionRef.current || terminal?.options.disableStdin) return;
    const toSend = autoEnter ? `${commandText}\r` : commandText;
    sessionRef.current.send(encoder.encode(toSend));
    setCommandHistory((prev) => [commandText, ...prev.filter((c) => c !== commandText)].slice(0, 50));
    setHistoryIndex(-1);
    setCommandText('');
  };

  const handleCommandKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeCommandBar();
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      if (commandHistory.length === 0) return;
      const nextIdx = Math.min(historyIndex + 1, commandHistory.length - 1);
      setHistoryIndex(nextIdx);
      setCommandText(commandHistory[nextIdx]);
    } else if (event.key === 'ArrowDown') {
      event.preventDefault();
      if (historyIndex <= 0) {
        setHistoryIndex(-1);
        setCommandText('');
      } else {
        const nextIdx = historyIndex - 1;
        setHistoryIndex(nextIdx);
        setCommandText(commandHistory[nextIdx]);
      }
    }
  };

  const showVerificationWarning = status === 'idle'
    ? source === 'saved' && selected?.unverified
    : destination?.unverified;

  return (
    <section ref={paneRef} aria-label="SSH 终端" className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-[#161618]">
      <header className="flex shrink-0 flex-wrap items-center gap-2 border-b border-white/[0.08] bg-[#1e1e21] px-3 py-2 text-xs">
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
            className="max-w-full rounded border border-slate-700 bg-slate-800 px-2 py-1.5 text-slate-100"
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
        <div className="ml-auto flex items-center gap-1">
          <select
            aria-label="终端编码"
            title={status === 'idle' ? '终端编码' : '断开连接后可更换编码'}
            value={encoding}
            disabled={status !== 'idle'}
            onChange={(event) => setEncoding(event.target.value as TerminalEncoding)}
            className="rounded border border-slate-700 bg-slate-800 px-2 py-1 text-slate-100 disabled:opacity-60"
          >
            <option value="utf-8">UTF-8</option>
            <option value="gbk">GBK</option>
            <option value="gb18030">GB18030</option>
          </select>
          {onSplit && (
            <>
              <button type="button" aria-label="垂直分屏" title="垂直分屏（左右排列）"
                onClick={() => onSplit('columns', currentOrSelectedTarget?.target, encoding)}
                className="rounded p-1.5 text-slate-400 hover:bg-white/10 hover:text-white">
                <Columns2 className="h-4 w-4" />
              </button>
              <button type="button" aria-label="水平分屏" title="水平分屏（上下排列）"
                onClick={() => onSplit('rows', currentOrSelectedTarget?.target, encoding)}
                className="rounded p-1.5 text-slate-400 hover:bg-white/10 hover:text-white">
                <Rows2 className="h-4 w-4" />
              </button>
            </>
          )}
          {onBrowserProxy && (
            <button type="button" aria-label="在本机配置 Chrome 代理" title="在本机配置 Chrome 代理"
              disabled={!hasTarget} onClick={() => {
                if (currentOrSelectedTarget) onBrowserProxy(currentOrSelectedTarget.target);
              }}
              className="rounded p-1.5 text-slate-400 hover:bg-white/10 hover:text-white disabled:opacity-40">
              <ExternalLink className="h-4 w-4" />
            </button>
          )}
          {onClosePane && (
            <button type="button" aria-label="关闭分屏" title="关闭分屏" onClick={onClosePane}
              className="rounded p-1.5 text-slate-400 hover:bg-white/10 hover:text-white">
              <X className="h-4 w-4" />
            </button>
          )}
          {!directFormVisible && (
            <button
              type="button"
              aria-label="终端操作"
              title="终端操作"
              aria-expanded={contextMenu !== null}
              onClick={(event) => {
                event.stopPropagation();
                const { left, bottom } = event.currentTarget.getBoundingClientRect();
                const selection = terminalRef.current?.getSelection() ?? '';
                setContextMenu((menu) => menu ? null : terminalMenu(left - 200, bottom + 4, selection));
              }}
              className="rounded p-1.5 text-slate-400 hover:bg-white/10 hover:text-white"
            >
              <Ellipsis className="h-4 w-4" />
            </button>
          )}
        </div>
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
      <div className={directFormVisible ? 'hidden' : 'relative min-h-0 min-w-0 flex-1 overflow-hidden bg-[#10141c] p-3'}>
        <div ref={viewport} className="h-full w-full" aria-label="终端输入与输出" />
        {searchOpen && (
          <div className="absolute inset-x-2 top-2 z-20 ml-auto flex max-w-sm flex-wrap items-center gap-1.5 rounded-xl border border-white/[0.12] bg-[#1e1e22]/95 px-3 py-1.5 text-xs text-slate-200 shadow-2xl backdrop-blur-2xl">
            <Search className="h-3.5 w-3.5 shrink-0 text-slate-400" />
            <input
              ref={searchInputRef}
              type="text"
              value={searchQuery}
              onChange={(e) => handleSearchChange(e.target.value)}
              onKeyDown={handleSearchKeyDown}
              placeholder="查找…"
              className="min-w-20 flex-1 bg-transparent font-mono text-xs text-slate-100 outline-none placeholder:text-slate-500"
            />
            {searchQuery && (
              <span aria-live="polite" className="min-w-[36px] select-none text-center font-mono text-[11px] text-slate-400">
                {searchError || (searchResultCount > 0
                  ? `${searchResultIndex + 1}/${searchResultCount}`
                  : '无结果')}
              </span>
            )}
            <div className="mx-0.5 h-4 w-[1px] bg-white/[0.12]" />
            <button
              type="button"
              title="区分大小写 (Alt+C)"
              onClick={toggleCaseSensitive}
              className={`rounded p-1 transition-colors ${
                caseSensitive
                  ? 'bg-indigo-600/40 text-indigo-300'
                  : 'text-slate-400 hover:bg-white/[0.08] hover:text-slate-200'
              }`}
            >
              <CaseSensitive className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              title="正则表达式 (Alt+R)"
              onClick={toggleRegex}
              className={`rounded p-1 transition-colors ${
                useRegex
                  ? 'bg-indigo-600/40 text-indigo-300'
                  : 'text-slate-400 hover:bg-white/[0.08] hover:text-slate-200'
              }`}
            >
              <Regex className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              title="上一个 (Shift+Enter)"
              onClick={findPrevious}
              disabled={!searchQuery}
              className="rounded p-1 text-slate-400 transition-colors hover:bg-white/[0.08] hover:text-slate-200 disabled:opacity-30"
            >
              <ChevronUp className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              title="下一个 (Enter)"
              onClick={findNext}
              disabled={!searchQuery}
              className="rounded p-1 text-slate-400 transition-colors hover:bg-white/[0.08] hover:text-slate-200 disabled:opacity-30"
            >
              <ChevronDown className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              title="关闭查找 (Esc)"
              onClick={closeSearch}
              className="rounded p-1 text-slate-400 transition-colors hover:bg-white/[0.08] hover:text-slate-200"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        )}
      </div>
      {pendingPaste !== null && (
        <div role="alert" className="flex flex-wrap items-center gap-2 border-t border-amber-700/60 bg-amber-950/50 px-4 py-2 text-xs text-amber-100">
          <span className="flex-1">剪贴板内容含换行或控制字符，粘贴可能执行多条命令。</span>
          <button type="button" onClick={() => finishPaste(false)} className="rounded bg-slate-700 px-2 py-1 text-white">取消</button>
          <button type="button" onClick={() => finishPaste(true)} className="rounded bg-amber-600 px-2 py-1 font-medium text-white">继续粘贴</button>
        </div>
      )}
      {commandBarOpen && (
        <div className="flex shrink-0 flex-wrap items-center gap-2 border-t border-white/[0.08] bg-[#19191c] px-4 py-2">
          <div className="flex select-none items-center gap-1.5 text-xs text-indigo-400">
            <TerminalSquare className="h-4 w-4" />
            <span className="font-semibold">命令</span>
          </div>
          <form onSubmit={handleSendCommand} className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
            <input
              ref={commandInputRef}
              type="text"
              value={commandText}
              onChange={(e) => setCommandText(e.target.value)}
              onKeyDown={handleCommandKeyDown}
              placeholder={status === 'connected' ? '输入要在远端终端执行的命令，按 Enter 发送…' : '终端未连接，无法发送命令'}
              disabled={status !== 'connected'}
              className="min-w-20 flex-1 rounded border border-slate-700 bg-slate-800/80 px-2.5 py-1 font-mono text-xs text-slate-100 outline-none focus:border-indigo-500 disabled:opacity-50"
            />
            <label className="flex cursor-pointer select-none items-center gap-1.5 text-xs text-slate-400">
              <input
                type="checkbox"
                checked={autoEnter}
                onChange={(e) => setAutoEnter(e.target.checked)}
                className="rounded border-slate-700 bg-slate-800 text-indigo-600 focus:ring-0"
              />
              <span>回车执行</span>
            </label>
            <button
              type="submit"
              disabled={!commandText || status !== 'connected'}
              className="flex items-center gap-1 rounded bg-indigo-600 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
            >
              <span>发送</span>
              <CornerDownLeft className="h-3 w-3" />
            </button>
            <button
              type="button"
              title="关闭命令输入 (Esc)"
              onClick={closeCommandBar}
              className="rounded p-1 text-slate-400 transition-colors hover:bg-white/[0.08] hover:text-slate-200"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </form>
        </div>
      )}
      {contextMenu && (
        <div
          style={{
            top: Math.max(10, Math.min(contextMenu.y, window.innerHeight - 360)),
            left: Math.max(10, Math.min(contextMenu.x, window.innerWidth - 240)),
          }}
          onClick={(e) => e.stopPropagation()}
          className="fixed z-50 max-h-[calc(100vh-20px)] w-56 overflow-y-auto rounded-xl border border-white/[0.12] bg-[#1e1e22]/95 p-1.5 text-xs text-slate-200 shadow-2xl backdrop-blur-2xl"
        >
          <div className="px-2.5 py-1 text-[11px] font-medium text-slate-400">操作</div>
          <button
            type="button"
            disabled={!contextMenu.hasSelection}
            onClick={handleCopySelection}
            className={`group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors ${
              !contextMenu.hasSelection
                ? 'cursor-not-allowed text-slate-500 opacity-40'
                : 'cursor-pointer text-slate-200 hover:bg-[#0a84ff] hover:text-white'
            }`}
          >
            <Copy className="h-3.5 w-3.5 shrink-0 opacity-80" />
            <span className="flex-1 truncate">复制</span>
            <span className="ml-auto font-mono text-[10.5px] text-slate-400 group-hover:text-white/80">
              {isMac ? '⌘+⇧+C' : 'Ctrl+Shift+C'}
            </span>
          </button>
          <button
            type="button"
            disabled={status !== 'connected' || Boolean(terminalRef.current?.options.disableStdin)}
            onClick={() => void handleClipboardPaste()}
            className={`group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors ${
              status !== 'connected' || Boolean(terminalRef.current?.options.disableStdin)
                ? 'cursor-not-allowed text-slate-500 opacity-40'
                : 'cursor-pointer text-slate-200 hover:bg-[#0a84ff] hover:text-white'
            }`}
          >
            <Clipboard className="h-3.5 w-3.5 shrink-0 opacity-80" />
            <span className="flex-1 truncate">粘贴</span>
            <span className="ml-auto font-mono text-[10.5px] text-slate-400 group-hover:text-white/80">
              {isMac ? '⌘+⇧+V' : 'Ctrl+Shift+V'}
            </span>
          </button>
          <button
            type="button"
            disabled={status !== 'connected' || Boolean(terminalRef.current?.options.disableStdin) || !contextMenu.hasSelection}
            onClick={handlePasteSelection}
            className={`group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors ${
              status !== 'connected' || Boolean(terminalRef.current?.options.disableStdin) || !contextMenu.hasSelection
                ? 'cursor-not-allowed text-slate-500 opacity-40'
                : 'cursor-pointer text-slate-200 hover:bg-[#0a84ff] hover:text-white'
            }`}
          >
            <ClipboardPaste className="h-3.5 w-3.5 shrink-0 opacity-80" />
            <span className="flex-1 truncate">粘贴选中文本</span>
          </button>
          <button
            type="button"
            onClick={() => openSearch()}
            className="group flex w-full cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs text-slate-200 transition-colors hover:bg-[#0a84ff] hover:text-white"
          >
            <Search className="h-3.5 w-3.5 shrink-0 opacity-80" />
            <span className="flex-1 truncate">搜索</span>
            <span className="ml-auto font-mono text-[10.5px] text-slate-400 group-hover:text-white/80">
              {isMac ? '⌘+F' : 'Ctrl+F'}
            </span>
          </button>
          <div className="my-1 border-t border-white/[0.08]" />
          <button
            type="button"
            disabled={!hasTarget || !onCloneTerminal}
            onClick={handleCloneTab}
            className={`group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors ${
              !hasTarget || !onCloneTerminal
                ? 'cursor-not-allowed text-slate-500 opacity-40'
                : 'cursor-pointer text-slate-200 hover:bg-[#0a84ff] hover:text-white'
            }`}
          >
            <CopyPlus className="h-3.5 w-3.5 shrink-0 opacity-80" />
            <span className="flex-1 truncate">复用连接打开新标签</span>
          </button>
          <button
            type="button"
            disabled={!hasTarget}
            onClick={handleReconnect}
            className={`group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors ${
              !hasTarget
                ? 'cursor-not-allowed text-slate-500 opacity-40'
                : 'cursor-pointer text-slate-200 hover:bg-[#0a84ff] hover:text-white'
            }`}
          >
            <RotateCw className="h-3.5 w-3.5 shrink-0 opacity-80" />
            <span className="flex-1 truncate">重新连接</span>
            <span className="ml-auto font-mono text-[10.5px] text-slate-400 group-hover:text-white/80">
              Alt+Shift+R
            </span>
          </button>
          <button
            type="button"
            onClick={handleClearScreen}
            className="group flex w-full cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs text-slate-200 transition-colors hover:bg-[#0a84ff] hover:text-white"
          >
            <Trash2 className="h-3.5 w-3.5 shrink-0 opacity-80" />
            <span className="flex-1 truncate">清屏</span>
            <span className="ml-auto font-mono text-[10.5px] text-slate-400 group-hover:text-white/80">
              Alt+Shift+K
            </span>
          </button>
          <button
            type="button"
            disabled={status === 'idle'}
            onClick={handleDisconnect}
            className={`group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors ${
              status === 'idle'
                ? 'cursor-not-allowed text-slate-500 opacity-40'
                : 'cursor-pointer text-slate-200 hover:bg-[#0a84ff] hover:text-white'
            }`}
          >
            <PowerOff className="h-3.5 w-3.5 shrink-0 opacity-80" />
            <span className="flex-1 truncate">断开连接</span>
            <span className="ml-auto font-mono text-[10.5px] text-slate-400 group-hover:text-white/80">
              Alt+Shift+W
            </span>
          </button>
          <div className="my-1 border-t border-white/[0.08]" />
          <button
            type="button"
            disabled={status !== 'connected'}
            onClick={openCommandBar}
            className={`group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors ${
              status !== 'connected'
                ? 'cursor-not-allowed text-slate-500 opacity-40'
                : 'cursor-pointer text-slate-200 hover:bg-[#0a84ff] hover:text-white'
            }`}
          >
            <TerminalSquare className="h-3.5 w-3.5 shrink-0 opacity-80" />
            <span className="flex-1 truncate">命令输入</span>
            <span className="ml-auto font-mono text-[10.5px] text-slate-400 group-hover:text-white/80">
              {isMac ? '⌥+I' : 'Alt+I'}
            </span>
          </button>
        </div>
      )}
      <footer className="flex shrink-0 flex-wrap justify-between gap-2 border-t border-white/[0.08] bg-[#1e1e21] px-4 py-2 text-xs text-slate-400">
        <span role="status">{notice || (status === 'connected' ? '已连接' : status === 'connecting' ? '正在连接' : '未连接')}</span>
        <span>右键菜单 · 搜索 ({isMac ? '⌘F' : 'Ctrl+F'}) · 命令输入 ({isMac ? '⌥I' : 'Alt+I'}) · Ctrl+滚轮 调整字号 ({fontSize}px)</span>
      </footer>
    </section>
  );
}
