import React, { useEffect, useState } from 'react';
import {
  ChevronDown,
  ChevronRight,
  Copy,
  Database,
  Edit3,
  Eye,
  FilePlus2,
  Folder,
  FolderOpen,
  Info,
  KeyRound,
  Layers,
  Loader2,
  Plus,
  RefreshCw,
  Search,
  Table2,
  Terminal,
  TerminalSquare,
  Trash2,
  Unplug,
  X,
} from 'lucide-react';
import { api, ApiError } from '../api/client';
import { readSidebarWidth, saveSidebarWidth, SIDEBAR_WIDTH } from '../preferences';
import type { ConnectionView, DatabaseObject, EngineCapabilities, EngineType } from '../types/db';
import type { ObjectCategory } from './ObjectBrowserPane';

export interface SelectedTreeContext {
  connection: ConnectionView;
  database: string;
  schema: string;
}

interface Props {
  connections: ConnectionView[];
  capabilities: Record<EngineType, EngineCapabilities>;
  selectedContext: SelectedTreeContext | null;
  selectedObject: DatabaseObject | null;
  onSelectContext: (ctx: SelectedTreeContext) => void;
  onSelectObject: (obj: DatabaseObject | null) => void;
  onShowDetails?: () => void;
  onOpenObjectsOverview?: (cat: ObjectCategory, ctx: SelectedTreeContext) => void;
  onNewConnection: (engine?: EngineType) => void;
  onEditConnection: (conn: ConnectionView) => void;
  onDeleteConnection: (conn: ConnectionView) => void;
  onDisconnectConnection: (conn: ConnectionView) => void;
  onOpenTableData: (conn: ConnectionView, database: string, schema: string, table: string) => void;
  onDesignTable: (conn: ConnectionView, database: string, schema: string, table: string, mode: 'create' | 'alter') => void;
  onOpenQuery: (conn: ConnectionView, database: string, schema: string, initialSql?: string) => void;
  onOpenSSHTerminal?: (conn: ConnectionView) => void;
  onSaveSSHHost?: (conn: ConnectionView) => Promise<void>;
  refreshSignal?: number;
  sshSection?: React.ReactNode;
  sshFirst?: boolean;
}

type TreeContextMenu =
  | { x: number; y: number; kind: 'conn'; conn: ConnectionView }
  | { x: number; y: number; kind: 'db'; conn: ConnectionView; database: string }
  | { x: number; y: number; kind: 'schema'; conn: ConnectionView; database: string; schema: string }
  | {
      x: number;
      y: number;
      kind: 'object';
      conn: ConnectionView;
      database: string;
      schema: string;
      object: DatabaseObject;
    };

const ENGINE_BADGES: Record<EngineType, { label: string; cls: string }> = {
  mysql: { label: 'MySQL', cls: 'bg-accent/20 text-icon-cyan border-accent/35' },
  postgres: { label: 'PG', cls: 'bg-icon-purple/25 text-icon-lavender border-icon-purple/40' },
  sqlite: { label: 'SQLite', cls: 'bg-icon-green/20 text-icon-mint border-icon-green/35' },
  sqlserver: { label: 'MSSQL', cls: 'bg-icon-orange/20 text-icon-amber border-icon-orange/35' },
  oracle: { label: 'Oracle', cls: 'bg-danger/20 text-icon-red border-danger/35' },
  dameng: { label: '达梦', cls: 'bg-icon-teal/20 text-icon-aqua border-icon-teal/35' },
};

export const SidebarTree: React.FC<Props> = ({
  connections,
  capabilities,
  selectedContext,
  selectedObject,
  onSelectContext,
  onSelectObject,
  onShowDetails,
  onOpenObjectsOverview,
  onNewConnection,
  onEditConnection,
  onDeleteConnection,
  onDisconnectConnection,
  onOpenTableData,
  onDesignTable,
  onOpenQuery,
  onOpenSSHTerminal,
  onSaveSSHHost,
  refreshSignal = 0,
  sshSection,
  sshFirst = false,
}) => {
  const [expandedConns, setExpandedConns] = useState<Record<string, boolean>>({});
  const [expandedDBs, setExpandedDBs] = useState<Record<string, boolean>>({});
  const [expandedSchemas, setExpandedSchemas] = useState<Record<string, boolean>>({});
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({});

  const [databasesByConn, setDatabasesByConn] = useState<Record<string, string[]>>({});
  const [schemasByDB, setSchemasByDB] = useState<Record<string, string[]>>({});
  const [objectsByKey, setObjectsByKey] = useState<Record<string, DatabaseObject[]>>({});
  const [loadingKeys, setLoadingKeys] = useState<Record<string, boolean>>({});
  const [treeError, setTreeError] = useState('');
  const [copyingSSHHost, setCopyingSSHHost] = useState(false);
  const [searchFilter, setSearchFilter] = useState('');
  const [contextMenu, setContextMenu] = useState<TreeContextMenu | null>(null);
  const [sidebarWidth, setSidebarWidth] = useState(readSidebarWidth);
  const [resizing, setResizing] = useState(false);

  const startResize = (e: React.MouseEvent) => {
    e.preventDefault();
    const startX = e.clientX;
    const startWidth = sidebarWidth;
    let latestWidth = startWidth;
    setResizing(true);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    const onMouseMove = (moveEvt: MouseEvent) => {
      const width = Math.round(startWidth + moveEvt.clientX - startX);
      latestWidth = Math.max(SIDEBAR_WIDTH.min, Math.min(SIDEBAR_WIDTH.max, width));
      setSidebarWidth(latestWidth);
    };

    const onMouseUp = () => {
      if (latestWidth !== startWidth) saveSidebarWidth(latestWidth);
      setResizing(false);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      window.removeEventListener('mousemove', onMouseMove);
      window.removeEventListener('mouseup', onMouseUp);
    };

    window.addEventListener('mousemove', onMouseMove);
    window.addEventListener('mouseup', onMouseUp);
  };

  useEffect(() => {
    const close = () => setContextMenu(null);
    window.addEventListener('click', close);
    return () => window.removeEventListener('click', close);
  }, []);

  useEffect(() => {
    if (refreshSignal <= 0 || !selectedContext) return;
    const { connection, database, schema } = selectedContext;
    if (!database) return;
    const supportsSchemas = capabilities[connection.engine]?.supportsSchemas ?? false;
    const key = supportsSchemas
      ? `${connection.id}::${database}::${schema}`
      : `${connection.id}::${database}::`;
    api
      .listObjects(connection.id, database, schema)
      .then((res) => {
        setObjectsByKey((p) => ({ ...p, [key]: res.objects || [] }));
      })
      .catch(() => {
        // ignore
      });
  }, [refreshSignal]);

  const setLoading = (k: string, v: boolean) =>
    setLoadingKeys((prev) => ({ ...prev, [k]: v }));

  const toggleConn = async (conn: ConnectionView, forceRefresh = false) => {
    onSelectObject(null);
    onSelectContext({
      connection: conn,
      database: conn.database || databasesByConn[conn.id]?.[0] || '',
      schema: '',
    });
    const isOpen = !!expandedConns[conn.id];
    if (isOpen && !forceRefresh) {
      setExpandedConns((p) => ({ ...p, [conn.id]: false }));
      return;
    }
    setExpandedConns((p) => ({ ...p, [conn.id]: true }));
    if (databasesByConn[conn.id] && !forceRefresh) return;

    setTreeError('');
    setLoading(`conn:${conn.id}`, true);
    try {
      const res = await api.listDatabases(conn.id);
      const dbs = res.databases || [];
      setDatabasesByConn((p) => ({ ...p, [conn.id]: dbs }));
      if (conn.engine === 'sqlite' && dbs.length > 0) {
        await toggleDatabase(conn, dbs[0], true);
      }
    } catch (err) {
      setTreeError(err instanceof ApiError ? err.message : '加载数据库失败');
    } finally {
      setLoading(`conn:${conn.id}`, false);
    }
  };

  const toggleDatabase = async (conn: ConnectionView, dbName: string, forceRefresh = false) => {
    onSelectObject(null);
    onSelectContext({ connection: conn, database: dbName, schema: '' });
    const key = `${conn.id}::${dbName}`;
    const isOpen = !!expandedDBs[key];
    if (isOpen && !forceRefresh) {
      setExpandedDBs((p) => ({ ...p, [key]: false }));
      return;
    }
    setExpandedDBs((p) => ({ ...p, [key]: true }));
    const supportsSchemas = capabilities[conn.engine]?.supportsSchemas ?? false;

    setTreeError('');
    setLoading(`db:${key}`, true);
    try {
      if (supportsSchemas) {
        if (!schemasByDB[key] || forceRefresh) {
          const res = await api.listSchemas(conn.id, dbName);
          const schemas = res.schemas || [];
          setSchemasByDB((p) => ({ ...p, [key]: schemas }));
          if (schemas.length > 0) {
            await toggleSchema(conn, dbName, schemas[0], true);
          }
        }
      } else {
        const objKey = `${key}::`;
        setExpandedGroups((p) => ({
          ...p,
          [`${objKey}:tables`]: true,
        }));
        if (!objectsByKey[objKey] || forceRefresh) {
          const res = await api.listObjects(conn.id, dbName, '');
          setObjectsByKey((p) => ({ ...p, [objKey]: res.objects || [] }));
        }
      }
    } catch (err) {
      setTreeError(err instanceof ApiError ? err.message : '加载对象列表失败');
    } finally {
      setLoading(`db:${key}`, false);
    }
  };

  const toggleSchema = async (conn: ConnectionView, dbName: string, schemaName: string, forceRefresh = false) => {
    onSelectObject(null);
    onSelectContext({ connection: conn, database: dbName, schema: schemaName });
    const key = `${conn.id}::${dbName}::${schemaName}`;
    const isOpen = !!expandedSchemas[key];
    if (isOpen && !forceRefresh) {
      setExpandedSchemas((p) => ({ ...p, [key]: false }));
      return;
    }
    setExpandedSchemas((p) => ({ ...p, [key]: true }));
    setExpandedGroups((p) => ({
      ...p,
      [`${key}:tables`]: true,
    }));
    if (objectsByKey[key] && !forceRefresh) return;

    setTreeError('');
    setLoading(`sch:${key}`, true);
    try {
      const res = await api.listObjects(conn.id, dbName, schemaName);
      setObjectsByKey((p) => ({ ...p, [key]: res.objects || [] }));
    } catch (err) {
      setTreeError(err instanceof ApiError ? err.message : '加载表结构失败');
    } finally {
      setLoading(`sch:${key}`, false);
    }
  };

  const toggleGroup = (groupKey: string) => {
    setExpandedGroups((p) => ({ ...p, [groupKey]: !(p[groupKey] ?? true) }));
  };

  const buildSelectSql = (conn: ConnectionView, sch: string, tbl: string) => {
    const q = (ident: string) => {
      if (conn.engine === 'mysql') return '`' + ident.replace(/`/g, '``') + '`';
      if (conn.engine === 'sqlserver') return '[' + ident.replace(/]/g, ']]') + ']';
      return '"' + ident.replace(/"/g, '""') + '"';
    };
    const target =
      conn.engine === 'postgres' || conn.engine === 'sqlserver' || conn.engine === 'oracle' || conn.engine === 'dameng'
        ? sch
          ? `${q(sch)}.${q(tbl)}`
          : q(tbl)
        : q(tbl);
    if (conn.engine === 'sqlserver') return `SELECT TOP 100 * FROM ${target};`;
    if (conn.engine === 'oracle') return `SELECT * FROM ${target} WHERE ROWNUM <= 100;`;
    return `SELECT * FROM ${target} LIMIT 100;`;
  };

  const renderObjectCategoryGroups = (
    conn: ConnectionView,
    dbName: string,
    schemaName: string,
    parentKey: string,
    objs: DatabaseObject[],
  ) => {
    const q = searchFilter.trim().toLowerCase();
    const filtered = q ? objs.filter((o) => o.name.toLowerCase().includes(q)) : objs;
    const tables = filtered.filter((o) => o.type === 'TABLE');
    const views = filtered.filter((o) => o.type === 'VIEW' || o.type === 'MATERIALIZED VIEW');

    const tablesGroupKey = `${parentKey}:tables`;
    const viewsGroupKey = `${parentKey}:views`;
    const isTablesOpen = expandedGroups[tablesGroupKey] ?? true;
    const isViewsOpen = expandedGroups[viewsGroupKey] ?? (q.length > 0);
    const effectiveSch = schemaName || (conn.engine === 'sqlite' ? 'main' : '');

    return (
      <div className="mt-1 space-y-1 pl-3.5">
        {/* Tables Category Node */}
        <div>
          <div className="group flex items-center justify-between rounded-[8px] px-2 py-1.5 hover:bg-interaction/[0.06]">
            <button
              type="button"
              onClick={() => {
                onSelectContext({ connection: conn, database: dbName, schema: schemaName });
                toggleGroup(tablesGroupKey);
              }}
              onDoubleClick={() =>
                onOpenObjectsOverview?.('tables', { connection: conn, database: dbName, schema: schemaName })
              }
              className="flex min-w-0 flex-1 items-center gap-2 text-left text-[13px] text-content-detail"
              title="点击展开/收起表目录，双击在主界面打开表总览"
            >
              {isTablesOpen ? (
                <ChevronDown className="h-3.5 w-3.5 shrink-0 text-content-muted" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-content-muted" />
              )}
              <Table2 className="h-4 w-4 shrink-0 text-icon-cyan" />
              <span className="font-medium">表</span>
              <span className="text-[11.5px] text-content-hint">({tables.length})</span>
            </button>
            <button
              type="button"
              onClick={() => onDesignTable(conn, dbName, effectiveSch, '', 'create')}
              className="hidden rounded p-1 text-content-muted hover:bg-interaction/[0.1] hover:text-strong group-hover:block"
              title="新建表"
            >
              <Plus className="h-3.5 w-3.5" />
            </button>
          </div>

          {isTablesOpen && (
            <div className="mt-1 space-y-1 pl-3.5">
              {tables.length === 0 ? (
                <div className="py-1.5 pl-4 text-xs text-content-subtle">（无表）</div>
              ) : (
                tables.map((obj) => {
                  const isObjSelected =
                    selectedContext?.connection.id === conn.id &&
                    selectedContext.database === dbName &&
                    selectedObject?.name === obj.name &&
                    selectedObject?.type === obj.type;
                  return (
                    <div
                      key={`tbl:${obj.name}`}
                      onClick={() => {
                        onOpenTableData(conn, dbName, obj.schema || effectiveSch, obj.name);
                      }}
                      onContextMenu={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        onSelectContext({ connection: conn, database: dbName, schema: schemaName });
                        onSelectObject(obj);
                        setContextMenu({
                          x: e.clientX,
                          y: e.clientY,
                          kind: 'object',
                          conn,
                          database: dbName,
                          schema: obj.schema || effectiveSch,
                          object: obj,
                        });
                      }}
                      className={`group flex cursor-pointer items-center justify-between rounded-[8px] py-1.5 pl-3 pr-2 transition-colors ${
                        isObjSelected
                          ? 'bg-accent font-medium text-on-accent shadow-sm'
                          : 'text-content-row hover:bg-interaction/[0.07]'
                      }`}
                      title={`点击打开 ${obj.name} 数据表，右键更多操作`}
                    >
                      <div className="flex min-w-0 flex-1 items-center gap-2">
                        <Table2
                          className={`h-4 w-4 shrink-0 ${
                            isObjSelected ? 'text-on-accent' : 'text-icon-cyan'
                          }`}
                        />
                        <span className="truncate font-mono text-[13.5px] leading-snug">{obj.name}</span>
                      </div>
                      <div className="hidden items-center gap-0.5 group-hover:flex" onClick={(e) => e.stopPropagation()}>
                        <button
                          type="button"
                          onClick={() => onDesignTable(conn, dbName, obj.schema || effectiveSch, obj.name, 'alter')}
                          className={`rounded p-1 ${
                            isObjSelected
                              ? 'text-on-accent/80 hover:bg-white/20 hover:text-on-accent'
                              : 'text-content-muted hover:bg-interaction/[0.12] hover:text-strong'
                          }`}
                          title="设计表结构"
                        >
                          <Edit3 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          )}
        </div>

        {/* Views Category Node */}
        {views.length > 0 && (
          <div>
            <div className="group flex items-center justify-between rounded-[8px] px-2 py-1.5 hover:bg-interaction/[0.06]">
              <button
                type="button"
                onClick={() => {
                  onSelectContext({ connection: conn, database: dbName, schema: schemaName });
                  toggleGroup(viewsGroupKey);
                }}
                className="flex min-w-0 flex-1 items-center gap-2 text-left text-[13px] text-content-detail"
              >
                {isViewsOpen ? (
                  <ChevronDown className="h-3.5 w-3.5 shrink-0 text-content-muted" />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5 shrink-0 text-content-muted" />
                )}
                <Eye className="h-4 w-4 shrink-0 text-icon-teal" />
                <span className="font-medium">视图</span>
                <span className="text-[11.5px] text-content-hint">({views.length})</span>
              </button>
            </div>

            {isViewsOpen && (
              <div className="mt-1 space-y-1 pl-3.5">
                {views.map((obj) => {
                  const isObjSelected =
                    selectedContext?.connection.id === conn.id &&
                    selectedContext.database === dbName &&
                    selectedObject?.name === obj.name &&
                    selectedObject?.type === obj.type;
                  return (
                    <div
                      key={`vw:${obj.name}`}
                      onClick={() => {
                        onOpenTableData(conn, dbName, obj.schema || effectiveSch, obj.name);
                      }}
                      onContextMenu={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        onSelectContext({ connection: conn, database: dbName, schema: schemaName });
                        onSelectObject(obj);
                        setContextMenu({
                          x: e.clientX,
                          y: e.clientY,
                          kind: 'object',
                          conn,
                          database: dbName,
                          schema: obj.schema || effectiveSch,
                          object: obj,
                        });
                      }}
                      className={`group flex cursor-pointer items-center justify-between rounded-[8px] py-1.5 pl-3 pr-2 transition-colors ${
                        isObjSelected
                          ? 'bg-accent font-medium text-on-accent shadow-sm'
                          : 'text-content-row hover:bg-interaction/[0.07]'
                      }`}
                    >
                      <div className="flex min-w-0 flex-1 items-center gap-2">
                        <Eye
                          className={`h-4 w-4 shrink-0 ${
                            isObjSelected ? 'text-on-accent' : 'text-icon-teal'
                          }`}
                        />
                        <span className="truncate font-mono text-[13.5px] leading-snug">{obj.name}</span>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>
    );
  };

  const sshNavigation = sshSection && (
    <div className={`max-h-[45%] shrink-0 overflow-y-auto border-interaction/[0.06] ${sshFirst ? 'border-b' : 'border-t'}`}>
      {sshSection}
    </div>
  );

  return (
    <aside
      style={{ width: `${sidebarWidth}px` }}
      className="relative flex h-full shrink-0 flex-col border-r border-interaction/[0.08] bg-surface-panel select-none"
    >
      {sshFirst && sshNavigation}
      {/* macOS Sidebar Header */}
      <div className="flex items-center justify-between border-b border-interaction/[0.06] px-3.5 py-2.5">
        <span className="text-[12.5px] font-semibold tracking-wide text-content-caption">数据库连接</span>
        <button
          type="button"
          onClick={() => onNewConnection()}
          className="apple-btn-secondary px-2.5 py-1 text-[12px]"
          title="新建数据库连接"
        >
          <Plus className="h-3.5 w-3.5 text-icon-blue" />
          新建
        </button>
      </div>

      {treeError && (
        <div className="mx-2 mt-2 flex items-center justify-between rounded-[7px] border border-danger/30 bg-danger/15 px-2.5 py-1.5 text-xs text-icon-red">
          <span className="truncate">{treeError}</span>
          <button type="button" onClick={() => setTreeError('')} className="ml-1 text-icon-red hover:text-strong">
            <X className="h-3 w-3" />
          </button>
        </div>
      )}

      {/* Tree Content */}
      <div className="min-h-0 flex-1 space-y-1.5 overflow-y-auto p-2">
        {connections.length === 0 ? (
          <div className="p-5 text-center text-xs text-content-subtle">
            暂无数据库连接，请点击右上角「新建」
          </div>
        ) : (
          connections.map((conn) => {
            const badge = ENGINE_BADGES[conn.engine];
            const isConnOpen = !!expandedConns[conn.id];
            const isConnLoading = !!loadingKeys[`conn:${conn.id}`];
            const dbs = databasesByConn[conn.id] || [];
            const supportsSchemas = capabilities[conn.engine]?.supportsSchemas ?? false;
            const isConnSelected = selectedContext?.connection.id === conn.id && !selectedContext.database;

            return (
              <div key={conn.id} className="mb-1">
                <div
                  onClick={() => void toggleConn(conn)}
                  onContextMenu={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    onSelectContext({
                      connection: conn,
                      database: conn.database || dbs[0] || '',
                      schema: '',
                    });
                    setContextMenu({ x: e.clientX, y: e.clientY, kind: 'conn', conn });
                  }}
                  className={`group flex cursor-pointer items-center justify-between rounded-[8px] px-2.5 py-2 transition-colors ${
                    isConnSelected
                      ? 'bg-interaction/[0.11] text-strong'
                      : 'hover:bg-interaction/[0.06]'
                  }`}
                >
                  <div className="flex min-w-0 flex-1 items-center gap-2">
                    {isConnLoading ? (
                      <Loader2 className="h-4 w-4 shrink-0 animate-spin text-icon-blue" />
                    ) : isConnOpen ? (
                      <ChevronDown className="h-4 w-4 shrink-0 text-content-muted" />
                    ) : (
                      <ChevronRight className="h-4 w-4 shrink-0 text-content-muted" />
                    )}

                    {/* Connection active dot */}
                    <span
                      className={`h-2.5 w-2.5 shrink-0 rounded-full transition-colors ${
                        isConnOpen ? 'bg-icon-green shadow-[0_0_6px_rgba(48,209,88,0.8)]' : 'bg-content-inactive'
                      }`}
                    />

                    <span className={`shrink-0 rounded-[5px] border px-1.5 py-0.5 text-[11px] font-semibold ${badge.cls}`}>
                      {badge.label}
                    </span>
                    {conn.ssh.enabled && (
                      <span title="已启用 SSH 隧道">
                        <KeyRound className="h-3.5 w-3.5 shrink-0 text-icon-orange" />
                      </span>
                    )}
                    <span className="truncate text-[14px] font-semibold text-content">{conn.name}</span>
                  </div>

                  {conn.ssh.enabled && onOpenSSHTerminal && (
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        onOpenSSHTerminal(conn);
                      }}
                      aria-label="打开 SSH 终端"
                      title="打开 SSH 终端"
                      className="ml-1 shrink-0 rounded p-1 text-icon-lavender hover:bg-interaction/[0.1] hover:text-strong"
                    >
                      <TerminalSquare aria-hidden="true" className="h-3.5 w-3.5" />
                    </button>
                  )}
                  <div className="hidden shrink-0 items-center gap-0.5 group-hover:flex group-focus-within:flex" onClick={(e) => e.stopPropagation()}>
                    <button
                      type="button"
                      onClick={() => onOpenQuery(conn, conn.database || dbs[0] || '', '')}
                      className="rounded p-1 text-icon-green hover:bg-interaction/[0.1]"
                      title="新建 SQL 查询"
                    >
                      <Terminal className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => void toggleConn(conn, true)}
                      className="rounded p-1 text-content-muted hover:bg-interaction/[0.1] hover:text-strong"
                      title="刷新连接"
                    >
                      <RefreshCw className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => onEditConnection(conn)}
                      className="rounded p-1 text-content-muted hover:bg-interaction/[0.1] hover:text-strong"
                      title="编辑连接"
                    >
                      <Edit3 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>

                {isConnOpen && (
                  <div className="mt-1 space-y-1 pl-3.5">
                    {dbs.map((dbName) => {
                      const dbKey = `${conn.id}::${dbName}`;
                      const isDbOpen = !!expandedDBs[dbKey];
                      const isDbLoading = !!loadingKeys[`db:${dbKey}`];
                      const schemas = schemasByDB[dbKey] || [];
                      const directObjs = objectsByKey[`${dbKey}::`] || [];
                      const isDbSelected =
                        selectedContext?.connection.id === conn.id &&
                        selectedContext.database === dbName &&
                        !selectedContext.schema &&
                        !selectedObject;

                      return (
                        <div key={dbName}>
                          <div
                            onClick={() => void toggleDatabase(conn, dbName)}
                            onContextMenu={(e) => {
                              e.preventDefault();
                              e.stopPropagation();
                              onSelectContext({ connection: conn, database: dbName, schema: '' });
                              setContextMenu({
                                x: e.clientX,
                                y: e.clientY,
                                kind: 'db',
                                conn,
                                database: dbName,
                              });
                            }}
                            className={`group flex cursor-pointer items-center justify-between rounded-[8px] px-2.5 py-2 transition-colors ${
                              isDbSelected
                                ? 'bg-interaction/[0.1] text-strong'
                                : 'text-content-row hover:bg-interaction/[0.06]'
                            }`}
                          >
                            <div className="flex min-w-0 flex-1 items-center gap-2 text-[13.5px]">
                              {isDbLoading ? (
                                <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-icon-blue" />
                              ) : isDbOpen ? (
                                <ChevronDown className="h-3.5 w-3.5 shrink-0 text-content-muted" />
                              ) : (
                                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-content-muted" />
                              )}
                              <Database
                                className={`h-4 w-4 shrink-0 ${
                                  isDbOpen ? 'text-icon-orange' : 'text-content-muted'
                                }`}
                              />
                              <span className="truncate font-medium">{dbName}</span>
                            </div>

                            <div className="hidden items-center gap-0.5 group-hover:flex" onClick={(e) => e.stopPropagation()}>
                              <button
                                type="button"
                                onClick={() => onOpenQuery(conn, dbName, '')}
                                className="rounded p-1 text-icon-green hover:bg-interaction/[0.1]"
                                title="新建查询"
                              >
                                <Terminal className="h-3.5 w-3.5" />
                              </button>
                              <button
                                type="button"
                                onClick={() => void toggleDatabase(conn, dbName, true)}
                                className="rounded p-1 text-content-muted hover:bg-interaction/[0.1] hover:text-strong"
                                title="刷新库"
                              >
                                <RefreshCw className="h-3.5 w-3.5" />
                              </button>
                            </div>
                          </div>

                          {isDbOpen && (
                            <div className="mt-1 space-y-1">
                              {supportsSchemas
                                ? schemas.map((schemaName) => {
                                    const schKey = `${dbKey}::${schemaName}`;
                                    const isSchOpen = !!expandedSchemas[schKey];
                                    const isSchLoading = !!loadingKeys[`sch:${schKey}`];
                                    const objs = objectsByKey[schKey] || [];
                                    const isSchSelected =
                                      selectedContext?.connection.id === conn.id &&
                                      selectedContext.database === dbName &&
                                      selectedContext.schema === schemaName &&
                                      !selectedObject;

                                    return (
                                      <div key={schemaName} className="pl-3.5">
                                        <div
                                          onClick={() => void toggleSchema(conn, dbName, schemaName)}
                                          onContextMenu={(e) => {
                                            e.preventDefault();
                                            e.stopPropagation();
                                            onSelectContext({
                                              connection: conn,
                                              database: dbName,
                                              schema: schemaName,
                                            });
                                            setContextMenu({
                                              x: e.clientX,
                                              y: e.clientY,
                                              kind: 'schema',
                                              conn,
                                              database: dbName,
                                              schema: schemaName,
                                            });
                                          }}
                                          className={`group flex cursor-pointer items-center justify-between rounded-[8px] px-2.5 py-1.5 transition-colors ${
                                            isSchSelected
                                              ? 'bg-interaction/[0.1] text-strong'
                                              : 'text-content-detail hover:bg-interaction/[0.06]'
                                          }`}
                                        >
                                          <div className="flex min-w-0 flex-1 items-center gap-2 text-[13px]">
                                            {isSchLoading ? (
                                              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-icon-blue" />
                                            ) : isSchOpen ? (
                                              <ChevronDown className="h-3.5 w-3.5 shrink-0 text-content-muted" />
                                            ) : (
                                              <ChevronRight className="h-3.5 w-3.5 shrink-0 text-content-muted" />
                                            )}
                                            <Layers className="h-4 w-4 shrink-0 text-icon-yellow" />
                                            <span className="truncate font-medium">{schemaName}</span>
                                          </div>
                                          <div
                                            className="hidden items-center gap-0.5 group-hover:flex"
                                            onClick={(e) => e.stopPropagation()}
                                          >
                                            <button
                                              type="button"
                                              onClick={() => onDesignTable(conn, dbName, schemaName, '', 'create')}
                                              className="rounded p-0.5 text-content-muted hover:bg-interaction/[0.1] hover:text-strong"
                                              title="新建表"
                                            >
                                              <FilePlus2 className="h-3 w-3" />
                                            </button>
                                            <button
                                              type="button"
                                              onClick={() => onOpenQuery(conn, dbName, schemaName)}
                                              className="rounded p-0.5 text-icon-green hover:bg-interaction/[0.1]"
                                              title="新建查询"
                                            >
                                              <Terminal className="h-3 w-3" />
                                            </button>
                                            <button
                                              type="button"
                                              onClick={() => void toggleSchema(conn, dbName, schemaName, true)}
                                              className="rounded p-0.5 text-content-muted hover:bg-interaction/[0.1] hover:text-strong"
                                              title="刷新模式"
                                            >
                                              <RefreshCw className="h-3 w-3" />
                                            </button>
                                          </div>
                                        </div>
                                        {isSchOpen &&
                                          renderObjectCategoryGroups(conn, dbName, schemaName, schKey, objs)}
                                      </div>
                                    );
                                  })
                                : renderObjectCategoryGroups(
                                    conn,
                                    dbName,
                                    conn.engine === 'sqlite' ? 'main' : '',
                                    `${dbKey}::`,
                                    directObjs,
                                  )}
                            </div>
                          )}
                        </div>
                      );
                    })}
                    {dbs.length === 0 && !isConnLoading && (
                      <div className="flex items-center gap-1 py-1 pl-4 text-xs text-content-subtle">
                        <Folder className="h-3 w-3" /> 无可见数据库
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>

      {!sshFirst && sshNavigation}

      {/* macOS Bottom Sidebar Filter Box */}
      <div className="border-t border-interaction/[0.06] bg-surface-inset p-2">
        <div className="relative flex items-center">
          <Search className="pointer-events-none absolute left-2.5 h-3.5 w-3.5 text-content-subtle" />
          <input
            type="text"
            placeholder="搜索表或视图..."
            value={searchFilter}
            onChange={(e) => setSearchFilter(e.target.value)}
            className="apple-input w-full pl-8 pr-6"
          />
          {searchFilter && (
            <button
              type="button"
              onClick={() => setSearchFilter('')}
              className="absolute right-2 text-content-muted hover:text-strong"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
      </div>

      {/* macOS Frosted Context Menu */}
      {contextMenu && (
        <div
          style={{
            top: Math.min(contextMenu.y, window.innerHeight - 250),
            left: Math.min(contextMenu.x, window.innerWidth - 210),
          }}
          onClick={(e) => e.stopPropagation()}
          className="fixed z-50 w-52 rounded-xl border border-interaction/[0.12] bg-surface-menu/95 p-1 text-xs text-content shadow-2xl backdrop-blur-2xl"
        >
          {contextMenu.kind === 'conn' && (
            <>
              <div className="border-b border-interaction/[0.08] px-2.5 py-1 font-medium text-content-muted truncate">
                {contextMenu.conn.name}
              </div>
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  void toggleConn(c);
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <FolderOpen className="h-3.5 w-3.5 text-icon-green" />
                {expandedConns[contextMenu.conn.id] ? '收起连接' : '打开连接'}
              </button>
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  onOpenQuery(c, c.database || databasesByConn[c.id]?.[0] || '', '');
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <Terminal className="h-3.5 w-3.5 text-icon-green" />
                新建 SQL 查询
              </button>
              {contextMenu.conn.ssh.enabled && onSaveSSHHost && (
                <button
                  type="button"
                  disabled={copyingSSHHost}
                  onClick={() => {
                    const conn = contextMenu.conn;
                    setContextMenu(null);
                    setTreeError('');
                    setCopyingSSHHost(true);
                    void onSaveSSHHost(conn)
                      .catch((err) => setTreeError(err instanceof Error ? err.message : '无法另存 SSH 主机'))
                      .finally(() => setCopyingSSHHost(false));
                  }}
                  className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent disabled:opacity-50"
                >
                  <TerminalSquare className="h-3.5 w-3.5 text-icon-lavender" />
                  另存为 SSH 主机
                </button>
              )}
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  onEditConnection(c);
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <Edit3 className="h-3.5 w-3.5 text-icon-blue" />
                编辑连接属性...
              </button>
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  void toggleConn(c, true);
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <RefreshCw className="h-3.5 w-3.5 text-content-muted" />
                刷新
              </button>
              <div className="my-1 border-t border-interaction/[0.08]" />
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  setExpandedConns((p) => ({ ...p, [c.id]: false }));
                  onDisconnectConnection(c);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left text-icon-orange hover:bg-icon-orange/20"
              >
                <Unplug className="h-3.5 w-3.5" />
                关闭连接 / 断开隧道
              </button>
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  onDeleteConnection(c);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left text-icon-red hover:bg-danger/20"
              >
                <Trash2 className="h-3.5 w-3.5" />
                删除连接
              </button>
            </>
          )}

          {contextMenu.kind === 'db' && (
            <>
              <div className="border-b border-interaction/[0.08] px-2.5 py-1 font-mono font-medium text-content-muted truncate">
                {contextMenu.database}
              </div>
              {onShowDetails && (
                <button
                  type="button"
                  onClick={() => {
                    onSelectContext({ connection: contextMenu.conn, database: contextMenu.database, schema: '' });
                    onSelectObject(null);
                    setContextMenu(null);
                    onShowDetails();
                  }}
                  className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
                >
                  <Info className="h-3.5 w-3.5 text-icon-cyan" />
                  查看数据库注释
                </button>
              )}
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName } = contextMenu;
                  setContextMenu(null);
                  onOpenObjectsOverview?.('tables', { connection: conn, database: dbName, schema: '' });
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <Table2 className="h-3.5 w-3.5 text-icon-cyan" />
                在对象总览中查看所有表
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName } = contextMenu;
                  setContextMenu(null);
                  onDesignTable(conn, dbName, conn.engine === 'sqlite' ? 'main' : '', '', 'create');
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <FilePlus2 className="h-3.5 w-3.5 text-icon-green" />
                新建表...
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName } = contextMenu;
                  setContextMenu(null);
                  onOpenQuery(conn, dbName, '');
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <Terminal className="h-3.5 w-3.5 text-icon-green" />
                新建查询
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName } = contextMenu;
                  setContextMenu(null);
                  void toggleDatabase(conn, dbName, true);
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <RefreshCw className="h-3.5 w-3.5 text-content-muted" />
                刷新
              </button>
            </>
          )}

          {contextMenu.kind === 'schema' && (
            <>
              <div className="border-b border-interaction/[0.08] px-2.5 py-1 font-mono font-medium text-content-muted truncate">
                {contextMenu.schema}
              </div>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch } = contextMenu;
                  setContextMenu(null);
                  onOpenObjectsOverview?.('tables', { connection: conn, database: dbName, schema: sch });
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <Table2 className="h-3.5 w-3.5 text-icon-cyan" />
                在对象总览中查看所有表
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch } = contextMenu;
                  setContextMenu(null);
                  onDesignTable(conn, dbName, sch, '', 'create');
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <FilePlus2 className="h-3.5 w-3.5 text-icon-green" />
                新建表...
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch } = contextMenu;
                  setContextMenu(null);
                  onOpenQuery(conn, dbName, sch);
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <Terminal className="h-3.5 w-3.5 text-icon-green" />
                新建查询
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch } = contextMenu;
                  setContextMenu(null);
                  void toggleSchema(conn, dbName, sch, true);
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <RefreshCw className="h-3.5 w-3.5 text-content-muted" />
                刷新
              </button>
            </>
          )}

          {contextMenu.kind === 'object' && (
            <>
              <div className="border-b border-interaction/[0.08] px-2.5 py-1 font-mono font-medium text-content-muted truncate">
                {contextMenu.object.name}
              </div>
              {onShowDetails && (
                <button
                  type="button"
                  onClick={() => {
                    onSelectContext({
                      connection: contextMenu.conn,
                      database: contextMenu.database,
                      schema: contextMenu.schema,
                    });
                    onSelectObject(contextMenu.object);
                    setContextMenu(null);
                    onShowDetails();
                  }}
                  className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
                >
                  <Info className="h-3.5 w-3.5 text-icon-cyan" />
                  查看注释
                </button>
              )}
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch, object: obj } = contextMenu;
                  setContextMenu(null);
                  onOpenTableData(conn, dbName, sch, obj.name);
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <FolderOpen className="h-3.5 w-3.5 text-icon-cyan" />
                打开表数据
              </button>
              {contextMenu.object.type === 'TABLE' && (
                <button
                  type="button"
                  onClick={() => {
                    const { conn, database: dbName, schema: sch, object: obj } = contextMenu;
                    setContextMenu(null);
                    onDesignTable(conn, dbName, sch, obj.name, 'alter');
                  }}
                  className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
                >
                  <Edit3 className="h-3.5 w-3.5 text-icon-orange" />
                  设计表结构
                </button>
              )}
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch, object: obj } = contextMenu;
                  setContextMenu(null);
                  onOpenQuery(conn, dbName, sch, buildSelectSql(conn, sch, obj.name));
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <Terminal className="h-3.5 w-3.5 text-icon-green" />
                新建查询 (SELECT 前100行)
              </button>
              <button
                type="button"
                onClick={() => {
                  void navigator.clipboard?.writeText(contextMenu.object.name);
                  setContextMenu(null);
                }}
                className="theme-menu-item flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-accent hover:text-on-accent"
              >
                <Copy className="h-3.5 w-3.5 text-content-muted" />
                复制表名
              </button>
            </>
          )}
        </div>
      )}

      {/* Right Edge Drag Resize Handle */}
      <div
        onMouseDown={startResize}
        onDoubleClick={() => {
          setSidebarWidth(SIDEBAR_WIDTH.initial);
          saveSidebarWidth(SIDEBAR_WIDTH.initial);
        }}
        title="左右拖动调整侧边栏宽度（双击恢复默认宽度）"
        className={`absolute -right-1 top-0 z-30 h-full w-2 cursor-col-resize transition-colors ${
          resizing ? 'bg-accent/70' : 'hover:bg-accent/50'
        }`}
      />
    </aside>
  );
};
