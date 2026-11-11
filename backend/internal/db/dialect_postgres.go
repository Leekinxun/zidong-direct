package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type postgresDialect struct{}

func init() {
	RegisterDialect(&postgresDialect{})
}

func (d *postgresDialect) Engine() EngineType { return EnginePostgres }
func (d *postgresDialect) DriverName() string { return "pgx" }

func (d *postgresDialect) Capabilities() EngineCapabilities {
	return EngineCapabilities{
		Engine:              EnginePostgres,
		DefaultPort:         5432,
		SupportsSchemas:     true,
		SupportsSSH:         true,
		SupportsAlterColumn: true,
		SupportsForeignKeys: true,
		DataTypes: []string{
			"INTEGER", "BIGINT", "SMALLINT", "SERIAL", "BIGSERIAL",
			"VARCHAR(255)", "VARCHAR(100)", "TEXT", "CHAR(36)",
			"NUMERIC(18,2)", "DOUBLE PRECISION", "REAL", "BOOLEAN",
			"DATE", "TIMESTAMP", "TIMESTAMPTZ", "TIME",
			"UUID", "JSONB", "JSON", "BYTEA",
		},
	}
}

func (d *postgresDialect) QuoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func (d *postgresDialect) Placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

func (d *postgresDialect) BuildDSN(cfg ConnectionConfig, targetHost string, targetPort int, databaseOverride string) (string, error) {
	dbName := cfg.Database
	if databaseOverride != "" {
		dbName = databaseOverride
	}
	if dbName == "" {
		dbName = "postgres"
	}
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.Username, cfg.Password),
		Host:   net.JoinHostPort(targetHost, strconv.Itoa(targetPort)),
		Path:   "/" + dbName,
	}
	q := u.Query()
	q.Set("sslmode", sslMode)
	q.Set("connect_timeout", "10")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (d *postgresDialect) ListDatabases(ctx context.Context, conn *sql.DB, _ ConnectionConfig) ([]string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname")
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

func (d *postgresDialect) ListSchemas(ctx context.Context, conn *sql.DB, _ ConnectionConfig, _ string) ([]string, error) {
	q := `
SELECT schema_name
FROM information_schema.schemata
WHERE schema_name NOT LIKE 'pg_%' AND schema_name <> 'information_schema'
ORDER BY CASE WHEN schema_name = 'public' THEN 0 ELSE 1 END, schema_name`
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

func (d *postgresDialect) ListObjects(ctx context.Context, conn *sql.DB, _, schema string) ([]DatabaseObject, error) {
	if schema == "" {
		schema = "public"
	}
	q := `
SELECT c.relname,
       CASE c.relkind
         WHEN 'r' THEN 'TABLE'
         WHEN 'p' THEN 'TABLE'
         WHEN 'v' THEN 'VIEW'
         WHEN 'm' THEN 'MATERIALIZED VIEW'
         ELSE 'TABLE'
       END AS obj_type,
       c.reltuples::bigint,
       COALESCE(obj_description(c.oid, 'pg_class'), '')
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p', 'v', 'm')
ORDER BY obj_type, c.relname`
	rows, err := conn.QueryContext(ctx, q, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DatabaseObject
	for rows.Next() {
		var name, objType, comment string
		var est sql.NullInt64
		if err := rows.Scan(&name, &objType, &est, &comment); err != nil {
			return nil, err
		}
		var estPtr *int64
		if est.Valid && est.Int64 >= 0 {
			v := est.Int64
			estPtr = &v
		}
		out = append(out, DatabaseObject{
			Name:        name,
			Type:        objType,
			Schema:      schema,
			RowEstimate: estPtr,
			Comment:     comment,
		})
	}
	return out, rows.Err()
}

func (d *postgresDialect) DescribeTable(ctx context.Context, conn *sql.DB, database, schema, table string) (TableSchema, error) {
	if schema == "" {
		schema = "public"
	}
	ts := TableSchema{
		Database: database,
		Schema:   schema,
		Table:    table,
	}

	colQuery := `
SELECT a.attname,
       pg_catalog.format_type(a.atttypid, a.atttypmod) AS data_type,
       NOT a.attnotnull AS is_nullable,
       pg_get_expr(ad.adbin, ad.adrelid) AS default_val,
       COALESCE(to_jsonb(a)->>'attgenerated', '') AS att_generated,
       COALESCE(pk.is_pk, false) AS is_pk,
       (COALESCE(to_jsonb(a)->>'attidentity', '') <> '' OR COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '') LIKE 'nextval(%') AS is_auto,
       COALESCE(col_description(c.oid, a.attnum), '') AS comment,
       a.attnum
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
LEFT JOIN (
  SELECT i.indrelid, unnest(i.indkey) AS attnum, true AS is_pk
  FROM pg_index i
  WHERE i.indisprimary
) pk ON pk.indrelid = a.attrelid AND pk.attnum = a.attnum
WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`
	rows, err := conn.QueryContext(ctx, colQuery, schema, table)
	if err != nil {
		return ts, err
	}
	for rows.Next() {
		var col ColumnDef
		var defVal sql.NullString
		var attGenerated string
		if err := rows.Scan(&col.Name, &col.Type, &col.Nullable, &defVal, &attGenerated, &col.PrimaryKey, &col.AutoIncrement, &col.Comment, &col.Ordinal); err != nil {
			_ = rows.Close()
			return ts, err
		}
		if defVal.Valid {
			v := defVal.String
			switch strings.ToLower(strings.TrimSpace(attGenerated)) {
			case "s":
				col.GeneratedExpr = v
				col.GeneratedKind = "STORED"
			case "v":
				col.GeneratedExpr = v
				col.GeneratedKind = "VIRTUAL"
			default:
				col.DefaultValue = &v
			}
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
SELECT ic.relname AS index_name,
       ix.indisunique,
       ix.indisprimary,
       a.attname
FROM pg_index ix
JOIN pg_class t ON t.oid = ix.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
JOIN pg_class ic ON ic.oid = ix.indexrelid
JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
WHERE n.nspname = $1 AND t.relname = $2
ORDER BY ic.relname, k.ord`
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
SELECT con.conname,
       a.attname,
       rn.nspname,
       rt.relname,
       ra.attname,
       CASE con.confdeltype WHEN 'a' THEN 'NO ACTION' WHEN 'r' THEN 'RESTRICT' WHEN 'c' THEN 'CASCADE' WHEN 'n' THEN 'SET NULL' WHEN 'd' THEN 'SET DEFAULT' ELSE '' END,
       CASE con.confupdtype WHEN 'a' THEN 'NO ACTION' WHEN 'r' THEN 'RESTRICT' WHEN 'c' THEN 'CASCADE' WHEN 'n' THEN 'SET NULL' WHEN 'd' THEN 'SET DEFAULT' ELSE '' END
FROM pg_constraint con
JOIN pg_class t ON t.oid = con.conrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
JOIN pg_class rt ON rt.oid = con.confrelid
JOIN pg_namespace rn ON rn.oid = rt.relnamespace
JOIN LATERAL unnest(con.conkey, con.confkey) WITH ORDINALITY AS k(colnum, refnum, ord) ON true
JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.colnum
JOIN pg_attribute ra ON ra.attrelid = rt.oid AND ra.attnum = k.refnum
WHERE con.contype = 'f' AND n.nspname = $1 AND t.relname = $2
ORDER BY con.conname, k.ord`
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
						OnDelete:  onDel,
						OnUpdate:  onUpd,
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

func (d *postgresDialect) PaginateSelect(baseSelect string, orderBy string, limit, offset, startParamIdx int) (string, []any) {
	var sb strings.Builder
	sb.WriteString(baseSelect)
	if orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}
	sb.WriteString(fmt.Sprintf(" LIMIT %s OFFSET %s", d.Placeholder(startParamIdx), d.Placeholder(startParamIdx+1)))
	return sb.String(), []any{limit, offset}
}
