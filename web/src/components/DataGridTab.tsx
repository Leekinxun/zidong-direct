import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ArrowDown,
  ArrowUp,
  Check,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Code2,
  Download,
  Edit3,
  Filter,
  Loader2,
  Minus,
  Plus,
  RefreshCw,
  Terminal,
  Trash2,
  Upload,
  X,
} from 'lucide-react';
import { api, ApiError } from '../api/client';
import type { EngineType, FilterCondition, QueryResult, RowMutation } from '../types/db';
import { ImportExportModal } from './ImportExportModal';

interface Props {
  connectionId: string;
  engine: EngineType;
  database: string;
  schema: string;
  table: string;
  onOpenDesign: () => void;
  onOpenSql: (initialSql: string) => void;
}

interface EditingCell {
  rowKey: string;
  colIndex: number;
  value: string;
  isNull: boolean;
}

interface SelectedCellCoord {
  rowIdx: number;
  colIdx: number;
}

export const DataGridTab: React.FC<Props> = ({
  connectionId,
  engine,
  database,
  schema,
  table,
  onOpenDesign,
  onOpenSql,
}) => {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(100);
  const [sortColumn, setSortColumn] = useState('');
  const [sortDir, setSortDir] = useState<'ASC' | 'DESC' | ''>('');
  const [filters, setFilters] = useState<FilterCondition[]>([]);
  const [appliedFilters, setAppliedFilters] = useState<FilterCondition[]>([]);
  const [filterBarOpen, setFilterBarOpen] = useState(false);
  const [showSqlBar, setShowSqlBar] = useState(false);

  const [result, setResult] = useState<QueryResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [statusMsg, setStatusMsg] = useState('');

  const [pendingUpdates, setPendingUpdates] = useState<Record<number, Record<string, unknown>>>({});
  const [pendingDeletes, setPendingDeletes] = useState<Record<number, boolean>>({});
  const [pendingInserts, setPendingInserts] = useState<Array<Record<string, unknown>>>([]);
  const [editingCell, setEditingCell] = useState<EditingCell | null>(null);
  const [selectedCell, setSelectedCell] = useState<SelectedCellCoord | null>(null);
  const [ioModal, setIoModal] = useState<'export' | 'import' | null>(null);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await api.queryTableData({
        connectionId,
        database,
        schema,
        table,
        page,
        pageSize,
        sortColumn,
        sortDir,
        filters: appliedFilters,
      });
      setResult(res);
      setPendingUpdates({});
      setPendingDeletes({});
      setPendingInserts([]);
      setEditingCell(null);
      setSelectedCell(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载表数据失败');
    } finally {
      setLoading(false);
    }
  }, [connectionId, database, schema, table, page, pageSize, sortColumn, sortDir, appliedFilters]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const pendingCount = useMemo(() => {
    return Object.keys(pendingUpdates).length + Object.keys(pendingDeletes).length + pendingInserts.length;
  }, [pendingUpdates, pendingDeletes, pendingInserts]);

  const handleSort = (colName: string) => {
    if (sortColumn !== colName) {
      setSortColumn(colName);
      setSortDir('ASC');
    } else if (sortDir === 'ASC') {
      setSortDir('DESC');
    } else {
      setSortColumn('');
      setSortDir('');
    }
    setPage(1);
  };

  const addFilter = () => {
    const firstCol = result?.columns[0]?.name || '';
    setFilterBarOpen(true);
    setFilters((prev) => [...prev, { column: firstCol, operator: '=', value: '' }]);
  };

  const handleApplyFilters = () => {
    setPage(1);
    setAppliedFilters(filters.filter((f) => f.column));
  };

  const handleClearFilters = () => {
    setFilters([]);
    setAppliedFilters([]);
    setPage(1);
  };

  const handleAddDraftRow = () => {
    if (!result) return;
    const blank: Record<string, unknown> = {};
    for (const col of result.columns) {
      blank[col.name] = null;
    }
    setPendingInserts((prev) => [...prev, blank]);
  };

  const handleToggleDeleteSelectedRow = () => {
    if (!selectedCell) return;
    const rowIdx = selectedCell.rowIdx;
    setPendingDeletes((prev) => {
      const next = { ...prev };
      if (next[rowIdx]) delete next[rowIdx];
      else next[rowIdx] = true;
      return next;
    });
  };

  const startEditExisting = (rowIdx: number, colIdx: number) => {
    if (!result) return;
    const colName = result.columns[colIdx].name;
    const updated = pendingUpdates[rowIdx]?.[colName];
    const current = updated !== undefined ? updated : result.rows[rowIdx][colIdx];
    setEditingCell({
      rowKey: `row:${rowIdx}`,
      colIndex: colIdx,
      value: current === null || current === undefined ? '' : String(current),
      isNull: current === null || current === undefined,
    });
  };

  const startEditInsert = (insIdx: number, colIdx: number) => {
    if (!result) return;
    const colName = result.columns[colIdx].name;
    const current = pendingInserts[insIdx][colName];
    setEditingCell({
      rowKey: `ins:${insIdx}`,
      colIndex: colIdx,
      value: current === null || current === undefined ? '' : String(current),
      isNull: current === null || current === undefined,
    });
  };

  const commitCellEdit = (forceNull?: boolean) => {
    if (!editingCell || !result) return;
    const colName = result.columns[editingCell.colIndex].name;
    const finalVal: unknown = (forceNull ?? editingCell.isNull) ? null : editingCell.value;

    if (editingCell.rowKey.startsWith('row:')) {
      const rowIdx = Number(editingCell.rowKey.slice(4));
      const origVal = result.rows[rowIdx][editingCell.colIndex];
      const normalizedOrig = origVal === undefined ? null : origVal;

      setPendingUpdates((prev) => {
        const next = { ...prev };
        const rowEdits = { ...(next[rowIdx] || {}) };
        if (String(finalVal) === String(normalizedOrig) && (finalVal === null) === (normalizedOrig === null)) {
          delete rowEdits[colName];
        } else {
          rowEdits[colName] = finalVal;
        }
        if (Object.keys(rowEdits).length === 0) {
          delete next[rowIdx];
        } else {
          next[rowIdx] = rowEdits;
        }
        return next;
      });
    } else if (editingCell.rowKey.startsWith('ins:')) {
      const insIdx = Number(editingCell.rowKey.slice(4));
      setPendingInserts((prev) =>
        prev.map((row, idx) => (idx === insIdx ? { ...row, [colName]: finalVal } : row)),
      );
    }
    setEditingCell(null);
  };

  const buildRowKeys = (row: unknown[]): Record<string, unknown> => {
    if (!result) return {};
    const keys: Record<string, unknown> = {};
    const pkCols = result.primaryKeys || [];
    if (pkCols.length > 0) {
      for (const pk of pkCols) {
        const idx = result.columns.findIndex((c) => c.name === pk);
        if (idx >= 0) {
          keys[pk] = row[idx];
        }
      }
      return keys;
    }
    result.columns.forEach((col, idx) => {
      const val = row[idx];
      if (typeof val === 'string' && val.startsWith('[HEX')) return;
      keys[col.name] = val;
    });
    return keys;
  };

  const handleCommitMutations = async () => {
    if (!result || pendingCount === 0) return;
    const mutations: RowMutation[] = [];

    for (const insRow of pendingInserts) {
      const cleanValues: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(insRow)) {
        if (v !== null && v !== undefined) {
          cleanValues[k] = v;
        }
      }
      if (Object.keys(cleanValues).length > 0) {
        mutations.push({ type: 'insert', values: cleanValues });
      }
    }

    for (const [rowIdxStr, values] of Object.entries(pendingUpdates)) {
      const rowIdx = Number(rowIdxStr);
      if (pendingDeletes[rowIdx]) continue;
      mutations.push({
        type: 'update',
        keys: buildRowKeys(result.rows[rowIdx]),
        values,
      });
    }

    for (const [rowIdxStr, marked] of Object.entries(pendingDeletes)) {
      if (!marked) continue;
      const rowIdx = Number(rowIdxStr);
      mutations.push({
        type: 'delete',
        keys: buildRowKeys(result.rows[rowIdx]),
      });
    }

    if (mutations.length === 0) {
      setPendingInserts([]);
      return;
    }

    setSaving(true);
    setError('');
    setStatusMsg('');
    try {
      const res = await api.mutateTableRows({
        connectionId,
        database,
        schema,
        table,
        mutations,
      });
      setStatusMsg(
        `已应用修改：影响 ${res.affectedRows} 行` + (res.warning ? `（${res.warning}）` : ''),
      );
      await loadData();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '提交修改失败');
    } finally {
      setSaving(false);
    }
  };

  const discardChanges = () => {
    setPendingUpdates({});
    setPendingDeletes({});
    setPendingInserts([]);
    setEditingCell(null);
  };

  const buildSelectSqlText = () => {
    const q = (ident: string) => {
      if (engine === 'mysql') return '`' + ident.replace(/`/g, '``') + '`';
      if (engine === 'sqlserver') return '[' + ident.replace(/]/g, ']]') + ']';
      return '"' + ident.replace(/"/g, '""') + '"';
    };
    const target =
      engine === 'postgres' || engine === 'sqlserver' || engine === 'oracle' || engine === 'dameng'
        ? schema
          ? `${q(schema)}.${q(table)}`
          : q(table)
        : q(table);
    if (result?.statement) return result.statement + ';';
    if (engine === 'sqlserver') return `SELECT TOP ${pageSize} * FROM ${target};`;
    if (engine === 'oracle') return `SELECT * FROM ${target} WHERE ROWNUM <= ${pageSize};`;
    return `SELECT * FROM ${target} LIMIT ${pageSize};`;
  };

  const totalCount = result?.totalCount ?? null;
  const totalPages = totalCount !== null ? Math.max(1, Math.ceil(totalCount / pageSize)) : null;
  const startRecord = result && result.rowCount > 0 ? (page - 1) * pageSize + 1 : 0;
  const endRecord = result ? (page - 1) * pageSize + result.rowCount : 0;

  return (
    <div className="flex h-full flex-col bg-slate-950 select-none">
      {/* Top Navicat-Style Table View Sub-Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/[0.08] bg-[#1e1e21] px-3 py-1.5">
        <div className="flex flex-wrap items-center gap-1.5">
          <button
            type="button"
            onClick={() => {
              if (!filterBarOpen && filters.length === 0 && result?.columns[0]) {
                setFilters([{ column: result.columns[0].name, operator: '=', value: '' }]);
              }
              setFilterBarOpen((v) => !v);
            }}
            className={filterBarOpen || appliedFilters.length > 0 ? 'apple-btn-primary' : 'apple-btn-secondary'}
          >
            <Filter className="h-3.5 w-3.5" />
            筛选与排序 {appliedFilters.length > 0 ? `(${appliedFilters.length})` : ''}
          </button>

          <button
            type="button"
            onClick={() => setShowSqlBar((v) => !v)}
            className={showSqlBar ? 'apple-btn-primary' : 'apple-btn-secondary'}
          >
            <Code2 className="h-3.5 w-3.5" />
            SQL 预览
          </button>

          {pendingCount > 0 && (
            <div className="flex items-center gap-1.5 pl-1">
              <button
                type="button"
                disabled={saving}
                onClick={() => void handleCommitMutations()}
                className="apple-btn-success"
              >
                {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
                应用修改 ({pendingCount})
              </button>
              <button
                type="button"
                onClick={discardChanges}
                className="apple-btn-secondary"
              >
                <X className="h-3.5 w-3.5 text-[#ff9f0a]" />
                放弃
              </button>
            </div>
          )}
        </div>

        {/* Right Side View Switchers: Import / Export / Open in Query / Switch to Design View */}
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={() => setIoModal('import')}
            className="apple-btn-secondary"
          >
            <Upload className="h-3.5 w-3.5 text-[#5e5ce6]" />
            导入
          </button>
          <button
            type="button"
            onClick={() => setIoModal('export')}
            className="apple-btn-secondary"
          >
            <Download className="h-3.5 w-3.5 text-[#30d158]" />
            导出
          </button>
          <button
            type="button"
            onClick={() => onOpenSql(buildSelectSqlText())}
            className="apple-btn-secondary"
            title="在 SQL 查询窗口中打开当前表查询"
          >
            <Terminal className="h-3.5 w-3.5 text-[#30d158]" />
            新建查询
          </button>
          <button
            type="button"
            onClick={onOpenDesign}
            className="apple-btn-secondary"
            title="切换到表结构设计器"
          >
            <Edit3 className="h-3.5 w-3.5 text-[#ff9f0a]" />
            设计表
          </button>
        </div>
      </div>

      {/* Live SQL Preview Bar */}
      {showSqlBar && (
        <div className="flex items-center justify-between border-b border-slate-800 bg-slate-950 px-3 py-1.5 font-mono text-xs text-sky-300 select-text">
          <span className="truncate">{buildSelectSqlText()}</span>
          <button
            type="button"
            onClick={() => onOpenSql(buildSelectSqlText())}
            className="ml-2 shrink-0 rounded bg-slate-800 px-2 py-0.5 text-[11px] text-slate-300 hover:bg-slate-700"
          >
            在查询编辑器打开
          </button>
        </div>
      )}

      {/* Navicat Filter Builder Bar */}
      {filterBarOpen && (
        <div className="space-y-1.5 border-b border-slate-800 bg-slate-900/60 px-3 py-2">
          <div className="flex items-center justify-between text-[11px] text-slate-400">
            <span>字段筛选条件（参数化安全绑定）</span>
            <button
              type="button"
              onClick={addFilter}
              className="flex items-center gap-1 text-sky-400 hover:text-sky-300"
            >
              <Plus className="h-3 w-3" /> 添加条件
            </button>
          </div>
          {filters.map((f, idx) => (
            <div key={idx} className="flex flex-wrap items-center gap-2">
              <select
                value={f.column}
                onChange={(e) =>
                  setFilters((prev) => prev.map((it, i) => (i === idx ? { ...it, column: e.target.value } : it)))
                }
                className="rounded border border-slate-700 bg-slate-800 px-2 py-1 text-xs text-slate-100"
              >
                {(result?.columns || []).map((c) => (
                  <option key={c.name} value={c.name}>
                    {c.name}
                  </option>
                ))}
              </select>
              <select
                value={f.operator}
                onChange={(e) =>
                  setFilters((prev) => prev.map((it, i) => (i === idx ? { ...it, operator: e.target.value } : it)))
                }
                className="rounded border border-slate-700 bg-slate-800 px-2 py-1 text-xs text-slate-100"
              >
                <option value="=">= (等于)</option>
                <option value="!=">!= (不等于)</option>
                <option value="LIKE">LIKE (包含匹配)</option>
                <option value=">">&gt; (大于)</option>
                <option value=">=">&gt;= (大于等于)</option>
                <option value="<">&lt; (小于)</option>
                <option value="<=">&lt;= (小于等于)</option>
                <option value="IS NULL">IS NULL (为空)</option>
                <option value="IS NOT NULL">IS NOT NULL (非空)</option>
              </select>
              {f.operator !== 'IS NULL' && f.operator !== 'IS NOT NULL' && (
                <input
                  type="text"
                  placeholder="输入筛选值，按回车应用..."
                  value={f.value}
                  onChange={(e) =>
                    setFilters((prev) => prev.map((it, i) => (i === idx ? { ...it, value: e.target.value } : it)))
                  }
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') handleApplyFilters();
                  }}
                  className="w-56 rounded border border-slate-700 bg-slate-800 px-2 py-1 text-xs text-slate-100 outline-none focus:border-indigo-500"
                />
              )}
              <button
                type="button"
                onClick={() => setFilters((prev) => prev.filter((_, i) => i !== idx))}
                className="text-xs text-slate-400 hover:text-rose-400"
              >
                移除
              </button>
            </div>
          ))}
          <div className="flex items-center gap-2 pt-1">
            <button
              type="button"
              onClick={handleApplyFilters}
              className="rounded bg-indigo-600 px-3 py-1 text-xs font-medium text-white hover:bg-indigo-500"
            >
              应用筛选
            </button>
            <button
              type="button"
              onClick={handleClearFilters}
              className="rounded border border-slate-700 px-3 py-1 text-xs text-slate-300 hover:bg-slate-800"
            >
              清除条件
            </button>
          </div>
        </div>
      )}

      {error && (
        <div className="border-b border-rose-800 bg-rose-950/70 px-3 py-1.5 text-xs text-rose-200">{error}</div>
      )}
      {statusMsg && (
        <div className="flex items-center justify-between border-b border-emerald-800 bg-emerald-950/60 px-3 py-1.5 text-xs text-emerald-200">
          <span>{statusMsg}</span>
          <button type="button" onClick={() => setStatusMsg('')} className="text-emerald-300 hover:text-white">
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}

      {/* Main Spreadsheet Data Grid */}
      <div className="flex-1 overflow-auto select-text">
        {loading && !result ? (
          <div className="flex h-full items-center justify-center text-sm text-slate-400">
            <Loader2 className="mr-2 h-4 w-4 animate-spin text-indigo-400" /> 正在查询表数据...
          </div>
        ) : result ? (
          <table className="w-full border-collapse text-left text-[13.5px]">
            <thead className="sticky top-0 z-10 bg-slate-900">
              <tr>
                <th className="w-14 border-b border-r border-slate-800 px-2.5 py-2.5 text-center font-mono text-xs text-slate-500">
                  #
                </th>
                {result.columns.map((col) => {
                  const isPK = (result.primaryKeys || []).includes(col.name);
                  return (
                    <th
                      key={col.name}
                      onClick={() => handleSort(col.name)}
                      className="cursor-pointer select-none whitespace-nowrap border-b border-r border-slate-800 px-3.5 py-2.5 font-medium text-slate-200 hover:bg-slate-800"
                      title="点击按此列排序"
                    >
                      <div className="flex items-center gap-1.5">
                        <span>{col.name}</span>
                        {isPK && (
                          <span className="rounded bg-amber-900/80 px-1.5 py-0.5 text-[10.5px] font-semibold text-amber-300">
                            PK
                          </span>
                        )}
                        {sortColumn === col.name &&
                          (sortDir === 'ASC' ? (
                            <ArrowUp className="h-3.5 w-3.5 text-indigo-400" />
                          ) : (
                            <ArrowDown className="h-3.5 w-3.5 text-indigo-400" />
                          ))}
                      </div>
                      {col.type && <div className="mt-0.5 text-[11.5px] font-normal text-slate-400">{col.type}</div>}
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody>
              {/* Pending inserts */}
              {pendingInserts.map((insRow, insIdx) => (
                <tr key={`ins-${insIdx}`} className="bg-emerald-950/35">
                  <td className="border-b border-r border-slate-800 px-2.5 py-2 text-center">
                    <button
                      type="button"
                      onClick={() => setPendingInserts((prev) => prev.filter((_, i) => i !== insIdx))}
                      className="text-rose-400 hover:text-rose-300"
                      title="移除此新增行"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </td>
                  {result.columns.map((col, colIdx) => {
                    const isEditing =
                      editingCell?.rowKey === `ins:${insIdx}` && editingCell.colIndex === colIdx;
                    const val = insRow[col.name];
                    return (
                      <td
                        key={col.name}
                        onClick={() => !isEditing && startEditInsert(insIdx, colIdx)}
                        className="min-w-[110px] max-w-xs cursor-pointer border-b border-r border-slate-800 px-3.5 py-2 font-mono"
                      >
                        {isEditing ? (
                          <div className="flex items-center gap-1">
                            <input
                              type="text"
                              autoFocus
                              value={editingCell.isNull ? '' : editingCell.value}
                              placeholder={editingCell.isNull ? '(NULL)' : ''}
                              onChange={(e) =>
                                setEditingCell((c) =>
                                  c ? { ...c, value: e.target.value, isNull: false } : c,
                                )
                              }
                              onKeyDown={(e) => {
                                if (e.key === 'Enter') commitCellEdit();
                                if (e.key === 'Escape') setEditingCell(null);
                              }}
                              onBlur={() => commitCellEdit()}
                              className="w-full rounded border border-indigo-500 bg-slate-900 px-2 py-1 text-[13.5px] text-slate-100 outline-none"
                            />
                            <button
                              type="button"
                              onMouseDown={(e) => {
                                e.preventDefault();
                                commitCellEdit(true);
                              }}
                              className="rounded bg-slate-700 px-1.5 py-1 text-xs text-slate-200"
                            >
                              NULL
                            </button>
                          </div>
                        ) : val === null || val === undefined ? (
                          <span className="italic text-slate-500">(NULL)</span>
                        ) : (
                          String(val)
                        )}
                      </td>
                    );
                  })}
                </tr>
              ))}

              {/* Existing rows */}
              {result.rows.map((row, rowIdx) => {
                const isDeleted = !!pendingDeletes[rowIdx];
                const isRowSelected = selectedCell?.rowIdx === rowIdx;
                return (
                  <tr
                    key={rowIdx}
                    className={
                      isDeleted
                        ? 'bg-rose-950/50 line-through opacity-65'
                        : isRowSelected
                        ? 'bg-indigo-950/40'
                        : rowIdx % 2 === 1
                        ? 'bg-slate-900/25 hover:bg-slate-900/70'
                        : 'hover:bg-slate-900/70'
                    }
                  >
                    <td
                      onClick={() => setSelectedCell({ rowIdx, colIdx: 0 })}
                      className="cursor-pointer border-b border-r border-slate-800/80 px-2.5 py-2 text-center font-mono text-xs text-slate-500"
                    >
                      {(page - 1) * pageSize + rowIdx + 1}
                    </td>

                    {row.map((cell, colIdx) => {
                      const colName = result.columns[colIdx].name;
                      const editedVal = pendingUpdates[rowIdx]?.[colName];
                      const hasEdit =
                        pendingUpdates[rowIdx] !== undefined &&
                        Object.prototype.hasOwnProperty.call(pendingUpdates[rowIdx], colName);
                      const displayVal = hasEdit ? editedVal : cell;
                      const isEditing =
                        editingCell?.rowKey === `row:${rowIdx}` && editingCell.colIndex === colIdx;
                      const isCellSelected =
                        selectedCell?.rowIdx === rowIdx && selectedCell?.colIdx === colIdx;

                      return (
                        <td
                          key={colName}
                          onClick={() => setSelectedCell({ rowIdx, colIdx })}
                          onDoubleClick={() => !isDeleted && startEditExisting(rowIdx, colIdx)}
                          className={`max-w-xs truncate border-b border-r border-slate-800/80 px-3.5 py-2 font-mono ${
                            hasEdit
                              ? 'bg-amber-950/60 text-amber-200'
                              : isCellSelected
                              ? 'outline outline-1 outline-indigo-500/80 bg-indigo-950/60 text-white'
                              : 'text-slate-200'
                          }`}
                          title={displayVal === null ? '(NULL) - 双击编辑' : `${String(displayVal)} (双击编辑)`}
                        >
                          {isEditing ? (
                            <div className="flex items-center gap-1">
                              <input
                                type="text"
                                autoFocus
                                value={editingCell.isNull ? '' : editingCell.value}
                                placeholder={editingCell.isNull ? '(NULL)' : ''}
                                onChange={(e) =>
                                  setEditingCell((c) =>
                                    c ? { ...c, value: e.target.value, isNull: false } : c,
                                  )
                                }
                                onKeyDown={(e) => {
                                  if (e.key === 'Enter') commitCellEdit();
                                  if (e.key === 'Escape') setEditingCell(null);
                                }}
                                onBlur={() => commitCellEdit()}
                                className="w-full rounded border border-indigo-500 bg-slate-900 px-2 py-1 text-[13.5px] text-slate-100 outline-none"
                              />
                              <button
                                type="button"
                                onMouseDown={(e) => {
                                  e.preventDefault();
                                  commitCellEdit(true);
                                }}
                                className="rounded bg-slate-700 px-1.5 py-1 text-xs text-slate-200"
                              >
                                NULL
                              </button>
                            </div>
                          ) : displayVal === null || displayVal === undefined ? (
                            <span className="italic text-slate-500">(NULL)</span>
                          ) : (
                            String(displayVal)
                          )}
                        </td>
                      );
                    })}
                  </tr>
                );
              })}
            </tbody>
          </table>
        ) : null}
      </div>

      {/* Navicat Bottom Record Action & Navigation Bar (+ / - / ✓ / ✕ / ↻ | |<< < Page > >>|) */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-slate-800 bg-slate-900 px-3.5 py-2 text-[13px] text-slate-300">
        {/* Left: Navicat Classic Record Buttons */}
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={handleAddDraftRow}
            className="flex h-7 w-7 items-center justify-center rounded border border-slate-700 bg-slate-800 text-emerald-400 hover:bg-slate-700"
            title="添加记录 (+)"
          >
            <Plus className="h-4 w-4" />
          </button>
          <button
            type="button"
            disabled={!selectedCell}
            onClick={handleToggleDeleteSelectedRow}
            className="flex h-7 w-7 items-center justify-center rounded border border-slate-700 bg-slate-800 text-rose-400 hover:bg-slate-700 disabled:opacity-40"
            title="删除当前选中行 (-)"
          >
            <Minus className="h-4 w-4" />
          </button>
          <button
            type="button"
            disabled={pendingCount === 0 || saving}
            onClick={() => void handleCommitMutations()}
            className={`flex h-7 items-center gap-1 rounded border px-2.5 ${
              pendingCount > 0
                ? 'border-emerald-600 bg-emerald-600 text-white hover:bg-emerald-500'
                : 'border-slate-700 bg-slate-800 text-slate-400 opacity-40'
            }`}
            title="应用修改 (✓)"
          >
            <Check className="h-4 w-4" />
            {pendingCount > 0 && <span>({pendingCount})</span>}
          </button>
          <button
            type="button"
            disabled={pendingCount === 0}
            onClick={discardChanges}
            className="flex h-7 w-7 items-center justify-center rounded border border-slate-700 bg-slate-800 text-amber-400 hover:bg-slate-700 disabled:opacity-40"
            title="放弃修改 (✕)"
          >
            <X className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={() => void loadData()}
            className="flex h-7 w-7 items-center justify-center rounded border border-slate-700 bg-slate-800 text-slate-200 hover:bg-slate-700"
            title="刷新数据 (↻)"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </button>

          <div className="mx-1.5 h-4 w-px bg-slate-800" />

          {/* Page Navigation Controls */}
          <button
            type="button"
            disabled={page <= 1 || loading}
            onClick={() => setPage(1)}
            className="flex h-7 w-7 items-center justify-center rounded border border-slate-700 bg-slate-800 text-slate-200 hover:bg-slate-700 disabled:opacity-40"
            title="第一页"
          >
            <ChevronsLeft className="h-4 w-4" />
          </button>
          <button
            type="button"
            disabled={page <= 1 || loading}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            className="flex h-7 w-7 items-center justify-center rounded border border-slate-700 bg-slate-800 text-slate-200 hover:bg-slate-700 disabled:opacity-40"
            title="上一页"
          >
            <ChevronLeft className="h-4 w-4" />
          </button>

          <span className="px-2 text-[13px] text-slate-300">
            第 <strong className="font-mono text-white">{page}</strong>
            {totalPages ? ` / ${totalPages}` : ''} 页
          </span>

          <button
            type="button"
            disabled={
              (totalPages !== null && page >= totalPages) ||
              (result ? result.rowCount < pageSize : false) ||
              loading
            }
            onClick={() => setPage((p) => p + 1)}
            className="flex h-7 w-7 items-center justify-center rounded border border-slate-700 bg-slate-800 text-slate-200 hover:bg-slate-700 disabled:opacity-40"
            title="下一页"
          >
            <ChevronRight className="h-4 w-4" />
          </button>
          <button
            type="button"
            disabled={totalPages === null || page >= totalPages || loading}
            onClick={() => totalPages && setPage(totalPages)}
            className="flex h-7 w-7 items-center justify-center rounded border border-slate-700 bg-slate-800 text-slate-200 hover:bg-slate-700 disabled:opacity-40"
            title="最后一页"
          >
            <ChevronsRight className="h-4 w-4" />
          </button>

          <select
            value={pageSize}
            onChange={(e) => {
              setPageSize(Number(e.target.value));
              setPage(1);
            }}
            className="ml-1 h-7 rounded border border-slate-700 bg-slate-800 px-2 text-[12.5px] text-slate-200"
            title="每页限制记录数"
          >
            <option value={50}>每页 50 条</option>
            <option value={100}>每页 100 条</option>
            <option value={200}>每页 200 条</option>
            <option value={500}>每页 500 条</option>
          </select>
        </div>

        {/* Right: Record Counter & Timing */}
        <div className="text-[13px] text-slate-400">
          {result ? (
            <>
              显示第 <span className="font-mono text-slate-200">{startRecord}</span> -{' '}
              <span className="font-mono text-slate-200">{endRecord}</span> 条
              {totalCount !== null ? (
                <>
                  {' '}
                  （共 <span className="font-mono text-slate-200">{totalCount.toLocaleString()}</span> 条记录）
                </>
              ) : null}
              {' · '}耗时 <span className="font-mono text-slate-200">{result.durationMs} ms</span>
            </>
          ) : (
            <span>双击任意单元格可直接编辑</span>
          )}
        </div>
      </div>

      {ioModal && (
        <ImportExportModal
          mode={ioModal}
          connectionId={connectionId}
          database={database}
          schema={schema}
          table={table}
          filters={appliedFilters}
          sortColumn={sortColumn}
          sortDir={sortDir}
          onClose={() => setIoModal(null)}
          onImported={() => void loadData()}
        />
      )}
    </div>
  );
};
