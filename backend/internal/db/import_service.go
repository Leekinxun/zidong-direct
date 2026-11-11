package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type ImportPreview struct {
	Columns       []string             `json:"columns"`
	Rows          [][]any              `json:"rows"`
	TargetColumns []ImportTargetColumn `json:"targetColumns"`
	Sheets        []string             `json:"sheets,omitempty"`
	SourceDigest  string               `json:"sourceDigest,omitempty"`
	Sheet         string               `json:"sheet,omitempty"`
}

type ImportTargetColumn struct {
	Name       string `json:"name"`
	PrimaryKey bool   `json:"primaryKey"`
	Generated  bool   `json:"generated,omitempty"`
}

type ImportSummary struct {
	InsertedRows int64    `json:"insertedRows"`
	UpdatedRows  int64    `json:"updatedRows"`
	DeletedRows  int64    `json:"deletedRows"`
	SkippedRows  int64    `json:"skippedRows"`
	FailedRows   int64    `json:"failedRows"`
	Errors       []string `json:"errors,omitempty"`
}

func PreviewImport(ctx context.Context, conn *sql.DB, d Dialect, database, schema, table string, file io.ReaderAt, size int64, opts ImportOptions) (ImportPreview, error) {
	description, err := d.DescribeTable(ctx, conn, database, schema, table)
	if err != nil {
		return ImportPreview{}, err
	}
	src, err := openImportSource(ctx, file, size, opts)
	if err != nil {
		return ImportPreview{}, err
	}
	defer src.finish()
	result := ImportPreview{
		Columns:       append([]string{}, src.columns...),
		Rows:          make([][]any, 0),
		TargetColumns: make([]ImportTargetColumn, 0, len(description.Columns)),
		Sheets:        src.sheets, Sheet: src.sheet,
	}
	for _, column := range description.Columns {
		result.TargetColumns = append(result.TargetColumns, ImportTargetColumn{
			Name: column.Name, PrimaryKey: column.PrimaryKey, Generated: column.GeneratedExpr != "",
		})
	}
	for len(result.Rows) < 12 {
		if err := ctx.Err(); err != nil {
			return ImportPreview{}, err
		}
		row, err := src.read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ImportPreview{}, err
		}
		if len(row) > len(src.columns) {
			return ImportPreview{}, errors.New("源数据行的列数超过字段行")
		}
		preview := make([]any, len(src.columns))
		for i, value := range row {
			if _, missing := value.(missingImportValue); missing {
				value = "缺失"
			}
			if binary, ok := value.([]byte); ok {
				value = fmt.Sprintf("二进制（%d 字节）", len(binary))
			}
			if text, ok := value.(string); ok && len(text) > 256 {
				text = text[:256]
				for !utf8.ValidString(text) && len(text) > 0 {
					text = text[:len(text)-1]
				}
				value = text + "…"
			}
			preview[i] = value
		}
		result.Rows = append(result.Rows, preview)
	}
	return result, nil
}

func ImportRows(ctx context.Context, conn *sql.DB, d Dialect, database, schema, table string, file io.ReaderAt, size int64, opts ImportOptions) (ImportSummary, error) {
	mode := opts.Mode
	switch mode {
	case "":
		mode = "append"
	case "append", "update", "upsert", "skip", "delete", "replace":
	default:
		return ImportSummary{}, fmt.Errorf("未知的导入模式 %q", mode)
	}
	if mode == "replace" && opts.ContinueOnError {
		return ImportSummary{}, errors.New("清空并导入不能启用跳过失败行")
	}
	description, err := d.DescribeTable(ctx, conn, database, schema, table)
	if err != nil {
		return ImportSummary{}, err
	}
	src, err := openImportSource(ctx, file, size, opts)
	if err != nil {
		return ImportSummary{}, err
	}
	defer src.finish()
	row, err := src.read()
	if err == io.EOF {
		if mode == "replace" {
			return ImportSummary{}, errors.New("源文件无数据，未清空目标表")
		}
		return ImportSummary{}, nil
	}
	if err != nil {
		return ImportSummary{}, err
	}
	if description.Database != "" {
		database = description.Database
	}
	if description.Schema != "" {
		schema = description.Schema
	}
	if description.Table != "" {
		table = description.Table
	}
	plan, err := makeImportPlan(d, QualifiedTable(d, database, schema, table), mode, src.columns, description.Columns, opts.Mappings)
	if err != nil {
		return ImportSummary{}, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return ImportSummary{}, err
	}
	defer tx.Rollback()

	if d.Engine() == EngineMySQL && mode != "append" {
		if err := requireTransactionalImport(ctx, tx, plan.table, database, table); err != nil {
			return ImportSummary{}, err
		}
	}
	var summary ImportSummary
	if mode == "replace" {
		cleared, err := tx.ExecContext(ctx, "DELETE FROM "+plan.table)
		if err != nil {
			return ImportSummary{}, fmt.Errorf("清空目标表: %w", err)
		}
		summary.DeletedRows, _ = cleared.RowsAffected()
	}
	if err := plan.prepare(ctx, tx); err != nil {
		return ImportSummary{}, err
	}
	defer plan.close()

	for rowNumber := 1; ; rowNumber++ {
		if len(row) > len(src.columns) {
			return ImportSummary{}, fmt.Errorf("第 %d 条记录的列数超过字段行", rowNumber)
		}
		if err := ctx.Err(); err != nil {
			return ImportSummary{}, err
		}
		action, err := importOneRow(ctx, tx, plan, row, opts.EmptyAsNull, opts.ContinueOnError, d.Engine())
		if err != nil {
			return ImportSummary{}, fmt.Errorf("第 %d 条记录导入失败: %w", rowNumber, err)
		}
		switch action.kind {
		case "inserted":
			summary.InsertedRows++
		case "updated":
			summary.UpdatedRows++
		case "deleted":
			summary.DeletedRows++
		case "failed":
			summary.FailedRows++
			if len(summary.Errors) < 20 {
				summary.Errors = append(summary.Errors, fmt.Sprintf("第 %d 条: %v", rowNumber, action.err))
			}
		default:
			summary.SkippedRows++
		}
		row, err = src.read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ImportSummary{}, fmt.Errorf("读取第 %d 条记录: %w", rowNumber+1, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ImportSummary{}, err
	}
	return summary, nil
}

func requireTransactionalImport(ctx context.Context, tx *sql.Tx, tableRef, database, table string) error {
	// Hold a metadata lock on the destination until this import commits, so
	// the storage engine cannot change between this check and the writes.
	rows, err := tx.QueryContext(ctx, "SELECT 1 FROM "+tableRef+" WHERE 1 = 0")
	if err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	const query = `SELECT COALESCE(e.TRANSACTIONS, 'NO')
FROM information_schema.TABLES t
LEFT JOIN information_schema.ENGINES e ON e.ENGINE = t.ENGINE
WHERE BINARY t.TABLE_SCHEMA = BINARY COALESCE(NULLIF(?, ''), DATABASE())
  AND BINARY t.TABLE_NAME = BINARY ?`
	var support string
	err = tx.QueryRowContext(ctx, query, database, table).Scan(&support)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !strings.EqualFold(support, "YES") {
		return errors.New("目标表未确认支持事务，请使用支持事务的表或选择追加")
	}
	return err
}

type importPlan struct {
	mode, table                                string
	mapping                                    []ImportMapping
	keyIndices                                 []int
	updateIndices                              []int
	insertSQL, updateSQL, existsSQL, deleteSQL string
	insert, update, exists, delete             *sql.Stmt
}

func makeImportPlan(d Dialect, table, mode string, fields []string, columns []ColumnDef, mappings []ImportMapping) (*importPlan, error) {
	targets := make(map[string]ColumnDef, len(columns))
	keys := make(map[string]bool)
	for _, column := range columns {
		targets[column.Name] = column
		if column.PrimaryKey {
			keys[column.Name] = true
		}
	}
	if mappings == nil {
		for i, field := range fields {
			if field = strings.TrimSpace(field); field != "" {
				mappings = append(mappings, ImportMapping{Source: i, Target: field})
			}
		}
	}
	if len(mappings) == 0 || len(mappings) > len(columns) {
		return nil, errors.New("请至少映射一个有效的目标字段")
	}
	plan := &importPlan{mode: mode, table: table, mapping: mappings}
	requiresKey := mode == "update" || mode == "upsert" || mode == "skip" || mode == "delete"
	mapped := make(map[string]bool, len(mappings))
	var names, placeholders, assignments, where []string
	for i, mapping := range mappings {
		column, ok := targets[mapping.Target]
		if !ok || mapping.Source < 0 || mapping.Source >= len(fields) {
			return nil, fmt.Errorf("映射中的源字段或目标字段 %q 无效", mapping.Target)
		}
		if mapped[mapping.Target] || column.GeneratedExpr != "" {
			return nil, fmt.Errorf("目标字段 %q 已重复映射或为生成列", mapping.Target)
		}
		mapped[mapping.Target] = true
		name := d.QuoteIdent(column.Name)
		names = append(names, name)
		placeholders = append(placeholders, d.Placeholder(i+1))
		if column.PrimaryKey {
			plan.keyIndices = append(plan.keyIndices, i)
			where = append(where, name+" = "+d.Placeholder(len(where)+1))
		} else {
			plan.updateIndices = append(plan.updateIndices, i)
			assignments = append(assignments, name+" = "+d.Placeholder(len(assignments)+1))
		}
	}
	if requiresKey && (len(keys) == 0 || len(plan.keyIndices) != len(keys)) {
		return nil, errors.New("此导入模式要求映射目标表的全部主键字段")
	}
	switch mode {
	case "append", "replace", "upsert", "skip":
		plan.insertSQL = "INSERT INTO " + table + " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	}
	switch mode {
	case "delete":
		plan.deleteSQL = "DELETE FROM " + table + " WHERE " + strings.Join(where, " AND ")
	case "update", "upsert", "skip":
		plan.existsSQL = "SELECT 1 FROM " + table + " WHERE " + strings.Join(where, " AND ")
		if mode != "skip" && len(assignments) > 0 {
			predicates := make([]string, len(plan.keyIndices))
			for i, index := range plan.keyIndices {
				predicates[i] = names[index] + " = " + d.Placeholder(len(assignments)+i+1)
			}
			plan.updateSQL = "UPDATE " + table + " SET " + strings.Join(assignments, ", ") + " WHERE " + strings.Join(predicates, " AND ")
		}
	}
	return plan, nil
}

func (p *importPlan) prepare(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []struct {
		query string
		dest  **sql.Stmt
	}{{p.insertSQL, &p.insert}, {p.updateSQL, &p.update}, {p.existsSQL, &p.exists}, {p.deleteSQL, &p.delete}} {
		if statement.query == "" {
			continue
		}
		stmt, err := tx.PrepareContext(ctx, statement.query)
		if err != nil {
			p.close()
			return err
		}
		*statement.dest = stmt
	}
	return nil
}

func (p *importPlan) close() {
	for _, stmt := range []*sql.Stmt{p.insert, p.update, p.exists, p.delete} {
		if stmt != nil {
			_ = stmt.Close()
		}
	}
}

type importAction struct {
	kind string
	err  error
}

func importOneRow(ctx context.Context, tx *sql.Tx, p *importPlan, row []any, emptyAsNull, continueOnError bool, engine EngineType) (importAction, error) {
	if !continueOnError {
		kind, err := p.exec(ctx, row, emptyAsNull)
		return importAction{kind: kind}, err
	}
	save, rollback, release := importSavepoint(engine)
	if _, err := tx.ExecContext(ctx, save); err != nil {
		return importAction{}, err
	}
	kind, err := p.exec(ctx, row, emptyAsNull)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, rollback); rollbackErr != nil {
			return importAction{}, errors.Join(err, rollbackErr)
		}
	}
	if release != "" {
		if _, releaseErr := tx.ExecContext(ctx, release); releaseErr != nil {
			return importAction{}, errors.Join(err, releaseErr)
		}
	}
	if err != nil {
		return importAction{kind: "failed", err: err}, nil
	}
	return importAction{kind: kind}, nil
}

func importSavepoint(engine EngineType) (save, rollback, release string) {
	const name = "zidong_import_row"
	switch engine {
	case EngineSQLServer:
		return "SAVE TRANSACTION " + name, "ROLLBACK TRANSACTION " + name, ""
	case EngineOracle, EngineDameng:
		return "SAVEPOINT " + name, "ROLLBACK TO SAVEPOINT " + name, ""
	default:
		return "SAVEPOINT " + name, "ROLLBACK TO SAVEPOINT " + name, "RELEASE SAVEPOINT " + name
	}
}

func (p *importPlan) exec(ctx context.Context, row []any, emptyAsNull bool) (string, error) {
	values := make([]any, len(p.mapping))
	for i, mapping := range p.mapping {
		value := any(missingImportValue{})
		if mapping.Source < len(row) {
			value = row[mapping.Source]
		}
		if _, missing := value.(missingImportValue); missing {
			if p.mode == "update" || p.mode == "upsert" {
				return "", fmt.Errorf("缺少映射到 %q 的源字段，未写入 NULL", mapping.Target)
			}
			continue
		}
		if text, ok := value.(string); !ok || text != "" || !emptyAsNull {
			values[i] = value
		}
	}
	if p.mode == "append" || p.mode == "replace" {
		_, err := p.insert.ExecContext(ctx, values...)
		return "inserted", err
	}
	keys := make([]any, len(p.keyIndices))
	for i, index := range p.keyIndices {
		if values[index] == nil {
			return "", fmt.Errorf("主键 %q 没有值", p.mapping[index].Target)
		}
		keys[i] = values[index]
	}
	if p.delete != nil {
		result, err := p.delete.ExecContext(ctx, keys...)
		if err != nil {
			return "", err
		}
		if count, err := result.RowsAffected(); err == nil && count == 0 {
			return "skipped", nil
		}
		return "deleted", nil
	}
	var match int
	err := p.exists.QueryRowContext(ctx, keys...).Scan(&match)
	switch {
	case err == nil:
		if p.update == nil {
			return "skipped", nil
		}
		args := make([]any, 0, len(p.updateIndices)+len(keys))
		for _, index := range p.updateIndices {
			args = append(args, values[index])
		}
		_, err := p.update.ExecContext(ctx, append(args, keys...)...)
		return "updated", err
	case errors.Is(err, sql.ErrNoRows):
		if p.insert == nil {
			return "skipped", nil
		}
		_, err := p.insert.ExecContext(ctx, values...)
		return "inserted", err
	default:
		return "", err
	}
}
