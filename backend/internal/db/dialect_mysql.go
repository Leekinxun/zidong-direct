package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type mysqlDialect struct{}

func init() {
	RegisterDialect(&mysqlDialect{})
}

func (d *mysqlDialect) Engine() EngineType { return EngineMySQL }
func (d *mysqlDialect) DriverName() string { return "mysql" }

func (d *mysqlDialect) Capabilities() EngineCapabilities {
	return EngineCapabilities{
		Engine:              EngineMySQL,
		DefaultPort:         3306,
		SupportsSchemas:     false,
		SupportsSSH:         true,
		SupportsAlterColumn: true,
		SupportsForeignKeys: true,
		DataTypes: []string{
			"INT", "BIGINT", "TINYINT", "SMALLINT",
			"VARCHAR(255)", "VARCHAR(100)", "CHAR(36)", "TEXT", "MEDIUMTEXT", "LONGTEXT",
			"DECIMAL(18,2)", "DOUBLE", "FLOAT", "BOOLEAN",
			"DATE", "DATETIME", "TIMESTAMP", "TIME",
			"JSON", "BLOB", "LONGBLOB",
		},
	}
}

func (d *mysqlDialect) QuoteIdent(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}

func (d *mysqlDialect) Placeholder(_ int) string { return "?" }

func (d *mysqlDialect) BuildDSN(cfg ConnectionConfig, targetHost string, targetPort int, databaseOverride string) (string, error) {
	c := mysql.NewConfig()
	c.User = cfg.Username
	c.Passwd = cfg.Password
	c.Net = "tcp"
	c.Addr = net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	if databaseOverride != "" {
		c.DBName = databaseOverride
	} else {
		c.DBName = cfg.Database
	}
	c.ParseTime = true
	c.Timeout = 10 * time.Second
	c.ReadTimeout = 60 * time.Second
	c.WriteTimeout = 60 * time.Second
	c.Params = map[string]string{"charset": "utf8mb4"}

	switch strings.ToLower(cfg.SSLMode) {
	case "prefer":
		c.TLSConfig = "preferred"
	case "require":
		c.TLSConfig = "skip-verify"
	case "verify-full":
		c.TLSConfig = "true"
	}
	return c.FormatDSN(), nil
}

func (d *mysqlDialect) ListDatabases(ctx context.Context, conn *sql.DB, _ ConnectionConfig) ([]string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
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

func (d *mysqlDialect) ListSchemas(_ context.Context, _ *sql.DB, _ ConnectionConfig, _ string) ([]string, error) {
	return []string{}, nil
}

func (d *mysqlDialect) ListObjects(ctx context.Context, conn *sql.DB, database, _ string) ([]DatabaseObject, error) {
	q := `
SELECT TABLE_NAME, TABLE_TYPE, COALESCE(ENGINE, ''), TABLE_ROWS,
       CASE WHEN TABLE_TYPE = 'VIEW' THEN '' ELSE COALESCE(TABLE_COMMENT, '') END
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = ?
ORDER BY TABLE_TYPE, TABLE_NAME`
	rows, err := conn.QueryContext(ctx, q, database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DatabaseObject
	for rows.Next() {
		var name, tblType, engine, comment string
		var rowEst sql.NullInt64
		if err := rows.Scan(&name, &tblType, &engine, &rowEst, &comment); err != nil {
			return nil, err
		}
		objType := "TABLE"
		if strings.Contains(strings.ToUpper(tblType), "VIEW") {
			objType = "VIEW"
		}
		var estPtr *int64
		if rowEst.Valid {
			v := rowEst.Int64
			estPtr = &v
		}
		out = append(out, DatabaseObject{
			Name:        name,
			Type:        objType,
			RowEstimate: estPtr,
			Engine:      engine,
			Comment:     comment,
		})
	}
	return out, rows.Err()
}

func (d *mysqlDialect) DescribeTable(ctx context.Context, conn *sql.DB, database, _, table string) (TableSchema, error) {
	schema := TableSchema{
		Database: database,
		Table:    table,
	}

	colQuery := `
SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY, EXTRA, COALESCE(COLUMN_COMMENT, ''), ORDINAL_POSITION
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
ORDER BY ORDINAL_POSITION`
	rows, err := conn.QueryContext(ctx, colQuery, database, table)
	if err != nil {
		return schema, err
	}
	for rows.Next() {
		var col ColumnDef
		var nullable, colKey, extra string
		var defVal sql.NullString
		if err := rows.Scan(&col.Name, &col.Type, &nullable, &defVal, &colKey, &extra, &col.Comment, &col.Ordinal); err != nil {
			_ = rows.Close()
			return schema, err
		}
		col.Nullable = strings.EqualFold(nullable, "YES")
		col.PrimaryKey = strings.EqualFold(colKey, "PRI")
		col.AutoIncrement = strings.Contains(strings.ToLower(extra), "auto_increment")
		if defVal.Valid {
			v := defVal.String
			col.DefaultValue = &v
		}
		schema.Columns = append(schema.Columns, col)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return schema, err
	}
	_ = rows.Close()

	if len(schema.Columns) == 0 {
		return schema, fmt.Errorf("table %s.%s not found", database, table)
	}

	idxQuery := `
SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
ORDER BY INDEX_NAME, SEQ_IN_INDEX`
	idxRows, err := conn.QueryContext(ctx, idxQuery, database, table)
	if err == nil {
		idxMap := map[string]*IndexDef{}
		var idxOrder []string
		for idxRows.Next() {
			var idxName, colName string
			var nonUnique int
			if scanErr := idxRows.Scan(&idxName, &nonUnique, &colName); scanErr == nil {
				entry, ok := idxMap[idxName]
				if !ok {
					entry = &IndexDef{
						Name:    idxName,
						Unique:  nonUnique == 0,
						Primary: strings.EqualFold(idxName, "PRIMARY"),
					}
					idxMap[idxName] = entry
					idxOrder = append(idxOrder, idxName)
				}
				entry.Columns = append(entry.Columns, colName)
			}
		}
		_ = idxRows.Close()
		for _, name := range idxOrder {
			schema.Indexes = append(schema.Indexes, *idxMap[name])
		}
	}

	fkQuery := `
SELECT kcu.CONSTRAINT_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_TABLE_SCHEMA, kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME,
       COALESCE(rc.DELETE_RULE, ''), COALESCE(rc.UPDATE_RULE, '')
FROM information_schema.KEY_COLUMN_USAGE kcu
LEFT JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
  ON rc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA
 AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
 AND rc.TABLE_NAME = kcu.TABLE_NAME
WHERE kcu.TABLE_SCHEMA = ? AND kcu.TABLE_NAME = ? AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY kcu.CONSTRAINT_NAME, kcu.ORDINAL_POSITION`
	fkRows, err := conn.QueryContext(ctx, fkQuery, database, table)
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
			schema.ForeignKeys = append(schema.ForeignKeys, *fkMap[name])
		}
	}

	var tblName, createSQL string
	showQ := "SHOW CREATE TABLE " + QualifiedTable(d, database, "", table)
	if err := conn.QueryRowContext(ctx, showQ).Scan(&tblName, &createSQL); err == nil {
		schema.DDL = createSQL
	}

	return schema, nil
}

func (d *mysqlDialect) PaginateSelect(baseSelect string, orderBy string, limit, offset, _ int) (string, []any) {
	var sb strings.Builder
	sb.WriteString(baseSelect)
	if orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}
	sb.WriteString(" LIMIT ? OFFSET ?")
	return sb.String(), []any{limit, offset}
}
