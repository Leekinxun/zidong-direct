package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type sqliteDialect struct{}

func init() {
	RegisterDialect(&sqliteDialect{})
}

func (d *sqliteDialect) Engine() EngineType { return EngineSQLite }
func (d *sqliteDialect) DriverName() string { return "sqlite" }

func (d *sqliteDialect) Capabilities() EngineCapabilities {
	return EngineCapabilities{
		Engine:              EngineSQLite,
		DefaultPort:         0,
		SupportsSchemas:     false,
		SupportsSSH:         false,
		SupportsAlterColumn: false,
		SupportsForeignKeys: true,
		DataTypes: []string{
			"INTEGER", "TEXT", "REAL", "NUMERIC", "BLOB",
		},
	}
}

func (d *sqliteDialect) QuoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func (d *sqliteDialect) Placeholder(_ int) string { return "?" }

func (d *sqliteDialect) BuildDSN(cfg ConnectionConfig, _ string, _ int, _ string) (string, error) {
	if strings.TrimSpace(cfg.SQLitePath) == "" {
		return "", errors.New("SQLite file path is required")
	}
	u := sqliteFileURL(cfg.SQLitePath)
	return u.String() + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", nil
}

func sqliteFileURL(path string) url.URL {
	name := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	return url.URL{Scheme: "file", Path: name}
}

func (d *sqliteDialect) ListDatabases(_ context.Context, _ *sql.DB, _ ConnectionConfig) ([]string, error) {
	return []string{"main"}, nil
}

func (d *sqliteDialect) ListSchemas(_ context.Context, _ *sql.DB, _ ConnectionConfig, _ string) ([]string, error) {
	return []string{}, nil
}

func (d *sqliteDialect) ListObjects(ctx context.Context, conn *sql.DB, _, _ string) ([]DatabaseObject, error) {
	q := `
SELECT name, UPPER(type)
FROM sqlite_master
WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
ORDER BY type, name`
	rows, err := conn.QueryContext(ctx, q)
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
			Schema: "main",
		})
	}
	return out, rows.Err()
}

func (d *sqliteDialect) DescribeTable(ctx context.Context, conn *sql.DB, database, schema, table string) (TableSchema, error) {
	if schema == "" {
		schema = "main"
	}
	ts := TableSchema{
		Database: database,
		Schema:   schema,
		Table:    table,
	}

	var createSQL sql.NullString
	_ = conn.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type IN ('table', 'view') AND name = ?", table).Scan(&createSQL)
	if createSQL.Valid {
		ts.DDL = createSQL.String
	}
	hasAutoIncKeyword := strings.Contains(strings.ToUpper(ts.DDL), "AUTOINCREMENT")

	rows, err := conn.QueryContext(ctx, "PRAGMA table_info("+d.QuoteIdent(table)+")")
	if err != nil {
		return ts, err
	}
	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var defVal sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defVal, &pk); err != nil {
			_ = rows.Close()
			return ts, err
		}
		col := ColumnDef{
			Name:       name,
			Type:       colType,
			Nullable:   notNull == 0 && pk == 0,
			PrimaryKey: pk > 0,
			Ordinal:    cid + 1,
		}
		if defVal.Valid {
			v := defVal.String
			col.DefaultValue = &v
		}
		if pk > 0 && strings.EqualFold(strings.TrimSpace(colType), "INTEGER") && hasAutoIncKeyword {
			col.AutoIncrement = true
		}
		ts.Columns = append(ts.Columns, col)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return ts, err
	}
	_ = rows.Close()
	if len(ts.Columns) == 0 {
		return ts, fmt.Errorf("table %q not found", table)
	}

	idxListRows, err := conn.QueryContext(ctx, "PRAGMA index_list("+d.QuoteIdent(table)+")")
	if err == nil {
		type rawIdx struct {
			name   string
			unique int
			origin string
		}
		var rawIndexes []rawIdx
		for idxListRows.Next() {
			var seq, unique, partial int
			var name, origin string
			if scanErr := idxListRows.Scan(&seq, &name, &unique, &origin, &partial); scanErr == nil {
				rawIndexes = append(rawIndexes, rawIdx{name: name, unique: unique, origin: origin})
			}
		}
		_ = idxListRows.Close()

		for _, ri := range rawIndexes {
			infoRows, infoErr := conn.QueryContext(ctx, "PRAGMA index_info("+d.QuoteIdent(ri.name)+")")
			if infoErr != nil {
				continue
			}
			var cols []string
			for infoRows.Next() {
				var seqno, cid int
				var colName sql.NullString
				if scanErr := infoRows.Scan(&seqno, &cid, &colName); scanErr == nil && colName.Valid {
					cols = append(cols, colName.String)
				}
			}
			_ = infoRows.Close()
			ts.Indexes = append(ts.Indexes, IndexDef{
				Name:    ri.name,
				Columns: cols,
				Unique:  ri.unique != 0,
				Primary: ri.origin == "pk",
			})
		}
	}

	fkRows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_list("+d.QuoteIdent(table)+")")
	if err == nil {
		fkMap := map[int]*ForeignKeyDef{}
		var fkOrder []int
		for fkRows.Next() {
			var id, seq int
			var refTable, fromCol, toCol, onUpdate, onDelete, match string
			if scanErr := fkRows.Scan(&id, &seq, &refTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); scanErr == nil {
				fk, ok := fkMap[id]
				if !ok {
					fk = &ForeignKeyDef{
						Name:     fmt.Sprintf("fk_%s_%d", table, id),
						RefTable: refTable,
						OnDelete: onDelete,
						OnUpdate: onUpdate,
					}
					fkMap[id] = fk
					fkOrder = append(fkOrder, id)
				}
				fk.Columns = append(fk.Columns, fromCol)
				fk.RefColumns = append(fk.RefColumns, toCol)
			}
		}
		_ = fkRows.Close()
		for _, id := range fkOrder {
			ts.ForeignKeys = append(ts.ForeignKeys, *fkMap[id])
		}
	}

	return ts, nil
}

func (d *sqliteDialect) PaginateSelect(baseSelect string, orderBy string, limit, offset, _ int) (string, []any) {
	var sb strings.Builder
	sb.WriteString(baseSelect)
	if orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}
	sb.WriteString(" LIMIT ? OFFSET ?")
	return sb.String(), []any{limit, offset}
}
