import React, { useEffect, useRef, useState } from 'react';
import { basicSetup } from 'codemirror';
import { Compartment, EditorState } from '@codemirror/state';
import { EditorView, keymap } from '@codemirror/view';
import { MSSQL, MySQL, PostgreSQL, SQLite, StandardSQL, sql } from '@codemirror/lang-sql';
import { oneDark } from '@codemirror/theme-one-dark';
import { Clock, Download, Eraser, Loader2, Play, Sparkles, Terminal } from 'lucide-react';
import { api, ApiError } from '../api/client';
import type { ConnectionView, EngineCapabilities, EngineType, QueryResult } from '../types/db';
import { ImportExportModal } from './ImportExportModal';

interface Props {
  connections: ConnectionView[];
  capabilities: Record<EngineType, EngineCapabilities>;
  connectionId: string;
  engine: EngineType;
  database: string;
  schema: string;
  initialSql?: string;
  onContextChange?: (conn: ConnectionView, database: string, schema: string) => void;
}

interface SqlHistoryItem {
  sql: string;
  connName: string;
  database: string;
  time: string;
}

const HISTORY_STORAGE_KEY = 'webdb_sql_history_v1';

function loadHistory(): SqlHistoryItem[] {
  try {
    const raw = localStorage.getItem(HISTORY_STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.slice(0, 30) : [];
  } catch {
    return [];
  }
}

function saveHistoryItem(item: SqlHistoryItem): SqlHistoryItem[] {
  const prev = loadHistory().filter((h) => h.sql !== item.sql);
  const next = [item, ...prev].slice(0, 30);
  try {
    localStorage.setItem(HISTORY_STORAGE_KEY, JSON.stringify(next));
  } catch {
    // ignore storage quota errors
  }
  return next;
}

function getSqlDialect(engine: EngineType) {
  switch (engine) {
    case 'mysql':
      return MySQL;
    case 'postgres':
      return PostgreSQL;
    case 'sqlite':
      return SQLite;
    case 'sqlserver':
      return MSSQL;
    default:
      return StandardSQL;
  }
}

function defaultTemplateForEngine(engine: EngineType): string {
  switch (engine) {
    case 'oracle':
      return '-- 按 Ctrl+Enter / Cmd+Enter 执行 SQL\nSELECT 1 FROM DUAL;\n';
    case 'dameng':
      return '-- 按 Ctrl+Enter / Cmd+Enter 执行 SQL\nSELECT BANNER FROM V$VERSION;\n';
    case 'sqlite':
      return "-- 按 Ctrl+Enter / Cmd+Enter 执行 SQL\nSELECT name, type FROM sqlite_master WHERE type IN ('table', 'view');\n";
    default:
      return '-- 按 Ctrl+Enter / Cmd+Enter 执行 SQL\nSELECT 1 AS ok;\n';
  }
}

export const SqlWorkspace: React.FC<Props> = ({
  connections,
  capabilities,
  connectionId,
  engine,
  database,
  schema,
  initialSql,
  onContextChange,
}) => {
  const [selectedConnId, setSelectedConnId] = useState(connectionId);
  const [selectedDb, setSelectedDb] = useState(database);
  const [selectedSchema, setSelectedSchema] = useState(schema);
  const [dbList, setDbList] = useState<string[]>([]);
  const [schemaList, setSchemaList] = useState<string[]>([]);
  const [maxRows, setMaxRows] = useState(1000);

  const activeConn = connections.find((c) => c.id === selectedConnId) || connections[0];
  const activeEngine: EngineType = activeConn?.engine || engine;
  const supportsSchemas = capabilities[activeEngine]?.supportsSchemas ?? false;

  const editorHostRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const sqlCompartmentRef = useRef<Compartment>(new Compartment());
  const runRef = useRef<(selectedOnly: boolean) => void>(() => {});

  const [sqlSchema, setSqlSchema] = useState<Record<string, string[]>>({});
  const [running, setRunning] = useState(false);
  const [results, setResults] = useState<QueryResult[]>([]);
  const [activeResultIdx, setActiveResultIdx] = useState(0);
  const [error, setError] = useState('');
  const [exportSql, setExportSql] = useState<string | null>(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [history, setHistory] = useState<SqlHistoryItem[]>(() => loadHistory());

  // Sync external initial connection if list loads later
  useEffect(() => {
    if (!selectedConnId && connections.length > 0) {
      setSelectedConnId(connections[0].id);
      setSelectedDb(connections[0].database || '');
    }
  }, [connections, selectedConnId]);

  // Load databases for selected connection
  useEffect(() => {
    if (!selectedConnId) return;
    let cancelled = false;
    api
      .listDatabases(selectedConnId)
      .then((res) => {
        if (cancelled) return;
        const dbs = res.databases || [];
        setDbList(dbs);
        if (!selectedDb && dbs.length > 0) {
          setSelectedDb(dbs[0]);
        }
      })
      .catch(() => {
        if (!cancelled) setDbList([]);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedConnId]);

  // Load schemas when engine supports schemas
  useEffect(() => {
    if (!selectedConnId || !supportsSchemas) {
      setSchemaList([]);
      return;
    }
    let cancelled = false;
    api
      .listSchemas(selectedConnId, selectedDb)
      .then((res) => {
        if (cancelled) return;
        const schemas = res.schemas || [];
        setSchemaList(schemas);
        if (!selectedSchema && schemas.length > 0) {
          setSelectedSchema(schemas[0]);
        }
      })
      .catch(() => {
        if (!cancelled) setSchemaList([]);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedConnId, selectedDb, supportsSchemas]);

  // Load autocomplete metadata
  useEffect(() => {
    if (!selectedConnId) return;
    let cancelled = false;
    api
      .autocomplete(selectedConnId, selectedDb, selectedSchema)
      .then((res) => {
        if (cancelled) return;
        const map: Record<string, string[]> = {};
        for (const t of res.tables || []) {
          map[t.name] = t.columns || [];
          if (t.schema) {
            map[`${t.schema}.${t.name}`] = t.columns || [];
          }
        }
        setSqlSchema(map);
      })
      .catch(() => {
        // best-effort
      });
    return () => {
      cancelled = true;
    };
  }, [selectedConnId, selectedDb, selectedSchema]);

  const executeQuery = async (selectedOnly: boolean) => {
    const view = viewRef.current;
    if (!view || !selectedConnId) return;
    const sel = view.state.selection.main;
    let queryText = view.state.doc.toString();
    if (selectedOnly || !sel.empty) {
      const selectedText = view.state.sliceDoc(sel.from, sel.to).trim();
      if (selectedText) {
        queryText = selectedText;
      }
    }
    if (!queryText.trim()) return;

    setRunning(true);
    setError('');
    setHistory(
      saveHistoryItem({
        sql: queryText.trim(),
        connName: activeConn?.name || '',
        database: selectedDb,
        time: new Date().toLocaleTimeString(),
      }),
    );
    try {
      const res = await api.executeSQL({
        connectionId: selectedConnId,
        database: selectedDb,
        schema: selectedSchema,
        sql: queryText,
        maxRows,
      });
      setResults(res.results || []);
      setActiveResultIdx(0);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'SQL 执行失败');
    } finally {
      setRunning(false);
    }
  };

  runRef.current = (selectedOnly: boolean) => {
    void executeQuery(selectedOnly);
  };

  // Mount CodeMirror once
  useEffect(() => {
    const host = editorHostRef.current;
    if (!host) return;

    const sqlCompartment = sqlCompartmentRef.current;
    const state = EditorState.create({
      doc: initialSql || defaultTemplateForEngine(activeEngine),
      extensions: [
        basicSetup,
        oneDark,
        sqlCompartment.of(
          sql({
            dialect: getSqlDialect(activeEngine),
            schema: sqlSchema,
            upperCaseKeywords: true,
          }),
        ),
        keymap.of([
          {
            key: 'Mod-Enter',
            run: () => {
              runRef.current(false);
              return true;
            },
          },
          {
            key: 'Ctrl-Enter',
            run: () => {
              runRef.current(false);
              return true;
            },
          },
        ]),
      ],
    });

    const view = new EditorView({ state, parent: host });
    viewRef.current = view;

    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, []);

  // Reconfigure SQL dialect & autocomplete schema without wiping typed text
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    view.dispatch({
      effects: sqlCompartmentRef.current.reconfigure(
        sql({
          dialect: getSqlDialect(activeEngine),
          schema: sqlSchema,
          upperCaseKeywords: true,
        }),
      ),
    });
  }, [activeEngine, sqlSchema]);

  const setEditorContent = (text: string) => {
    const view = viewRef.current;
    if (!view) return;
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: text },
    });
    view.focus();
  };

  const handleConnectionChange = (nextId: string) => {
    const nextConn = connections.find((c) => c.id === nextId);
    setSelectedConnId(nextId);
    const nextDb = nextConn?.database || '';
    setSelectedDb(nextDb);
    setSelectedSchema('');
    setDbList([]);
    setSchemaList([]);
    if (nextConn && onContextChange) {
      onContextChange(nextConn, nextDb, '');
    }
  };

  const handleDbChange = (nextDb: string) => {
    setSelectedDb(nextDb);
    setSelectedSchema('');
    if (activeConn && onContextChange) {
      onContextChange(activeConn, nextDb, '');
    }
  };

  const handleSchemaChange = (nextSch: string) => {
    setSelectedSchema(nextSch);
    if (activeConn && onContextChange) {
      onContextChange(activeConn, selectedDb, nextSch);
    }
  };

  const currentResult = results[activeResultIdx];

  return (
    <div className="relative flex h-full flex-col">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.08] bg-[#1e1e21] px-3 py-1.5">
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            disabled={running || !selectedConnId}
            onClick={() => void executeQuery(false)}
            className="apple-btn-primary"
          >
            {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
            运行 (Ctrl+Enter)
          </button>
          <button
            type="button"
            disabled={running || !selectedConnId}
            onClick={() => void executeQuery(true)}
            className="apple-btn-secondary"
          >
            运行选中
          </button>

          {/* Connection / Database / Schema selectors */}
          <div className="flex items-center gap-1.5 border-l border-white/[0.08] pl-2">
            <select
              value={selectedConnId}
              onChange={(e) => handleConnectionChange(e.target.value)}
              className="apple-input max-w-[160px]"
              title="选择数据库连接"
            >
              {connections.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>

            <select
              value={selectedDb}
              onChange={(e) => handleDbChange(e.target.value)}
              className="apple-input max-w-[140px]"
              title="选择数据库"
            >
              {dbList.length === 0 && <option value={selectedDb}>{selectedDb || '默认库'}</option>}
              {dbList.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>

            {supportsSchemas && (
              <select
                value={selectedSchema}
                onChange={(e) => handleSchemaChange(e.target.value)}
                className="apple-input max-w-[130px]"
                title="选择 Schema"
              >
                {schemaList.length === 0 && <option value={selectedSchema}>{selectedSchema || '默认 Schema'}</option>}
                {schemaList.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            )}
          </div>

          <select
            value={maxRows}
            onChange={(e) => setMaxRows(Number(e.target.value))}
            className="apple-input"
            title="单次查询最大返回行数"
          >
            <option value={100}>限 100 行</option>
            <option value={500}>限 500 行</option>
            <option value={1000}>限 1000 行</option>
            <option value={5000}>限 5000 行</option>
          </select>
        </div>

        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={() => setHistoryOpen((v) => !v)}
            className="apple-btn-secondary"
          >
            <Clock className="h-3.5 w-3.5 text-[#ff9f0a]" />
            历史 ({history.length})
          </button>
          <button
            type="button"
            onClick={() => setEditorContent('')}
            className="apple-btn-secondary"
            title="清空编辑器"
          >
            <Eraser className="h-3.5 w-3.5" />
            清空
          </button>
          {currentResult && currentResult.columns && currentResult.columns.length > 0 && currentResult.statement && (
            <button
              type="button"
              onClick={() => setExportSql(currentResult.statement || null)}
              className="apple-btn-secondary"
            >
              <Download className="h-3.5 w-3.5 text-[#30d158]" />
              导出结果
            </button>
          )}
        </div>
      </div>

      {/* History drawer */}
      {historyOpen && (
        <div className="absolute right-3 top-12 z-30 max-h-80 w-96 overflow-y-auto rounded-lg border border-slate-700 bg-slate-900 p-2 shadow-2xl">
          <div className="mb-1.5 flex items-center justify-between px-1 text-xs font-semibold text-slate-300">
            <span>最近执行的 SQL</span>
            <button type="button" onClick={() => setHistoryOpen(false)} className="text-slate-400 hover:text-slate-200">
              关闭
            </button>
          </div>
          {history.length === 0 ? (
            <div className="p-3 text-center text-xs text-slate-500">暂无执行历史</div>
          ) : (
            <div className="space-y-1">
              {history.map((item, i) => (
                <button
                  key={i}
                  type="button"
                  onClick={() => {
                    setEditorContent(item.sql);
                    setHistoryOpen(false);
                  }}
                  className="w-full rounded border border-slate-800 bg-slate-950/70 p-2 text-left hover:border-indigo-700 hover:bg-slate-800"
                >
                  <div className="mb-1 flex items-center justify-between text-[10px] text-slate-400">
                    <span>
                      {item.connName} {item.database ? `/ ${item.database}` : ''}
                    </span>
                    <span>{item.time}</span>
                  </div>
                  <div className="line-clamp-2 font-mono text-xs text-slate-200">{item.sql}</div>
                </button>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Editor */}
      <div className="h-64 shrink-0 border-b border-slate-800 bg-[#282c34]" ref={editorHostRef} />

      <div className="flex items-center justify-between border-b border-slate-800 bg-slate-900/60 px-3 py-1 text-[11px] text-slate-400">
        <span className="flex items-center gap-1.5">
          <Sparkles className="h-3 w-3 text-indigo-400" />
          支持多条 SQL 分号分隔批量执行，选中文本时可点击「运行选中」只执行片段；输入表名/列名支持自动补全。
        </span>
        <span className="flex items-center gap-1 font-mono text-slate-400">
          <Terminal className="h-3 w-3" />
          {activeEngine.toUpperCase()}
        </span>
      </div>

      {error && (
        <div className="border-b border-rose-800 bg-rose-950/80 px-3 py-2 text-xs text-rose-200">
          {error}
        </div>
      )}

      {/* Results area */}
      <div className="flex min-h-0 flex-1 flex-col bg-slate-950">
        {results.length > 0 ? (
          <>
            <div className="flex items-center gap-1 overflow-x-auto border-b border-slate-800 bg-slate-900/70 px-2 py-1">
              {results.map((r, i) => (
                <button
                  key={i}
                  type="button"
                  onClick={() => setActiveResultIdx(i)}
                  className={`whitespace-nowrap rounded px-2.5 py-1 text-xs ${
                    i === activeResultIdx
                      ? 'bg-indigo-600 text-white'
                      : r.error
                      ? 'bg-rose-950 text-rose-300 hover:bg-rose-900'
                      : 'text-slate-300 hover:bg-slate-800'
                  }`}
                >
                  结果 #{i + 1} ({r.durationMs}ms)
                </button>
              ))}
            </div>

            {currentResult && (
              <div className="flex min-h-0 flex-1 flex-col">
                {currentResult.error ? (
                  <div className="m-3 rounded border border-rose-800 bg-rose-950/70 p-3 font-mono text-xs text-rose-200">
                    <div className="mb-1 font-semibold">执行报错：</div>
                    <div>{currentResult.error}</div>
                    {currentResult.statement && (
                      <pre className="mt-2 whitespace-pre-wrap text-slate-400">{currentResult.statement}</pre>
                    )}
                  </div>
                ) : currentResult.columns && currentResult.columns.length > 0 ? (
                  <>
                    <div className="flex-1 overflow-auto">
                      <table className="w-full border-collapse text-left text-[13.5px]">
                        <thead className="sticky top-0 z-10 bg-slate-900">
                          <tr>
                            <th className="border-b border-r border-slate-800 px-2.5 py-2.5 text-center font-medium text-slate-500">
                              #
                            </th>
                            {currentResult.columns.map((col) => (
                              <th
                                key={col.name}
                                className="whitespace-nowrap border-b border-r border-slate-800 px-3.5 py-2.5 font-medium text-slate-200"
                              >
                                <div>{col.name}</div>
                                {col.type && <div className="mt-0.5 text-[11.5px] font-normal text-slate-400">{col.type}</div>}
                              </th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {(currentResult.rows || []).map((row, rIdx) => (
                            <tr key={rIdx} className="hover:bg-slate-900/80">
                              <td className="border-b border-r border-slate-800/80 px-2.5 py-2 text-center text-slate-500">
                                {rIdx + 1}
                              </td>
                              {row.map((cell, cIdx) => (
                                <td
                                  key={cIdx}
                                  className="max-w-xs truncate border-b border-r border-slate-800/80 px-3.5 py-2 font-mono text-slate-200"
                                  title={cell === null ? 'NULL' : String(cell)}
                                >
                                  {cell === null ? (
                                    <span className="italic text-slate-500">NULL</span>
                                  ) : (
                                    String(cell)
                                  )}
                                </td>
                              ))}
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                    <div className="border-t border-slate-800 bg-slate-900 px-3.5 py-2 text-[13px] text-slate-400">
                      返回 {currentResult.rowCount} 行 · 耗时 {currentResult.durationMs} ms
                      {currentResult.truncated ? '（已按行上限截断）' : ''}
                    </div>
                  </>
                ) : (
                  <div className="m-3 rounded border border-emerald-800 bg-emerald-950/50 p-3 text-xs text-emerald-200">
                    执行成功：影响 {currentResult.affectedRows ?? 0} 行，耗时 {currentResult.durationMs} ms
                    {currentResult.statement && (
                      <pre className="mt-2 whitespace-pre-wrap font-mono text-slate-400">{currentResult.statement}</pre>
                    )}
                  </div>
                )}
              </div>
            )}
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center text-xs text-slate-500">
            在上方编写 SQL 语句后按 Ctrl+Enter 或点击「运行」查看结果
          </div>
        )}
      </div>

      {exportSql && (
        <ImportExportModal
          mode="export"
          connectionId={selectedConnId}
          database={selectedDb}
          schema={selectedSchema}
          table=""
          sql={exportSql}
          onClose={() => setExportSql(null)}
        />
      )}
    </div>
  );
};
