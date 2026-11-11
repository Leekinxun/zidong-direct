import { useEffect, useState } from 'react';
import { ClipboardPaste, Eye, Loader2, Upload, X } from 'lucide-react';
import {
  api,
  type ImportFormat,
  type ImportPreview,
  type ImportSourceParams,
  type ImportWriteMode,
} from '../api/client';

interface Props {
  connectionId: string;
  database: string;
  schema: string;
  table: string;
  onClose: () => void;
  onImported?: () => void;
}

const formats: { key: ImportFormat; label: string; pasteable?: boolean }[] = [
  { key: 'csv', label: 'CSV 逗号分隔 (.csv)', pasteable: true },
  { key: 'txt', label: '文本 / 定宽文本 (.txt, .dat)', pasteable: true },
  { key: 'tsv', label: 'TSV 制表符分隔 (.tsv / Excel 复制)', pasteable: true },
  { key: 'xlsx', label: 'Excel 工作簿 (.xlsx, .xlsm)' },
  { key: 'xls', label: 'Excel 97–2003 (.xls)' },
  { key: 'ods', label: 'OpenDocument 表格 (.ods)' },
  { key: 'json', label: 'JSON 数组/对象 (.json)', pasteable: true },
  { key: 'jsonl', label: 'JSON Lines (.jsonl, .ndjson)', pasteable: true },
  { key: 'xml', label: 'XML 数据 (.xml)', pasteable: true },
  { key: 'html', label: 'HTML 网页表格 (.html, .htm)', pasteable: true },
  { key: 'dbf', label: 'dBASE 数据库表 (.dbf)' },
  { key: 'parquet', label: 'Apache Parquet (.parquet)' },
  { key: 'sqlite', label: 'SQLite 数据库表 (.sqlite, .db, .sqlite3)' },
  { key: 'mdb', label: 'Microsoft Access (.mdb)' },
  { key: 'accdb', label: 'Microsoft Access 2007+ (.accdb)' },
  { key: 'sql', label: 'SQL 脚本 (.sql)', pasteable: true },
];

const modeOptions: { key: ImportWriteMode; label: string; matchKey?: boolean }[] = [
  { key: 'append', label: '追加：添加记录到目标表' },
  { key: 'update', label: '更新：用源记录更新匹配的目标记录', matchKey: true },
  { key: 'upsert', label: '追加或更新：若目标存在则更新，否则添加', matchKey: true },
  { key: 'skip', label: '追加不更新：跳过已匹配主键，仅添加新记录', matchKey: true },
  { key: 'delete', label: '删除：删除目标表中与源记录匹配的记录', matchKey: true },
  { key: 'replace', label: '复制：清空目标表现有全部记录并导入新记录' },
];

const field =
  'min-w-0 w-full rounded border border-slate-700 bg-slate-800 px-2.5 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500';
const button =
  'rounded border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-50';

function formatForName(name: string): ImportFormat | undefined {
  const ext = name.split('.').pop()?.toLowerCase();
  if (ext === 'xlsm') return 'xlsx';
  if (ext === 'ndjson') return 'jsonl';
  if (ext === 'dat') return 'txt';
  if (ext === 'htm') return 'html';
  if (ext === 'db' || ext === 'sqlite3') return 'sqlite';
  return formats.find(({ key }) => key === ext)?.key;
}

function mapByName(data: ImportPreview): number[] {
  return data.targetColumns.map(({ name, generated }) => {
    if (generated) return -1;
    const matches = (predicate: (column: string) => boolean) =>
      data.columns.flatMap((column, index) => (predicate(column) ? [index] : []));
    const exact = matches((column) => column === name);
    if (exact.length) return exact.length === 1 ? exact[0] : -1;
    const candidates = matches((column) => column.toLowerCase() === name.toLowerCase());
    return candidates.length === 1 ? candidates[0] : -1;
  });
}

function mapByPosition(data: ImportPreview): number[] {
  let index = 0;
  return data.targetColumns.map(({ generated }) =>
    generated || index >= data.columns.length ? -1 : index++,
  );
}

export function DataImportModal({
  connectionId,
  database,
  schema,
  table,
  onClose,
  onImported,
}: Props) {
  const [format, setFormat] = useState<ImportFormat>(table ? 'csv' : 'sql');
  const [target, setTarget] = useState(table);
  const [availableTables, setAvailableTables] = useState<string[]>([]);
  const [source, setSource] = useState<'file' | 'paste' | 'url' | 'odbc'>('file');
  const [file, setFile] = useState<File>();
  const [pastedText, setPastedText] = useState('');
  const [sourceUrl, setSourceUrl] = useState('');
  const [sourceDsn, setSourceDsn] = useState('');
  const [sourceTable, setSourceTable] = useState('');
  const [sourceUser, setSourceUser] = useState('');
  const [sourcePassword, setSourcePassword] = useState('');
  const [encoding, setEncoding] = useState('auto');
  const [delimiter, setDelimiter] = useState('');
  const [customDelimiter, setCustomDelimiter] = useState('');
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

  const script = format === 'sql';
  const access = format === 'mdb' || format === 'accdb';
  const nativeSource = source === 'odbc' || access;
  const delimited = ['csv', 'txt', 'tsv'].includes(format);
  const workbook = format === 'xlsx' || format === 'xls' || format === 'ods';
  const multiTableSource = workbook || format === 'sqlite' || format === 'html';
  const binarySource = workbook || nativeSource || format === 'parquet' || format === 'sqlite';
  const rowsConfigurable = workbook || delimited || format === 'html';
  const usesPrimaryKey = modeOptions.find(({ key }) => key === writeMode)?.matchKey;
  const primaryColumns =
    preview?.targetColumns.map((column, i) => (column.primaryKey ? i : -1)).filter((i) => i >= 0) ??
    [];
  const keyReady = Boolean(primaryColumns.length) && primaryColumns.every((i) => mapping[i] >= 0);
  const destructive = !script && (writeMode === 'replace' || writeMode === 'delete');

  useEffect(() => {
    if (script) return;
    let active = true;
    api
      .listObjects(connectionId, database, schema)
      .then(({ objects }) => {
        if (active) {
          setAvailableTables(objects.filter(({ type }) => type === 'TABLE').map(({ name }) => name));
        }
      })
      .catch(() => {
        if (active) setAvailableTables([]);
      });
    return () => {
      active = false;
    };
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
    setCustomDelimiter('');
    setFixedWidth(false);
    clearPreview(true);
  }

  function selectFile(next?: File) {
    setFile(next);
    const detected = next && formatForName(next.name);
    if (detected) changeFormat(detected);
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
      // Address can be incomplete while being edited.
    }
    clearPreview(true);
  }

  async function handleReadClipboard() {
    try {
      if (!navigator.clipboard?.readText) {
        setError('当前浏览器环境不支持自动读取剪贴板，请在下方文本框按 Ctrl+V 粘贴');
        return;
      }
      const text = await navigator.clipboard.readText();
      if (text) {
        setPastedText(text);
        clearPreview(true);
      }
    } catch {
      setError('无法读取系统剪贴板，请在下方文本框直接按 Ctrl+V 粘贴');
    }
  }

  function effectiveFile(): File | undefined {
    if (source === 'file') return file;
    if (source === 'paste') {
      const trimmed = pastedText.trim();
      if (!trimmed) return undefined;
      return new File([pastedText], `clipboard.${format}`, { type: 'text/plain;charset=utf-8' });
    }
    return undefined;
  }

  function params(): ImportSourceParams {
    const resolvedFile = effectiveFile();
    if (source === 'file' && !resolvedFile) throw new Error('请选择要导入的本地文件');
    if (source === 'paste' && !resolvedFile) throw new Error('请粘贴要导入的文本内容');
    if (source === 'url' && !sourceUrl.trim()) throw new Error('请输入数据文件的 HTTP(S) URL');
    if (script && source === 'url') throw new Error('请先下载并核对 SQL 脚本，然后选择本地文件或粘贴执行');
    if (nativeSource && !sourceTable.trim()) throw new Error('请输入要导入的源表名');
    if (source === 'odbc' && !sourceDsn.trim()) throw new Error('请输入 Windows ODBC 数据源名称');
    if (!script && !target.trim()) throw new Error('请选择或输入要导入的目标表');
    if (delimited && fixedWidth && !widths.trim()) throw new Error('请输入固定列宽，如 8,20,16');

    const activeDelimiter = delimiter === '__custom__' ? customDelimiter : delimiter;
    return {
      connectionId,
      database,
      schema,
      table: script ? undefined : target.trim(),
      format,
      file: resolvedFile,
      sourceUrl: source === 'url' ? sourceUrl.trim() : undefined,
      sourceDsn: source === 'odbc' ? sourceDsn.trim() : undefined,
      sourceUser: source === 'odbc' ? sourceUser : undefined,
      sourceTable: nativeSource ? sourceTable.trim() : undefined,
      sourcePassword: nativeSource ? sourcePassword : undefined,
      encoding: binarySource ? undefined : encoding,
      delimiter: delimited && !fixedWidth ? activeDelimiter : undefined,
      widths: delimited && fixedWidth ? widths : undefined,
      skipRows: rowsConfigurable ? skipRows : undefined,
      noHeader: rowsConfigurable ? noHeader : undefined,
      xmlRow: format === 'xml' ? xmlRow : undefined,
      sheet: multiTableSource ? sheet : undefined,
    };
  }

  async function loadPreview(overrideSheet?: string) {
    setWorking('preview');
    clearPreview();
    try {
      const reqParams = params();
      if (overrideSheet !== undefined) {
        reqParams.sheet = overrideSheet;
      }
      const next = await api.previewImportData(reqParams);
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
      ]
        .filter(Boolean)
        .join(' · ');
      setResult([totals, ...(stats.errors ?? [])].join('\n'));
      setConfirmed(false);
      onImported?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : '导入失败');
    } finally {
      setWorking(undefined);
    }
  }

  const ready = script
    ? (source === 'file' && Boolean(file)) || (source === 'paste' && Boolean(pastedText.trim()))
    : Boolean(preview?.rows.length && mapping.some((index) => index >= 0)) &&
      (!usesPrimaryKey || keyReady) &&
      (!destructive || confirmed);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-label="导入向导"
        className="flex max-h-[90vh] w-full max-w-4xl flex-col rounded-lg border border-slate-700 bg-slate-900 shadow-2xl"
      >
        <header className="flex items-center justify-between border-b border-slate-800 px-4 py-3">
          <div>
            <h3 className="text-sm font-semibold text-slate-100">数据导入向导 (Navicat 全格式兼容)</h3>
            <p className="mt-0.5 text-[11px] text-slate-400">
              支持 CSV、TXT/定宽、TSV、Excel (XLSX/XLS)、ODS、JSON/JSONL、XML、HTML 表格、dBASE (DBF)、Parquet、SQLite、Access/ODBC 及 SQL 脚本
            </p>
          </div>
          <button
            aria-label="关闭"
            type="button"
            onClick={onClose}
            disabled={Boolean(working)}
            className="text-slate-400 hover:text-slate-200 disabled:opacity-50"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="min-h-0 overflow-y-auto p-4">
          <fieldset disabled={Boolean(working)} className="space-y-4 text-xs text-slate-300">
            <div className="grid gap-3 sm:grid-cols-3">
              <label className="space-y-1">
                <span>数据来源</span>
                <select
                  className={field}
                  value={source}
                  onChange={(e) => {
                    const kind = e.target.value as 'file' | 'paste' | 'url' | 'odbc';
                    setSource(kind);
                    setFile(undefined);
                    setSourcePassword('');
                    if (kind === 'odbc') {
                      changeFormat('odbc');
                    } else if (kind === 'paste') {
                      const cur = formats.find((f) => f.key === format);
                      if (!cur?.pasteable) changeFormat('csv');
                      else clearPreview(true);
                    } else if (source === 'odbc' || (kind === 'url' && (script || access))) {
                      changeFormat('csv');
                    } else {
                      clearPreview(true);
                    }
                  }}
                >
                  <option value="file">本地文件上传</option>
                  <option value="paste">剪贴板 / 直接粘贴文本</option>
                  <option value="url">远程 HTTP(S) 文件 URL</option>
                  <option value="odbc">Windows ODBC 数据源</option>
                </select>
              </label>

              <label className="space-y-1">
                <span>数据源格式</span>
                <select
                  className={field}
                  value={format}
                  onChange={(e) => changeFormat(e.target.value as ImportFormat)}
                >
                  {source === 'odbc' ? (
                    <option value="odbc">ODBC 源表</option>
                  ) : (
                    formats
                      .filter(({ key, pasteable }) => {
                        if (source === 'paste') return Boolean(pasteable);
                        if (key === 'mdb' || key === 'accdb') return source === 'file';
                        return source !== 'url' || key !== 'sql';
                      })
                      .map(({ key, label }) => (
                        <option key={key} value={key}>
                          {label}
                        </option>
                      ))
                  )}
                </select>
              </label>

              {!script && (
                <label className="space-y-1">
                  <span>目标表</span>
                  <input
                    className={field}
                    list="data-import-targets"
                    placeholder="选择或输入目标表名"
                    value={target}
                    onChange={(e) => {
                      setTarget(e.target.value);
                      clearPreview();
                    }}
                  />
                  <datalist id="data-import-targets">
                    {availableTables.map((name) => (
                      <option key={name} value={name} />
                    ))}
                  </datalist>
                </label>
              )}
            </div>

            {source === 'file' && (
              <label className="block space-y-1">
                <span>选择本地文件</span>
                <input
                  type="file"
                  className={field}
                  accept=".csv,.txt,.dat,.tsv,.json,.jsonl,.ndjson,.xml,.html,.htm,.xlsx,.xlsm,.xls,.ods,.dbf,.parquet,.sqlite,.db,.sqlite3,.mdb,.accdb,.sql"
                  onChange={(e) => selectFile(e.target.files?.[0])}
                />
              </label>
            )}

            {source === 'paste' && (
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <span>
                    粘贴文本内容（支持直接从 Excel/WPS 复制表格后选 TSV 格式粘贴，或粘贴 CSV / JSON / XML / HTML / SQL）
                  </span>
                  <button
                    type="button"
                    onClick={() => void handleReadClipboard()}
                    className={`flex items-center gap-1 ${button}`}
                  >
                    <ClipboardPaste className="h-3.5 w-3.5" />
                    从剪贴板读取
                  </button>
                </div>
                <textarea
                  rows={5}
                  value={pastedText}
                  onChange={(e) => {
                    setPastedText(e.target.value);
                    clearPreview(true);
                  }}
                  placeholder={
                    format === 'tsv'
                      ? 'id\tname\n1\tAlice'
                      : format === 'json'
                        ? '[{"id": 1, "name": "Alice"}]'
                        : 'id,name\n1,Alice'
                  }
                  className="min-h-[100px] max-h-72 w-full resize-y rounded border border-slate-700 bg-slate-950 p-2.5 font-mono text-xs text-slate-100 outline-none focus:border-indigo-500"
                />
              </div>
            )}

            {source === 'url' && (
              <label className="block space-y-1">
                <span>公网 HTTP(S) 数据文件地址</span>
                <input
                  type="url"
                  className={field}
                  placeholder="https://example.com/data.csv"
                  value={sourceUrl}
                  onChange={(e) => editURL(e.target.value)}
                />
              </label>
            )}

            {nativeSource && (
              <div className="grid gap-3 sm:grid-cols-2">
                {source === 'odbc' && (
                  <label className="space-y-1">
                    <span>ODBC 数据源名称（DSN）</span>
                    <input
                      className={field}
                      value={sourceDsn}
                      onChange={(e) => {
                        setSourceDsn(e.target.value);
                        clearPreview();
                      }}
                    />
                  </label>
                )}
                <label className="space-y-1">
                  <span>源表名</span>
                  <input
                    className={field}
                    value={sourceTable}
                    onChange={(e) => {
                      setSourceTable(e.target.value);
                      clearPreview();
                    }}
                  />
                </label>
                {source === 'odbc' && (
                  <label className="space-y-1">
                    <span>源用户名（可选）</span>
                    <input
                      className={field}
                      autoComplete="off"
                      value={sourceUser}
                      onChange={(e) => {
                        setSourceUser(e.target.value);
                        clearPreview();
                      }}
                    />
                  </label>
                )}
                <label className="space-y-1">
                  <span>源密码（可选）</span>
                  <input
                    type="password"
                    autoComplete="new-password"
                    className={field}
                    value={sourcePassword}
                    onChange={(e) => {
                      setSourcePassword(e.target.value);
                      clearPreview();
                    }}
                  />
                </label>
                <p className="text-slate-400 sm:col-span-2">
                  注：ODBC 与 Access 源需要后端运行在安装了对应 64 位驱动的 Windows 主机上。跨平台导入数据库文件推荐使用 SQLite (.sqlite/.db)、CSV、Excel 或 DBF。
                </p>
              </div>
            )}

            <div className="grid gap-3 sm:grid-cols-3">
              {!binarySource && source !== 'paste' && (
                <label className="space-y-1">
                  <span>字符编码</span>
                  <select
                    className={field}
                    value={encoding}
                    onChange={(e) => {
                      setEncoding(e.target.value);
                      clearPreview();
                    }}
                  >
                    <option value="auto">自动检测 / UTF-8</option>
                    <option value="utf-8">UTF-8</option>
                    <option value="gb18030">GB18030 / GBK (简体中文)</option>
                    <option value="big5">Big5 (繁体中文)</option>
                    <option value="utf-16le">UTF-16 LE</option>
                    <option value="utf-16be">UTF-16 BE</option>
                    <option value="windows-1252">Windows-1252 (西欧)</option>
                    {format === 'dbf' && <option value="cp437">DOS CP437</option>}
                    {format === 'dbf' && <option value="cp850">DOS CP850</option>}
                  </select>
                </label>
              )}

              {delimited && (
                <>
                  <label className="space-y-1">
                    <span>列切分方式</span>
                    <select
                      className={field}
                      value={fixedWidth ? 'fixed' : 'delimited'}
                      onChange={(e) => {
                        setFixedWidth(e.target.value === 'fixed');
                        clearPreview();
                      }}
                    >
                      <option value="delimited">按分隔符切分</option>
                      <option value="fixed">按固定字符列宽切分</option>
                    </select>
                  </label>
                  <label className="space-y-1">
                    <span>{fixedWidth ? '各列字符宽度' : '字段分隔符'}</span>
                    {fixedWidth ? (
                      <input
                        className={field}
                        value={widths}
                        placeholder="8,20,16"
                        onChange={(e) => {
                          setWidths(e.target.value);
                          clearPreview();
                        }}
                      />
                    ) : (
                      <div className="flex gap-1.5">
                        <select
                          className={field}
                          value={delimiter}
                          onChange={(e) => {
                            setDelimiter(e.target.value);
                            clearPreview();
                          }}
                        >
                          <option value="">格式默认</option>
                          <option value=",">逗号 (,)</option>
                          <option value={'\t'}>制表符 (Tab)</option>
                          <option value=";">分号 (;)</option>
                          <option value="|">竖线 (|)</option>
                          <option value=" ">空格</option>
                          <option value="__custom__">自定义字符...</option>
                        </select>
                        {delimiter === '__custom__' && (
                          <input
                            type="text"
                            maxLength={1}
                            placeholder=":"
                            value={customDelimiter}
                            onChange={(e) => {
                              setCustomDelimiter(e.target.value);
                              clearPreview();
                            }}
                            className="w-16 shrink-0 rounded border border-slate-700 bg-slate-800 px-2 py-1.5 text-center font-mono text-sm text-slate-100 outline-none focus:border-indigo-500"
                          />
                        )}
                      </div>
                    )}
                  </label>
                </>
              )}

              {rowsConfigurable && (
                <label className="space-y-1">
                  <span>跳过前置行</span>
                  <input
                    className={field}
                    type="number"
                    min={0}
                    max={100000}
                    value={skipRows}
                    onChange={(e) => {
                      setSkipRows(Number(e.target.value));
                      clearPreview();
                    }}
                  />
                </label>
              )}

              {format === 'xml' && (
                <label className="space-y-1">
                  <span>XML 行节点标签</span>
                  <input
                    className={field}
                    value={xmlRow}
                    onChange={(e) => {
                      setXMLRow(e.target.value);
                      clearPreview();
                    }}
                  />
                </label>
              )}

              {multiTableSource && sheets.length > 0 && (
                <label className="space-y-1">
                  <span>{format === 'sqlite' ? 'SQLite 源表' : format === 'html' ? 'HTML 表格' : '工作表'}</span>
                  <select
                    className={field}
                    value={sheet}
                    onChange={(e) => {
                      const nextSheet = e.target.value;
                      setSheet(nextSheet);
                      void loadPreview(nextSheet);
                    }}
                  >
                    {sheets.map((name) => (
                      <option key={name} value={name}>
                        {name}
                      </option>
                    ))}
                  </select>
                </label>
              )}
            </div>

            {rowsConfigurable && (
              <label className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={!noHeader}
                  onChange={(e) => {
                    setNoHeader(!e.target.checked);
                    clearPreview();
                  }}
                />
                第一条记录作为字段标题行
              </label>
            )}

            {preview && !script && (
              <section className="grid min-w-0 gap-4 border-t border-slate-800 pt-3 lg:grid-cols-[1fr_1.25fr]">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-1.5 pb-2">
                    <span className="mr-auto font-semibold text-slate-100">字段映射</span>
                    <button
                      type="button"
                      className={button}
                      onClick={() => {
                        setMapping(mapByName(preview));
                        setConfirmed(false);
                      }}
                    >
                      按名称匹配
                    </button>
                    <button
                      type="button"
                      className={button}
                      onClick={() => {
                        setMapping(mapByPosition(preview));
                        setConfirmed(false);
                      }}
                    >
                      按顺序匹配
                    </button>
                    <button
                      type="button"
                      className={button}
                      onClick={() => {
                        setMapping(preview.targetColumns.map(() => -1));
                        setConfirmed(false);
                      }}
                    >
                      清空映射
                    </button>
                  </div>
                  <div className="max-h-52 space-y-1 overflow-auto pr-1">
                    {preview.targetColumns.map(
                      (column, i) =>
                        !column.generated && (
                          <label key={column.name} className="grid grid-cols-[1fr_1.2fr] items-center gap-2">
                            <span className="truncate" title={column.name}>
                              {column.name}
                              {column.primaryKey ? ' · 主键' : ''}
                            </span>
                            <select
                              className={field}
                              value={mapping[i] ?? -1}
                              onChange={(e) => {
                                const value = Number(e.target.value);
                                setMapping((previous) =>
                                  previous.map((item, index) => (i === index ? value : item)),
                                );
                                setConfirmed(false);
                              }}
                            >
                              <option value={-1}>-- 不导入 --</option>
                              {preview.columns.map((name, sourceIndex) => (
                                <option key={sourceIndex} value={sourceIndex}>
                                  {sourceIndex + 1}. {name || '未命名'}
                                </option>
                              ))}
                            </select>
                          </label>
                        ),
                    )}
                  </div>
                </div>

                <div className="min-w-0">
                  <h4 className="mb-2 font-semibold text-slate-100">源数据预览</h4>
                  {preview.rows.length ? (
                    <div className="max-h-52 overflow-auto rounded border border-slate-700">
                      <table className="w-full border-collapse text-left">
                        <thead className="sticky top-0 bg-slate-800">
                          <tr>
                            {preview.columns.map((name, i) => (
                              <th
                                key={i}
                                className="whitespace-nowrap border-b border-slate-700 px-2 py-1.5"
                              >
                                {name || `列 ${i + 1}`}
                              </th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {preview.rows.map((row, i) => (
                            <tr key={i}>
                              {row.map((value, j) => (
                                <td
                                  key={j}
                                  className="max-w-48 truncate whitespace-nowrap border-b border-slate-800 px-2 py-1"
                                  title={value == null ? '' : String(value)}
                                >
                                  {value == null ? (
                                    <span className="text-slate-500">NULL</span>
                                  ) : (
                                    String(value)
                                  )}
                                </td>
                              ))}
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  ) : (
                    <p className="text-slate-400">源文件没有可导入的记录。</p>
                  )}
                </div>
              </section>
            )}

            {!script && (
              <div className="grid gap-3 border-t border-slate-800 pt-3 sm:grid-cols-2">
                <label className="space-y-1">
                  <span>导入模式 (参照 Navicat 写入模式)</span>
                  <select
                    className={field}
                    value={writeMode}
                    onChange={(e) => {
                      setWriteMode(e.target.value as ImportWriteMode);
                      setConfirmed(false);
                    }}
                  >
                    {modeOptions.map(({ key, label }) => (
                      <option key={key} value={key}>
                        {label}
                      </option>
                    ))}
                  </select>
                  {usesPrimaryKey && !keyReady && (
                    <span className="block text-amber-300">
                      此模式需根据主键匹配记录，请先预览并映射目标表的全部主键字段。
                    </span>
                  )}
                </label>

                <div className="space-y-1.5 pt-1">
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={emptyAsNull}
                      onChange={(e) => setEmptyAsNull(e.target.checked)}
                    />
                    空字符串写入为 NULL
                  </label>
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      disabled={writeMode === 'replace'}
                      checked={writeMode !== 'replace' && continueOnError}
                      onChange={(e) => setContinueOnError(e.target.checked)}
                    />
                    遇到单行错误时跳过并继续导入
                  </label>
                  {destructive && (
                    <label className="flex items-center gap-2 text-amber-300">
                      <input
                        type="checkbox"
                        checked={confirmed}
                        onChange={(e) => setConfirmed(e.target.checked)}
                      />
                      {writeMode === 'replace' ? '我确认清空目标表后再导入数据' : '我确认删除匹配记录'}
                    </label>
                  )}
                </div>
              </div>
            )}
          </fieldset>

          {result && (
            <div
              role="status"
              className="mt-3 whitespace-pre-wrap rounded border border-emerald-800 bg-emerald-950/70 px-3 py-2 text-xs text-emerald-200"
            >
              {result}
            </div>
          )}
          {error && (
            <div
              role="alert"
              className="mt-3 rounded border border-rose-800 bg-rose-950/70 px-3 py-2 text-xs text-rose-200"
            >
              {error}
            </div>
          )}
        </div>

        <footer className="flex justify-end gap-2 border-t border-slate-800 px-4 py-3">
          <button
            type="button"
            className={button}
            disabled={Boolean(working)}
            onClick={onClose}
          >
            关闭
          </button>
          {!script && (
            <button
              type="button"
              onClick={() => void loadPreview()}
              disabled={Boolean(working)}
              className={`flex items-center gap-1.5 ${button}`}
            >
              {working === 'preview' ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Eye className="h-3.5 w-3.5" />
              )}
              预览和映射
            </button>
          )}
          <button
            type="button"
            onClick={() => void submit()}
            disabled={Boolean(working) || !ready}
            className="flex items-center gap-1.5 rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
          >
            {working === 'import' ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Upload className="h-3.5 w-3.5" />
            )}
            {script ? '执行 SQL 脚本' : '开始导入'}
          </button>
        </footer>
      </div>
    </div>
  );
}
