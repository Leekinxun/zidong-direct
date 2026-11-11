import React, { useEffect, useState } from 'react';
import { Code2, KeyRound, Loader2, Play, Plus, Save, Table2, Trash2 } from 'lucide-react';
import { api, ApiError } from '../api/client';
import type {
  ColumnDef,
  DDLPreviewResponse,
  EngineCapabilities,
  EngineType,
  ForeignKeyDef,
  IndexDef,
  TableSchema,
} from '../types/db';

interface Props {
  connectionId: string;
  engine: EngineType;
  capabilities?: EngineCapabilities;
  database: string;
  schema: string;
  table: string;
  mode: 'create' | 'alter';
  onApplied: (tableName: string) => void;
  onOpenData?: (tableName: string) => void;
}

export const StructureDesigner: React.FC<Props> = ({
  connectionId,
  engine,
  capabilities,
  database,
  schema,
  table,
  mode,
  onApplied,
  onOpenData,
}) => {
  const [subTab, setSubTab] = useState<'columns' | 'indexes' | 'fks' | 'sql'>('columns');
  const [tableName, setTableName] = useState(mode === 'create' ? 'new_table' : table);
  const [originalSchema, setOriginalSchema] = useState<TableSchema | undefined>(undefined);
  const [columns, setColumns] = useState<ColumnDef[]>([]);
  const [indexes, setIndexes] = useState<IndexDef[]>([]);
  const [foreignKeys, setForeignKeys] = useState<ForeignKeyDef[]>([]);

  const [loading, setLoading] = useState(false);
  const [preview, setPreview] = useState<DDLPreviewResponse | null>(null);
  const [executing, setExecuting] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  const typeSuggestions = capabilities?.dataTypes || ['INT', 'VARCHAR(255)', 'TEXT', 'DATETIME'];

  const loadExisting = async () => {
    if (mode === 'create') {
      const idType = engine === 'sqlite' ? 'INTEGER' : engine === 'oracle' ? 'NUMBER(19)' : 'BIGINT';
      setColumns([
        {
          name: 'id',
          type: idType,
          nullable: false,
          primaryKey: true,
          autoIncrement: true,
          ordinal: 1,
        },
        {
          name: 'name',
          type: engine === 'oracle' ? 'VARCHAR2(255)' : engine === 'sqlite' ? 'TEXT' : 'VARCHAR(255)',
          nullable: false,
          primaryKey: false,
          autoIncrement: false,
          ordinal: 2,
        },
      ]);
      setIndexes([]);
      setForeignKeys([]);
      return;
    }

    setLoading(true);
    setError('');
    try {
      const res = await api.describeTable(connectionId, database, schema, table);
      const s = res.schema;
      setOriginalSchema(s);
      setTableName(s.table);
      setColumns(
        s.columns.map((c) => ({
          ...c,
          originalName: c.name,
        })),
      );
      setIndexes(s.indexes || []);
      setForeignKeys(s.foreignKeys || []);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '读取表结构失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadExisting();
  }, [connectionId, database, schema, table, mode]);

  const buildTargetSchema = (): TableSchema => ({
    database,
    schema,
    table: tableName.trim(),
    columns: columns.map((c, idx) => ({ ...c, ordinal: idx + 1 })),
    indexes,
    foreignKeys,
  });

  const generatePreview = async (): Promise<DDLPreviewResponse | null> => {
    setError('');
    try {
      const res = await api.previewDDL({
        mode,
        connectionId,
        database,
        schema,
        tableName: mode === 'alter' ? table : tableName.trim(),
        newTableName: mode === 'alter' && tableName.trim() !== table ? tableName.trim() : undefined,
        original: originalSchema,
        target: buildTargetSchema(),
      });
      setPreview(res);
      return res;
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '生成 DDL 预览失败');
      return null;
    }
  };

  const handleSwitchToSqlTab = async () => {
    setSubTab('sql');
    await generatePreview();
  };

  const handleSaveAndExecute = async () => {
    setError('');
    setMessage('');
    const currentPreview = await generatePreview();
    if (!currentPreview) return;
    if (currentPreview.statements.length === 0) {
      setMessage('未检测到需要提交的表结构变更。');
      return;
    }
    setExecuting(true);
    try {
      await api.executeDDL({
        connectionId,
        database,
        statements: currentPreview.statements,
      });
      setMessage('表结构保存成功！');
      setPreview(null);
      onApplied(tableName.trim());
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '执行 DDL 失败');
    } finally {
      setExecuting(false);
    }
  };

  const addColumn = () => {
    const defaultType = typeSuggestions[0] || 'VARCHAR(255)';
    setColumns((prev) => [
      ...prev,
      {
        name: `col_${prev.length + 1}`,
        type: defaultType,
        nullable: true,
        primaryKey: false,
        autoIncrement: false,
        ordinal: prev.length + 1,
      },
    ]);
  };

  const addIndex = () => {
    const firstCol = columns[0]?.name || '';
    setIndexes((prev) => [
      ...prev,
      {
        name: `idx_${tableName}_${prev.length + 1}`,
        columns: firstCol ? [firstCol] : [],
        unique: false,
        primary: false,
      },
    ]);
  };

  const addFK = () => {
    const firstCol = columns[0]?.name || '';
    setForeignKeys((prev) => [
      ...prev,
      {
        name: `fk_${tableName}_${prev.length + 1}`,
        columns: firstCol ? [firstCol] : [],
        refSchema: schema,
        refTable: '',
        refColumns: ['id'],
        onDelete: 'NO ACTION',
        onUpdate: 'NO ACTION',
      },
    ]);
  };

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-xs text-slate-400">
        <Loader2 className="mr-2 h-4 w-4 animate-spin text-indigo-400" /> 正在加载表结构...
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col bg-slate-950 select-none">
      {/* Top Navicat-Style Table Designer Action Bar */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.08] bg-[#1e1e21] px-3 py-1.5">
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            disabled={executing}
            onClick={() => void handleSaveAndExecute()}
            className="apple-btn-primary"
          >
            {executing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            保存变更
          </button>

          {subTab === 'columns' && (
            <button
              type="button"
              onClick={addColumn}
              className="apple-btn-secondary"
            >
              <Plus className="h-3.5 w-3.5 text-[#30d158]" />
              添加字段
            </button>
          )}
          {subTab === 'indexes' && (
            <button
              type="button"
              onClick={addIndex}
              className="apple-btn-secondary"
            >
              <Plus className="h-3.5 w-3.5 text-[#30d158]" />
              添加索引
            </button>
          )}
          {subTab === 'fks' && (
            <button
              type="button"
              onClick={addFK}
              className="apple-btn-secondary"
            >
              <Plus className="h-3.5 w-3.5 text-[#30d158]" />
              添加外键
            </button>
          )}

          <div className="mx-1 h-4 w-px bg-white/[0.08]" />

          <div className="flex items-center gap-1.5">
            <span className="text-xs text-[#9e9ea9]">表名：</span>
            <input
              type="text"
              value={tableName}
              onChange={(e) => setTableName(e.target.value)}
              className="apple-input font-mono font-medium"
            />
          </div>
        </div>

        <div className="flex items-center gap-1.5">
          {mode === 'alter' && onOpenData && (
            <button
              type="button"
              onClick={() => onOpenData(tableName.trim())}
              className="apple-btn-secondary"
              title="切换到数据表视图"
            >
              <Table2 className="h-3.5 w-3.5 text-[#64d2ff]" />
              打开表数据
            </button>
          )}
        </div>
      </div>

      {/* Apple Segmented Sub-Tab Strip: 字段 | 索引 | 外键 | SQL 预览 */}
      <div className="flex items-center border-b border-white/[0.08] bg-[#1a1a1d] px-3 py-1.5 text-xs">
        <div className="apple-segmented">
          <button
            type="button"
            onClick={() => setSubTab('columns')}
            className={`apple-segmented-item ${subTab === 'columns' ? 'apple-segmented-item-active' : ''}`}
          >
            字段 ({columns.length})
          </button>
          <button
            type="button"
            onClick={() => setSubTab('indexes')}
            className={`apple-segmented-item ${subTab === 'indexes' ? 'apple-segmented-item-active' : ''}`}
          >
            索引 ({indexes.length})
          </button>
          <button
            type="button"
            onClick={() => setSubTab('fks')}
            className={`apple-segmented-item ${subTab === 'fks' ? 'apple-segmented-item-active' : ''}`}
          >
            外键 ({foreignKeys.length})
          </button>
          <button
            type="button"
            onClick={() => void handleSwitchToSqlTab()}
            className={`apple-segmented-item ${subTab === 'sql' ? 'apple-segmented-item-active' : ''}`}
          >
            <Code2 className="h-3.5 w-3.5" />
            SQL 预览
          </button>
        </div>
      </div>

      {error && (
        <div className="border-b border-rose-800 bg-rose-950/70 px-3 py-2 text-xs text-rose-200">{error}</div>
      )}
      {message && (
        <div className="border-b border-emerald-800 bg-emerald-950/60 px-3 py-2 text-xs text-emerald-200">
          {message}
        </div>
      )}

      <div className="flex-1 overflow-auto p-3 select-text">
        {subTab === 'columns' && (
          <div>
            <datalist id={`types-${engine}`}>
              {typeSuggestions.map((t) => (
                <option key={t} value={t} />
              ))}
            </datalist>

            <table className="w-full border-collapse text-left text-[13.5px]">
              <thead className="bg-slate-900">
                <tr>
                  <th className="w-12 border border-slate-800 px-2.5 py-2.5 text-center">#</th>
                  <th className="border border-slate-800 px-3 py-2.5">字段名</th>
                  <th className="border border-slate-800 px-3 py-2.5">数据类型</th>
                  <th className="w-20 border border-slate-800 px-2.5 py-2.5 text-center">允许空</th>
                  <th className="w-20 border border-slate-800 px-2.5 py-2.5 text-center">主键</th>
                  <th className="w-20 border border-slate-800 px-2.5 py-2.5 text-center">自增</th>
                  <th className="border border-slate-800 px-3 py-2.5">默认值</th>
                  <th className="border border-slate-800 px-3 py-2.5">注释</th>
                  <th className="w-14 border border-slate-800 px-2.5 py-2.5 text-center">操作</th>
                </tr>
              </thead>
              <tbody>
                {columns.map((col, idx) => (
                  <tr key={idx} className="hover:bg-slate-900/60">
                    <td className="border border-slate-800 p-1.5 text-center font-mono text-slate-500">
                      {idx + 1}
                    </td>
                    <td className="border border-slate-800 p-1.5">
                      <input
                        type="text"
                        value={col.name}
                        onChange={(e) =>
                          setColumns((prev) =>
                            prev.map((c, i) => (i === idx ? { ...c, name: e.target.value } : c)),
                          )
                        }
                        className="w-full rounded bg-slate-900 px-2.5 py-1.5 font-mono text-[13.5px] text-slate-100 outline-none focus:ring-1 focus:ring-indigo-500"
                      />
                    </td>
                    <td className="border border-slate-800 p-1.5">
                      <input
                        type="text"
                        list={`types-${engine}`}
                        value={col.type}
                        onChange={(e) =>
                          setColumns((prev) =>
                            prev.map((c, i) => (i === idx ? { ...c, type: e.target.value } : c)),
                          )
                        }
                        className="w-full rounded bg-slate-900 px-2.5 py-1.5 font-mono text-[13.5px] text-slate-100 outline-none focus:ring-1 focus:ring-indigo-500"
                      />
                    </td>
                    <td className="border border-slate-800 p-1.5 text-center">
                      <input
                        type="checkbox"
                        checked={col.nullable}
                        onChange={(e) =>
                          setColumns((prev) =>
                            prev.map((c, i) => (i === idx ? { ...c, nullable: e.target.checked } : c)),
                          )
                        }
                      />
                    </td>
                    <td className="border border-slate-800 p-1.5 text-center">
                      <button
                        type="button"
                        onClick={() =>
                          setColumns((prev) =>
                            prev.map((c, i) =>
                              i === idx
                                ? {
                                    ...c,
                                    primaryKey: !c.primaryKey,
                                    nullable: !c.primaryKey ? false : c.nullable,
                                  }
                                : c,
                            ),
                          )
                        }
                        className={`inline-flex items-center justify-center rounded px-2 py-0.5 text-xs font-semibold ${
                          col.primaryKey
                            ? 'bg-amber-900/90 text-amber-300'
                            : 'bg-slate-800 text-slate-500 hover:text-slate-300'
                        }`}
                        title="点击切换主键"
                      >
                        <KeyRound className="mr-1 h-3.5 w-3.5" />
                        {col.primaryKey ? 'PK' : '-'}
                      </button>
                    </td>
                    <td className="border border-slate-800 p-1.5 text-center">
                      <input
                        type="checkbox"
                        checked={col.autoIncrement}
                        onChange={(e) =>
                          setColumns((prev) =>
                            prev.map((c, i) => (i === idx ? { ...c, autoIncrement: e.target.checked } : c)),
                          )
                        }
                      />
                    </td>
                    <td className="border border-slate-800 p-1.5">
                      <input
                        type="text"
                        placeholder="无"
                        value={col.defaultValue ?? ''}
                        onChange={(e) =>
                          setColumns((prev) =>
                            prev.map((c, i) =>
                              i === idx
                                ? { ...c, defaultValue: e.target.value === '' ? null : e.target.value }
                                : c,
                            ),
                          )
                        }
                        className="w-full rounded bg-slate-900 px-2.5 py-1.5 font-mono text-[13.5px] text-slate-100 outline-none focus:ring-1 focus:ring-indigo-500"
                      />
                    </td>
                    <td className="border border-slate-800 p-1.5">
                      <input
                        type="text"
                        value={col.comment ?? ''}
                        onChange={(e) =>
                          setColumns((prev) =>
                            prev.map((c, i) => (i === idx ? { ...c, comment: e.target.value } : c)),
                          )
                        }
                        className="w-full rounded bg-slate-900 px-2.5 py-1.5 text-[13.5px] text-slate-100 outline-none focus:ring-1 focus:ring-indigo-500"
                      />
                    </td>
                    <td className="border border-slate-800 p-1.5 text-center">
                      <button
                        type="button"
                        onClick={() => setColumns((prev) => prev.filter((_, i) => i !== idx))}
                        className="text-slate-400 hover:text-rose-400"
                        title="删除此字段"
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {subTab === 'indexes' && (
          <table className="w-full border-collapse text-left text-xs">
            <thead className="bg-slate-900">
              <tr>
                <th className="border border-slate-800 px-2 py-1.5">索引名</th>
                <th className="border border-slate-800 px-2 py-1.5">字段列表（逗号分隔）</th>
                <th className="w-28 border border-slate-800 px-2 py-1.5 text-center">唯一索引</th>
                <th className="w-12 border border-slate-800 px-2 py-1.5 text-center">删除</th>
              </tr>
            </thead>
            <tbody>
              {indexes.map((idxItem, i) => (
                <tr key={i}>
                  <td className="border border-slate-800 p-1">
                    <input
                      type="text"
                      disabled={idxItem.primary}
                      value={idxItem.name}
                      onChange={(e) =>
                        setIndexes((prev) =>
                          prev.map((it, k) => (k === i ? { ...it, name: e.target.value } : it)),
                        )
                      }
                      className="w-full rounded bg-slate-900 px-2 py-1 text-xs text-slate-100 disabled:opacity-50"
                    />
                  </td>
                  <td className="border border-slate-800 p-1">
                    <input
                      type="text"
                      disabled={idxItem.primary}
                      value={idxItem.columns.join(', ')}
                      onChange={(e) =>
                        setIndexes((prev) =>
                          prev.map((it, k) =>
                            k === i
                              ? {
                                  ...it,
                                  columns: e.target.value
                                    .split(',')
                                    .map((s) => s.trim())
                                    .filter(Boolean),
                                }
                              : it,
                          ),
                        )
                      }
                      className="w-full rounded bg-slate-900 px-2 py-1 font-mono text-xs text-slate-100 disabled:opacity-50"
                    />
                  </td>
                  <td className="border border-slate-800 p-1 text-center">
                    <input
                      type="checkbox"
                      disabled={idxItem.primary}
                      checked={idxItem.unique}
                      onChange={(e) =>
                        setIndexes((prev) =>
                          prev.map((it, k) => (k === i ? { ...it, unique: e.target.checked } : it)),
                        )
                      }
                    />
                  </td>
                  <td className="border border-slate-800 p-1 text-center">
                    {!idxItem.primary && (
                      <button
                        type="button"
                        onClick={() => setIndexes((prev) => prev.filter((_, k) => k !== i))}
                        className="text-slate-400 hover:text-rose-400"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {subTab === 'fks' && (
          <table className="w-full border-collapse text-left text-xs">
            <thead className="bg-slate-900">
              <tr>
                <th className="border border-slate-800 px-2 py-1.5">外键名</th>
                <th className="border border-slate-800 px-2 py-1.5">字段</th>
                <th className="border border-slate-800 px-2 py-1.5">被引用表</th>
                <th className="border border-slate-800 px-2 py-1.5">被引用字段</th>
                <th className="border border-slate-800 px-2 py-1.5">删除时动作 (ON DELETE)</th>
                <th className="w-12 border border-slate-800 px-2 py-1.5 text-center">删除</th>
              </tr>
            </thead>
            <tbody>
              {foreignKeys.map((fk, i) => (
                <tr key={i}>
                  <td className="border border-slate-800 p-1">
                    <input
                      type="text"
                      value={fk.name}
                      onChange={(e) =>
                        setForeignKeys((prev) =>
                          prev.map((it, k) => (k === i ? { ...it, name: e.target.value } : it)),
                        )
                      }
                      className="w-full rounded bg-slate-900 px-2 py-1 text-xs text-slate-100"
                    />
                  </td>
                  <td className="border border-slate-800 p-1">
                    <input
                      type="text"
                      value={fk.columns.join(', ')}
                      onChange={(e) =>
                        setForeignKeys((prev) =>
                          prev.map((it, k) =>
                            k === i
                              ? { ...it, columns: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) }
                              : it,
                          ),
                        )
                      }
                      className="w-full rounded bg-slate-900 px-2 py-1 font-mono text-xs text-slate-100"
                    />
                  </td>
                  <td className="border border-slate-800 p-1">
                    <input
                      type="text"
                      value={fk.refTable}
                      onChange={(e) =>
                        setForeignKeys((prev) =>
                          prev.map((it, k) => (k === i ? { ...it, refTable: e.target.value } : it)),
                        )
                      }
                      className="w-full rounded bg-slate-900 px-2 py-1 text-xs text-slate-100"
                    />
                  </td>
                  <td className="border border-slate-800 p-1">
                    <input
                      type="text"
                      value={fk.refColumns.join(', ')}
                      onChange={(e) =>
                        setForeignKeys((prev) =>
                          prev.map((it, k) =>
                            k === i
                              ? { ...it, refColumns: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) }
                              : it,
                          ),
                        )
                      }
                      className="w-full rounded bg-slate-900 px-2 py-1 font-mono text-xs text-slate-100"
                    />
                  </td>
                  <td className="border border-slate-800 p-1">
                    <select
                      value={fk.onDelete || 'NO ACTION'}
                      onChange={(e) =>
                        setForeignKeys((prev) =>
                          prev.map((it, k) => (k === i ? { ...it, onDelete: e.target.value } : it)),
                        )
                      }
                      className="w-full rounded bg-slate-900 px-2 py-1 text-xs text-slate-100"
                    >
                      <option value="NO ACTION">NO ACTION</option>
                      <option value="CASCADE">CASCADE</option>
                      <option value="SET NULL">SET NULL</option>
                      <option value="RESTRICT">RESTRICT</option>
                    </select>
                  </td>
                  <td className="border border-slate-800 p-1 text-center">
                    <button
                      type="button"
                      onClick={() => setForeignKeys((prev) => prev.filter((_, k) => k !== i))}
                      className="text-slate-400 hover:text-rose-400"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {subTab === 'sql' && (
          <div className="space-y-3">
            {preview?.warnings && preview.warnings.length > 0 && (
              <div className="rounded border border-amber-800 bg-amber-950/60 px-3 py-2 text-xs text-amber-200">
                {preview.warnings.join(' ')}
              </div>
            )}
            <pre className="overflow-x-auto rounded border border-slate-800 bg-slate-900 p-3 font-mono text-xs leading-relaxed text-sky-200">
              {preview?.sql || '-- 正在生成 SQL 预览...'}
            </pre>
            {preview && preview.statements.length > 0 && (
              <button
                type="button"
                disabled={executing}
                onClick={() => void handleSaveAndExecute()}
                className="flex items-center gap-1.5 rounded bg-emerald-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-emerald-500 disabled:opacity-50"
              >
                {executing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                立即执行此 SQL 变更
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
};
