import { useEffect, useState } from 'react';
import { Eye, Loader2, Upload, X } from 'lucide-react';
import {
  api, type ImportFormat, type ImportPreview, type ImportSourceParams, type ImportWriteMode,
} from '../api/client';
import { isDesktop } from '../preferences';

interface Props {
  connectionId: string;
  database: string;
  schema: string;
  table: string;
  onClose: () => void;
  onImported?: () => void;
}

const formats: { key: ImportFormat; label: string }[] = [
  { key: 'csv', label: 'CSV (.csv)' },
  { key: 'txt', label: '文本 (.txt, .dat)' },
  { key: 'tsv', label: '制表符分隔 (.tsv)' },
  { key: 'json', label: 'JSON (.json)' },
  { key: 'jsonl', label: 'JSON Lines (.jsonl, .ndjson)' },
  { key: 'xml', label: 'XML (.xml)' },
  { key: 'xlsx', label: 'Excel (.xlsx, .xlsm)' },
  { key: 'xls', label: 'Excel 97–2003 (.xls)' },
  { key: 'ods', label: 'OpenDocument (.ods)' },
  { key: 'dbf', label: 'dBASE (.dbf)' },
  { key: 'parquet', label: 'Parquet (.parquet)' },
  { key: 'mdb', label: 'Access (.mdb)' },
  { key: 'accdb', label: 'Access (.accdb)' },
  { key: 'sql', label: 'SQL 脚本 (.sql)' },
];

const modeOptions: { key: ImportWriteMode; label: string; matchKey?: boolean }[] = [
  { key: 'append', label: '追加新记录' },
  { key: 'update', label: '更新匹配记录', matchKey: true },
  { key: 'upsert', label: '匹配则更新，否则追加', matchKey: true },
  { key: 'skip', label: '跳过匹配记录，只追加新记录', matchKey: true },
  { key: 'delete', label: '删除匹配记录', matchKey: true },
  { key: 'replace', label: '清空目标表并导入' },
];

const field = 'min-w-0 w-full rounded border border-slate-700 bg-slate-800 px-2.5 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500';
const button = 'rounded border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-50';

function formatForName(name: string): ImportFormat | undefined {
  const ext = name.split('.').pop()?.toLowerCase();
  if (ext === 'xlsm') return 'xlsx';
  if (ext === 'ndjson') return 'jsonl';
  if (ext === 'dat') return 'txt';
  return formats.find(({ key }) => key === ext)?.key;
}

function mapByName(data: ImportPreview): number[] {
  return data.targetColumns.map(({ name, generated }) => {
    if (generated) return -1;
    const matches = (predicate: (column: string) => boolean) => data.columns.flatMap((column, index) =>
      predicate(column) ? [index] : [],
    );
    const exact = matches((column) => column === name);
    if (exact.length) return exact.length === 1 ? exact[0] : -1;
    const candidates = matches((column) => column.toLowerCase() === name.toLowerCase());
    return candidates.length === 1 ? candidates[0] : -1;
  });
}

function mapByPosition(data: ImportPreview): number[] {
  let index = 0;
  return data.targetColumns.map(({ generated }) => generated || index >= data.columns.length ? -1 : index++);
}

export function DataImportModal({ connectionId, database, schema, table, onClose, onImported }: Props) {
  const [format, setFormat] = useState<ImportFormat>(table ? 'csv' : 'sql');
  const [target, setTarget] = useState(table);
  const [availableTables, setAvailableTables] = useState<string[]>([]);
  const [allowURL, setAllowURL] = useState(isDesktop);
  const [source, setSource] = useState<'file' | 'url' | 'odbc'>('file');
  const [file, setFile] = useState<File>();
  const [sourceUrl, setSourceUrl] = useState('');
  const [sourceDsn, setSourceDsn] = useState('');
  const [sourceTable, setSourceTable] = useState('');
  const [sourceUser, setSourceUser] = useState('');
  const [sourcePassword, setSourcePassword] = useState('');
  const [encoding, setEncoding] = useState('auto');
  const [delimiter, setDelimiter] = useState('');
  const [widths, setWidths] = useState('');
  const [fixedWidth, setFixedWidth] = useState(false);
  const [skipRows, setSkipRows] = useState(0);
  const [noHeader, setNoHeader] = useState(false);
  const [xmlRow, setXMLRow] = useState('row');
  const [sheet, setSheet] = useState('');
  const [sheets, setSheets] = useState<string[]>([]);
  const [writeMode, setWriteMode] = useState<ImportWriteMode>('append');
  const [emptyAsNull, setEmptyAsNull] = useState(true);
  const [continueOnError, setContinueOnError] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const [preview, setPreview] = useState<ImportPreview>();
  const [mapping, setMapping] = useState<number[]>([]);
  const [working, setWorking] = useState<'preview' | 'import'>();
  const [error, setError] = useState('');
  const [result, setResult] = useState('');

  const windowsDesktop = isDesktop && /Windows/.test(navigator.userAgent);
  const script = format === 'sql';
  const access = format === 'mdb' || format === 'accdb';
  const nativeSource = source === 'odbc' || access;
  const delimited = ['csv', 'txt', 'tsv'].includes(format);
  const workbook = format === 'xlsx' || format === 'xls' || format === 'ods';
  const rowsConfigurable = workbook || delimited;
  const usesPrimaryKey = modeOptions.find(({ key }) => key === writeMode)?.matchKey;
  const primaryColumns = preview?.targetColumns.map((column, i) => column.primaryKey ? i : -1).filter((i) => i >= 0) ?? [];
  const keyReady = Boolean(primaryColumns.length) && primaryColumns.every((i) => mapping[i] >= 0);
  const destructive = !script && (writeMode === 'replace' || writeMode === 'delete');

  useEffect(() => {
    if (isDesktop) return;
    let active = true;
    void api.authStatus().then(({ authEnabled }) => {
      if (active) setAllowURL(authEnabled);
    }).catch(() => {});
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (script) return;
    let active = true;
    api.listObjects(connectionId, database, schema).then(({ objects }) => {
      if (active) setAvailableTables(objects.filter(({ type }) => type === 'TABLE').map(({ name }) => name));
    }).catch(() => {
      if (active) setAvailableTables([]);
    });
    return () => { active = false; };
  }, [script, connectionId, database, schema]);

  function clearPreview(resetSheets = false) {
    setPreview(undefined);
    setMapping([]);
    setConfirmed(false);
    setError('');
    setResult('');
    if (resetSheets) {
      setSheets([]);
      setSheet('');
    }
  }

  function changeFormat(next: ImportFormat) {
    setFormat(next);
    setDelimiter('');
    setFixedWidth(false);
    clearPreview(true);
  }

  function selectFile(next?: File) {
    setFile(next);
    const detected = next && formatForName(next.name);
    if (detected && (windowsDesktop || (detected !== 'mdb' && detected !== 'accdb'))) changeFormat(detected);
    else clearPreview(true);
  }

  function editURL(value: string) {
    setSourceUrl(value);
    try {
      const detected = formatForName(new URL(value).pathname);
      if (detected && detected !== 'sql' && detected !== 'mdb' && detected !== 'accdb') {
        changeFormat(detected);
        return;
      }
    } catch {
      // The address can be incomplete while being edited.
    }
    clearPreview(true);
  }

  function params(): ImportSourceParams {
    if (source === 'file' && !file) throw new Error('请选择要导入的文件');
    if (source === 'url' && !sourceUrl.trim()) throw new Error('请输入数据文件的 URL');
    if (script && source === 'url') throw new Error('请先下载并核对 SQL 脚本，然后选择本地文件');
    if (nativeSource && !sourceTable.trim()) throw new Error('请输入要导入的源表名');
    if (source === 'odbc' && !sourceDsn.trim()) throw new Error('请输入 Windows ODBC 数据源名称');
    if (!script && !target.trim()) throw new Error('请选择要导入的目标表');
    if (delimited && fixedWidth && !widths.trim()) throw new Error('请输入固定列宽，如 8,20,16');
    return {
      connectionId, database, schema,
      table: script ? undefined : target.trim(),
      format,
      file: source === 'file' ? file : undefined,
      sourceUrl: source === 'url' ? sourceUrl.trim() : undefined,
      sourceDsn: source === 'odbc' ? sourceDsn.trim() : undefined,
      sourceUser: source === 'odbc' ? sourceUser : undefined,
      sourceTable: nativeSource ? sourceTable.trim() : undefined,
      sourcePassword: nativeSource ? sourcePassword : undefined,
      encoding: workbook || nativeSource || format === 'parquet' ? undefined : encoding,
      delimiter: delimited && !fixedWidth ? delimiter : undefined,
      widths: delimited && fixedWidth ? widths : undefined,
      skipRows: rowsConfigurable ? skipRows : undefined,
      noHeader: rowsConfigurable ? noHeader : undefined,
      xmlRow: format === 'xml' ? xmlRow : undefined,
      sheet: workbook ? sheet : undefined,
    };
  }

  async function loadPreview() {
    setWorking('preview');
    clearPreview();
    try {
      const next = await api.previewImportData(params());
      setPreview(next);
      setSheets(next.sheets ?? []);
      setSheet(next.sheet ?? '');
      setMapping(noHeader && rowsConfigurable ? mapByPosition(next) : mapByName(next));
      setConfirmed(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : '预览失败');
    } finally {
      setWorking(undefined);
    }
  }

  async function submit() {
    setWorking('import');
    setError('');
    setResult('');
    try {
      const mappings = preview?.targetColumns.flatMap((column, i) =>
        mapping[i] >= 0 ? [{ source: mapping[i], target: column.name }] : [],
      );
      const stats = await api.importData({
        ...params(),
        mode: script ? undefined : writeMode,
        emptyAsNull,
        continueOnError: !script && writeMode !== 'replace' && continueOnError,
        mappings,
        confirmed,
        sourceDigest: preview?.sourceDigest,
      });
      const totals = [
        stats.statements && `语句 ${stats.statements}`,
        `插入 ${stats.insertedRows} 行`,
        stats.updatedRows && `更新 ${stats.updatedRows} 行`,
        stats.deletedRows && `删除 ${stats.deletedRows} 行`,
        stats.skippedRows && `跳过 ${stats.skippedRows} 行`,
        stats.failedRows && `失败 ${stats.failedRows} 行`,
      ].filter(Boolean).join(' · ');
      setResult([totals, ...(stats.errors ?? [])].join('\n'));
      setConfirmed(false);
      onImported?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : '导入失败');
    } finally {
      setWorking(undefined);
    }
  }

  const ready = script ? source === 'file' && Boolean(file) :
    Boolean(preview?.rows.length && mapping.some((index) => index >= 0)) && (!usesPrimaryKey || keyReady) && (!destructive || confirmed);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4">
      <div role="dialog" aria-modal="true" aria-label="导入数据" className="flex max-h-[90vh] w-full max-w-4xl flex-col rounded-lg border border-slate-700 bg-slate-900 shadow-2xl">
        <header className="flex items-center justify-between border-b border-slate-800 px-4 py-3">
          <h3 className="text-sm font-semibold text-slate-100">导入数据</h3>
          <button aria-label="关闭" type="button" onClick={onClose} disabled={Boolean(working)} className="text-slate-400 hover:text-slate-200 disabled:opacity-50">
            <X className="h-4 w-4" />
          </button>
        </header>
        <div className="min-h-0 overflow-y-auto p-4">
          <fieldset disabled={Boolean(working)} className="space-y-4 text-xs text-slate-300">
            <div className="grid gap-3 sm:grid-cols-3">
              <label className="space-y-1">
                <span>数据来源</span>
                <select className={field} value={source} onChange={(e) => {
                  const kind = e.target.value as 'file' | 'url' | 'odbc';
                  setSource(kind);
                  setFile(undefined);
                  setSourcePassword('');
                  if (kind === 'odbc') changeFormat('odbc');
                  else if (source === 'odbc' || kind === 'url' && (script || access)) changeFormat('csv');
                  else clearPreview(true);
                }}>
                  <option value="file">本地文件</option>
                  {allowURL && <option value="url">公网 URL</option>}
                  {windowsDesktop && <option value="odbc">Windows ODBC 数据源</option>}
                </select>
              </label>
              <label className="space-y-1">
                <span>文件格式</span>
                <select className={field} value={format} onChange={(e) => changeFormat(e.target.value as ImportFormat)}>
                  {source === 'odbc' ? <option value="odbc">ODBC 源表</option> : formats.filter(({ key }) => {
                    if (key === 'mdb' || key === 'accdb') return windowsDesktop && source === 'file';
                    return source !== 'url' || key !== 'sql';
                  }).map(({ key, label }) => <option key={key} value={key}>{label}</option>)}
                </select>
              </label>
              {!script && (
                <label className="space-y-1">
                  <span>目标表</span>
                  <input className={field} list="data-import-targets" value={target} onChange={(e) => {
                    setTarget(e.target.value);
                    clearPreview();
                  }} />
                  <datalist id="data-import-targets">
                    {availableTables.map((name) => <option key={name} value={name} />)}
                  </datalist>
                </label>
              )}
            </div>
            {source === 'file' ? (
              <label className="block space-y-1">
                <span>选择本地文件</span>
                <input type="file" className={field} accept={'.csv,.txt,.dat,.tsv,.json,.jsonl,.ndjson,.xml,.xlsx,.xlsm,.xls,.ods,.dbf,.parquet,.sql' + (windowsDesktop ? ',.mdb,.accdb' : '')} onChange={(e) => selectFile(e.target.files?.[0])} />
              </label>
            ) : source === 'url' && (
              <label className="block space-y-1">
                <span>公网 HTTP(S) 文件地址</span>
                <input type="url" className={field} placeholder="https://example.com/data.csv" value={sourceUrl} onChange={(e) => editURL(e.target.value)} />
              </label>
            )}
            {nativeSource && (
              <div className="grid gap-3 sm:grid-cols-2">
                {source === 'odbc' && (
                  <label className="space-y-1">
                    <span>ODBC 数据源名称（DSN）</span>
                    <input className={field} value={sourceDsn} onChange={(e) => { setSourceDsn(e.target.value); clearPreview(); }} />
                  </label>
                )}
                <label className="space-y-1">
                  <span>源表名</span>
                  <input className={field} value={sourceTable} onChange={(e) => { setSourceTable(e.target.value); clearPreview(); }} />
                </label>
                {source === 'odbc' && (
                  <label className="space-y-1">
                    <span>源用户名（可选）</span>
                    <input className={field} autoComplete="off" value={sourceUser} onChange={(e) => { setSourceUser(e.target.value); clearPreview(); }} />
                  </label>
                )}
                <label className="space-y-1">
                  <span>源密码（可选）</span>
                  <input type="password" autoComplete="new-password" className={field} value={sourcePassword} onChange={(e) => { setSourcePassword(e.target.value); clearPreview(); }} />
                </label>
                <p className="text-slate-400 sm:col-span-2">请在 Windows 安装对应的 64 位驱动。{source === 'odbc' ? '在 ODBC 数据源管理器中配置 DSN。' : 'Access 文件需要 Microsoft Access ODBC 驱动。'}</p>
              </div>
            )}
            <div className="grid gap-3 sm:grid-cols-3">
              {!workbook && !nativeSource && format !== 'parquet' && (
                <label className="space-y-1">
                  <span>编码</span>
                  <select className={field} value={encoding} onChange={(e) => { setEncoding(e.target.value); clearPreview(); }}>
                    <option value="auto">自动 / UTF-8</option>
                    <option value="utf-8">UTF-8</option>
                    <option value="gb18030">GB18030 / GBK</option>
                    <option value="big5">Big5</option>
                    <option value="utf-16le">UTF-16 LE</option>
                    <option value="utf-16be">UTF-16 BE</option>
                    <option value="windows-1252">Windows-1252</option>
                    {format === 'dbf' && <option value="cp437">DOS CP437</option>}
                    {format === 'dbf' && <option value="cp850">DOS CP850</option>}
                  </select>
                </label>
              )}
              {delimited && (
                <>
                  <label className="space-y-1">
                    <span>列格式</span>
                    <select className={field} value={fixedWidth ? 'fixed' : 'delimited'} onChange={(e) => { setFixedWidth(e.target.value === 'fixed'); clearPreview(); }}>
                      <option value="delimited">分隔符</option>
                      <option value="fixed">固定宽度</option>
                    </select>
                  </label>
                  <label className="space-y-1">
                    <span>{fixedWidth ? '各列字符宽度' : '字段分隔符'}</span>
                    {fixedWidth ? (
                      <input className={field} value={widths} placeholder="8,20,16" onChange={(e) => { setWidths(e.target.value); clearPreview(); }} />
                    ) : (
                      <select className={field} value={delimiter} onChange={(e) => { setDelimiter(e.target.value); clearPreview(); }}>
                        <option value="">格式默认值</option>
                        <option value=",">逗号</option>
                        <option value={'\t'}>制表符</option>
                        <option value=";">分号</option>
                        <option value="|">竖线</option>
                        <option value=" ">空格</option>
                      </select>
                    )}
                  </label>
                </>
              )}
              {rowsConfigurable && (
                <label className="space-y-1">
                  <span>跳过前置行</span>
                  <input className={field} type="number" min={0} max={100000} value={skipRows} onChange={(e) => { setSkipRows(Number(e.target.value)); clearPreview(); }} />
                </label>
              )}
              {format === 'xml' && (
                <label className="space-y-1">
                  <span>XML 行标签</span>
                  <input className={field} value={xmlRow} onChange={(e) => { setXMLRow(e.target.value); clearPreview(); }} />
                </label>
              )}
              {workbook && sheets.length > 0 && (
                <label className="space-y-1">
                  <span>工作表</span>
                  <select className={field} value={sheet} onChange={(e) => { setSheet(e.target.value); clearPreview(); }}>
                    {sheets.map((name) => <option key={name} value={name}>{name}</option>)}
                  </select>
                </label>
              )}
            </div>
            {rowsConfigurable && (
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={!noHeader} onChange={(e) => { setNoHeader(!e.target.checked); clearPreview(); }} />
                第一条记录作为字段名
              </label>
            )}
            {preview && !script && (
              <section className="grid min-w-0 gap-4 border-t border-slate-800 pt-3 lg:grid-cols-[1fr_1.25fr]">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-1.5 pb-2">
                    <span className="mr-auto font-semibold text-slate-100">字段映射</span>
                    <button type="button" className={button} onClick={() => { setMapping(mapByName(preview)); setConfirmed(false); }}>按名称</button>
                    <button type="button" className={button} onClick={() => { setMapping(mapByPosition(preview)); setConfirmed(false); }}>按位置</button>
                    <button type="button" className={button} onClick={() => { setMapping(preview.targetColumns.map(() => -1)); setConfirmed(false); }}>清除</button>
                  </div>
                  <div className="max-h-52 space-y-1 overflow-auto">
                    {preview.targetColumns.map((column, i) => !column.generated && (
                      <label key={column.name} className="grid grid-cols-[1fr_1.2fr] items-center gap-2">
                        <span className="truncate" title={column.name}>{column.name}{column.primaryKey ? ' · 主键' : ''}</span>
                        <select className={field} value={mapping[i] ?? -1} onChange={(e) => {
                          const value = Number(e.target.value);
                          setMapping((previous) => previous.map((item, index) => i === index ? value : item));
                          setConfirmed(false);
                        }}>
                          <option value={-1}>不导入</option>
                          {preview.columns.map((name, sourceIndex) => (
                            <option key={sourceIndex} value={sourceIndex}>{sourceIndex + 1}. {name || '未命名'}</option>
                          ))}
                        </select>
                      </label>
                    ))}
                  </div>
                </div>
                <div className="min-w-0">
                  <h4 className="mb-2 font-semibold text-slate-100">源数据预览</h4>
                  {preview.rows.length ? (
                    <div className="max-h-52 overflow-auto rounded border border-slate-700">
                      <table className="w-full border-collapse text-left">
                        <thead className="sticky top-0 bg-slate-800">
                          <tr>{preview.columns.map((name, i) => (
                            <th key={i} className="whitespace-nowrap border-b border-slate-700 px-2 py-1.5">{name || `列 ${i + 1}`}</th>
                          ))}</tr>
                        </thead>
                        <tbody>{preview.rows.map((row, i) => (
                          <tr key={i}>{row.map((value, j) => (
                            <td key={j} className="max-w-48 truncate whitespace-nowrap border-b border-slate-800 px-2 py-1" title={value == null ? '' : String(value)}>
                              {value == null ? <span className="text-slate-500">NULL</span> : String(value)}
                            </td>
                          ))}</tr>
                        ))}</tbody>
                      </table>
                    </div>
                  ) : <p className="text-slate-400">源文件没有可导入的记录。</p>}
                </div>
              </section>
            )}
            {!script && (
              <div className="grid gap-3 border-t border-slate-800 pt-3 sm:grid-cols-2">
                <label className="space-y-1">
                  <span>写入方式</span>
                  <select className={field} value={writeMode} onChange={(e) => {
                    setWriteMode(e.target.value as ImportWriteMode);
                    setConfirmed(false);
                  }}>
                    {modeOptions.map(({ key, label }) => <option key={key} value={key}>{label}</option>)}
                  </select>
                  {usesPrimaryKey && !keyReady && <span className="block text-amber-300">请预览并映射目标表的全部主键字段。</span>}
                </label>
                <div className="space-y-1">
                  <label className="flex items-center gap-2">
                    <input type="checkbox" checked={emptyAsNull} onChange={(e) => setEmptyAsNull(e.target.checked)} />
                    空字符串写入为 NULL
                  </label>
                  <label className="flex items-center gap-2">
                    <input type="checkbox" disabled={writeMode === 'replace'} checked={writeMode !== 'replace' && continueOnError} onChange={(e) => setContinueOnError(e.target.checked)} />
                    单条失败时继续导入
                  </label>
                  {destructive && (
                    <label className="flex items-center gap-2 text-amber-300">
                      <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} />
                      {writeMode === 'replace' ? '确认清空目标表后导入' : '确认删除匹配记录'}
                    </label>
                  )}
                </div>
              </div>
            )}
          </fieldset>
          {result && <div role="status" className="mt-3 whitespace-pre-wrap rounded border border-emerald-800 bg-emerald-950/70 px-3 py-2 text-xs text-emerald-200">{result}</div>}
          {error && <div role="alert" className="mt-3 rounded border border-rose-800 bg-rose-950/70 px-3 py-2 text-xs text-rose-200">{error}</div>}
        </div>
        <footer className="flex justify-end gap-2 border-t border-slate-800 px-4 py-3">
          <button type="button" className={button} disabled={Boolean(working)} onClick={onClose}>关闭</button>
          {!script && (
            <button type="button" onClick={loadPreview} disabled={Boolean(working)} className={`flex items-center gap-1.5 ${button}`}>
              {working === 'preview' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Eye className="h-3.5 w-3.5" />}
              预览和映射
            </button>
          )}
          <button type="button" onClick={submit} disabled={Boolean(working) || !ready} className="flex items-center gap-1.5 rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50">
            {working === 'import' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
            {script ? '执行 SQL 脚本' : '开始导入'}
          </button>
        </footer>
      </div>
    </div>
  );
}
