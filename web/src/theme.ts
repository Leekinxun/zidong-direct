import { useSyncExternalStore } from 'react';

export type Theme = 'dark' | 'light';
const storageKey = 'zd_theme';
const desktop = import.meta.env.MODE === 'desktop';
const listeners = new Set<() => void>();
let current: Theme = document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
let initialized = false;

function applyTheme(theme: Theme) {
  current = theme;
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
  listeners.forEach((listener) => listener());
}

export function initializeTheme(saved?: Theme) {
  let theme = saved;
  if (!desktop) {
    try {
      theme = localStorage.getItem(storageKey) === 'light' ? 'light' : 'dark';
    } catch {
      theme = current;
    }
  }
  applyTheme(theme === 'light' ? 'light' : 'dark');
  if (initialized) return;
  initialized = true;
  if (desktop) {
    window.zidongDesktop?.onThemeChanged((value) => {
      if (value === 'dark' || value === 'light') applyTheme(value);
    });
  } else {
    window.addEventListener('storage', (event) => {
      if (event.key === storageKey || event.key === null) {
        applyTheme(event.newValue === 'light' ? 'light' : 'dark');
      }
    });
  }
}

export async function setTheme(theme: Theme) {
  applyTheme(theme);
  if (desktop) {
    if (!window.zidongDesktop) throw new Error('外观设置暂时无法保存');
    await window.zidongDesktop.setTheme(theme);
  } else {
    try {
      localStorage.setItem(storageKey, theme);
    } catch {
      throw new Error('当前浏览器无法保存外观设置');
    }
  }
}

export function useTheme(): Theme {
  return useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  }, () => current);
}
