import type { Extension } from '@codemirror/state';
import { defaultHighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { EditorView } from '@codemirror/view';
import { oneDark } from '@codemirror/theme-one-dark';
import type { ITheme } from '@xterm/xterm';

const lightEditorTheme = [
  EditorView.theme({
    '&': { color: '#243247', backgroundColor: '#ffffff' },
    '.cm-content': { caretColor: '#1d4ed8' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: '#1d4ed8' },
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
      backgroundColor: '#cbdffa',
    },
    '.cm-panels': { backgroundColor: '#f2f5f9', color: '#243247' },
    '.cm-panels.cm-panels-top': { borderBottom: '1px solid #cbd2de' },
    '.cm-panels.cm-panels-bottom': { borderTop: '1px solid #cbd2de' },
    '.cm-panel input, .cm-panel button': { color: '#243247', backgroundColor: '#ffffff' },
    '.cm-activeLine': { backgroundColor: '#edf4fd' },
    '.cm-selectionMatch': { backgroundColor: '#dbeafe' },
    '.cm-searchMatch': { backgroundColor: '#fff0ad', outline: '1px solid #a66b00' },
    '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: '#ffdc73' },
    '&.cm-focused .cm-matchingBracket': { backgroundColor: '#dce8ff', outline: '1px solid #5a79b6' },
    '.cm-gutters': { color: '#66758a', backgroundColor: '#f5f7fa', borderRight: '1px solid #d8dee8' },
    '.cm-activeLineGutter': { color: '#243247', backgroundColor: '#e5eefb' },
    '.cm-foldPlaceholder': { color: '#475569', backgroundColor: '#e8edf4', borderColor: '#cbd2de' },
    '.cm-tooltip': { color: '#243247', backgroundColor: '#ffffff', border: '1px solid #cbd2de' },
    '.cm-tooltip .cm-tooltip-arrow:before': { borderTopColor: '#cbd2de', borderBottomColor: '#cbd2de' },
    '.cm-tooltip .cm-tooltip-arrow:after': { borderTopColor: '#ffffff', borderBottomColor: '#ffffff' },
    '.cm-tooltip-autocomplete ul li[aria-selected]': { color: '#ffffff', backgroundColor: '#155cb6' },
  }, { dark: false }),
  syntaxHighlighting(defaultHighlightStyle),
];

export function sqlEditorTheme(theme: 'dark' | 'light'): Extension {
  return theme === 'dark' ? oneDark : lightEditorTheme;
}

export function terminalTheme(theme: 'dark' | 'light'): ITheme {
  if (theme === 'dark') {
    return { background: '#10141c', foreground: '#d9e3f4', cursor: '#a3bfff', selectionBackground: '#344775' };
  }
  return {
    background: '#ffffff',
    foreground: '#243247',
    cursor: '#1d4ed8',
    cursorAccent: '#ffffff',
    selectionBackground: '#bfd8fb',
    selectionInactiveBackground: '#dbe6f4',
    selectionForeground: '#172a45',
    black: '#1f2937',
    red: '#b42318',
    green: '#187139',
    yellow: '#8a5b00',
    blue: '#175cd3',
    magenta: '#8b2bb9',
    cyan: '#087585',
    white: '#667085',
    brightBlack: '#58677a',
    brightRed: '#c43227',
    brightGreen: '#16763f',
    brightYellow: '#946200',
    brightBlue: '#245cbd',
    brightMagenta: '#932aad',
    brightCyan: '#087080',
    brightWhite: '#344054',
  };
}

export function terminalSearchTheme(theme: 'dark' | 'light') {
  return theme === 'dark' ? {
    matchBackground: '#344775',
    matchBorder: '#4a6baf',
    matchOverviewRuler: '#344775',
    activeMatchBackground: '#e5a50a',
    activeMatchBorder: '#ffd700',
    activeMatchColorOverviewRuler: '#e5a50a',
  } : {
    matchBackground: '#fff0ad',
    matchBorder: '#946200',
    matchOverviewRuler: '#946200',
    activeMatchBackground: '#f6c453',
    activeMatchBorder: '#794d00',
    activeMatchColorOverviewRuler: '#794d00',
  };
}
