package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func ReadDatabaseDetails(ctx context.Context, conn *sql.DB, engine EngineType, database string) (DatabaseDetails, error) {
	details := DatabaseDetails{Name: database}
	var query string
	args := []any{database}
	switch engine {
	case EnginePostgres:
		query = `SELECT pg_catalog.shobj_description(oid, 'pg_database')
FROM pg_catalog.pg_database WHERE datname = $1`
	case EngineSQLServer:
		// The connection is already scoped to the requested database.
		query = `SELECT CONVERT(nvarchar(4000), value) FROM sys.extended_properties
WHERE class = 0 AND major_id = 0 AND minor_id = 0 AND name = N'MS_Description'`
		args = nil
	case EngineMySQL:
		// MariaDB exposes SCHEMA_COMMENT. Checking the column first lets the
		// same connection type represent MySQL and older MariaDB releases.
		var available int
		err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = 'information_schema' AND TABLE_NAME = 'SCHEMATA' AND COLUMN_NAME = 'SCHEMA_COMMENT'`).Scan(&available)
		if err != nil || available == 0 {
			return details, err
		}
		query = `SELECT SCHEMA_COMMENT FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?`
	default:
		return details, nil
	}

	comment, err := queryComment(ctx, conn, query, args...)
	if err != nil {
		return details, err
	}
	details.Comment = *comment
	details.CommentSupported = true
	return details, nil
}

func ReadTableComment(ctx context.Context, conn *sql.DB, engine EngineType, table TableSchema) (*string, error) {
	var query string
	args := []any{table.Schema, table.Table}
	switch engine {
	case EngineMySQL:
		query = `SELECT CASE WHEN TABLE_TYPE = 'VIEW' THEN NULL ELSE TABLE_COMMENT END
FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`
		args[0] = table.Database
	case EnginePostgres:
		query = `SELECT pg_catalog.obj_description(c.oid, 'pg_class')
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2`
	case EngineSQLServer:
		query = `SELECT CONVERT(nvarchar(4000), ep.value)
FROM sys.objects o JOIN sys.schemas s ON s.schema_id = o.schema_id
JOIN sys.extended_properties ep ON ep.major_id = o.object_id
 AND ep.class = 1 AND ep.minor_id = 0 AND ep.name = N'MS_Description'
WHERE s.name = @p1 AND o.name = @p2`
	case EngineOracle:
		query = `SELECT comments FROM all_tab_comments WHERE owner = :1 AND table_name = :2`
	case EngineDameng:
		query = `SELECT COMMENTS FROM ALL_TAB_COMMENTS WHERE OWNER = ? AND TABLE_NAME = ?`
	default:
		return nil, nil
	}
	return queryComment(ctx, conn, query, args...)
}

func queryComment(ctx context.Context, conn *sql.DB, query string, args ...any) (*string, error) {
	var value sql.NullString
	err := conn.QueryRowContext(ctx, query, args...).Scan(&value)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return &value.String, nil
}

func BuildTableCommentStatement(d Dialect, database, schema, table, objectType, comment string) string {
	escaped := strings.ReplaceAll(comment, "'", "''")
	tableRef := QualifiedTable(d, database, schema, table)
	switch d.Engine() {
	case EngineMySQL:
		if strings.EqualFold(objectType, "VIEW") {
			return ""
		}
		return fmt.Sprintf("ALTER TABLE %s COMMENT = '%s'", tableRef, escaped)
	case EnginePostgres:
		kind := "TABLE"
		if strings.EqualFold(objectType, "VIEW") {
			kind = "VIEW"
		} else if strings.EqualFold(objectType, "MATERIALIZED VIEW") {
			kind = "MATERIALIZED VIEW"
		}
		return fmt.Sprintf("COMMENT ON %s %s IS '%s'", kind, tableRef, escaped)
	case EngineOracle, EngineDameng:
		return fmt.Sprintf("COMMENT ON TABLE %s IS '%s'", tableRef, escaped)
	case EngineSQLServer:
		sch := schema
		if strings.TrimSpace(sch) == "" {
			sch = "dbo"
		}
		lvl1 := "TABLE"
		if strings.EqualFold(objectType, "VIEW") {
			lvl1 = "VIEW"
		}
		escSch := strings.ReplaceAll(sch, "'", "''")
		escTbl := strings.ReplaceAll(table, "'", "''")
		return fmt.Sprintf(
			"IF EXISTS (SELECT 1 FROM sys.extended_properties ep JOIN sys.objects o ON ep.major_id = o.object_id JOIN sys.schemas s ON o.schema_id = s.schema_id WHERE ep.class = 1 AND ep.minor_id = 0 AND ep.name = N'MS_Description' AND s.name = N'%s' AND o.name = N'%s') EXEC sys.sp_updateextendedproperty @name=N'MS_Description', @value=N'%s', @level0type=N'SCHEMA', @level0name=N'%s', @level1type=N'%s', @level1name=N'%s' ELSE EXEC sys.sp_addextendedproperty @name=N'MS_Description', @value=N'%s', @level0type=N'SCHEMA', @level0name=N'%s', @level1type=N'%s', @level1name=N'%s'",
			escSch, escTbl, escaped, escSch, lvl1, escTbl, escaped, escSch, lvl1, escTbl,
		)
	default:
		return ""
	}
}

func UpdateDatabaseComment(ctx context.Context, conn *sql.DB, d Dialect, database, comment string) error {
	escaped := strings.ReplaceAll(comment, "'", "''")
	switch d.Engine() {
	case EnginePostgres:
		stmt := fmt.Sprintf("COMMENT ON DATABASE %s IS '%s'", d.QuoteIdent(database), escaped)
		_, err := conn.ExecContext(ctx, stmt)
		return err
	case EngineMySQL:
		stmt := fmt.Sprintf("ALTER DATABASE %s COMMENT = '%s'", d.QuoteIdent(database), escaped)
		_, err := conn.ExecContext(ctx, stmt)
		return err
	case EngineSQLServer:
		stmt := fmt.Sprintf(
			"IF EXISTS (SELECT 1 FROM sys.extended_properties WHERE class = 0 AND major_id = 0 AND minor_id = 0 AND name = N'MS_Description') EXEC sys.sp_updateextendedproperty @name=N'MS_Description', @value=N'%s' ELSE EXEC sys.sp_addextendedproperty @name=N'MS_Description', @value=N'%s'",
			escaped, escaped,
		)
		_, err := conn.ExecContext(ctx, stmt)
		return err
	default:
		return errors.New("当前数据库引擎不支持设置库级注释")
	}
}

func UpdateTableComment(ctx context.Context, conn *sql.DB, d Dialect, database, schema, table, objectType, comment string) error {
	stmt := BuildTableCommentStatement(d, database, schema, table, objectType, comment)
	if stmt == "" {
		return errors.New("当前数据库引擎或对象类型不支持设置注释")
	}
	_, err := conn.ExecContext(ctx, stmt)
	return err
}
