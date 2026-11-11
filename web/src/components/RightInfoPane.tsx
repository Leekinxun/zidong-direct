import React, { useEffect, useState } from 'react';
import {
  Code2,
  Copy,
  Database,
  Edit3,
  Eye,
  Info,
  KeyRound,
  Layers,
  Loader2,
  Server,
  Table2,
  Terminal,
} from 'lucide-react';
import { api } from '../api/client';
import type { ConnectionView, DatabaseObject, EngineType, TableSchema } from '../types/db';

interface Props {
  connection: ConnectionView | null;
  database: string;
  schema: string;
  selectedObject: DatabaseObject | null;
  onOpenTableData: (conn: ConnectionView, db: string, sch: string, table: string) => void;
  onDesignTable: (conn: ConnectionView, db: string, sch: string, table: string, mode: 'create' | 'alter') => void;
  onOpenQuery: (conn: ConnectionView, db: string, sch: string, initialSql?: string) => void;
}

const ENGINE_NAMES: Record<EngineType, string> = {
  mysql: 'MySQL / MariaDB',
  postgres: 'PostgreSQL',
  sqlite: 'SQLite 3',
  sqlserver: 'Microsoft SQL Server',
  oracle: 'Oracle Database',
  dameng: '达梦数据库 (DM8)',
};

export const RightInfoPane: React.FC<Props> = ({
  connection,
  database,
  schema,
  selectedObject,
  onOpenTableData,
  onDesignTable,
  onOpenQuery,
}) => {
  const [activeTab, setActiveTab] = useState<'general' | 'columns' | 'ddl'>('general');
  const [tableSchema, setTableSchema] = useState<TableSchema | null>(null);
  const [loading, setLoading] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!connection || !selectedObject) {
      setTableSchema(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    api
      .describeTable(
        connection.id,
        database,
        schema || selectedObject.schema || '',
        selectedObject.name,
      )
      .then((res) => {
        if (!cancelled) {
          setTableSchema(res.schema);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setTableSchema(null);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [connection?.id, database, schema, selectedObject?.name, selectedObject?.schema]);

  const handleCopyDDL = () => {
    if (!tableSchema?.ddl) return;
    void navigator.clipboard?.writeText(tableSchema.ddl);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <aside className="flex h-full w-80 shrink-0 flex-col border-l border-white/[0.08] bg-[#1e1e21] text-[13px] select-none">
      {/* Info Pane Header Segmented Tabs */}
      <div className="flex items-center justify-center border-b border-white/[0.08] bg-[#1a1a1d] px-3 py-2.5">
        <div className="apple-segmented w-full justify-between">
          <button
            type="button"
            onClick={() => setActiveTab('general')}
            className={`apple-segmented-item flex-1 justify-center ${
              activeTab === 'general' ? 'apple-segmented-item-active' : ''
            }`}
          >
            <Info className="h-3.5 w-3.5" />
            常规
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('columns')}
            className={`apple-segmented-item flex-1 justify-center ${
              activeTab === 'columns' ? 'apple-segmented-item-active' : ''
            }`}
          >
            <Layers className="h-3.5 w-3.5" />
            字段 {tableSchema ? `(${tableSchema.columns.length})` : ''}
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('ddl')}
            className={`apple-segmented-item flex-1 justify-center ${
              activeTab === 'ddl' ? 'apple-segmented-item-active' : ''
            }`}
          >
            <Code2 className="h-3.5 w-3.5" />
            DDL
          </button>
        </div>
      </div>

      {/* Info Pane Body */}
      <div className="flex-1 overflow-y-auto p-3.5 select-text">
        {!connection ? (
          <div className="flex h-full flex-col items-center justify-center text-center text-slate-500">
            <Server className="mb-2 h-6 w-6 text-slate-600" />
            <span>未选择连接或对象</span>
          </div>
        ) : activeTab === 'general' ? (
          <div className="space-y-4">
            {/* Object Header Card if selected */}
            {selectedObject ? (
              <div className="rounded-xl border border-slate-800 bg-slate-950/70 p-3.5">
                <div className="flex items-start gap-3">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-slate-800 bg-slate-900">
                    {selectedObject.type === 'VIEW' || selectedObject.type === 'MATERIALIZED VIEW' ? (
                      <Eye className="h-5 w-5 text-teal-400" />
                    ) : (
                      <Table2 className="h-5 w-5 text-sky-400" />
                    )}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[15px] font-semibold text-slate-100" title={selectedObject.name}>
                      {selectedObject.name}
                    </div>
                    <div className="mt-0.5 text-xs text-slate-400">
                      {selectedObject.type === 'VIEW' ? '视图 (View)' : '数据表 (Table)'}
                    </div>
                  </div>
                </div>

                <div className="mt-3 flex flex-wrap gap-2 border-t border-slate-800/80 pt-3">
                  <button
                    type="button"
                    onClick={() =>
                      onOpenTableData(
                        connection,
                        database,
                        schema || selectedObject.schema || '',
                        selectedObject.name,
                      )
                    }
                    className="apple-btn-primary flex-1 py-1.5 text-[13px]"
                  >
                    <Table2 className="h-3.5 w-3.5" />
                    打开表
                  </button>
                  {selectedObject.type === 'TABLE' && (
                    <button
                      type="button"
                      onClick={() =>
                        onDesignTable(
                          connection,
                          database,
                          schema || selectedObject.schema || '',
                          selectedObject.name,
                          'alter',
                        )
                      }
                      className="apple-btn-secondary flex-1 py-1.5 text-[13px]"
                    >
                      <Edit3 className="h-3.5 w-3.5 text-amber-400" />
                      设计表
                    </button>
                  )}
                </div>
              </div>
            ) : (
              <div className="rounded-xl border border-slate-800 bg-slate-950/70 p-3.5">
                <div className="flex items-center gap-3">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-slate-800 bg-slate-900">
                    <Database className="h-5 w-5 text-indigo-400" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[15px] font-semibold text-slate-100">{connection.name}</div>
                    <div className="mt-0.5 text-xs text-slate-400">{ENGINE_NAMES[connection.engine]}</div>
                  </div>
                </div>
              </div>
            )}

            {/* Object details */}
            {selectedObject && (
              <div>
                <div className="mb-2 text-xs font-semibold uppercase tracking-wider text-slate-400">
                  对象属性
                </div>
                <dl className="divide-y divide-slate-800/70 rounded-lg border border-slate-800 bg-slate-950/50 px-3 py-1">
                  <div className="flex justify-between py-2">
                    <dt className="text-slate-400">对象名称</dt>
                    <dd className="max-w-[160px] truncate font-mono text-slate-200">{selectedObject.name}</dd>
                  </div>
                  <div className="flex justify-between py-2">
                    <dt className="text-slate-400">类型</dt>
                    <dd className="text-slate-200">{selectedObject.type}</dd>
                  </div>
                  {(schema || selectedObject.schema) && (
                    <div className="flex justify-between py-2">
                      <dt className="text-slate-400">所属模式</dt>
                      <dd className="font-mono text-slate-200">{schema || selectedObject.schema}</dd>
                    </div>
                  )}
                  {selectedObject.rowEstimate !== undefined && selectedObject.rowEstimate !== null && (
                    <div className="flex justify-between py-2">
                      <dt className="text-slate-400">估计行数</dt>
                      <dd className="font-mono text-slate-200">{selectedObject.rowEstimate.toLocaleString()}</dd>
                    </div>
                  )}
                  {selectedObject.engine && (
                    <div className="flex justify-between py-2">
                      <dt className="text-slate-400">存储引擎</dt>
                      <dd className="text-slate-200">{selectedObject.engine}</dd>
                    </div>
                  )}
                  {tableSchema && (
                    <>
                      <div className="flex justify-between py-2">
                        <dt className="text-slate-400">字段数量</dt>
                        <dd className="font-mono text-slate-200">{tableSchema.columns.length}</dd>
                      </div>
                      <div className="flex justify-between py-2">
                        <dt className="text-slate-400">索引数量</dt>
                        <dd className="font-mono text-slate-200">{(tableSchema.indexes || []).length}</dd>
                      </div>
                      <div className="flex justify-between py-2">
                        <dt className="text-slate-400">外键数量</dt>
                        <dd className="font-mono text-slate-200">{(tableSchema.foreignKeys || []).length}</dd>
                      </div>
                    </>
                  )}
                  {selectedObject.comment && (
                    <div className="py-2">
                      <dt className="mb-1 text-slate-400">表注释</dt>
                      <dd className="rounded bg-slate-900 p-2 text-slate-300">{selectedObject.comment}</dd>
                    </div>
                  )}
                </dl>
              </div>
            )}

            {/* Connection details */}
            <div>
              <div className="mb-2 text-xs font-semibold uppercase tracking-wider text-slate-400">
                连接信息
              </div>
              <dl className="divide-y divide-slate-800/70 rounded-lg border border-slate-800 bg-slate-950/50 px-3 py-1">
                <div className="flex justify-between py-2">
                  <dt className="text-slate-400">连接名</dt>
                  <dd className="max-w-[160px] truncate text-slate-200">{connection.name}</dd>
                </div>
                <div className="flex justify-between py-2">
                  <dt className="text-slate-400">数据库引擎</dt>
                  <dd className="text-slate-200">{ENGINE_NAMES[connection.engine]}</dd>
                </div>
                {connection.engine === 'sqlite' ? (
                  <div className="flex justify-between py-2">
                    <dt className="text-slate-400">文件路径</dt>
                    <dd className="max-w-[160px] truncate font-mono text-slate-200">{connection.sqlitePath}</dd>
                  </div>
                ) : (
                  <>
                    <div className="flex justify-between py-2">
                      <dt className="text-slate-400">主机地址</dt>
                      <dd className="font-mono text-slate-200">
                        {connection.host}:{connection.port}
                      </dd>
                    </div>
                    <div className="flex justify-between py-2">
                      <dt className="text-slate-400">当前用户</dt>
                      <dd className="font-mono text-slate-200">{connection.username || '-'}</dd>
                    </div>
                  </>
                )}
                <div className="flex justify-between py-2">
                  <dt className="text-slate-400">当前库</dt>
                  <dd className="max-w-[160px] truncate font-mono text-slate-200">
                    {database || connection.database || '-'}
                  </dd>
                </div>
                {schema && (
                  <div className="flex justify-between py-2">
                    <dt className="text-slate-400">当前模式</dt>
                    <dd className="max-w-[160px] truncate font-mono text-slate-200">{schema}</dd>
                  </div>
                )}
                <div className="flex justify-between py-2">
                  <dt className="text-slate-400">SSH 隧道</dt>
                  <dd className="text-slate-200">
                    {connection.ssh.enabled ? (
                      <span className="inline-flex items-center gap-1 text-amber-300">
                        <KeyRound className="h-3.5 w-3.5" />
                        {connection.ssh.host}:{connection.ssh.port}
                      </span>
                    ) : (
                      <span className="text-slate-500">未启用</span>
                    )}
                  </dd>
                </div>
              </dl>
            </div>
          </div>
        ) : activeTab === 'columns' ? (
          loading ? (
            <div className="flex h-32 items-center justify-center text-slate-400">
              <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> 正在读取字段...
            </div>
          ) : !tableSchema ? (
            <div className="py-8 text-center text-slate-500">请先在左侧或对象列表中单击选择一个表</div>
          ) : (
            <div className="space-y-1.5">
              {tableSchema.columns.map((col) => (
                <div
                  key={col.name}
                  className="flex items-center justify-between rounded-lg border border-slate-800/80 bg-slate-950/60 px-3 py-2"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="truncate font-mono text-[13.5px] font-medium text-slate-200">{col.name}</span>
                      {col.primaryKey && (
                        <span className="rounded bg-amber-900/80 px-1.5 py-0.5 text-[10.5px] font-semibold text-amber-300">
                          PK
                        </span>
                      )}
                      {col.generatedExpr && (
                        <span
                          className="rounded border border-purple-700/60 bg-purple-950/90 px-1.5 py-0.5 text-[10.5px] font-semibold text-purple-300"
                          title={`GENERATED ALWAYS AS (${col.generatedExpr}) ${col.generatedKind || 'STORED'}`}
                        >
                          GEN {col.generatedKind === 'VIRTUAL' ? 'VIRTUAL' : 'STORED'}
                        </span>
                      )}
                      {!col.nullable && (
                        <span className="rounded bg-slate-800 px-1.5 py-0.5 text-[10.5px] text-slate-400">
                          NOT NULL
                        </span>
                      )}
                    </div>
                    {col.generatedExpr && (
                      <div className="mt-0.5 truncate font-mono text-[11px] text-purple-300/90" title={col.generatedExpr}>
                        = {col.generatedExpr}
                      </div>
                    )}
                    {col.comment && <div className="mt-0.5 truncate text-xs text-slate-500">{col.comment}</div>}
                  </div>
                  <span className="ml-2 shrink-0 font-mono text-xs text-indigo-300">{col.type}</span>
                </div>
              ))}
            </div>
          )
        ) : loading ? (
          <div className="flex h-32 items-center justify-center text-slate-400">
            <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> 正在生成 DDL...
          </div>
        ) : !tableSchema?.ddl ? (
          <div className="py-8 text-center text-slate-500">请先在左侧或对象列表中单击选择一个表以查看 DDL</div>
        ) : (
          <div className="flex h-full flex-col gap-2.5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-slate-400">CREATE TABLE 语句</span>
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={handleCopyDDL}
                  className="apple-btn-secondary px-2.5 py-1 text-xs"
                >
                  <Copy className="h-3.5 w-3.5" />
                  {copied ? '已复制' : '复制'}
                </button>
                <button
                  type="button"
                  onClick={() =>
                    onOpenQuery(
                      connection,
                      database,
                      schema || selectedObject?.schema || '',
                      tableSchema.ddl,
                    )
                  }
                  className="apple-btn-primary px-2.5 py-1 text-xs"
                  title="在查询编辑器中打开此 DDL"
                >
                  <Terminal className="h-3.5 w-3.5" />
                  查询打开
                </button>
              </div>
            </div>
            <pre className="flex-1 overflow-auto rounded-lg border border-slate-800 bg-slate-950 p-3 font-mono text-[13px] leading-relaxed text-sky-200">
              {tableSchema.ddl}
            </pre>
          </div>
        )}
      </div>
    </aside>
  );
};
