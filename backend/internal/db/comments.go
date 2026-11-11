package db

import (
	"context"
	"database/sql"
	"errors"
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
