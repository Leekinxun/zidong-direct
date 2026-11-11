package db

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	dm "gitee.com/chunanyong/dm"
)

type Dialect interface {
	Engine() EngineType
	DriverName() string
	Capabilities() EngineCapabilities
	QuoteIdent(ident string) string
	Placeholder(index int) string
	BuildDSN(cfg ConnectionConfig, targetHost string, targetPort int, databaseOverride string) (string, error)
	ListDatabases(ctx context.Context, conn *sql.DB, cfg ConnectionConfig) ([]string, error)
	ListSchemas(ctx context.Context, conn *sql.DB, cfg ConnectionConfig, database string) ([]string, error)
	ListObjects(ctx context.Context, conn *sql.DB, database, schema string) ([]DatabaseObject, error)
	DescribeTable(ctx context.Context, conn *sql.DB, database, schema, table string) (TableSchema, error)
	PaginateSelect(baseSelect string, orderBy string, limit, offset, startParamIdx int) (string, []any)
}

var registry = map[EngineType]Dialect{}

func RegisterDialect(d Dialect) {
	registry[d.Engine()] = d
}

func GetDialect(engine EngineType) (Dialect, error) {
	d, ok := registry[engine]
	if !ok {
		return nil, fmt.Errorf("unsupported database engine %q", engine)
	}
	return d, nil
}

func AllCapabilities() []EngineCapabilities {
	order := []EngineType{EngineMySQL, EnginePostgres, EngineSQLite, EngineSQLServer, EngineOracle, EngineDameng}
	out := make([]EngineCapabilities, 0, len(order))
	for _, e := range order {
		if d, ok := registry[e]; ok {
			out = append(out, d.Capabilities())
		}
	}
	return out
}

func QualifiedTable(d Dialect, database, schema, table string) string {
	switch d.Engine() {
	case EngineMySQL:
		if database != "" {
			return d.QuoteIdent(database) + "." + d.QuoteIdent(table)
		}
		return d.QuoteIdent(table)
	case EnginePostgres, EngineSQLServer, EngineOracle, EngineDameng:
		if schema != "" {
			return d.QuoteIdent(schema) + "." + d.QuoteIdent(table)
		}
		return d.QuoteIdent(table)
	case EngineSQLite:
		if schema != "" && schema != "main" {
			return d.QuoteIdent(schema) + "." + d.QuoteIdent(table)
		}
		return d.QuoteIdent(table)
	default:
		return d.QuoteIdent(table)
	}
}

func ScanRows(rows *sql.Rows, maxRows int) (cols []ColumnMeta, data [][]any, truncated bool, err error) {
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, false, err
	}
	colNames, err := rows.Columns()
	if err != nil {
		return nil, nil, false, err
	}

	cols = make([]ColumnMeta, len(colNames))
	for i, name := range colNames {
		dbType := ""
		nullable := true
		if i < len(colTypes) && colTypes[i] != nil {
			dbType = colTypes[i].DatabaseTypeName()
			if n, ok := colTypes[i].Nullable(); ok {
				nullable = n
			}
		}
		cols[i] = ColumnMeta{
			Name:     name,
			Type:     dbType,
			Nullable: nullable,
		}
	}

	data = make([][]any, 0)
	rawVals := make([]any, len(colNames))
	ptrs := make([]any, len(colNames))
	for i := range rawVals {
		ptrs[i] = &rawVals[i]
	}

	for rows.Next() {
		if maxRows > 0 && len(data) >= maxRows {
			truncated = true
			break
		}
		for i := range rawVals {
			rawVals[i] = nil
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, false, err
		}
		row := make([]any, len(colNames))
		for i, v := range rawVals {
			row[i] = NormalizeCellValue(v, cols[i].Type)
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, err
	}
	return cols, data, truncated, nil
}

func NormalizeCellValue(v any, colType string) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case time.Time:
		if val.Nanosecond() == 0 {
			return val.Format("2006-01-02 15:04:05")
		}
		return val.Format(time.RFC3339Nano)
	case []byte:
		if isBinaryType(colType) || !utf8.Valid(val) {
			const maxHexBytes = 4096
			if len(val) > maxHexBytes {
				return fmt.Sprintf("[HEX %d bytes] %s...", len(val), hex.EncodeToString(val[:maxHexBytes]))
			}
			return "[HEX] " + hex.EncodeToString(val)
		}
		return string(val)
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return fmt.Sprintf("%v", val)
		}
		return val
	case float32:
		f := float64(val)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Sprintf("%v", val)
		}
		return f
	case int64, int32, int16, int8, int, uint64, uint32, uint16, uint8, uint, bool, string:
		return val
	case *dm.DmClob:
		if val == nil || !val.Valid {
			return nil
		}
		n, err := val.GetLength()
		if err != nil || n <= 0 {
			return ""
		}
		const maxClobChars = 65536
		readLen := int(n)
		if readLen > maxClobChars {
			readLen = maxClobChars
		}
		text, err := val.ReadString(1, readLen)
		if err != nil {
			return ""
		}
		if n > maxClobChars {
			return text + fmt.Sprintf("... (%d chars)", n)
		}
		return text
	case *dm.DmBlob:
		if val == nil || !val.Valid {
			return nil
		}
		n, err := val.GetLength()
		if err != nil || n <= 0 {
			return "[HEX] "
		}
		const maxBlobBytes = 4096
		readLen := int(n)
		if readLen > maxBlobBytes {
			readLen = maxBlobBytes
		}
		buf := make([]byte, readLen)
		got, err := val.ReadAt(0, buf)
		if err != nil && got == 0 {
			return fmt.Sprintf("[BLOB %d bytes]", n)
		}
		if n > maxBlobBytes {
			return fmt.Sprintf("[HEX %d bytes] %s...", n, hex.EncodeToString(buf[:got]))
		}
		return "[HEX] " + hex.EncodeToString(buf[:got])
	default:
		if s, ok := val.(fmt.Stringer); ok {
			return s.String()
		}
		return fmt.Sprintf("%v", val)
	}
}

func isBinaryType(colType string) bool {
	u := strings.ToUpper(strings.TrimSpace(colType))
	return strings.Contains(u, "BLOB") ||
		strings.Contains(u, "BYTEA") ||
		strings.Contains(u, "BINARY") ||
		strings.Contains(u, "IMAGE") ||
		u == "RAW" ||
		u == "LONG RAW" ||
		u == "UNIQUEIDENTIFIER"
}

var allowedOperators = map[string]string{
	"=":           "=",
	"!=":          "!=",
	"<>":          "!=",
	"LIKE":        "LIKE",
	">":           ">",
	">=":          ">=",
	"<":           "<",
	"<=":          "<=",
	"IS NULL":     "IS NULL",
	"IS NOT NULL": "IS NOT NULL",
}

func BuildWhereClause(d Dialect, cols []ColumnDef, filters []FilterCondition, startParamIdx int) (string, []any, int, error) {
	if len(filters) == 0 {
		return "", nil, startParamIdx, nil
	}
	validCols := make(map[string]ColumnDef, len(cols))
	for _, c := range cols {
		validCols[c.Name] = c
	}

	var parts []string
	var args []any
	idx := startParamIdx

	for _, f := range filters {
		colName := strings.TrimSpace(f.Column)
		if colName == "" {
			continue
		}
		if _, ok := validCols[colName]; !ok {
			return "", nil, idx, fmt.Errorf("unknown filter column %q", colName)
		}
		opKey := strings.ToUpper(strings.TrimSpace(f.Operator))
		opSQL, ok := allowedOperators[opKey]
		if !ok {
			return "", nil, idx, fmt.Errorf("unsupported filter operator %q", f.Operator)
		}

		quotedCol := d.QuoteIdent(colName)
		if opSQL == "IS NULL" || opSQL == "IS NOT NULL" {
			parts = append(parts, quotedCol+" "+opSQL)
			continue
		}
		if opSQL == "LIKE" && d.Engine() == EnginePostgres {
			parts = append(parts, "CAST("+quotedCol+" AS TEXT) ILIKE "+d.Placeholder(idx))
		} else {
			parts = append(parts, quotedCol+" "+opSQL+" "+d.Placeholder(idx))
		}
		args = append(args, f.Value)
		idx++
	}

	if len(parts) == 0 {
		return "", nil, idx, nil
	}
	return " WHERE " + strings.Join(parts, " AND "), args, idx, nil
}

func QueryTableData(ctx context.Context, conn *sql.DB, d Dialect, req TableDataRequest, maxLimit int) (QueryResult, error) {
	start := time.Now()
	schemaInfo, err := d.DescribeTable(ctx, conn, req.Database, req.Schema, req.Table)
	if err != nil {
		return QueryResult{}, err
	}

	validCols := make(map[string]ColumnDef, len(schemaInfo.Columns))
	var pkCols []string
	for _, c := range schemaInfo.Columns {
		validCols[c.Name] = c
		if c.PrimaryKey {
			pkCols = append(pkCols, c.Name)
		}
	}

	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	if maxLimit > 0 && pageSize > maxLimit {
		pageSize = maxLimit
	}
	offset := (page - 1) * pageSize

	whereSQL, whereArgs, nextIdx, err := BuildWhereClause(d, schemaInfo.Columns, req.Filters, 1)
	if err != nil {
		return QueryResult{}, err
	}

	orderBy := ""
	if strings.TrimSpace(req.SortColumn) != "" {
		if _, ok := validCols[req.SortColumn]; !ok {
			return QueryResult{}, fmt.Errorf("unknown sort column %q", req.SortColumn)
		}
		dir := "ASC"
		if strings.EqualFold(req.SortDir, "DESC") {
			dir = "DESC"
		}
		orderBy = d.QuoteIdent(req.SortColumn) + " " + dir
	} else if len(pkCols) > 0 {
		orderBy = d.QuoteIdent(pkCols[0]) + " ASC"
	}

	tableRef := QualifiedTable(d, req.Database, req.Schema, req.Table)

	countSQL := "SELECT COUNT(*) FROM " + tableRef + whereSQL
	var totalCount int64
	var totalPtr *int64
	if err := conn.QueryRowContext(ctx, countSQL, whereArgs...).Scan(&totalCount); err == nil {
		totalPtr = &totalCount
	}

	baseSelect := "SELECT * FROM " + tableRef + whereSQL
	pagedSQL, pageArgs := d.PaginateSelect(baseSelect, orderBy, pageSize, offset, nextIdx)
	allArgs := append(append([]any(nil), whereArgs...), pageArgs...)

	rows, err := conn.QueryContext(ctx, pagedSQL, allArgs...)
	if err != nil {
		return QueryResult{}, err
	}
	defer rows.Close()

	cols, data, truncated, err := ScanRows(rows, pageSize)
	if err != nil {
		return QueryResult{}, err
	}

	return QueryResult{
		Columns:     cols,
		Rows:        data,
		RowCount:    len(data),
		TotalCount:  totalPtr,
		DurationMs:  time.Since(start).Milliseconds(),
		Truncated:   truncated,
		Statement:   pagedSQL,
		PrimaryKeys: pkCols,
	}, nil
}

func ApplyRowMutations(ctx context.Context, conn *sql.DB, d Dialect, req RowMutationRequest) (MutationResult, error) {
	if len(req.Mutations) == 0 {
		return MutationResult{}, errors.New("no mutations provided")
	}

	schemaInfo, err := d.DescribeTable(ctx, conn, req.Database, req.Schema, req.Table)
	if err != nil {
		return MutationResult{}, err
	}

	validCols := make(map[string]ColumnDef, len(schemaInfo.Columns))
	var pkCols []string
	for _, c := range schemaInfo.Columns {
		validCols[c.Name] = c
		if c.PrimaryKey {
			pkCols = append(pkCols, c.Name)
		}
	}

	tableRef := QualifiedTable(d, req.Database, req.Schema, req.Table)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return MutationResult{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var totalAffected int64
	var stmts []string
	var warning string
	if len(pkCols) == 0 {
		warning = "当前表未检测到主键，更新/删除将按传入键值条件匹配，请确认目标行唯一。"
	}

	for _, m := range req.Mutations {
		var sqlText string
		var args []any

		switch m.Type {
		case "insert":
			if len(m.Values) == 0 {
				return MutationResult{}, errors.New("insert mutation requires values")
			}
			colNames := sortedMapKeys(m.Values)
			quotedCols := make([]string, 0, len(colNames))
			placeholders := make([]string, 0, len(colNames))
			idx := 1
			for _, name := range colNames {
				if _, ok := validCols[name]; !ok {
					return MutationResult{}, fmt.Errorf("unknown column %q", name)
				}
				quotedCols = append(quotedCols, d.QuoteIdent(name))
				placeholders = append(placeholders, d.Placeholder(idx))
				args = append(args, m.Values[name])
				idx++
			}
			sqlText = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
				tableRef, strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))

		case "update":
			if len(m.Values) == 0 {
				return MutationResult{}, errors.New("update mutation requires values")
			}
			if len(m.Keys) == 0 {
				return MutationResult{}, errors.New("update mutation requires identifying keys")
			}
			valNames := sortedMapKeys(m.Values)
			keyNames := selectKeyNames(pkCols, m.Keys)
			idx := 1
			var setParts []string
			for _, name := range valNames {
				if _, ok := validCols[name]; !ok {
					return MutationResult{}, fmt.Errorf("unknown column %q", name)
				}
				setParts = append(setParts, fmt.Sprintf("%s = %s", d.QuoteIdent(name), d.Placeholder(idx)))
				args = append(args, m.Values[name])
				idx++
			}
			whereParts, whereArgs, err := buildKeyWhere(d, validCols, keyNames, m.Keys, idx)
			if err != nil {
				return MutationResult{}, err
			}
			args = append(args, whereArgs...)
			sqlText = fmt.Sprintf("UPDATE %s SET %s WHERE %s",
				tableRef, strings.Join(setParts, ", "), strings.Join(whereParts, " AND "))

		case "delete":
			if len(m.Keys) == 0 {
				return MutationResult{}, errors.New("delete mutation requires identifying keys")
			}
			keyNames := selectKeyNames(pkCols, m.Keys)
			whereParts, whereArgs, err := buildKeyWhere(d, validCols, keyNames, m.Keys, 1)
			if err != nil {
				return MutationResult{}, err
			}
			args = append(args, whereArgs...)
			sqlText = fmt.Sprintf("DELETE FROM %s WHERE %s",
				tableRef, strings.Join(whereParts, " AND "))

		default:
			return MutationResult{}, fmt.Errorf("unsupported mutation type %q", m.Type)
		}

		res, err := tx.ExecContext(ctx, sqlText, args...)
		if err != nil {
			return MutationResult{}, fmt.Errorf("execute %s: %w", m.Type, err)
		}
		n, _ := res.RowsAffected()
		totalAffected += n
		stmts = append(stmts, sqlText)
	}

	if err := tx.Commit(); err != nil {
		return MutationResult{}, err
	}

	return MutationResult{
		AffectedRows: totalAffected,
		Statements:   stmts,
		Warning:      warning,
	}, nil
}

func selectKeyNames(pkCols []string, keys map[string]any) []string {
	if len(pkCols) > 0 {
		allPKPresent := true
		for _, pk := range pkCols {
			if _, ok := keys[pk]; !ok {
				allPKPresent = false
				break
			}
		}
		if allPKPresent {
			return pkCols
		}
	}
	return sortedMapKeys(keys)
}

func buildKeyWhere(d Dialect, validCols map[string]ColumnDef, keyNames []string, keys map[string]any, startIdx int) ([]string, []any, error) {
	var parts []string
	var args []any
	idx := startIdx
	for _, name := range keyNames {
		if _, ok := validCols[name]; !ok {
			return nil, nil, fmt.Errorf("unknown key column %q", name)
		}
		val := keys[name]
		if val == nil {
			parts = append(parts, d.QuoteIdent(name)+" IS NULL")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s = %s", d.QuoteIdent(name), d.Placeholder(idx)))
		args = append(args, val)
		idx++
	}
	if len(parts) == 0 {
		return nil, nil, errors.New("empty key condition")
	}
	return parts, args, nil
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
