import { useSyncExternalStore } from 'react';
import { initializeTheme, type Theme } from './theme';
import type { TerminalTarget } from './components/TerminalSession';

interface SqlHistoryItem {
  sql: string;
  connName: string;
  database: string;
  time: string;
}

interface DesktopPreferences {
  theme?: Theme;
  sidebarWidth?: number;
  sqlHistory?: SqlHistoryItem[];
  databaseFontSize?: number;
  terminalFontSize?: number;
}

type FontSizePreference = 'databaseFontSize' | 'terminalFontSize';

declare global {
  interface Window {
    zidongDesktop?: {
      getPreferences(): Promise<DesktopPreferences>;
      setTheme(theme: Theme): Promise<void>;
      onThemeChanged(callback: (theme: Theme) => void): () => void;
      setSidebarWidth(width: number): Promise<void>;
      setSqlHistory(history: SqlHistoryItem[]): Promise<void>;
      setFontSize(preference: FontSizePreference, size: number): Promise<void>;
      openTerminalWindow(): Promise<void>;
      pasteTerminal(): Promise<void>;
      openProxiedChrome(request: { target: TerminalTarget; url?: string }): Promise<void>;
    };
  }
}

export const isDesktop = import.meta.env.MODE === 'desktop';
export const SIDEBAR_WIDTH = { min: 220, max: 680, initial: 300 } as const;

const SIDEBAR_WIDTH_KEY = 'zd_sidebar_width_v2';
const SQL_HISTORY_KEY = 'webdb_sql_history_v1';
const MAX_HISTORY_ENTRIES = 30;
const MAX_DESKTOP_HISTORY_BYTES = 2 * 1024 * 1024;
const EMPTY_SQL_HISTORY: SqlHistoryItem[] = [];
const FONT_SIZE_DEFAULT = 14;
const FONT_SIZE_MIN = 10;
const FONT_SIZE_MAX = 32;

let desktopSidebarWidth: number | undefined;
let cachedHistory = EMPTY_SQL_HISTORY;
let cachedWebHistoryJson: string | null | undefined;
const historyListeners = new Set<() => void>();
const fontSizeListeners = new Set<() => void>();

function validFontSize(value: number | undefined): value is number {
  return value !== undefined && Number.isInteger(value) && value >= FONT_SIZE_MIN && value <= FONT_SIZE_MAX;
}

function loadWebFontSize(preference: FontSizePreference) {
  if (!isDesktop) {
    try {
      const value = Number(localStorage.getItem(`zd_${preference}`));
      if (validFontSize(value)) return value;
    } catch {
      // localStorage is optional.
    }
  }
  return FONT_SIZE_DEFAULT;
}

const fontSizes: Record<FontSizePreference, number> = {
  databaseFontSize: loadWebFontSize('databaseFontSize'),
  terminalFontSize: loadWebFontSize('terminalFontSize'),
};

export async function loadDesktopPreferences(): Promise<void> {
  try {
    if (!window.zidongDesktop) throw new Error('桌面首选项接口不可用');
    const preferences = await window.zidongDesktop.getPreferences();
    initializeTheme(preferences.theme);
    desktopSidebarWidth = preferences.sidebarWidth;
    cachedHistory = preferences.sqlHistory ?? EMPTY_SQL_HISTORY;
    if (validFontSize(preferences.databaseFontSize)) fontSizes.databaseFontSize = preferences.databaseFontSize;
    if (validFontSize(preferences.terminalFontSize)) fontSizes.terminalFontSize = preferences.terminalFontSize;
  } catch {
    console.warn('无法读取桌面首选项');
  }
}

export function subscribeToFontSize(listener: () => void) {
  fontSizeListeners.add(listener);
  return () => { fontSizeListeners.delete(listener); };
}

function adjustFontSize(preference: FontSizePreference, step: number) {
  const size = Math.min(FONT_SIZE_MAX, Math.max(FONT_SIZE_MIN, fontSizes[preference] + step));
  if (size === fontSizes[preference]) return;
  fontSizes[preference] = size;
  if (isDesktop) {
    window.zidongDesktop?.setFontSize(preference, size).catch(() => console.warn('无法保存桌面字体大小'));
  } else {
    try {
      localStorage.setItem(`zd_${preference}`, String(size));
    } catch {
      // localStorage is optional.
    }
  }
  fontSizeListeners.forEach((listener) => listener());
}

export const readDatabaseFontSize = () => fontSizes.databaseFontSize;
export const readTerminalFontSize = () => fontSizes.terminalFontSize;
export const adjustDatabaseFontSize = (step: number) => adjustFontSize('databaseFontSize', step);
export const adjustTerminalFontSize = (step: number) => adjustFontSize('terminalFontSize', step);

export function readSidebarWidth(): number {
  let saved: number | undefined;
  if (isDesktop) {
    saved = desktopSidebarWidth;
  } else {
    try {
      saved = Number(localStorage.getItem(SIDEBAR_WIDTH_KEY));
    } catch {
      // localStorage is optional.
    }
  }

  return saved !== undefined && saved >= SIDEBAR_WIDTH.min && saved <= SIDEBAR_WIDTH.max
    ? saved
    : SIDEBAR_WIDTH.initial;
}

export function saveSidebarWidth(width: number): void {
  if (isDesktop) {
    desktopSidebarWidth = width;
    window.zidongDesktop?.setSidebarWidth(width).catch(() => {
      console.warn('无法保存桌面侧边栏宽度');
    });
    return;
  }

  try {
    localStorage.setItem(SIDEBAR_WIDTH_KEY, String(width));
  } catch {
    // localStorage is optional.
  }
}

function isSqlHistoryItem(value: unknown): value is SqlHistoryItem {
  if (value === null || typeof value !== 'object') return false;
  const item = value as Partial<SqlHistoryItem>;
  return typeof item.sql === 'string' && typeof item.connName === 'string'
    && typeof item.database === 'string' && typeof item.time === 'string';
}

function readSqlHistory(): SqlHistoryItem[] {
  if (isDesktop) return cachedHistory;

  let raw: string | null;
  try {
    raw = localStorage.getItem(SQL_HISTORY_KEY);
  } catch {
    return cachedHistory;
  }
  if (raw === cachedWebHistoryJson) return cachedHistory;

  cachedWebHistoryJson = raw;
  try {
    const parsed: unknown = JSON.parse(raw ?? 'null');
    cachedHistory = Array.isArray(parsed)
      ? parsed.filter(isSqlHistoryItem).slice(0, MAX_HISTORY_ENTRIES)
      : EMPTY_SQL_HISTORY;
  } catch {
    cachedHistory = EMPTY_SQL_HISTORY;
  }
  return cachedHistory;
}

function subscribeToSqlHistory(listener: () => void): () => void {
  historyListeners.add(listener);
  const onStorage = (event: StorageEvent) => {
    if (event.key === null || event.key === SQL_HISTORY_KEY) listener();
  };
  if (!isDesktop) window.addEventListener('storage', onStorage);
  return () => {
    historyListeners.delete(listener);
    if (!isDesktop) window.removeEventListener('storage', onStorage);
  };
}

export function useSqlHistory(): SqlHistoryItem[] {
  return useSyncExternalStore(subscribeToSqlHistory, readSqlHistory);
}

function limitDesktopHistory(entries: SqlHistoryItem[]): SqlHistoryItem[] {
  const encoder = new TextEncoder();
  let size = 2; // JSON array brackets.
  let count = 0;
  for (const entry of entries) {
    size += encoder.encode(JSON.stringify(entry)).byteLength + (count === 0 ? 0 : 1);
    if (size > MAX_DESKTOP_HISTORY_BYTES) break;
    count++;
  }
  return entries.slice(0, count);
}

export function saveSqlHistoryItem(item: SqlHistoryItem): void {
  const history = readSqlHistory();
  const next = [item, ...history.filter((entry) => entry.sql !== item.sql)].slice(0, MAX_HISTORY_ENTRIES);
  if (isDesktop) {
    const bounded = limitDesktopHistory(next);
    if (bounded.length === 0) return;
    cachedHistory = bounded;
    window.zidongDesktop?.setSqlHistory(bounded).catch(() => {
      console.warn('无法保存桌面 SQL 历史');
    });
  } else {
    cachedHistory = next;
    try {
      const raw = JSON.stringify(next);
      localStorage.setItem(SQL_HISTORY_KEY, raw);
      cachedWebHistoryJson = raw;
    } catch {
      // localStorage is optional.
    }
  }

  historyListeners.forEach((listener) => listener());
}
