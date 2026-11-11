package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
)

type mssqlDialect struct{}

func init() {
	RegisterDialect(&mssqlDialect{})
}

func (d *mssqlDialect) Engine() EngineType { return EngineSQLServer }
func (d *mssqlDialect) DriverName() string { return "sqlserver" }

func (d *mssqlDialect) Capabilities() EngineCapabilities {
	return EngineCapabilities{
		Engine:              EngineSQLServer,
		DefaultPort:         1433,
		SupportsSchemas:     true,
		SupportsSSH:         true,
		SupportsAlterColumn: true,
		SupportsForeignKeys: true,
		DataTypes: []string{
			"INT", "BIGINT", "SMALLINT", "TINYINT",
			"NVARCHAR(255)", "NVARCHAR(100)", "NVARCHAR(MAX)", "VARCHAR(255)", "CHAR(36)",
			"DECIMAL(18,2)", "FLOAT", "BIT",
			"DATE", "DATETIME2", "DATETIMEOFFSET", "TIME",
			"UNIQUEIDENTIFIER", "VARBINARY(MAX)",
		},
	}
}

func (d *mssqlDialect) QuoteIdent(ident string) string {
	return "[" + strings.ReplaceAll(ident, "]", "]]") + "]"
}

func (d *mssqlDialect) Placeholder(index int) string {
	return fmt.Sprintf("@p%d", index)
}

func (d *mssqlDialect) BuildDSN(cfg ConnectionConfig, targetHost string, targetPort int, databaseOverride string) (string, error) {
	dbName := cfg.Database
	if databaseOverride != "" {
		dbName = databaseOverride
	}
	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(cfg.Username, cfg.Password),
		Host:   net.JoinHostPort(targetHost, strconv.Itoa(targetPort)),
	}
	q := u.Query()
	if dbName != "" {
		q.Set("database", dbName)
	}
	encrypt := "disable"
	if strings.EqualFold(cfg.SSLMode, "require") || strings.EqualFold(cfg.SSLMode, "verify-full") || strings.EqualFold(cfg.SSLMode, "true") {
		encrypt = "true"
	}
	q.Set("encrypt", encrypt)
	if cfg.TrustServerCer || encrypt == "disable" {
		q.Set("TrustServerCertificate", "true")
	}
	q.Set("connection timeout", "10")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (d *mssqlDialect) ListDatabases(ctx context.Context, conn *sql.DB, _ ConnectionConfig) ([]string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT name FROM sys.databases WHERE state = 0 ORDER BY name")
	if err != nil {
		return nil, err
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

func (d *mssqlDialect) ListSchemas(ctx context.Context, conn *sql.DB, _ ConnectionConfig, _ string) ([]string, error) {
	q := `
SELECT name
FROM sys.schemas
WHERE name NOT IN ('sys', 'INFORMATION_SCHEMA', 'guest') AND name NOT LIKE 'db_%'
ORDER BY CASE WHEN name = 'dbo' THEN 0 ELSE 1 END, name`
	rows, err := conn.QueryContext(ctx, q)
	if err != nil {
		return nil, err
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

func (d *mssqlDialect) ListObjects(ctx context.Context, conn *sql.DB, _, schema string) ([]DatabaseObject, error) {
	if schema == "" {
		schema = "dbo"
	}
	q := `
SELECT o.name,
       CASE o.type WHEN 'U' THEN 'TABLE' WHEN 'V' THEN 'VIEW' ELSE 'TABLE' END AS obj_type,
       COALESCE(SUM(CASE WHEN p.index_id IN (0,1) THEN p.rows ELSE 0 END), 0) AS row_est
FROM sys.objects o
JOIN sys.schemas s ON s.schema_id = o.schema_id
LEFT JOIN sys.partitions p ON p.object_id = o.object_id
WHERE s.name = @p1 AND o.type IN ('U', 'V') AND o.is_ms_shipped = 0
GROUP BY o.name, o.type
ORDER BY obj_type, o.name`
	rows, err := conn.QueryContext(ctx, q, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DatabaseObject
	for rows.Next() {
		var name, objType string
		var est int64
		if err := rows.Scan(&name, &objType, &est); err != nil {
			return nil, err
		}
		estCopy := est
		out = append(out, DatabaseObject{
			Name:        name,
			Type:        objType,
			Schema:      schema,
			RowEstimate: &estCopy,
		})
	}
	return out, rows.Err()
}

func (d *mssqlDialect) DescribeTable(ctx context.Context, conn *sql.DB, database, schema, table string) (TableSchema, error) {
	if schema == "" {
		schema = "dbo"
	}
	ts := TableSchema{
		Database: database,
		Schema:   schema,
		Table:    table,
	}

	colQuery := `
SELECT c.name,
       CASE
         WHEN t.name IN ('varchar', 'nvarchar', 'char', 'nchar', 'varbinary', 'binary') THEN
           UPPER(t.name) + '(' + CASE WHEN c.max_length = -1 THEN 'MAX'
             WHEN t.name LIKE 'n%' THEN CAST(c.max_length / 2 AS VARCHAR(10))
             ELSE CAST(c.max_length AS VARCHAR(10)) END + ')'
         WHEN t.name IN ('decimal', 'numeric') THEN
           UPPER(t.name) + '(' + CAST(c.precision AS VARCHAR(10)) + ',' + CAST(c.scale AS VARCHAR(10)) + ')'
         ELSE UPPER(t.name)
       END AS data_type,
       c.is_nullable,
       dc.definition,
       CASE WHEN pkc.column_id IS NOT NULL THEN 1 ELSE 0 END AS is_pk,
       c.is_identity,
       c.column_id
FROM sys.columns c
JOIN sys.objects o ON o.object_id = c.object_id
JOIN sys.schemas s ON s.schema_id = o.schema_id
JOIN sys.types t ON t.user_type_id = c.user_type_id
LEFT JOIN sys.default_constraints dc ON dc.parent_object_id = c.object_id AND dc.parent_column_id = c.column_id
LEFT JOIN (
  SELECT ic.object_id, ic.column_id
  FROM sys.indexes i
  JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
  WHERE i.is_primary_key = 1
) pkc ON pkc.object_id = c.object_id AND pkc.column_id = c.column_id
WHERE s.name = @p1 AND o.name = @p2
ORDER BY c.column_id`
	rows, err := conn.QueryContext(ctx, colQuery, schema, table)
	if err != nil {
		return ts, err
	}
	for rows.Next() {
		var col ColumnDef
		var defVal sql.NullString
		var isPK int
		if err := rows.Scan(&col.Name, &col.Type, &col.Nullable, &defVal, &isPK, &col.AutoIncrement, &col.Ordinal); err != nil {
			_ = rows.Close()
			return ts, err
		}
		col.PrimaryKey = isPK == 1
		if defVal.Valid {
			v := defVal.String
			col.DefaultValue = &v
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

	idxQuery := `
SELECT i.name, i.is_unique, i.is_primary_key, c.name
FROM sys.indexes i
JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
JOIN sys.objects o ON o.object_id = i.object_id
JOIN sys.schemas s ON s.schema_id = o.schema_id
WHERE s.name = @p1 AND o.name = @p2 AND i.name IS NOT NULL AND ic.is_included_column = 0
ORDER BY i.name, ic.key_ordinal`
	idxRows, err := conn.QueryContext(ctx, idxQuery, schema, table)
	if err == nil {
		idxMap := map[string]*IndexDef{}
		var idxOrder []string
		for idxRows.Next() {
			var name, colName string
			var uniq, pri bool
			if scanErr := idxRows.Scan(&name, &uniq, &pri, &colName); scanErr == nil {
				entry, ok := idxMap[name]
				if !ok {
					entry = &IndexDef{Name: name, Unique: uniq, Primary: pri}
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
SELECT fk.name,
       pc.name,
       rs.name,
       rt.name,
       rc.name,
       fk.delete_referential_action_desc,
       fk.update_referential_action_desc
FROM sys.foreign_keys fk
JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
JOIN sys.objects po ON po.object_id = fk.parent_object_id
JOIN sys.schemas ps ON ps.schema_id = po.schema_id
JOIN sys.columns pc ON pc.object_id = po.object_id AND pc.column_id = fkc.parent_column_id
JOIN sys.objects rt ON rt.object_id = fk.referenced_object_id
JOIN sys.schemas rs ON rs.schema_id = rt.schema_id
JOIN sys.columns rc ON rc.object_id = rt.object_id AND rc.column_id = fkc.referenced_column_id
WHERE ps.name = @p1 AND po.name = @p2
ORDER BY fk.name, fkc.constraint_column_id`
	fkRows, err := conn.QueryContext(ctx, fkQuery, schema, table)
	if err == nil {
		fkMap := map[string]*ForeignKeyDef{}
		var fkOrder []string
		for fkRows.Next() {
			var name, col, refSchema, refTable, refCol, onDel, onUpd string
			if scanErr := fkRows.Scan(&name, &col, &refSchema, &refTable, &refCol, &onDel, &onUpd); scanErr == nil {
				fk, ok := fkMap[name]
				if !ok {
					fk = &ForeignKeyDef{
						Name:      name,
						RefSchema: refSchema,
						RefTable:  refTable,
						OnDelete:  strings.ReplaceAll(onDel, "_", " "),
						OnUpdate:  strings.ReplaceAll(onUpd, "_", " "),
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

func (d *mssqlDialect) PaginateSelect(baseSelect string, orderBy string, limit, offset, startParamIdx int) (string, []any) {
	if orderBy == "" {
		orderBy = "(SELECT NULL)"
	}
	var sb strings.Builder
	sb.WriteString(baseSelect)
	sb.WriteString(" ORDER BY ")
	sb.WriteString(orderBy)
	sb.WriteString(fmt.Sprintf(" OFFSET %s ROWS FETCH NEXT %s ROWS ONLY", d.Placeholder(startParamIdx), d.Placeholder(startParamIdx+1)))
	return sb.String(), []any{offset, limit}
}
