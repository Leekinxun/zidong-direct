import { useState } from 'react';
import { Download, Loader2, X } from 'lucide-react';
import { api, ApiError, type ExportFormat } from '../api/client';
import type { FilterCondition } from '../types/db';
import { DataImportModal } from './DataImportModal';

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

export function ImportExportModal({ mode, ...props }: Props) {
  return mode === 'import' ? <DataImportModal {...props} /> : <DataExportModal {...props} />;
}

function DataExportModal({
  connectionId, database, schema, table, sql, filters, sortColumn, sortDir, onClose,
}: Omit<Props, 'mode'>) {
  const [format, setFormat] = useState<ExportFormat>('csv');
  const [fixedWidth, setFixedWidth] = useState(false);
  const [delimiter, setDelimiter] = useState('\t');
  const [widths, setWidths] = useState('');
  const [withBom, setWithBom] = useState(true);
  const [maxRows, setMaxRows] = useState(50000);
  const [running, setRunning] = useState(false);
  const [resultMsg, setResultMsg] = useState('');
  const [error, setError] = useState('');
  const delimited = format === 'csv' || format === 'txt' || format === 'tsv';

  const handleExport = async () => {
    setResultMsg('');
    if (format === 'txt' && fixedWidth && !widths.trim()) {
      setError('请输入各列的固定宽度，例如 8,20,16。');
      return;
    }
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
        withBom: delimited && withBom,
        delimiter: format === 'txt' && !fixedWidth ? delimiter : undefined,
        widths: format === 'txt' && fixedWidth ? widths.trim() : undefined,
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

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4">
      <div className="w-full max-w-md rounded-lg border border-slate-700 bg-slate-900 shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-800 px-4 py-3">
          <h3 className="text-sm font-semibold text-slate-100">导出数据 ({table || '查询结果'})</h3>
          <button type="button" onClick={onClose} className="text-slate-400 hover:text-slate-200" aria-label="关闭">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="space-y-4 p-4">
          <label className="block space-y-1 text-xs text-slate-300">
            <span>文件格式</span>
            <select
              value={format}
              onChange={(e) => {
                setFormat(e.target.value as ExportFormat);
                setError('');
                setResultMsg('');
              }}
              className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
            >
              <optgroup label="文本与文档">
                <option value="csv">CSV 表格 (.csv)</option>
                <option value="txt">分隔或定宽文本 (.txt)</option>
                <option value="tsv">制表符分隔 (.tsv)</option>
                <option value="json">JSON 数组 (.json)</option>
                <option value="jsonl">JSON Lines (.jsonl)</option>
                <option value="xml">XML (.xml)</option>
                <option value="html">HTML 表格 (.html)</option>
                <option value="sql">SQL INSERT 脚本 (.sql)</option>
              </optgroup>
              <optgroup label="电子表格">
                <option value="xlsx">Excel (.xlsx)</option>
                <option value="xls">Excel 97–2003 (.xls)</option>
                <option value="ods">OpenDocument (.ods)</option>
              </optgroup>
              <optgroup label="数据文件">
                <option value="dbf">dBASE (.dbf)</option>
                <option value="parquet">Parquet (.parquet)</option>
              </optgroup>
            </select>
          </label>
          {format === 'txt' && (
            <div className="space-y-2">
              <label className="flex items-center gap-2 text-xs text-slate-300">
                <input type="checkbox" checked={fixedWidth} onChange={(e) => setFixedWidth(e.target.checked)} />
                定宽文本
              </label>
              <label className="block space-y-1 text-xs text-slate-300">
                <span>{fixedWidth ? '各列宽度（按字符数）' : '分隔符'}</span>
                {fixedWidth ? (
                  <input
                    value={widths}
                    onChange={(e) => setWidths(e.target.value)}
                    placeholder="例如 8,20,16"
                    className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                  />
                ) : (
                  <select
                    value={delimiter}
                    onChange={(e) => setDelimiter(e.target.value)}
                    className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
                  >
                    <option value={'\t'}>制表符</option>
                    <option value=",">逗号</option>
                    <option value=";">分号</option>
                    <option value="|">竖线</option>
                  </select>
                )}
              </label>
            </div>
          )}
          {delimited && (
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
          {format === 'xls' && <p className="text-xs text-slate-400">XLS 最多支持 256 列和 65,535 行数据。更大的表请选 XLSX。</p>}
          {format === 'dbf' && <p className="text-xs text-slate-400">DBF 使用 GBK 编码，列名最长 10 个字符。</p>}
          <label className="block space-y-1 text-xs text-slate-300">
            <span>最大导出行数</span>
            <input
              type="number"
              min={1}
              value={maxRows}
              onChange={(e) => setMaxRows(Number(e.target.value) || 50000)}
              className="w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500"
            />
          </label>
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
            <button type="button" onClick={onClose} className="rounded border border-slate-700 px-3 py-1.5 text-xs text-slate-300 hover:bg-slate-800">
              关闭
            </button>
            <button
              type="button"
              disabled={running}
              onClick={handleExport}
              className="flex items-center gap-1.5 rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
              开始导出
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
