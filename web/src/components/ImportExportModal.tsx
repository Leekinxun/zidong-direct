import React, { useState } from 'react';
import { Download, Loader2, Upload, X } from 'lucide-react';
import { api, ApiError } from '../api/client';
import type { FilterCondition } from '../types/db';

interface Props {
  mode: 'export' | 'import';
  connectionId: string;
  database: string;
  schema: string;
  table: string;
  sql?: string;
  filters?: FilterCondition[];
  sortColumn?: string;
  sortDir?: string;
  onClose: () => void;
  onImported?: () => void;
}

export const ImportExportModal: React.FC<Props> = ({
  mode,
  connectionId,
  database,
  schema,
  table,
  sql,
  filters,
  sortColumn,
  sortDir,
  onClose,
  onImported,
}) => {
  const [format, setFormat] = useState<'csv' | 'json' | 'sql'>('csv');
  const [withBom, setWithBom] = useState(true);
  const [maxRows, setMaxRows] = useState(50000);
  const [file, setFile] = useState<File | null>(null);
  const [emptyAsNull, setEmptyAsNull] = useState(true);
  const [continueOnError, setContinueOnError] = useState(false);
  const [running, setRunning] = useState(false);
  const [resultMsg, setResultMsg] = useState('');
  const [error, setError] = useState('');

  const handleExport = async () => {
    setRunning(true);
    setError('');
    try {
      await api.exportData({
        connectionId,
        database,
        schema,
        table: table || undefined,
        sql: sql || undefined,
        format,
        withBom,
        filters,
        sortColumn,
        sortDir,
        maxRows,
      });
      setResultMsg('导出已开始下载。');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '导出失败');
    } finally {
      setRunning(false);
    }
  };

  const handleImport = async () => {
    if (!file) {
      setError('请先选择要导入的文件');
      return;
    }
    setRunning(true);
    setError('');
    setResultMsg('');
    try {
      const res = await api.importData({
        connectionId,
        database,
        schema,
        table: format === 'sql' ? undefined : table,
        format,
        file,
        emptyAsNull,
        continueOnError,
      });
      const extra = res.failedRows ? `，失败 ${res.failedRows} 行` : '';
      const errList = res.errors && res.errors.length > 0 ? `\n示例错误：${res.errors.join(' | ')}` : '';
      setResultMsg(`导入完成：成功写入 ${res.insertedRows} 行${extra}${errList}`);
      onImported?.();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '导入失败');
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4">
      <div className="w-full max-w-md rounded-lg border border-slate-700 bg-slate-900 shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-800 px-4 py-3">
          <h3 className="text-sm font-semibold text-slate-100">
            {mode === 'export' ? `导出数据 (${table || '查询结果'})` : `导入数据 (${table || 'SQL 脚本'})`}
          </h3>
          <button type="button" onClick={onClose} className="text-slate-400 hover:text-slate-200">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="space-y-4 p-4">
          <div>
            <label className="mb-1 block text-xs text-slate-300">文件格式</label>
            <select
              value={format}
              onChange={(e) => setFormat(e.target.value as 'csv' | 'json' | 'sql')}
              className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
            >
              <option value="csv">CSV 表格 (.csv)</option>
              <option value="json">JSON 数组 (.json)</option>
              <option value="sql">{mode === 'export' ? 'SQL INSERT 脚本 (.sql)' : 'SQL 事务脚本 (.sql)'}</option>
            </select>
          </div>

          {mode === 'export' ? (
            <>
              {format === 'csv' && (
                <label className="flex items-center gap-2 text-xs text-slate-300">
                  <input
                    type="checkbox"
                    checked={withBom}
                    onChange={(e) => setWithBom(e.target.checked)}
                    className="rounded border-slate-600 bg-slate-800"
                  />
                  写入 UTF-8 BOM 头（推荐：解决 Win7 Excel 打开中文乱码）
                </label>
              )}
              <div>
                <label className="mb-1 block text-xs text-slate-300">最大导出行数</label>
                <input
                  type="number"
                  value={maxRows}
                  onChange={(e) => setMaxRows(Number(e.target.value) || 50000)}
                  className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                />
              </div>
            </>
          ) : (
            <>
              <div>
                <label className="mb-1 block text-xs text-slate-300">选择本地文件</label>
                <input
                  type="file"
                  accept=".csv,.json,.sql,.txt"
                  onChange={(e) => setFile(e.target.files?.[0] || null)}
                  className="w-full rounded border border-slate-700 bg-slate-800 px-2 py-1.5 text-xs text-slate-200"
                />
              </div>
              {format === 'csv' && (
                <label className="flex items-center gap-2 text-xs text-slate-300">
                  <input
                    type="checkbox"
                    checked={emptyAsNull}
                    onChange={(e) => setEmptyAsNull(e.target.checked)}
                    className="rounded border-slate-600 bg-slate-800"
                  />
                  空单元格自动转成 NULL
                </label>
              )}
              {format !== 'sql' && (
                <label className="flex items-center gap-2 text-xs text-slate-300">
                  <input
                    type="checkbox"
                    checked={continueOnError}
                    onChange={(e) => setContinueOnError(e.target.checked)}
                    className="rounded border-slate-600 bg-slate-800"
                  />
                  单行失败时继续导入其余行
                </label>
              )}
            </>
          )}

          {resultMsg && (
            <div className="whitespace-pre-wrap rounded border border-emerald-800 bg-emerald-950/70 px-3 py-2 text-xs text-emerald-200">
              {resultMsg}
            </div>
          )}
          {error && (
            <div className="rounded border border-rose-800 bg-rose-950/70 px-3 py-2 text-xs text-rose-200">
              {error}
            </div>
          )}

          <div className="flex justify-end gap-2 border-t border-slate-800 pt-3">
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-slate-700 px-3 py-1.5 text-xs text-slate-300 hover:bg-slate-800"
            >
              关闭
            </button>
            <button
              type="button"
              disabled={running}
              onClick={mode === 'export' ? handleExport : handleImport}
              className="flex items-center gap-1.5 rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              {running ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : mode === 'export' ? (
                <Download className="h-3.5 w-3.5" />
              ) : (
                <Upload className="h-3.5 w-3.5" />
              )}
              {mode === 'export' ? '开始导出' : '开始导入'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
