import type {
  AutocompleteTable,
  ConnectionInput,
  ConnectionView,
  DatabaseDetails,
  DatabaseObject,
  DDLPreviewResponse,
  EngineCapabilities,
  FilterCondition,
  QueryResult,
  RowMutation,
  SSHHostSaveInput,
  SSHHostView,
  SQLiteFileEntry,
  TableSchema,
} from '../types/db';

export class ApiError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers || {}),
    },
    ...init,
  });

  const text = await res.text();
  let parsed: Record<string, unknown> = {};
  if (text) {
    try {
      parsed = JSON.parse(text) as Record<string, unknown>;
    } catch {
      if (!res.ok) {
        throw new ApiError(res.status, 'http_error', text);
      }
    }
  }

  if (!res.ok) {
    throw new ApiError(
      res.status,
      typeof parsed.error === 'string' ? parsed.error : 'http_error',
      typeof parsed.message === 'string' ? parsed.message : `HTTP ${res.status}`,
    );
  }
  return parsed as T;
}

export const api = {
  authStatus: () =>
    requestJSON<{ authEnabled: boolean; authenticated: boolean; username: string }>('/api/auth/status'),
  login: (username: string, password: string) =>
    requestJSON<{ ok: boolean; username: string }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  logout: () => requestJSON<{ ok: boolean }>('/api/auth/logout', { method: 'POST' }),

  capabilities: () =>
    requestJSON<{
      engines: EngineCapabilities[];
      sqliteDir: string;
      allowArbitrarySqlitePaths: boolean;
      maxQueryRows: number;
    }>('/api/capabilities'),

  listConnections: () => requestJSON<{ connections: ConnectionView[] }>('/api/connections'),
  createConnection: (input: ConnectionInput) =>
    requestJSON<{ connection: ConnectionView }>('/api/connections', {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  updateConnection: (id: string, input: ConnectionInput) =>
    requestJSON<{ connection: ConnectionView }>(`/api/connections/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }),
  deleteConnection: (id: string) =>
    requestJSON<{ ok: boolean }>(`/api/connections/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  disconnectConnection: (id: string) =>
    requestJSON<{ ok: boolean }>(`/api/connections/${encodeURIComponent(id)}/disconnect`, { method: 'POST' }),
  testConnection: (input: ConnectionInput) =>
    requestJSON<{ ok: boolean; latencyMs: number }>('/api/connections/test', {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  listSSHHosts: () => requestJSON<{ hosts: SSHHostView[] }>('/api/ssh/hosts'),
  createSSHHost: (input: SSHHostSaveInput) =>
    requestJSON<{ host: SSHHostView }>('/api/ssh/hosts', {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  deleteSSHHost: (id: string) =>
    requestJSON<void>(`/api/ssh/hosts/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  listSqliteFiles: () => requestJSON<{ files: SQLiteFileEntry[]; sqliteDir: string }>('/api/sqlite/files'),
  createSqliteFile: (name: string) =>
    requestJSON<{ relativePath: string }>('/api/sqlite/create', {
      method: 'POST',
      body: JSON.stringify({ name }),
    }),
  uploadSqliteFile: async (file: File) => {
    const fd = new FormData();
    fd.append('file', file);
    const res = await fetch('/api/sqlite/upload', {
      method: 'POST',
      body: fd,
      credentials: 'same-origin',
    });
    const data = await res.json();
    if (!res.ok) {
      throw new ApiError(res.status, data.error || 'upload_error', data.message || '上传失败');
    }
    return data as { relativePath: string; name: string };
  },
  downloadSqliteUrl: (path: string) => `/api/sqlite/download?path=${encodeURIComponent(path)}`,

  listDatabases: (connId: string) =>
    requestJSON<{ databases: string[] }>(`/api/connections/${encodeURIComponent(connId)}/databases`),
  describeDatabase: (connId: string, database: string) =>
    requestJSON<{ database: DatabaseDetails }>(
      `/api/connections/${encodeURIComponent(connId)}/database/describe?${new URLSearchParams({ database })}`,
    ),
  updateDatabaseComment: (connId: string, database: string, comment: string) =>
    requestJSON<{ database: DatabaseDetails }>(
      `/api/connections/${encodeURIComponent(connId)}/database/comment`,
      {
        method: 'POST',
        body: JSON.stringify({ database, comment }),
      },
    ),
  listSchemas: (connId: string, database: string) =>
    requestJSON<{ schemas: string[] }>(
      `/api/connections/${encodeURIComponent(connId)}/schemas?database=${encodeURIComponent(database)}`,
    ),
  listObjects: (connId: string, database: string, schema: string) =>
    requestJSON<{ objects: DatabaseObject[] }>(
      `/api/connections/${encodeURIComponent(connId)}/objects?database=${encodeURIComponent(database)}&schema=${encodeURIComponent(schema)}`,
    ),
  describeTable: (connId: string, database: string, schema: string, table: string) =>
    requestJSON<{ schema: TableSchema }>(
      `/api/connections/${encodeURIComponent(connId)}/table/describe?database=${encodeURIComponent(database)}&schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`,
    ),
  updateTableComment: (
    connId: string,
    payload: { database: string; schema: string; table: string; type?: string; comment: string },
  ) =>
    requestJSON<{ comment: string }>(
      `/api/connections/${encodeURIComponent(connId)}/table/comment`,
      {
        method: 'POST',
        body: JSON.stringify(payload),
      },
    ),
  autocomplete: (connId: string, database: string, schema: string) =>
    requestJSON<{ tables: AutocompleteTable[] }>(
      `/api/connections/${encodeURIComponent(connId)}/autocomplete?database=${encodeURIComponent(database)}&schema=${encodeURIComponent(schema)}`,
    ),

  queryTableData: (payload: {
    connectionId: string;
    database: string;
    schema: string;
    table: string;
    page: number;
    pageSize: number;
    sortColumn?: string;
    sortDir?: 'ASC' | 'DESC' | '';
    filters?: FilterCondition[];
  }) =>
    requestJSON<QueryResult>('/api/table/data', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  mutateTableRows: (payload: {
    connectionId: string;
    database: string;
    schema: string;
    table: string;
    mutations: RowMutation[];
  }) =>
    requestJSON<{ affectedRows: number; statements: string[]; warning?: string }>('/api/table/mutate', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  executeSQL: (payload: {
    connectionId: string;
    database: string;
    schema: string;
    sql: string;
    maxRows?: number;
  }) =>
    requestJSON<{ results: QueryResult[] }>('/api/query/execute', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  previewDDL: (payload: {
    mode: 'create' | 'alter';
    connectionId: string;
    database: string;
    schema: string;
    tableName: string;
    newTableName?: string;
    original?: TableSchema;
    target: TableSchema;
  }) =>
    requestJSON<DDLPreviewResponse>('/api/table/ddl/preview', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  executeDDL: (payload: { connectionId: string; database: string; statements: string[] }) =>
    requestJSON<{ ok: boolean; executed: number }>('/api/table/ddl/execute', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  tableAction: (payload: {
    connectionId: string;
    database: string;
    schema: string;
    table: string;
    action: 'drop_table' | 'drop_view' | 'truncate_table' | 'empty_table';
  }) =>
    requestJSON<{ ok: boolean; statement: string }>('/api/table/action', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  exportData: async (payload: {
    connectionId: string;
    database: string;
    schema: string;
    table?: string;
    sql?: string;
    format: ExportFormat;
    withBom?: boolean;
    delimiter?: string;
    widths?: string;
    filters?: FilterCondition[];
    sortColumn?: string;
    sortDir?: string;
    maxRows?: number;
  }) => {
    const res = await fetch('/api/export', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      const text = await res.text();
      let message = '';
      if (text) {
        try {
          message = (JSON.parse(text) as { message?: string }).message ?? '';
        } catch {
          message = text;
        }
      }
      throw new ApiError(res.status, 'export_error', message || `导出失败 (${res.status})`);
    }
    const blob = await res.blob();
    const disp = res.headers.get('Content-Disposition') || '';
    const match = disp.match(/filename="([^"]+)"/);
    const filename = match ? match[1] : `export.${payload.format}`;
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  },

  previewImportData: (params: ImportSourceParams) =>
    submitImport<ImportPreview>(params, { preview: 'true' }),
  importData: (params: ImportSourceParams & {
    emptyAsNull: boolean;
    continueOnError: boolean;
    mode?: ImportWriteMode;
    mappings?: { source: number; target: string }[];
    confirmed?: boolean;
    sourceDigest?: string;
  }) => submitImport<ImportResult>(params, {
    emptyAsNull: String(params.emptyAsNull),
    continueOnError: String(params.continueOnError),
    mode: params.mode ?? '',
    mappings: params.mappings ? JSON.stringify(params.mappings) : '',
    confirmed: String(Boolean(params.confirmed)),
    sourceDigest: params.sourceDigest ?? '',
  }),

  resetSystem: (payload: { clearConnections: boolean; clearSqliteFiles: boolean; confirmed: boolean }) =>
    requestJSON<{ clearedConnections: number; clearedSshHosts?: number; clearedSqliteFiles: number }>('/api/system/reset', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
};

export type ExportFormat =
  | 'csv'
  | 'txt'
  | 'tsv'
  | 'json'
  | 'jsonl'
  | 'xml'
  | 'html'
  | 'sql'
  | 'xlsx'
  | 'xls'
  | 'ods'
  | 'dbf'
  | 'parquet';


export type ImportFormat =
  | 'csv'
  | 'txt'
  | 'tsv'
  | 'json'
  | 'jsonl'
  | 'xml'
  | 'html'
  | 'xlsx'
  | 'ods'
  | 'dbf'
  | 'xls'
  | 'parquet'
  | 'sqlite'
  | 'mdb'
  | 'accdb'
  | 'odbc'
  | 'sql';
export type ImportWriteMode = 'append' | 'update' | 'upsert' | 'skip' | 'delete' | 'replace';

export interface ImportSourceParams {
  connectionId: string;
  database: string;
  schema: string;
  table?: string;
  format: ImportFormat;
  file?: File;
  sourceUrl?: string;
  sourceDsn?: string;
  sourceUser?: string;
  sourcePassword?: string;
  sourceTable?: string;
  encoding?: string;
  delimiter?: string;
  widths?: string;
  sheet?: string;
  xmlRow?: string;
  skipRows?: number;
  noHeader?: boolean;
}

export interface ImportPreview {
  columns: string[];
  rows: unknown[][];
  targetColumns: { name: string; primaryKey: boolean; generated?: boolean }[];
  sheets?: string[];
  sheet?: string;
  sourceDigest?: string;
}

export interface ImportResult {
  insertedRows: number;
  updatedRows?: number;
  deletedRows?: number;
  skippedRows?: number;
  failedRows?: number;
  errors?: string[];
  statements?: number;
}

async function submitImport<T>(params: ImportSourceParams, options: Record<string, string>): Promise<T> {
  const fd = new FormData();
  const { file, ...source } = params;
  for (const [key, value] of Object.entries({ ...source, ...options })) {
    if (value !== undefined && value !== '') fd.append(key, String(value));
  }
  if (file) fd.append('file', file);
  const res = await fetch('/api/import', {
    method: 'POST',
    body: fd,
    credentials: 'same-origin',
  });
  const data = await res.json();
  if (!res.ok) {
    throw new ApiError(res.status, data.error || 'import_error', data.message || '导入失败');
  }
  return data as T;
}
