import React, { useEffect, useMemo, useState } from 'react';
import {
  Copy,
  Database,
  Download,
  Edit3,
  Eraser,
  Eye,
  FilePlus2,
  FolderOpen,
  Info,
  LayoutGrid,
  List,
  Loader2,
  Play,
  Plus,
  RefreshCw,
  Search,
  Table2,
  Terminal,
  Trash2,
  Upload,
  X,
} from 'lucide-react';
import { api, ApiError } from '../api/client';
import { useSqlHistory } from '../preferences';
import type { ConnectionView, DatabaseObject, EngineCapabilities, EngineType } from '../types/db';
import { BrandLogo } from './BrandLogo';
import { ImportExportModal } from './ImportExportModal';

export type ObjectCategory = 'all' | 'tables' | 'views' | 'queries';

interface Props {
  connections: ConnectionView[];
  capabilities: Record<EngineType, EngineCapabilities>;
  connection: ConnectionView | null;
  database: string;
  schema: string;
  category: ObjectCategory;
  onCategoryChange: (cat: ObjectCategory) => void;
  selectedObject: DatabaseObject | null;
  onSelectObject: (obj: DatabaseObject | null) => void;
  onShowDetails?: () => void;
  onSelectDatabaseSchema: (conn: ConnectionView, db: string, sch: string) => void;
  onOpenTableData: (conn: ConnectionView, db: string, sch: string, table: string) => void;
  onDesignTable: (conn: ConnectionView, db: string, sch: string, table: string, mode: 'create' | 'alter') => void;
  onOpenQuery: (conn: ConnectionView, db: string, sch: string, initialSql?: string) => void;
  onNewConnection: (engine?: EngineType) => void;
  refreshSignal?: number;
  onObjectsChanged?: () => void;
}

interface ContextMenuState {
  x: number;
  y: number;
  object: DatabaseObject;
}

export const ObjectBrowserPane: React.FC<Props> = ({
  connections,
  capabilities,
  connection,
  database,
  schema,
  category,
  onCategoryChange,
  selectedObject,
  onSelectObject,
  onShowDetails,
  onSelectDatabaseSchema,
  onOpenTableData,
  onDesignTable,
  onOpenQuery,
  onNewConnection,
  refreshSignal = 0,
  onObjectsChanged,
}) => {
  const [objects, setObjects] = useState<DatabaseObject[]>([]);
  const [databases, setDatabases] = useState<string[]>([]);
  const [schemas, setSchemas] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [actionNotice, setActionNotice] = useState('');
  const [filterText, setFilterText] = useState('');
  const [viewMode, setViewMode] = useState<'list' | 'grid'>('list');
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [ioModal, setIoModal] = useState<{ mode: 'export' | 'import'; table: string } | null>(null);
  const savedQueries = useSqlHistory();

  const supportsSchemas = connection ? (capabilities[connection.engine]?.supportsSchemas ?? false) : false;

  useEffect(() => {
    const closeMenu = () => setContextMenu(null);
    window.addEventListener('click', closeMenu);
    return () => window.removeEventListener('click', closeMenu);
  }, []);

  // Load databases and objects when connection/database/schema changes
  const fetchObjects = async () => {
    if (!connection) {
      setObjects([]);
      return;
    }
    setLoading(true);
    setError('');
    try {
      let targetDb = database || connection.database || '';
      const dbRes = await api.listDatabases(connection.id);
      const dbList = dbRes.databases || [];
      setDatabases(dbList);
      if (!targetDb && dbList.length > 0) {
        targetDb = dbList[0];
        onSelectDatabaseSchema(connection, targetDb, schema);
      }

      let targetSchema = schema;
      if (supportsSchemas && targetDb) {
        const schRes = await api.listSchemas(connection.id, targetDb);
        const schList = schRes.schemas || [];
        setSchemas(schList);
        if (!targetSchema && schList.length > 0) {
          targetSchema = schList[0];
          onSelectDatabaseSchema(connection, targetDb, targetSchema);
        }
      } else {
        setSchemas([]);
      }

      if (targetDb || connection.engine === 'sqlite') {
        const objRes = await api.listObjects(connection.id, targetDb, targetSchema);
        const list = objRes.objects || [];
        setObjects(list);
        if (selectedObject) {
          const stillExists = list.find((o) => o.name === selectedObject.name && o.type === selectedObject.type);
          onSelectObject(stillExists || null);
        }
      } else {
        setObjects([]);
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '读取数据库对象失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchObjects();
  }, [connection?.id, database, schema, refreshSignal]);

  const tableObjects = useMemo(
    () => objects.filter((o) => o.type === 'TABLE'),
    [objects],
  );
  const viewObjects = useMemo(
    () => objects.filter((o) => o.type === 'VIEW' || o.type === 'MATERIALIZED VIEW'),
    [objects],
  );

  const filteredObjects = useMemo(() => {
    let base = objects;
    if (category === 'tables') base = tableObjects;
    if (category === 'views') base = viewObjects;
    const q = filterText.trim().toLowerCase();
    if (!q) return base;
    return base.filter(
      (o) =>
        o.name.toLowerCase().includes(q) ||
        (o.comment && o.comment.toLowerCase().includes(q)),
    );
  }, [objects, tableObjects, viewObjects, category, filterText]);

  const buildSelectPreviewSql = (conn: ConnectionView, sch: string, tbl: string) => {
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

  const handleTableMaintenance = async (
    obj: DatabaseObject,
    action: 'drop_table' | 'drop_view' | 'truncate_table' | 'empty_table',
  ) => {
    if (!connection) return;
    const labelMap = {
      drop_table: `确定要永久删除表「${obj.name}」吗？此操作不可恢复！`,
      drop_view: `确定要删除视图「${obj.name}」吗？`,
      truncate_table: `确定要截断表「${obj.name}」(TRUNCATE) 吗？表中所有数据将被快速清空！`,
      empty_table: `确定要清空表「${obj.name}」(DELETE FROM) 中的所有记录吗？`,
    };
    if (!window.confirm(labelMap[action])) return;

    setError('');
    setActionNotice('');
    try {
      const res = await api.tableAction({
        connectionId: connection.id,
        database,
        schema: schema || obj.schema || '',
        table: obj.name,
        action,
      });
      setActionNotice(`已执行：${res.statement}`);
      if (action === 'drop_table' || action === 'drop_view') {
        onSelectObject(null);
      }
      await fetchObjects();
      onObjectsChanged?.();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '操作执行失败');
    }
  };

  if (connections.length === 0 || !connection) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-5 bg-slate-950 p-8 text-center select-none">
        <BrandLogo className="h-16 w-16 drop-shadow-xl" />
        <div>
          <h2 className="text-base font-semibold text-slate-100">欢迎使用 紫东直连 数据库管理工作台</h2>
          <p className="mt-1.5 max-w-lg text-xs leading-relaxed text-slate-400">
            采用经典桌面数据库客户端布局，支持 MySQL、PostgreSQL、SQLite、SQL Server、Oracle 与达梦 DM8。
            请点击上方工具栏「连接」或下方快捷入口创建您的第一个数据库连接。
          </p>
        </div>
        <div className="grid grid-cols-3 gap-2.5 sm:grid-cols-6">
          {(
            [
              { engine: 'mysql', label: 'MySQL', color: 'border-sky-800/80 bg-sky-950/50 text-sky-300 hover:bg-sky-900/60' },
              { engine: 'postgres', label: 'PostgreSQL', color: 'border-indigo-800/80 bg-indigo-950/50 text-indigo-300 hover:bg-indigo-900/60' },
              { engine: 'sqlite', label: 'SQLite', color: 'border-emerald-800/80 bg-emerald-950/50 text-emerald-300 hover:bg-emerald-900/60' },
              { engine: 'sqlserver', label: 'SQL Server', color: 'border-amber-800/80 bg-amber-950/50 text-amber-300 hover:bg-amber-900/60' },
              { engine: 'oracle', label: 'Oracle', color: 'border-rose-800/80 bg-rose-950/50 text-rose-300 hover:bg-rose-900/60' },
              { engine: 'dameng', label: '达梦 DM8', color: 'border-cyan-800/80 bg-cyan-950/50 text-cyan-300 hover:bg-cyan-900/60' },
            ] as Array<{ engine: EngineType; label: string; color: string }>
          ).map((item) => (
            <button
              key={item.engine}
              type="button"
              onClick={() => onNewConnection(item.engine)}
              className={`flex flex-col items-center gap-1.5 rounded-lg border px-4 py-3 text-xs font-medium transition ${item.color}`}
            >
              <Database className="h-5 w-5" />
              <span>新建 {item.label}</span>
            </button>
          ))}
        </div>
      </div>
    );
  }

  const effectiveSchema = schema || (connection.engine === 'sqlite' ? 'main' : '');

  return (
    <div className="flex h-full flex-col bg-slate-950 select-none">
      {/* Navicat-Style Object Action Sub-Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.08] bg-[#1e1e21] px-3 py-1.5">
        <div className="flex flex-wrap items-center gap-1.5">
          {category !== 'queries' ? (
            <>
              <button
                type="button"
                disabled={!selectedObject}
                onClick={() =>
                  selectedObject &&
                  onOpenTableData(
                    connection,
                    database,
                    selectedObject.schema || effectiveSchema,
                    selectedObject.name,
                  )
                }
                className="apple-btn-primary"
                title="打开选中表的数据网格"
              >
                <FolderOpen className="h-3.5 w-3.5" />
                打开表
              </button>

              <button
                type="button"
                disabled={!selectedObject || selectedObject.type !== 'TABLE'}
                onClick={() =>
                  selectedObject &&
                  onDesignTable(
                    connection,
                    database,
                    selectedObject.schema || effectiveSchema,
                    selectedObject.name,
                    'alter',
                  )
                }
                className="apple-btn-secondary"
                title="设计选中表的字段、索引与外键"
              >
                <Edit3 className="h-3.5 w-3.5 text-[#ff9f0a]" />
                设计表
              </button>

              <button
                type="button"
                onClick={() => onDesignTable(connection, database, effectiveSchema, '', 'create')}
                className="apple-btn-secondary"
              >
                <FilePlus2 className="h-3.5 w-3.5 text-[#30d158]" />
                新建表
              </button>

              <button
                type="button"
                disabled={!selectedObject}
                onClick={() =>
                  selectedObject &&
                  void handleTableMaintenance(
                    selectedObject,
                    selectedObject.type === 'VIEW' ? 'drop_view' : 'drop_table',
                  )
                }
                className="apple-btn-danger"
              >
                <Trash2 className="h-3.5 w-3.5" />
                删除
              </button>

              <button
                type="button"
                disabled={!selectedObject || selectedObject.type !== 'TABLE'}
                onClick={() => selectedObject && void handleTableMaintenance(selectedObject, 'empty_table')}
                className="apple-btn-secondary"
                title="清空表中所有数据 (DELETE FROM)"
              >
                <Eraser className="h-3.5 w-3.5 text-[#ff9f0a]" />
                清空表
              </button>

              <div className="mx-1 h-4 w-px bg-white/[0.08]" />

              <button
                type="button"
                onClick={() =>
                  setIoModal({
                    mode: 'import',
                    table: selectedObject?.type === 'TABLE' ? selectedObject.name : '',
                  })
                }
                className="apple-btn-secondary"
              >
                <Upload className="h-3.5 w-3.5 text-[#5e5ce6]" />
                导入
              </button>

              <button
                type="button"
                disabled={!selectedObject}
                onClick={() =>
                  selectedObject &&
                  setIoModal({
                    mode: 'export',
                    table: selectedObject.name,
                  })
                }
                className="apple-btn-secondary"
              >
                <Download className="h-3.5 w-3.5 text-[#30d158]" />
                导出
              </button>
            </>
          ) : (
            <button
              type="button"
              onClick={() => onOpenQuery(connection, database, effectiveSchema)}
              className="apple-btn-primary"
            >
              <Plus className="h-3.5 w-3.5" />
              新建查询
            </button>
          )}

          <button
            type="button"
            onClick={() => void fetchObjects()}
            className="apple-btn-secondary"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
            刷新
          </button>
        </div>

        {/* Context Selectors + Search + View Switcher */}
        <div className="flex flex-wrap items-center gap-2">
          {/* Quick DB / Schema switcher inside Object Browser */}
          {databases.length > 0 && (
            <select
              value={database}
              onChange={(e) => onSelectDatabaseSchema(connection, e.target.value, '')}
              className="apple-input max-w-[140px]"
              title="切换当前数据库"
            >
              {databases.map((dbName) => (
                <option key={dbName} value={dbName}>
                  库: {dbName}
                </option>
              ))}
            </select>
          )}

          {supportsSchemas && schemas.length > 0 && (
            <select
              value={schema}
              onChange={(e) => onSelectDatabaseSchema(connection, database, e.target.value)}
              className="apple-input max-w-[130px]"
              title="切换当前模式 (Schema)"
            >
              {schemas.map((s) => (
                <option key={s} value={s}>
                  模式: {s}
                </option>
              ))}
            </select>
          )}

          <div className="relative flex items-center">
            <Search className="pointer-events-none absolute left-2 h-3.5 w-3.5 text-[#767682]" />
            <input
              type="text"
              placeholder="搜索对象..."
              value={filterText}
              onChange={(e) => setFilterText(e.target.value)}
              className="apple-input w-40 pl-7 pr-6"
            />
            {filterText && (
              <button
                type="button"
                onClick={() => setFilterText('')}
                className="absolute right-1.5 text-[#9e9ea9] hover:text-white"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>

          <div className="apple-segmented">
            <button
              type="button"
              onClick={() => setViewMode('list')}
              className={`apple-segmented-item px-1.5 py-1 ${
                viewMode === 'list' ? 'apple-segmented-item-active' : ''
              }`}
              title="详细信息列表视图"
            >
              <List className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={() => setViewMode('grid')}
              className={`apple-segmented-item px-1.5 py-1 ${
                viewMode === 'grid' ? 'apple-segmented-item-active' : ''
              }`}
              title="图标网格视图"
            >
              <LayoutGrid className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>
      </div>

      {/* Object Type Filter Bar (All / Tables / Views / Queries) */}
      <div className="flex items-center justify-between border-b border-white/[0.08] bg-[#1a1a1d] px-3 py-1.5 text-xs">
        <div className="apple-segmented">
          <button
            type="button"
            onClick={() => onCategoryChange('all')}
            className={`apple-segmented-item ${category === 'all' ? 'apple-segmented-item-active' : ''}`}
          >
            全部对象 ({objects.length})
          </button>
          <button
            type="button"
            onClick={() => onCategoryChange('tables')}
            className={`apple-segmented-item ${category === 'tables' ? 'apple-segmented-item-active' : ''}`}
          >
            <Table2 className="h-3 w-3 text-[#64d2ff]" />
            表 ({tableObjects.length})
          </button>
          <button
            type="button"
            onClick={() => onCategoryChange('views')}
            className={`apple-segmented-item ${category === 'views' ? 'apple-segmented-item-active' : ''}`}
          >
            <Eye className="h-3 w-3 text-[#5ac8fa]" />
            视图 ({viewObjects.length})
          </button>
          <button
            type="button"
            onClick={() => onCategoryChange('queries')}
            className={`apple-segmented-item ${category === 'queries' ? 'apple-segmented-item-active' : ''}`}
          >
            <Terminal className="h-3 w-3 text-[#30d158]" />
            最近查询 ({savedQueries.length})
          </button>
        </div>

        <div className="text-[11px] text-[#9e9ea9]">
          当前位置：<span className="font-medium text-white">{connection.name}</span>
          {database ? ` / ${database}` : ''}
          {schema ? ` / ${schema}` : ''}
        </div>
      </div>

      {error && (
        <div className="border-b border-rose-800 bg-rose-950/70 px-3 py-1.5 text-xs text-rose-200">{error}</div>
      )}
      {actionNotice && (
        <div className="flex items-center justify-between border-b border-emerald-800 bg-emerald-950/60 px-3 py-1.5 text-xs text-emerald-200">
          <span>{actionNotice}</span>
          <button type="button" onClick={() => setActionNotice('')} className="text-emerald-300 hover:text-white">
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}

      {/* Main Content Area */}
      <div className="flex-1 overflow-auto">
        {category === 'queries' ? (
          savedQueries.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-2 text-xs text-slate-500">
              <Terminal className="h-6 w-6 text-slate-600" />
              <span>暂无保存或执行过的 SQL 查询记录</span>
              <button
                type="button"
                onClick={() => onOpenQuery(connection, database, effectiveSchema)}
                className="mt-1 flex items-center gap-1 rounded bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-emerald-500"
              >
                <Plus className="h-3.5 w-3.5" />
                新建查询
              </button>
            </div>
          ) : (
            <div className="divide-y divide-slate-800/70">
              {savedQueries.map((qItem, idx) => (
                <div
                  key={idx}
                  onDoubleClick={() => onOpenQuery(connection, database, effectiveSchema, qItem.sql)}
                  className="flex cursor-pointer items-center justify-between px-4 py-2.5 hover:bg-slate-900/80"
                >
                  <div className="min-w-0 flex-1 pr-4">
                    <div className="flex items-center gap-2 text-[11px] text-slate-400">
                      <span className="font-medium text-emerald-400">SQL #{idx + 1}</span>
                      <span>{qItem.connName}</span>
                      {qItem.database && <span>/ {qItem.database}</span>}
                      <span>· {qItem.time}</span>
                    </div>
                    <div className="mt-1 truncate font-mono text-xs text-slate-200">{qItem.sql}</div>
                  </div>
                  <button
                    type="button"
                    onClick={() => onOpenQuery(connection, database, effectiveSchema, qItem.sql)}
                    className="flex shrink-0 items-center gap-1 rounded border border-emerald-700 bg-emerald-950/60 px-2.5 py-1 text-xs text-emerald-300 hover:bg-emerald-900/70"
                  >
                    <Play className="h-3 w-3" />
                    在查询窗口打开
                  </button>
                </div>
              ))}
            </div>
          )
        ) : loading ? (
          <div className="flex h-full items-center justify-center text-xs text-slate-400">
            <Loader2 className="mr-2 h-4 w-4 animate-spin text-indigo-400" />
            正在加载数据库对象...
          </div>
        ) : filteredObjects.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-xs text-slate-500">
            <Table2 className="h-7 w-7 text-slate-700" />
            <span>
              {filterText ? `未找到匹配「${filterText}」的对象` : '当前数据库/模式下暂无表或视图'}
            </span>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => onDesignTable(connection, database, effectiveSchema, '', 'create')}
                className="flex items-center gap-1 rounded bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-500"
              >
                <FilePlus2 className="h-3.5 w-3.5" />
                新建表
              </button>
              <button
                type="button"
                onClick={() => onOpenQuery(connection, database, effectiveSchema)}
                className="flex items-center gap-1 rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-xs text-slate-200 hover:bg-slate-700"
              >
                <Terminal className="h-3.5 w-3.5 text-emerald-400" />
                新建 SQL 查询
              </button>
            </div>
          </div>
        ) : viewMode === 'list' ? (
          <table className="w-full border-collapse text-left text-[13px]">
            <thead className="sticky top-0 z-10 bg-slate-900 text-slate-300">
              <tr>
                <th className="border-b border-r border-slate-800 px-3.5 py-2.5 font-semibold">名称</th>
                <th className="w-28 border-b border-r border-slate-800 px-3.5 py-2.5 font-semibold">对象类型</th>
                <th className="w-28 border-b border-r border-slate-800 px-3.5 py-2.5 text-right font-semibold">
                  估计行数
                </th>
                <th className="w-32 border-b border-r border-slate-800 px-3.5 py-2.5 font-semibold">引擎 / 模式</th>
                <th className="border-b border-r border-slate-800 px-3.5 py-2.5 font-semibold">注释</th>
                <th className="w-44 border-b border-slate-800 px-3.5 py-2.5 text-center font-semibold">快捷操作</th>
              </tr>
            </thead>
            <tbody>
              {filteredObjects.map((obj) => {
                const isSelected =
                  selectedObject?.name === obj.name && selectedObject?.type === obj.type;
                const isView = obj.type === 'VIEW' || obj.type === 'MATERIALIZED VIEW';
                const objSchema = obj.schema || effectiveSchema;

                return (
                  <tr
                    key={`${obj.type}:${obj.name}`}
                    onClick={() => onSelectObject(obj)}
                    onDoubleClick={() => onOpenTableData(connection, database, objSchema, obj.name)}
                    onContextMenu={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      onSelectObject(obj);
                      setContextMenu({ x: e.clientX, y: e.clientY, object: obj });
                    }}
                    className={`cursor-pointer border-b border-slate-900 transition-colors ${
                      isSelected
                        ? 'bg-indigo-950/75 text-white'
                        : 'text-slate-200 hover:bg-slate-900/70'
                    }`}
                  >
                    <td className="border-r border-slate-900 px-3.5 py-2.5 font-medium">
                      <div className="flex items-center gap-2.5">
                        {isView ? (
                          <Eye className="h-4 w-4 shrink-0 text-teal-400" />
                        ) : (
                          <Table2 className="h-4 w-4 shrink-0 text-sky-400" />
                        )}
                        <span className="truncate font-mono text-[13.5px]">{obj.name}</span>
                      </div>
                    </td>
                    <td className="border-r border-slate-900 px-3.5 py-2.5 text-slate-400">
                      {isView ? '视图 (VIEW)' : '表 (TABLE)'}
                    </td>
                    <td className="border-r border-slate-900 px-3.5 py-2.5 text-right font-mono text-slate-300">
                      {obj.rowEstimate !== undefined && obj.rowEstimate !== null
                        ? obj.rowEstimate.toLocaleString()
                        : '-'}
                    </td>
                    <td className="border-r border-slate-900 px-3.5 py-2.5 text-slate-400">
                      {obj.engine || obj.schema || schema || '-'}
                    </td>
                    <td
                      className="max-w-xs truncate border-r border-slate-900 px-3.5 py-2.5 text-slate-400"
                      title={obj.comment}
                    >
                      {obj.comment || ''}
                    </td>
                    <td
                      className="px-2.5 py-2 text-center"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <div className="inline-flex items-center gap-1">
                        <button
                          type="button"
                          onClick={() => onOpenTableData(connection, database, objSchema, obj.name)}
                          className="rounded border border-slate-700 bg-slate-800 px-2 py-0.5 text-[11px] text-sky-300 hover:bg-slate-700"
                          title="打开表数据"
                        >
                          打开
                        </button>
                        {!isView && (
                          <button
                            type="button"
                            onClick={() => onDesignTable(connection, database, objSchema, obj.name, 'alter')}
                            className="rounded border border-slate-700 bg-slate-800 px-2 py-0.5 text-[11px] text-amber-300 hover:bg-slate-700"
                            title="设计表结构"
                          >
                            设计
                          </button>
                        )}
                        <button
                          type="button"
                          onClick={() =>
                            onOpenQuery(
                              connection,
                              database,
                              objSchema,
                              buildSelectPreviewSql(connection, objSchema, obj.name),
                            )
                          }
                          className="rounded border border-slate-700 bg-slate-800 px-2 py-0.5 text-[11px] text-emerald-300 hover:bg-slate-700"
                          title="生成 SELECT 查询"
                        >
                          SQL
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        ) : (
          /* Navicat Grid Icon View */
          <div className="grid grid-cols-2 gap-3.5 p-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {filteredObjects.map((obj) => {
              const isSelected =
                selectedObject?.name === obj.name && selectedObject?.type === obj.type;
              const isView = obj.type === 'VIEW' || obj.type === 'MATERIALIZED VIEW';
              const objSchema = obj.schema || effectiveSchema;

              return (
                <div
                  key={`${obj.type}:${obj.name}`}
                  onClick={() => onSelectObject(obj)}
                  onDoubleClick={() => onOpenTableData(connection, database, objSchema, obj.name)}
                  onContextMenu={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    onSelectObject(obj);
                    setContextMenu({ x: e.clientX, y: e.clientY, object: obj });
                  }}
                  className={`flex cursor-pointer items-center gap-3 rounded-xl border px-3.5 py-3 transition ${
                    isSelected
                      ? 'border-indigo-500 bg-indigo-950/70 text-white'
                      : 'border-slate-800/90 bg-slate-900/60 text-slate-200 hover:border-slate-700 hover:bg-slate-900'
                  }`}
                >
                  {isView ? (
                    <Eye className="h-5 w-5 shrink-0 text-teal-400" />
                  ) : (
                    <Table2 className="h-5 w-5 shrink-0 text-sky-400" />
                  )}
                  <div className="min-w-0 flex-1 space-y-0.5">
                    <div className="truncate font-mono text-[13.5px] font-medium" title={obj.name}>
                      {obj.name}
                    </div>
                    <div className="truncate text-[11.5px] text-slate-400">
                      {obj.rowEstimate !== undefined && obj.rowEstimate !== null
                        ? `${obj.rowEstimate.toLocaleString()} 行`
                        : isView
                        ? '视图'
                        : '数据表'}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Right-Click Context Menu */}
      {contextMenu && (
        <div
          style={{
            top: Math.min(contextMenu.y, window.innerHeight - 280),
            left: Math.min(contextMenu.x, window.innerWidth - 200),
          }}
          onClick={(e) => e.stopPropagation()}
          className="fixed z-50 w-48 rounded-lg border border-slate-700 bg-slate-900 py-1 text-xs text-slate-200 shadow-2xl"
        >
          <div className="border-b border-slate-800 px-3 py-1 font-mono text-[11px] font-semibold text-indigo-300 truncate">
            {contextMenu.object.name}
          </div>
          <button
            type="button"
            onClick={() => {
              const obj = contextMenu.object;
              setContextMenu(null);
              onOpenTableData(connection, database, obj.schema || effectiveSchema, obj.name);
            }}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-indigo-600 hover:text-white"
          >
            <FolderOpen className="h-3.5 w-3.5 text-sky-400" />
            打开表
          </button>
          {contextMenu.object.type === 'TABLE' && (
            <button
              type="button"
              onClick={() => {
                const obj = contextMenu.object;
                setContextMenu(null);
                onDesignTable(connection, database, obj.schema || effectiveSchema, obj.name, 'alter');
              }}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-indigo-600 hover:text-white"
            >
              <Edit3 className="h-3.5 w-3.5 text-amber-400" />
              设计表结构
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              setContextMenu(null);
              onDesignTable(connection, database, effectiveSchema, '', 'create');
            }}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-indigo-600 hover:text-white"
          >
            <FilePlus2 className="h-3.5 w-3.5 text-emerald-400" />
            新建表...
          </button>
          <button
            type="button"
            onClick={() => {
              const obj = contextMenu.object;
              setContextMenu(null);
              onOpenQuery(
                connection,
                database,
                obj.schema || effectiveSchema,
                buildSelectPreviewSql(connection, obj.schema || effectiveSchema, obj.name),
              );
            }}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-indigo-600 hover:text-white"
          >
            <Terminal className="h-3.5 w-3.5 text-emerald-400" />
            新建查询 (SELECT 前100条)
          </button>
          <div className="my-1 border-t border-slate-800" />
          <button
            type="button"
            onClick={() => {
              const obj = contextMenu.object;
              setContextMenu(null);
              setIoModal({ mode: 'import', table: obj.name });
            }}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-indigo-600 hover:text-white"
          >
            <Upload className="h-3.5 w-3.5 text-indigo-400" />
            导入向导...
          </button>
          <button
            type="button"
            onClick={() => {
              const obj = contextMenu.object;
              setContextMenu(null);
              setIoModal({ mode: 'export', table: obj.name });
            }}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-indigo-600 hover:text-white"
          >
            <Download className="h-3.5 w-3.5 text-teal-400" />
            导出向导...
          </button>
          <button
            type="button"
            onClick={() => {
              void navigator.clipboard?.writeText(contextMenu.object.name);
              setContextMenu(null);
            }}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-indigo-600 hover:text-white"
          >
            <Copy className="h-3.5 w-3.5 text-slate-400" />
            复制对象名
          </button>
          {onShowDetails && (
            <button
              type="button"
              onClick={() => {
                onSelectObject(contextMenu.object);
                setContextMenu(null);
                onShowDetails();
              }}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-indigo-600 hover:text-white"
            >
              <Info className="h-3.5 w-3.5 text-sky-400" />
              查看注释
            </button>
          )}
          <div className="my-1 border-t border-slate-800" />
          {contextMenu.object.type === 'TABLE' && (
            <>
              <button
                type="button"
                onClick={() => {
                  const obj = contextMenu.object;
                  setContextMenu(null);
                  void handleTableMaintenance(obj, 'empty_table');
                }}
                className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-amber-300 hover:bg-amber-900/60"
              >
                <Eraser className="h-3.5 w-3.5" />
                清空表 (DELETE)
              </button>
              <button
                type="button"
                onClick={() => {
                  const obj = contextMenu.object;
                  setContextMenu(null);
                  void handleTableMaintenance(obj, 'truncate_table');
                }}
                className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-amber-300 hover:bg-amber-900/60"
              >
                <Eraser className="h-3.5 w-3.5" />
                截断表 (TRUNCATE)
              </button>
            </>
          )}
          <button
            type="button"
            onClick={() => {
              const obj = contextMenu.object;
              setContextMenu(null);
              void handleTableMaintenance(
                obj,
                obj.type === 'VIEW' ? 'drop_view' : 'drop_table',
              );
            }}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-rose-400 hover:bg-rose-900/60 hover:text-rose-200"
          >
            <Trash2 className="h-3.5 w-3.5" />
            删除{contextMenu.object.type === 'VIEW' ? '视图' : '表'} (DROP)
          </button>
        </div>
      )}

      {ioModal && (
        <ImportExportModal
          mode={ioModal.mode}
          connectionId={connection.id}
          database={database}
          schema={effectiveSchema}
          table={ioModal.table}
          onClose={() => setIoModal(null)}
          onImported={() => void fetchObjects()}
        />
      )}
    </div>
  );
};
