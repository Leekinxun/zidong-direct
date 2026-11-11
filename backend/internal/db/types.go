package db

import "time"

type EngineType string

const (
	EngineMySQL     EngineType = "mysql"
	EnginePostgres  EngineType = "postgres"
	EngineSQLite    EngineType = "sqlite"
	EngineSQLServer EngineType = "sqlserver"
	EngineOracle    EngineType = "oracle"
	EngineDameng    EngineType = "dameng"
)

type SSHConfig struct {
	Enabled               bool   `json:"enabled"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	AuthType              string `json:"authType"` // "password" | "privateKey"
	Password              string `json:"password,omitempty"`
	PrivateKey            string `json:"privateKey,omitempty"`
	Passphrase            string `json:"passphrase,omitempty"`
	HostKeyFingerprint    string `json:"hostKeyFingerprint,omitempty"`
	InsecureIgnoreHostKey bool   `json:"insecureIgnoreHostKey"`
}

type SSHView struct {
	Enabled               bool   `json:"enabled"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	AuthType              string `json:"authType"`
	HostKeyFingerprint    string `json:"hostKeyFingerprint,omitempty"`
	InsecureIgnoreHostKey bool   `json:"insecureIgnoreHostKey"`
	HasPassword           bool   `json:"hasPassword"`
	HasPrivateKey         bool   `json:"hasPrivateKey"`
	HasPassphrase         bool   `json:"hasPassphrase"`
}

type SSHInput struct {
	Enabled               bool   `json:"enabled"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	AuthType              string `json:"authType"`
	Password              string `json:"password,omitempty"`
	PrivateKey            string `json:"privateKey,omitempty"`
	Passphrase            string `json:"passphrase,omitempty"`
	HostKeyFingerprint    string `json:"hostKeyFingerprint,omitempty"`
	InsecureIgnoreHostKey bool   `json:"insecureIgnoreHostKey"`
	ClearPassword         bool   `json:"clearPassword,omitempty"`
	ClearPrivateKey       bool   `json:"clearPrivateKey,omitempty"`
	ClearPassphrase       bool   `json:"clearPassphrase,omitempty"`
}

type ConnectionConfig struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Engine         EngineType `json:"engine"`
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	Database       string     `json:"database"`
	Username       string     `json:"username"`
	Password       string     `json:"password,omitempty"`
	SSLMode        string     `json:"sslMode,omitempty"`        // disable | require | verify-full (pg) | true/false (mysql/mssql)
	TrustServerCer bool       `json:"trustServerCert,omitempty"`
	OracleMode     string     `json:"oracleMode,omitempty"`     // service | sid
	SQLitePath     string     `json:"sqlitePath,omitempty"`
	SSH            SSHConfig  `json:"ssh"`
	Color          string     `json:"color,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type ConnectionView struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Engine         EngineType `json:"engine"`
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	Database       string     `json:"database"`
	Username       string     `json:"username"`
	HasPassword    bool       `json:"hasPassword"`
	SSLMode        string     `json:"sslMode,omitempty"`
	TrustServerCer bool       `json:"trustServerCert,omitempty"`
	OracleMode     string     `json:"oracleMode,omitempty"`
	SQLitePath     string     `json:"sqlitePath,omitempty"`
	SSH            SSHView    `json:"ssh"`
	Color          string     `json:"color,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type ConnectionInput struct {
	ID             string     `json:"id,omitempty"`
	Name           string     `json:"name"`
	Engine         EngineType `json:"engine"`
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	Database       string     `json:"database"`
	Username       string     `json:"username"`
	Password       string     `json:"password,omitempty"`
	ClearPassword  bool       `json:"clearPassword,omitempty"`
	SSLMode        string     `json:"sslMode,omitempty"`
	TrustServerCer bool       `json:"trustServerCert,omitempty"`
	OracleMode     string     `json:"oracleMode,omitempty"`
	SQLitePath     string     `json:"sqlitePath,omitempty"`
	SSH            SSHInput   `json:"ssh"`
	Color          string     `json:"color,omitempty"`
}

func (c ConnectionConfig) ToView() ConnectionView {
	return ConnectionView{
		ID:             c.ID,
		Name:           c.Name,
		Engine:         c.Engine,
		Host:           c.Host,
		Port:           c.Port,
		Database:       c.Database,
		Username:       c.Username,
		HasPassword:    c.Password != "",
		SSLMode:        c.SSLMode,
		TrustServerCer: c.TrustServerCer,
		OracleMode:     c.OracleMode,
		SQLitePath:     c.SQLitePath,
		SSH: SSHView{
			Enabled:               c.SSH.Enabled,
			Host:                  c.SSH.Host,
			Port:                  c.SSH.Port,
			Username:              c.SSH.Username,
			AuthType:              c.SSH.AuthType,
			HostKeyFingerprint:    c.SSH.HostKeyFingerprint,
			InsecureIgnoreHostKey: c.SSH.InsecureIgnoreHostKey,
			HasPassword:           c.SSH.Password != "",
			HasPrivateKey:         c.SSH.PrivateKey != "",
			HasPassphrase:         c.SSH.Passphrase != "",
		},
		Color:     c.Color,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

type EngineCapabilities struct {
	Engine              EngineType `json:"engine"`
	DefaultPort         int        `json:"defaultPort"`
	SupportsSchemas     bool       `json:"supportsSchemas"`
	SupportsSSH         bool       `json:"supportsSSH"`
	SupportsAlterColumn bool       `json:"supportsAlterColumn"`
	SupportsForeignKeys bool       `json:"supportsForeignKeys"`
	DataTypes           []string   `json:"dataTypes"`
}

type DatabaseObject struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "TABLE" | "VIEW" | "MATERIALIZED VIEW"
	Schema      string `json:"schema,omitempty"`
	RowEstimate *int64 `json:"rowEstimate,omitempty"`
	Engine      string `json:"engine,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

type ColumnDef struct {
	Name          string  `json:"name"`
	OriginalName  string  `json:"originalName,omitempty"`
	Type          string  `json:"type"`
	Nullable      bool    `json:"nullable"`
	DefaultValue  *string `json:"defaultValue,omitempty"`
	PrimaryKey    bool    `json:"primaryKey"`
	AutoIncrement bool    `json:"autoIncrement"`
	Comment       string  `json:"comment,omitempty"`
	Ordinal       int     `json:"ordinal"`
}

type IndexDef struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Primary bool     `json:"primary"`
}

type ForeignKeyDef struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema,omitempty"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
	OnDelete   string   `json:"onDelete,omitempty"`
	OnUpdate   string   `json:"onUpdate,omitempty"`
}

type TableSchema struct {
	Database    string          `json:"database"`
	Schema      string          `json:"schema"`
	Table       string          `json:"table"`
	Columns     []ColumnDef     `json:"columns"`
	Indexes     []IndexDef      `json:"indexes"`
	ForeignKeys []ForeignKeyDef `json:"foreignKeys"`
	DDL         string          `json:"ddl,omitempty"`
}

type ColumnMeta struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type FilterCondition struct {
	Column   string `json:"column"`
	Operator string `json:"operator"` // =, !=, LIKE, >, >=, <, <=, IS NULL, IS NOT NULL
	Value    string `json:"value"`
}

type TableDataRequest struct {
	ConnectionID string            `json:"connectionId"`
	Database     string            `json:"database"`
	Schema       string            `json:"schema"`
	Table        string            `json:"table"`
	Page         int               `json:"page"`
	PageSize     int               `json:"pageSize"`
	SortColumn   string            `json:"sortColumn"`
	SortDir      string            `json:"sortDir"` // ASC | DESC
	Filters      []FilterCondition `json:"filters"`
}

type QueryResult struct {
	Columns      []ColumnMeta `json:"columns"`
	Rows         [][]any      `json:"rows"`
	RowCount     int          `json:"rowCount"`
	TotalCount   *int64       `json:"totalCount,omitempty"`
	DurationMs   int64        `json:"durationMs"`
	Truncated    bool         `json:"truncated"`
	AffectedRows *int64       `json:"affectedRows,omitempty"`
	Statement    string       `json:"statement,omitempty"`
	PrimaryKeys  []string     `json:"primaryKeys,omitempty"`
	Warning      string       `json:"warning,omitempty"`
	Error        string       `json:"error,omitempty"`
}

type SQLBatchRequest struct {
	ConnectionID string `json:"connectionId"`
	Database     string `json:"database"`
	Schema       string `json:"schema"`
	SQL          string `json:"sql"`
	MaxRows      int    `json:"maxRows"`
}

type SQLBatchResponse struct {
	Results []QueryResult `json:"results"`
}

type RowMutation struct {
	Type   string         `json:"type"` // insert | update | delete
	Keys   map[string]any `json:"keys,omitempty"`
	Values map[string]any `json:"values,omitempty"`
}

type RowMutationRequest struct {
	ConnectionID string        `json:"connectionId"`
	Database     string        `json:"database"`
	Schema       string        `json:"schema"`
	Table        string        `json:"table"`
	Mutations    []RowMutation `json:"mutations"`
}

type MutationResult struct {
	AffectedRows int64    `json:"affectedRows"`
	Statements   []string `json:"statements"`
	Warning      string   `json:"warning,omitempty"`
}

type TableDesignDiff struct {
	Mode             string       `json:"mode"` // create | alter
	ConnectionID     string       `json:"connectionId"`
	Database         string       `json:"database"`
	Schema           string       `json:"schema"`
	TableName        string       `json:"tableName"`
	NewTableName     string       `json:"newTableName,omitempty"`
	Original         *TableSchema `json:"original,omitempty"`
	Target           TableSchema  `json:"target"`
	UseSQLiteRebuild bool         `json:"useSqliteRebuild,omitempty"`
}

type DDLPreviewResponse struct {
	Statements []string `json:"statements"`
	SQL        string   `json:"sql"`
	Warnings   []string `json:"warnings,omitempty"`
}

type AutocompleteTable struct {
	Schema  string   `json:"schema,omitempty"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Columns []string `json:"columns"`
}

type SQLiteFileEntry struct {
	Name         string `json:"name"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
	ModifiedAt   string `json:"modifiedAt"`
}
