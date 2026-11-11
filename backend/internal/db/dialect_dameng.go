package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	_ "gitee.com/chunanyong/dm"
)

type damengDialect struct{}

func init() {
	RegisterDialect(&damengDialect{})
}

func (d *damengDialect) Engine() EngineType { return EngineDameng }
func (d *damengDialect) DriverName() string { return "dm" }

func (d *damengDialect) Capabilities() EngineCapabilities {
	return EngineCapabilities{
		Engine:              EngineDameng,
		DefaultPort:         5236,
		SupportsSchemas:     true,
		SupportsSSH:         true,
		SupportsAlterColumn: true,
		SupportsForeignKeys: true,
		DataTypes: []string{
			"INT", "BIGINT", "SMALLINT", "TINYINT",
			"VARCHAR(255)", "VARCHAR(100)", "CHAR(36)", "TEXT", "CLOB",
			"DECIMAL(18,2)", "NUMBER(18,2)", "DOUBLE", "BIT",
			"DATE", "TIMESTAMP", "DATETIME",
			"BLOB", "BINARY(16)",
		},
	}
}

func (d *damengDialect) QuoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func (d *damengDialect) Placeholder(_ int) string { return "?" }

func (d *damengDialect) BuildDSN(cfg ConnectionConfig, targetHost string, targetPort int, databaseOverride string) (string, error) {
	schema := cfg.Database
	if databaseOverride != "" && !strings.EqualFold(databaseOverride, "DAMENG") {
		schema = databaseOverride
	}
	hostPort := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	// Note: gitee.com/chunanyong/dm parseDSN splits on LastIndex("@") and LastIndex("?")
	// and reads raw password without URL-unescaping. Keep ?connectTimeout=10000 trailing.
	dsn := fmt.Sprintf("dm://%s:%s@%s?connectTimeout=10000", cfg.Username, cfg.Password, hostPort)
	if schema != "" {
		dsn += "&schema=" + url.QueryEscape(schema)
	}
	return dsn, nil
}

func (d *damengDialect) ListDatabases(ctx context.Context, conn *sql.DB, cfg ConnectionConfig) ([]string, error) {
	var dbName string
	if err := conn.QueryRowContext(ctx, "SELECT NAME FROM V$DATABASE").Scan(&dbName); err == nil && dbName != "" {
		return []string{dbName}, nil
	}
	if cfg.Database != "" {
		return []string{cfg.Database}, nil
	}
	return []string{"DAMENG"}, nil
}

func (d *damengDialect) ListSchemas(ctx context.Context, conn *sql.DB, cfg ConnectionConfig, _ string) ([]string, error) {
	q := `
SELECT NAME
FROM SYS.SYSOBJECTS
WHERE TYPE$ = 'SCH'
ORDER BY CASE WHEN UPPER(NAME) = UPPER(?) THEN 0 ELSE 1 END, NAME`
	rows, err := conn.QueryContext(ctx, q, cfg.Username)
	if err != nil {
		// Fallback to ALL_USERS if SYS.SYSOBJECTS is restricted
		rows, err = conn.QueryContext(ctx, "SELECT USERNAME FROM ALL_USERS ORDER BY CASE WHEN UPPER(USERNAME) = UPPER(?) THEN 0 ELSE 1 END, USERNAME", cfg.Username)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (d *damengDialect) ListObjects(ctx context.Context, conn *sql.DB, _, schema string) ([]DatabaseObject, error) {
	if schema == "" {
		_ = conn.QueryRowContext(ctx, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL").Scan(&schema)
	}
	q := `
SELECT TABLE_NAME, 'TABLE'
FROM ALL_TABLES
WHERE OWNER = ?
UNION ALL
SELECT VIEW_NAME, 'VIEW'
FROM ALL_VIEWS
WHERE OWNER = ?
ORDER BY 2, 1`
	rows, err := conn.QueryContext(ctx, q, schema, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DatabaseObject
	for rows.Next() {
		var name, objType string
		if err := rows.Scan(&name, &objType); err != nil {
			return nil, err
		}
		out = append(out, DatabaseObject{
			Name:   name,
			Type:   objType,
			Schema: schema,
		})
	}
	return out, rows.Err()
}

func (d *damengDialect) DescribeTable(ctx context.Context, conn *sql.DB, database, schema, table string) (TableSchema, error) {
	if schema == "" {
		_ = conn.QueryRowContext(ctx, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL").Scan(&schema)
	}
	ts := TableSchema{
		Database: database,
		Schema:   schema,
		Table:    table,
	}

	colQuery := `
SELECT c.COLUMN_NAME,
       CASE
         WHEN c.DATA_TYPE IN ('VARCHAR', 'VARCHAR2', 'NVARCHAR', 'NVARCHAR2', 'CHAR', 'NCHAR', 'BINARY', 'VARBINARY') THEN
           c.DATA_TYPE || '(' || c.DATA_LENGTH || ')'
         WHEN c.DATA_TYPE IN ('NUMBER', 'NUMERIC', 'DECIMAL', 'DEC') AND c.DATA_PRECISION IS NOT NULL THEN
           CASE WHEN COALESCE(c.DATA_SCALE, 0) > 0
             THEN c.DATA_TYPE || '(' || c.DATA_PRECISION || ',' || c.DATA_SCALE || ')'
             ELSE c.DATA_TYPE || '(' || c.DATA_PRECISION || ')'
           END
         ELSE c.DATA_TYPE
       END AS col_type,
       c.NULLABLE,
       c.DATA_DEFAULT,
       CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END AS is_pk,
       COALESCE(cc.COMMENTS, ''),
       c.COLUMN_ID
FROM ALL_TAB_COLUMNS c
LEFT JOIN ALL_COL_COMMENTS cc
  ON cc.OWNER = c.OWNER AND cc.TABLE_NAME = c.TABLE_NAME AND cc.COLUMN_NAME = c.COLUMN_NAME
LEFT JOIN (
  SELECT acc.OWNER, acc.TABLE_NAME, acc.COLUMN_NAME
  FROM ALL_CONSTRAINTS ac
  JOIN ALL_CONS_COLUMNS acc ON acc.OWNER = ac.OWNER AND acc.CONSTRAINT_NAME = ac.CONSTRAINT_NAME
  WHERE ac.CONSTRAINT_TYPE = 'P'
) pk ON pk.OWNER = c.OWNER AND pk.TABLE_NAME = c.TABLE_NAME AND pk.COLUMN_NAME = c.COLUMN_NAME
WHERE c.OWNER = ? AND c.TABLE_NAME = ?
ORDER BY c.COLUMN_ID`
	rows, err := conn.QueryContext(ctx, colQuery, schema, table)
	if err != nil {
		return ts, err
	}
	for rows.Next() {
		var col ColumnDef
		var nullable string
		var defVal sql.NullString
		var isPK int
		if err := rows.Scan(&col.Name, &col.Type, &nullable, &defVal, &isPK, &col.Comment, &col.Ordinal); err != nil {
			_ = rows.Close()
			return ts, err
		}
		col.Nullable = strings.EqualFold(nullable, "Y")
		col.PrimaryKey = isPK == 1
		if defVal.Valid {
			trimmed := strings.TrimSpace(defVal.String)
			col.DefaultValue = &trimmed
		}
		ts.Columns = append(ts.Columns, col)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return ts, err
	}
	_ = rows.Close()
	if len(ts.Columns) == 0 {
		return ts, fmt.Errorf("table %s.%s not found", schema, table)
	}

	// Best-effort identity column detection via SYSCOLUMNS
	identQ := `
SELECT col.NAME
FROM SYS.SYSCOLUMNS col
JOIN SYS.SYSOBJECTS tab ON tab.ID = col.ID AND tab.TYPE$ = 'SCHOBJ'
JOIN SYS.SYSOBJECTS sch ON sch.ID = tab.SCHID AND sch.TYPE$ = 'SCH'
WHERE sch.NAME = ? AND tab.NAME = ? AND (col.INFO2 & 1) = 1`
	if idRows, idErr := conn.QueryContext(ctx, identQ, schema, table); idErr == nil {
		identCols := map[string]bool{}
		for idRows.Next() {
			var name string
			if idRows.Scan(&name) == nil {
				identCols[name] = true
			}
		}
		_ = idRows.Close()
		for i := range ts.Columns {
			if identCols[ts.Columns[i].Name] {
				ts.Columns[i].AutoIncrement = true
			}
		}
	}

	idxQuery := `
SELECT i.INDEX_NAME, i.UNIQUENESS, ic.COLUMN_NAME
FROM ALL_INDEXES i
JOIN ALL_IND_COLUMNS ic ON ic.INDEX_OWNER = i.OWNER AND ic.INDEX_NAME = i.INDEX_NAME
WHERE i.TABLE_OWNER = ? AND i.TABLE_NAME = ?
ORDER BY i.INDEX_NAME, ic.COLUMN_POSITION`
	if idxRows, idxErr := conn.QueryContext(ctx, idxQuery, schema, table); idxErr == nil {
		idxMap := map[string]*IndexDef{}
		var idxOrder []string
		for idxRows.Next() {
			var name, uniq, colName string
			if idxRows.Scan(&name, &uniq, &colName) == nil {
				entry, ok := idxMap[name]
				if !ok {
					entry = &IndexDef{
						Name:   name,
						Unique: strings.EqualFold(uniq, "UNIQUE"),
					}
					idxMap[name] = entry
					idxOrder = append(idxOrder, name)
				}
				entry.Columns = append(entry.Columns, colName)
			}
		}
		_ = idxRows.Close()
		for _, name := range idxOrder {
			ts.Indexes = append(ts.Indexes, *idxMap[name])
		}
	}

	fkQuery := `
SELECT ac.CONSTRAINT_NAME,
       acc.COLUMN_NAME,
       rac.OWNER,
       rac.TABLE_NAME,
       racc.COLUMN_NAME,
       COALESCE(ac.DELETE_RULE, '')
FROM ALL_CONSTRAINTS ac
JOIN ALL_CONS_COLUMNS acc ON acc.OWNER = ac.OWNER AND acc.CONSTRAINT_NAME = ac.CONSTRAINT_NAME
JOIN ALL_CONSTRAINTS rac ON rac.OWNER = ac.R_OWNER AND rac.CONSTRAINT_NAME = ac.R_CONSTRAINT_NAME
JOIN ALL_CONS_COLUMNS racc ON racc.OWNER = rac.OWNER AND racc.CONSTRAINT_NAME = rac.CONSTRAINT_NAME AND racc.POSITION = acc.POSITION
WHERE ac.CONSTRAINT_TYPE = 'R' AND ac.OWNER = ? AND ac.TABLE_NAME = ?
ORDER BY ac.CONSTRAINT_NAME, acc.POSITION`
	if fkRows, fkErr := conn.QueryContext(ctx, fkQuery, schema, table); fkErr == nil {
		fkMap := map[string]*ForeignKeyDef{}
		var fkOrder []string
		for fkRows.Next() {
			var name, col, refSchema, refTable, refCol, delRule string
			if fkRows.Scan(&name, &col, &refSchema, &refTable, &refCol, &delRule) == nil {
				fk, ok := fkMap[name]
				if !ok {
					fk = &ForeignKeyDef{
						Name:      name,
						RefSchema: refSchema,
						RefTable:  refTable,
						OnDelete:  delRule,
					}
					fkMap[name] = fk
					fkOrder = append(fkOrder, name)
				}
				fk.Columns = append(fk.Columns, col)
				fk.RefColumns = append(fk.RefColumns, refCol)
			}
		}
		_ = fkRows.Close()
		for _, name := range fkOrder {
			ts.ForeignKeys = append(ts.ForeignKeys, *fkMap[name])
		}
	}

	ts.DDL = BuildCreateTableSQL(d, ts)
	return ts, nil
}

func (d *damengDialect) PaginateSelect(baseSelect string, orderBy string, limit, offset, _ int) (string, []any) {
	var sb strings.Builder
	sb.WriteString(baseSelect)
	if orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}
	sb.WriteString(" LIMIT ? OFFSET ?")
	return sb.String(), []any{limit, offset}
}
