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
  refreshSignal?: number;
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
  mysql: { label: 'MySQL', cls: 'bg-[#0a84ff]/20 text-[#64d2ff] border-[#0a84ff]/35' },
  postgres: { label: 'PG', cls: 'bg-[#5e5ce6]/25 text-[#a4a3ff] border-[#5e5ce6]/40' },
  sqlite: { label: 'SQLite', cls: 'bg-[#30d158]/20 text-[#63e682] border-[#30d158]/35' },
  sqlserver: { label: 'MSSQL', cls: 'bg-[#ff9f0a]/20 text-[#ffb340] border-[#ff9f0a]/35' },
  oracle: { label: 'Oracle', cls: 'bg-[#ff453a]/20 text-[#ff6961] border-[#ff453a]/35' },
  dameng: { label: '达梦', cls: 'bg-[#5ac8fa]/20 text-[#70d7ff] border-[#5ac8fa]/35' },
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
  refreshSignal = 0,
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
          <div className="group flex items-center justify-between rounded-[8px] px-2 py-1.5 hover:bg-white/[0.06]">
            <button
              type="button"
              onClick={() => {
                onSelectContext({ connection: conn, database: dbName, schema: schemaName });
                toggleGroup(tablesGroupKey);
              }}
              onDoubleClick={() =>
                onOpenObjectsOverview?.('tables', { connection: conn, database: dbName, schema: schemaName })
              }
              className="flex min-w-0 flex-1 items-center gap-2 text-left text-[13px] text-[#d6d6de]"
              title="点击展开/收起表目录，双击在主界面打开表总览"
            >
              {isTablesOpen ? (
                <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[#9e9ea9]" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[#9e9ea9]" />
              )}
              <Table2 className="h-4 w-4 shrink-0 text-[#64d2ff]" />
              <span className="font-medium">表</span>
              <span className="text-[11.5px] text-[#8e8e98]">({tables.length})</span>
            </button>
            <button
              type="button"
              onClick={() => onDesignTable(conn, dbName, effectiveSch, '', 'create')}
              className="hidden rounded p-1 text-[#9e9ea9] hover:bg-white/[0.1] hover:text-white group-hover:block"
              title="新建表"
            >
              <Plus className="h-3.5 w-3.5" />
            </button>
          </div>

          {isTablesOpen && (
            <div className="mt-1 space-y-1 pl-3.5">
              {tables.length === 0 ? (
                <div className="py-1.5 pl-4 text-xs text-[#767682]">（无表）</div>
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
                          ? 'bg-[#0a84ff] font-medium text-white shadow-sm'
                          : 'text-[#e8e8ee] hover:bg-white/[0.07]'
                      }`}
                      title={`点击打开 ${obj.name} 数据表，右键更多操作`}
                    >
                      <div className="flex min-w-0 flex-1 items-center gap-2">
                        <Table2
                          className={`h-4 w-4 shrink-0 ${
                            isObjSelected ? 'text-white' : 'text-[#64d2ff]'
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
                              ? 'text-white/80 hover:bg-white/20 hover:text-white'
                              : 'text-[#9e9ea9] hover:bg-white/[0.12] hover:text-white'
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
            <div className="group flex items-center justify-between rounded-[8px] px-2 py-1.5 hover:bg-white/[0.06]">
              <button
                type="button"
                onClick={() => {
                  onSelectContext({ connection: conn, database: dbName, schema: schemaName });
                  toggleGroup(viewsGroupKey);
                }}
                className="flex min-w-0 flex-1 items-center gap-2 text-left text-[13px] text-[#d6d6de]"
              >
                {isViewsOpen ? (
                  <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[#9e9ea9]" />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[#9e9ea9]" />
                )}
                <Eye className="h-4 w-4 shrink-0 text-[#5ac8fa]" />
                <span className="font-medium">视图</span>
                <span className="text-[11.5px] text-[#8e8e98]">({views.length})</span>
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
                          ? 'bg-[#0a84ff] font-medium text-white shadow-sm'
                          : 'text-[#e8e8ee] hover:bg-white/[0.07]'
                      }`}
                    >
                      <div className="flex min-w-0 flex-1 items-center gap-2">
                        <Eye
                          className={`h-4 w-4 shrink-0 ${
                            isObjSelected ? 'text-white' : 'text-[#5ac8fa]'
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

  return (
    <aside
      style={{ width: `${sidebarWidth}px` }}
      className="relative flex h-full shrink-0 flex-col border-r border-white/[0.08] bg-[#1e1e21] select-none"
    >
      {/* macOS Sidebar Header */}
      <div className="flex items-center justify-between border-b border-white/[0.06] px-3.5 py-2.5">
        <span className="text-[12.5px] font-semibold tracking-wide text-[#b4b4be]">数据库连接</span>
        <button
          type="button"
          onClick={() => onNewConnection()}
          className="apple-btn-secondary px-2.5 py-1 text-[12px]"
          title="新建数据库连接"
        >
          <Plus className="h-3.5 w-3.5 text-[#52a9ff]" />
          新建
        </button>
      </div>

      {treeError && (
        <div className="mx-2 mt-2 flex items-center justify-between rounded-[7px] border border-[#ff453a]/30 bg-[#ff453a]/15 px-2.5 py-1.5 text-xs text-[#ff6961]">
          <span className="truncate">{treeError}</span>
          <button type="button" onClick={() => setTreeError('')} className="ml-1 text-[#ff6961] hover:text-white">
            <X className="h-3 w-3" />
          </button>
        </div>
      )}

      {/* Tree Content */}
      <div className="flex-1 space-y-1.5 overflow-y-auto p-2">
        {connections.length === 0 ? (
          <div className="p-5 text-center text-xs text-[#767682]">
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
                      ? 'bg-white/[0.11] text-white'
                      : 'hover:bg-white/[0.06]'
                  }`}
                >
                  <div className="flex min-w-0 flex-1 items-center gap-2">
                    {isConnLoading ? (
                      <Loader2 className="h-4 w-4 shrink-0 animate-spin text-[#52a9ff]" />
                    ) : isConnOpen ? (
                      <ChevronDown className="h-4 w-4 shrink-0 text-[#9e9ea9]" />
                    ) : (
                      <ChevronRight className="h-4 w-4 shrink-0 text-[#9e9ea9]" />
                    )}

                    {/* Connection active dot */}
                    <span
                      className={`h-2.5 w-2.5 shrink-0 rounded-full transition-colors ${
                        isConnOpen ? 'bg-[#30d158] shadow-[0_0_6px_rgba(48,209,88,0.8)]' : 'bg-[#54545e]'
                      }`}
                    />

                    <span className={`shrink-0 rounded-[5px] border px-1.5 py-0.5 text-[11px] font-semibold ${badge.cls}`}>
                      {badge.label}
                    </span>
                    {conn.ssh.enabled && (
                      <span title="已启用 SSH 隧道">
                        <KeyRound className="h-3.5 w-3.5 shrink-0 text-[#ff9f0a]" />
                      </span>
                    )}
                    <span className="truncate text-[14px] font-semibold text-[#f2f2f7]">{conn.name}</span>
                  </div>

                  <div className="hidden items-center gap-0.5 group-hover:flex" onClick={(e) => e.stopPropagation()}>
                    <button
                      type="button"
                      onClick={() => onOpenQuery(conn, conn.database || dbs[0] || '', '')}
                      className="rounded p-1 text-[#30d158] hover:bg-white/[0.1]"
                      title="新建 SQL 查询"
                    >
                      <Terminal className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => void toggleConn(conn, true)}
                      className="rounded p-1 text-[#9e9ea9] hover:bg-white/[0.1] hover:text-white"
                      title="刷新连接"
                    >
                      <RefreshCw className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => onEditConnection(conn)}
                      className="rounded p-1 text-[#9e9ea9] hover:bg-white/[0.1] hover:text-white"
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
                                ? 'bg-white/[0.1] text-white'
                                : 'text-[#e8e8ee] hover:bg-white/[0.06]'
                            }`}
                          >
                            <div className="flex min-w-0 flex-1 items-center gap-2 text-[13.5px]">
                              {isDbLoading ? (
                                <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-[#52a9ff]" />
                              ) : isDbOpen ? (
                                <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[#9e9ea9]" />
                              ) : (
                                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[#9e9ea9]" />
                              )}
                              <Database
                                className={`h-4 w-4 shrink-0 ${
                                  isDbOpen ? 'text-[#ff9f0a]' : 'text-[#9e9ea9]'
                                }`}
                              />
                              <span className="truncate font-medium">{dbName}</span>
                            </div>

                            <div className="hidden items-center gap-0.5 group-hover:flex" onClick={(e) => e.stopPropagation()}>
                              <button
                                type="button"
                                onClick={() => onOpenQuery(conn, dbName, '')}
                                className="rounded p-1 text-[#30d158] hover:bg-white/[0.1]"
                                title="新建查询"
                              >
                                <Terminal className="h-3.5 w-3.5" />
                              </button>
                              <button
                                type="button"
                                onClick={() => void toggleDatabase(conn, dbName, true)}
                                className="rounded p-1 text-[#9e9ea9] hover:bg-white/[0.1] hover:text-white"
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
                                              ? 'bg-white/[0.1] text-white'
                                              : 'text-[#d6d6de] hover:bg-white/[0.06]'
                                          }`}
                                        >
                                          <div className="flex min-w-0 flex-1 items-center gap-2 text-[13px]">
                                            {isSchLoading ? (
                                              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-[#52a9ff]" />
                                            ) : isSchOpen ? (
                                              <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[#9e9ea9]" />
                                            ) : (
                                              <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[#9e9ea9]" />
                                            )}
                                            <Layers className="h-4 w-4 shrink-0 text-[#ffd60a]" />
                                            <span className="truncate font-medium">{schemaName}</span>
                                          </div>
                                          <div
                                            className="hidden items-center gap-0.5 group-hover:flex"
                                            onClick={(e) => e.stopPropagation()}
                                          >
                                            <button
                                              type="button"
                                              onClick={() => onDesignTable(conn, dbName, schemaName, '', 'create')}
                                              className="rounded p-0.5 text-[#9e9ea9] hover:bg-white/[0.1] hover:text-white"
                                              title="新建表"
                                            >
                                              <FilePlus2 className="h-3 w-3" />
                                            </button>
                                            <button
                                              type="button"
                                              onClick={() => onOpenQuery(conn, dbName, schemaName)}
                                              className="rounded p-0.5 text-[#30d158] hover:bg-white/[0.1]"
                                              title="新建查询"
                                            >
                                              <Terminal className="h-3 w-3" />
                                            </button>
                                            <button
                                              type="button"
                                              onClick={() => void toggleSchema(conn, dbName, schemaName, true)}
                                              className="rounded p-0.5 text-[#9e9ea9] hover:bg-white/[0.1] hover:text-white"
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
                      <div className="flex items-center gap-1 py-1 pl-4 text-xs text-[#767682]">
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

      {/* macOS Bottom Sidebar Filter Box */}
      <div className="border-t border-white/[0.06] bg-[#1a1a1d] p-2">
        <div className="relative flex items-center">
          <Search className="pointer-events-none absolute left-2.5 h-3.5 w-3.5 text-[#767682]" />
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
              className="absolute right-2 text-[#9e9ea9] hover:text-white"
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
          className="fixed z-50 w-52 rounded-xl border border-white/[0.12] bg-[#2a2a2e]/95 p-1 text-xs text-[#f2f2f7] shadow-2xl backdrop-blur-2xl"
        >
          {contextMenu.kind === 'conn' && (
            <>
              <div className="border-b border-white/[0.08] px-2.5 py-1 font-medium text-[#9e9ea9] truncate">
                {contextMenu.conn.name}
              </div>
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  void toggleConn(c);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <FolderOpen className="h-3.5 w-3.5 text-[#30d158]" />
                {expandedConns[contextMenu.conn.id] ? '收起连接' : '打开连接'}
              </button>
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  onOpenQuery(c, c.database || databasesByConn[c.id]?.[0] || '', '');
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <Terminal className="h-3.5 w-3.5 text-[#30d158]" />
                新建 SQL 查询
              </button>
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  onEditConnection(c);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <Edit3 className="h-3.5 w-3.5 text-[#52a9ff]" />
                编辑连接属性...
              </button>
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  void toggleConn(c, true);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <RefreshCw className="h-3.5 w-3.5 text-[#9e9ea9]" />
                刷新
              </button>
              <div className="my-1 border-t border-white/[0.08]" />
              <button
                type="button"
                onClick={() => {
                  const c = contextMenu.conn;
                  setContextMenu(null);
                  setExpandedConns((p) => ({ ...p, [c.id]: false }));
                  onDisconnectConnection(c);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left text-[#ff9f0a] hover:bg-[#ff9f0a]/20"
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
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left text-[#ff6961] hover:bg-[#ff453a]/20"
              >
                <Trash2 className="h-3.5 w-3.5" />
                删除连接
              </button>
            </>
          )}

          {contextMenu.kind === 'db' && (
            <>
              <div className="border-b border-white/[0.08] px-2.5 py-1 font-mono font-medium text-[#9e9ea9] truncate">
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
                  className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
                >
                  <Info className="h-3.5 w-3.5 text-[#64d2ff]" />
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
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <Table2 className="h-3.5 w-3.5 text-[#64d2ff]" />
                在对象总览中查看所有表
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName } = contextMenu;
                  setContextMenu(null);
                  onDesignTable(conn, dbName, conn.engine === 'sqlite' ? 'main' : '', '', 'create');
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <FilePlus2 className="h-3.5 w-3.5 text-[#30d158]" />
                新建表...
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName } = contextMenu;
                  setContextMenu(null);
                  onOpenQuery(conn, dbName, '');
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <Terminal className="h-3.5 w-3.5 text-[#30d158]" />
                新建查询
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName } = contextMenu;
                  setContextMenu(null);
                  void toggleDatabase(conn, dbName, true);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <RefreshCw className="h-3.5 w-3.5 text-[#9e9ea9]" />
                刷新
              </button>
            </>
          )}

          {contextMenu.kind === 'schema' && (
            <>
              <div className="border-b border-white/[0.08] px-2.5 py-1 font-mono font-medium text-[#9e9ea9] truncate">
                {contextMenu.schema}
              </div>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch } = contextMenu;
                  setContextMenu(null);
                  onOpenObjectsOverview?.('tables', { connection: conn, database: dbName, schema: sch });
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <Table2 className="h-3.5 w-3.5 text-[#64d2ff]" />
                在对象总览中查看所有表
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch } = contextMenu;
                  setContextMenu(null);
                  onDesignTable(conn, dbName, sch, '', 'create');
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <FilePlus2 className="h-3.5 w-3.5 text-[#30d158]" />
                新建表...
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch } = contextMenu;
                  setContextMenu(null);
                  onOpenQuery(conn, dbName, sch);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <Terminal className="h-3.5 w-3.5 text-[#30d158]" />
                新建查询
              </button>
              <button
                type="button"
                onClick={() => {
                  const { conn, database: dbName, schema: sch } = contextMenu;
                  setContextMenu(null);
                  void toggleSchema(conn, dbName, sch, true);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <RefreshCw className="h-3.5 w-3.5 text-[#9e9ea9]" />
                刷新
              </button>
            </>
          )}

          {contextMenu.kind === 'object' && (
            <>
              <div className="border-b border-white/[0.08] px-2.5 py-1 font-mono font-medium text-[#9e9ea9] truncate">
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
                  className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
                >
                  <Info className="h-3.5 w-3.5 text-[#64d2ff]" />
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
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <FolderOpen className="h-3.5 w-3.5 text-[#64d2ff]" />
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
                  className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
                >
                  <Edit3 className="h-3.5 w-3.5 text-[#ff9f0a]" />
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
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <Terminal className="h-3.5 w-3.5 text-[#30d158]" />
                新建查询 (SELECT 前100行)
              </button>
              <button
                type="button"
                onClick={() => {
                  void navigator.clipboard?.writeText(contextMenu.object.name);
                  setContextMenu(null);
                }}
                className="flex w-full items-center gap-2 rounded-[6px] px-2.5 py-1.5 text-left hover:bg-[#0a84ff] hover:text-white"
              >
                <Copy className="h-3.5 w-3.5 text-[#9e9ea9]" />
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
          resizing ? 'bg-[#0a84ff]/70' : 'hover:bg-[#0a84ff]/50'
        }`}
      />
    </aside>
  );
};
