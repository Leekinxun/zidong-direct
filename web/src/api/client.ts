import type {
  AutocompleteTable,
  ConnectionInput,
  ConnectionView,
  DatabaseObject,
  DDLPreviewResponse,
  EngineCapabilities,
  FilterCondition,
  QueryResult,
  RowMutation,
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
    format: 'csv' | 'json' | 'sql';
    withBom?: boolean;
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
      throw new ApiError(res.status, 'export_error', text || `导出失败 (${res.status})`);
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

  importData: async (params: {
    connectionId: string;
    database: string;
    schema: string;
    table?: string;
    format: 'csv' | 'json' | 'sql';
    file: File;
    emptyAsNull: boolean;
    continueOnError: boolean;
  }) => {
    const fd = new FormData();
    fd.append('connectionId', params.connectionId);
    fd.append('database', params.database);
    fd.append('schema', params.schema);
    if (params.table) fd.append('table', params.table);
    fd.append('format', params.format);
    fd.append('emptyAsNull', params.emptyAsNull ? 'true' : 'false');
    fd.append('continueOnError', params.continueOnError ? 'true' : 'false');
    fd.append('file', params.file);

    const res = await fetch('/api/import', {
      method: 'POST',
      body: fd,
      credentials: 'same-origin',
    });
    const data = await res.json();
    if (!res.ok) {
      throw new ApiError(res.status, data.error || 'import_error', data.message || '导入失败');
    }
    return data as { insertedRows: number; failedRows?: number; errors?: string[]; statements?: number };
  },
};
