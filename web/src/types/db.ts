export type EngineType = 'mysql' | 'postgres' | 'sqlite' | 'sqlserver' | 'oracle' | 'dameng';

export interface SSHView {
  enabled: boolean;
  host: string;
  port: number;
  username: string;
  authType: 'password' | 'privateKey';
  hostKeyFingerprint?: string;
  insecureIgnoreHostKey: boolean;
  hasPassword: boolean;
  hasPrivateKey: boolean;
  hasPassphrase: boolean;
}

export interface SSHInput {
  enabled: boolean;
  host: string;
  port: number;
  username: string;
  authType: 'password' | 'privateKey';
  password?: string;
  privateKey?: string;
  passphrase?: string;
  hostKeyFingerprint?: string;
  insecureIgnoreHostKey: boolean;
  clearPassword?: boolean;
  clearPrivateKey?: boolean;
  clearPassphrase?: boolean;
}

export interface ConnectionView {
  id: string;
  name: string;
  engine: EngineType;
  host: string;
  port: number;
  database: string;
  username: string;
  hasPassword: boolean;
  sslMode?: string;
  trustServerCert?: boolean;
  oracleMode?: 'service' | 'sid' | string;
  sqlitePath?: string;
  ssh: SSHView;
  color?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ConnectionInput {
  id?: string;
  name: string;
  engine: EngineType;
  host: string;
  port: number;
  database: string;
  username: string;
  password?: string;
  clearPassword?: boolean;
  sslMode?: string;
  trustServerCert?: boolean;
  oracleMode?: string;
  sqlitePath?: string;
  ssh: SSHInput;
  color?: string;
}

export interface EngineCapabilities {
  engine: EngineType;
  defaultPort: number;
  supportsSchemas: boolean;
  supportsSSH: boolean;
  supportsAlterColumn: boolean;
  supportsForeignKeys: boolean;
  dataTypes: string[];
}

export interface DatabaseObject {
  name: string;
  type: 'TABLE' | 'VIEW' | 'MATERIALIZED VIEW' | string;
  schema?: string;
  rowEstimate?: number | null;
  engine?: string;
  comment?: string;
}

export interface ColumnDef {
  name: string;
  originalName?: string;
  type: string;
  nullable: boolean;
  defaultValue?: string | null;
  primaryKey: boolean;
  autoIncrement: boolean;
  comment?: string;
  ordinal: number;
}

export interface IndexDef {
  name: string;
  columns: string[];
  unique: boolean;
  primary: boolean;
}

export interface ForeignKeyDef {
  name: string;
  columns: string[];
  refSchema?: string;
  refTable: string;
  refColumns: string[];
  onDelete?: string;
  onUpdate?: string;
}

export interface TableSchema {
  database: string;
  schema: string;
  table: string;
  columns: ColumnDef[];
  indexes: IndexDef[];
  foreignKeys: ForeignKeyDef[];
  ddl?: string;
}

export interface ColumnMeta {
  name: string;
  type: string;
  nullable: boolean;
}

export interface FilterCondition {
  column: string;
  operator: string;
  value: string;
}

export interface QueryResult {
  columns: ColumnMeta[];
  rows: Array<Array<unknown>>;
  rowCount: number;
  totalCount?: number | null;
  durationMs: number;
  truncated: boolean;
  affectedRows?: number | null;
  statement?: string;
  primaryKeys?: string[];
  warning?: string;
  error?: string;
}

export interface RowMutation {
  type: 'insert' | 'update' | 'delete';
  keys?: Record<string, unknown>;
  values?: Record<string, unknown>;
}

export interface DDLPreviewResponse {
  statements: string[];
  sql: string;
  warnings?: string[];
}

export interface AutocompleteTable {
  schema?: string;
  name: string;
  type: string;
  columns: string[];
}

export interface SQLiteFileEntry {
  name: string;
  relativePath: string;
  size: number;
  modifiedAt: string;
}

export type WorkbenchTab =
  | {
      id: string;
      kind: 'data';
      title: string;
      connectionId: string;
      engine: EngineType;
      database: string;
      schema: string;
      table: string;
    }
  | {
      id: string;
      kind: 'query';
      title: string;
      connectionId: string;
      engine: EngineType;
      database: string;
      schema: string;
      initialSql?: string;
    }
  | {
      id: string;
      kind: 'design';
      title: string;
      connectionId: string;
      engine: EngineType;
      database: string;
      schema: string;
      table: string;
      mode: 'create' | 'alter';
    };
