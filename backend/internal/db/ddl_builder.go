package db

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var safeTypePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_ ()\.,\[\]-]*$`)
var numericDefaultPattern = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func validateTypeExpr(t string) (string, error) {
	trimmed := strings.TrimSpace(t)
	if trimmed == "" {
		return "", errors.New("column data type cannot be empty")
	}
	if !safeTypePattern.MatchString(trimmed) {
		return "", fmt.Errorf("invalid column data type %q", t)
	}
	return trimmed, nil
}

func formatDefaultValue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "''"
	}
	upper := strings.ToUpper(trimmed)
	if upper == "NULL" || upper == "TRUE" || upper == "FALSE" ||
		upper == "CURRENT_TIMESTAMP" || upper == "CURRENT_DATE" || upper == "CURRENT_TIME" ||
		upper == "NOW()" || upper == "GETDATE()" || upper == "SYSDATE" || upper == "SYSTIMESTAMP" ||
		numericDefaultPattern.MatchString(trimmed) ||
		(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) ||
		(strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")")) {
		return trimmed
	}
	return "'" + strings.ReplaceAll(trimmed, "'", "''") + "'"
}

func BuildCreateTableSQL(d Dialect, ts TableSchema) string {
	stmts, _ := BuildCreateTableStatements(d, ts)
	return strings.Join(stmts, ";\n")
}

func BuildCreateTableStatements(d Dialect, ts TableSchema) ([]string, error) {
	if strings.TrimSpace(ts.Table) == "" {
		return nil, errors.New("table name is required")
	}
	if len(ts.Columns) == 0 {
		return nil, errors.New("at least one column is required")
	}

	tableRef := QualifiedTable(d, ts.Database, ts.Schema, ts.Table)
	var pkCols []string
	for _, c := range ts.Columns {
		if c.PrimaryKey {
			pkCols = append(pkCols, d.QuoteIdent(c.Name))
		}
	}

	var defs []string
	var postStatements []string

	for _, c := range ts.Columns {
		if strings.TrimSpace(c.Name) == "" {
			return nil, errors.New("column name cannot be empty")
		}
		colType, err := validateTypeExpr(c.Type)
		if err != nil {
			return nil, err
		}
		line, err := formatColumnSpec(d, c, colType, len(pkCols) == 1 && c.PrimaryKey)
		if err != nil {
			return nil, err
		}
		defs = append(defs, "  "+line)

		if d.Engine() == EnginePostgres && strings.TrimSpace(c.Comment) != "" {
			postStatements = append(postStatements, fmt.Sprintf(
				"COMMENT ON COLUMN %s.%s IS '%s'",
				tableRef,
				d.QuoteIdent(c.Name),
				strings.ReplaceAll(c.Comment, "'", "''"),
			))
		}
	}

	if len(pkCols) > 1 || (len(pkCols) == 1 && !(d.Engine() == EngineSQLite && ts.Columns[findPKIdx(ts.Columns)].AutoIncrement)) {
		defs = append(defs, fmt.Sprintf("  PRIMARY KEY (%s)", strings.Join(pkCols, ", ")))
	}

	for _, fk := range ts.ForeignKeys {
		if len(fk.Columns) == 0 || len(fk.Columns) != len(fk.RefColumns) || strings.TrimSpace(fk.RefTable) == "" {
			continue
		}
		qCols := quoteList(d, fk.Columns)
		qRefs := quoteList(d, fk.RefColumns)
		refTable := QualifiedTable(d, ts.Database, fk.RefSchema, fk.RefTable)
		fkName := fk.Name
		if fkName == "" {
			fkName = fmt.Sprintf("fk_%s_%s", ts.Table, strings.Join(fk.Columns, "_"))
		}
		clause := fmt.Sprintf("  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
			d.QuoteIdent(fkName), strings.Join(qCols, ", "), refTable, strings.Join(qRefs, ", "))
		if fk.OnDelete != "" && !strings.EqualFold(fk.OnDelete, "NO ACTION") {
			clause += " ON DELETE " + strings.ToUpper(fk.OnDelete)
		}
		if fk.OnUpdate != "" && d.Engine() != EngineOracle && d.Engine() != EngineDameng && !strings.EqualFold(fk.OnUpdate, "NO ACTION") {
			clause += " ON UPDATE " + strings.ToUpper(fk.OnUpdate)
		}
		defs = append(defs, clause)
	}

	createSQL := fmt.Sprintf("CREATE TABLE %s (\n%s\n)", tableRef, strings.Join(defs, ",\n"))
	if ts.Comment != nil && strings.TrimSpace(*ts.Comment) != "" {
		if d.Engine() == EngineMySQL {
			createSQL += fmt.Sprintf(" COMMENT='%s'", strings.ReplaceAll(*ts.Comment, "'", "''"))
		} else if cStmt := BuildTableCommentStatement(d, ts.Database, ts.Schema, ts.Table, "TABLE", *ts.Comment); cStmt != "" {
			postStatements = append([]string{cStmt}, postStatements...)
		}
	}
	stmts := []string{createSQL}

	for _, idx := range ts.Indexes {
		if idx.Primary || len(idx.Columns) == 0 || strings.TrimSpace(idx.Name) == "" {
			continue
		}
		uniq := ""
		if idx.Unique {
			uniq = "UNIQUE "
		}
		stmts = append(stmts, fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
			uniq, d.QuoteIdent(idx.Name), tableRef, strings.Join(quoteList(d, idx.Columns), ", ")))
	}
	stmts = append(stmts, postStatements...)
	return stmts, nil
}

func findPKIdx(cols []ColumnDef) int {
	for i, c := range cols {
		if c.PrimaryKey {
			return i
		}
	}
	return 0
}

func normalizeGeneratedKind(kind string) string {
	if strings.EqualFold(strings.TrimSpace(kind), "VIRTUAL") {
		return "VIRTUAL"
	}
	return "STORED"
}

func formatColumnSpec(d Dialect, c ColumnDef, colType string, isSinglePK bool) (string, error) {
	var parts []string
	parts = append(parts, d.QuoteIdent(c.Name))

	genExpr := strings.TrimSpace(c.GeneratedExpr)
	isPgGenerated := d.Engine() == EnginePostgres && genExpr != ""

	upperType := strings.ToUpper(colType)
	switch d.Engine() {
	case EnginePostgres:
		if isPgGenerated {
			parts = append(parts, colType)
			parts = append(parts, fmt.Sprintf("GENERATED ALWAYS AS (%s) %s", genExpr, normalizeGeneratedKind(c.GeneratedKind)))
		} else if c.AutoIncrement && (upperType == "INT" || upperType == "INTEGER") {
			parts = append(parts, "SERIAL")
		} else if c.AutoIncrement && upperType == "BIGINT" {
			parts = append(parts, "BIGSERIAL")
		} else {
			parts = append(parts, colType)
			if c.AutoIncrement {
				parts = append(parts, "GENERATED BY DEFAULT AS IDENTITY")
			}
		}
	case EngineSQLite:
		if c.AutoIncrement && isSinglePK {
			parts = append(parts, "INTEGER PRIMARY KEY AUTOINCREMENT")
		} else {
			parts = append(parts, colType)
		}
	default:
		parts = append(parts, colType)
	}

	if !(d.Engine() == EngineSQLite && c.AutoIncrement && isSinglePK) {
		if !isPgGenerated && c.DefaultValue != nil && strings.TrimSpace(*c.DefaultValue) != "" {
			parts = append(parts, "DEFAULT "+formatDefaultValue(*c.DefaultValue))
		}
		if !c.Nullable {
			parts = append(parts, "NOT NULL")
		}
		if !isPgGenerated && c.AutoIncrement {
			switch d.Engine() {
			case EngineMySQL:
				parts = append(parts, "AUTO_INCREMENT")
			case EngineSQLServer, EngineDameng:
				parts = append(parts, "IDENTITY(1,1)")
			case EngineOracle:
				parts = append(parts, "GENERATED BY DEFAULT AS IDENTITY")
			}
		}
	}

	if d.Engine() == EngineMySQL && strings.TrimSpace(c.Comment) != "" {
		parts = append(parts, fmt.Sprintf("COMMENT '%s'", strings.ReplaceAll(c.Comment, "'", "''")))
	}
	return strings.Join(parts, " "), nil
}

func BuildDDLPreview(d Dialect, diff TableDesignDiff) (DDLPreviewResponse, error) {
	if diff.Mode == "" || diff.Mode == "create" {
		stmts, err := BuildCreateTableStatements(d, diff.Target)
		if err != nil {
			return DDLPreviewResponse{}, err
		}
		return DDLPreviewResponse{
			Statements: stmts,
			SQL:        strings.Join(stmts, ";\n") + ";",
		}, nil
	}

	if diff.Original == nil {
		return DDLPreviewResponse{}, errors.New("original schema is required for alter mode")
	}

	orig := *diff.Original
	target := diff.Target
	if target.Database == "" {
		target.Database = diff.Database
	}
	if target.Schema == "" {
		target.Schema = diff.Schema
	}
	if target.Table == "" {
		target.Table = diff.TableName
	}

	var stmts []string
	var warnings []string

	curTableName := orig.Table
	if diff.NewTableName != "" && diff.NewTableName != orig.Table {
		stmts = append(stmts, buildRenameTableStmt(d, orig.Database, orig.Schema, orig.Table, diff.NewTableName))
		curTableName = diff.NewTableName
	}

	tableRef := QualifiedTable(d, target.Database, target.Schema, curTableName)

	if d.Engine() == EngineSQLite && needsSQLiteRebuild(orig, target) {
		rebuildStmts, err := buildSQLiteRebuild(d, orig, target, curTableName)
		if err != nil {
			return DDLPreviewResponse{}, err
		}
		stmts = append(stmts, rebuildStmts...)
		warnings = append(warnings, "SQLite 不支持直接修改列类型/主键/非空约束，已自动生成事务内重建表 DDL（请先备份数据）。")
		return DDLPreviewResponse{
			Statements: stmts,
			SQL:        strings.Join(stmts, ";\n") + ";",
			Warnings:   warnings,
		}, nil
	}

	origByName := make(map[string]ColumnDef, len(orig.Columns))
	for _, c := range orig.Columns {
		origByName[c.Name] = c
	}

	matchedOrig := make(map[string]bool)
	for _, tc := range target.Columns {
		lookup := tc.OriginalName
		if lookup == "" {
			lookup = tc.Name
		}
		oc, exists := origByName[lookup]
		if !exists {
			colType, err := validateTypeExpr(tc.Type)
			if err != nil {
				return DDLPreviewResponse{}, err
			}
			spec, err := formatColumnSpec(d, tc, colType, false)
			if err != nil {
				return DDLPreviewResponse{}, err
			}
			if d.Engine() == EngineOracle || d.Engine() == EngineSQLServer || d.Engine() == EngineDameng {
				stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD %s", tableRef, spec))
			} else {
				stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tableRef, spec))
			}
			if d.Engine() == EnginePostgres && strings.TrimSpace(tc.Comment) != "" {
				stmts = append(stmts, fmt.Sprintf(
					"COMMENT ON COLUMN %s.%s IS '%s'",
					tableRef,
					d.QuoteIdent(tc.Name),
					strings.ReplaceAll(tc.Comment, "'", "''"),
				))
			}
			continue
		}

		matchedOrig[oc.Name] = true
		if oc.Name != tc.Name {
			stmts = append(stmts, buildRenameColumnStmt(d, target.Database, target.Schema, curTableName, oc.Name, tc.Name))
		}
		if columnDefinitionChanged(oc, tc) {
			colType, err := validateTypeExpr(tc.Type)
			if err != nil {
				return DDLPreviewResponse{}, err
			}
			modStmts, modWarn, err := buildModifyColumnStmts(d, tableRef, oc, tc, colType)
			if err != nil {
				return DDLPreviewResponse{}, err
			}
			stmts = append(stmts, modStmts...)
			warnings = append(warnings, modWarn...)
		}
		if d.Engine() == EnginePostgres && strings.TrimSpace(oc.Comment) != strings.TrimSpace(tc.Comment) {
			stmts = append(stmts, fmt.Sprintf(
				"COMMENT ON COLUMN %s.%s IS '%s'",
				tableRef,
				d.QuoteIdent(tc.Name),
				strings.ReplaceAll(tc.Comment, "'", "''"),
			))
		}
	}

	for _, oc := range orig.Columns {
		if !matchedOrig[oc.Name] {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableRef, d.QuoteIdent(oc.Name)))
		}
	}

	// Indexes diff (non-primary)
	origIdx := make(map[string]IndexDef)
	for _, idx := range orig.Indexes {
		if !idx.Primary {
			origIdx[idx.Name] = idx
		}
	}
	targetIdx := make(map[string]IndexDef)
	for _, idx := range target.Indexes {
		if !idx.Primary && strings.TrimSpace(idx.Name) != "" && len(idx.Columns) > 0 {
			targetIdx[idx.Name] = idx
		}
	}
	for name := range origIdx {
		if tIdx, ok := targetIdx[name]; !ok || !indexEqual(origIdx[name], tIdx) {
			stmts = append(stmts, buildDropIndexStmt(d, target.Database, target.Schema, curTableName, name))
		}
	}
	for name, tIdx := range targetIdx {
		if oIdx, ok := origIdx[name]; !ok || !indexEqual(oIdx, tIdx) {
			uniq := ""
			if tIdx.Unique {
				uniq = "UNIQUE "
			}
			stmts = append(stmts, fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
				uniq, d.QuoteIdent(tIdx.Name), tableRef, strings.Join(quoteList(d, tIdx.Columns), ", ")))
		}
	}

	if target.Comment != nil {
		origCmt := ""
		if orig.Comment != nil {
			origCmt = *orig.Comment
		}
		if strings.TrimSpace(origCmt) != strings.TrimSpace(*target.Comment) {
			if cStmt := BuildTableCommentStatement(d, target.Database, target.Schema, curTableName, "TABLE", *target.Comment); cStmt != "" {
				stmts = append(stmts, cStmt)
			}
		}
	}

	if len(stmts) == 0 {
		return DDLPreviewResponse{
			Statements: []string{},
			SQL:        "-- 未检测到结构变更",
			Warnings:   warnings,
		}, nil
	}

	return DDLPreviewResponse{
		Statements: stmts,
		SQL:        strings.Join(stmts, ";\n") + ";",
		Warnings:   warnings,
	}, nil
}

func buildRenameTableStmt(d Dialect, database, schema, oldName, newName string) string {
	oldRef := QualifiedTable(d, database, schema, oldName)
	switch d.Engine() {
	case EngineSQLServer:
		return fmt.Sprintf("EXEC sp_rename '%s', '%s'", strings.ReplaceAll(oldRef, "'", "''"), strings.ReplaceAll(newName, "'", "''"))
	default:
		return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", oldRef, d.QuoteIdent(newName))
	}
}

func buildRenameColumnStmt(d Dialect, database, schema, table, oldCol, newCol string) string {
	tableRef := QualifiedTable(d, database, schema, table)
	if d.Engine() == EngineSQLServer {
		target := strings.ReplaceAll(tableRef+"."+d.QuoteIdent(oldCol), "'", "''")
		return fmt.Sprintf("EXEC sp_rename '%s', '%s', 'COLUMN'", target, strings.ReplaceAll(newCol, "'", "''"))
	}
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", tableRef, d.QuoteIdent(oldCol), d.QuoteIdent(newCol))
}

func buildModifyColumnStmts(d Dialect, tableRef string, oc, tc ColumnDef, colType string) ([]string, []string, error) {
	switch d.Engine() {
	case EngineMySQL:
		spec, err := formatColumnSpec(d, tc, colType, false)
		if err != nil {
			return nil, nil, err
		}
		return []string{fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s", tableRef, spec)}, nil, nil
	case EnginePostgres:
		var out []string
		var warns []string
		colQ := d.QuoteIdent(tc.Name)

		origGen := strings.TrimSpace(oc.GeneratedExpr)
		targetGen := strings.TrimSpace(tc.GeneratedExpr)
		origKind := normalizeGeneratedKind(oc.GeneratedKind)
		targetKind := normalizeGeneratedKind(tc.GeneratedKind)

		// If converting a normal column into a generated column, or switching STORED <-> VIRTUAL,
		// PostgreSQL requires dropping and re-adding the column.
		if (origGen == "" && targetGen != "") || (origGen != "" && targetGen != "" && origKind != targetKind) {
			spec, err := formatColumnSpec(d, tc, colType, false)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableRef, colQ))
			out = append(out, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tableRef, spec))
			warns = append(warns, fmt.Sprintf("PostgreSQL 不支持原地将列 %s 切换为生成列或更改 STORED/VIRTUAL 模式，已生成 DROP + ADD COLUMN 语句（请确认无依赖视图）。", colQ))
			return out, warns, nil
		}

		if !strings.EqualFold(strings.TrimSpace(oc.Type), strings.TrimSpace(tc.Type)) {
			out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", tableRef, colQ, colType))
		}
		if oc.Nullable != tc.Nullable {
			if tc.Nullable {
				out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", tableRef, colQ))
			} else {
				out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL", tableRef, colQ))
			}
		}

		if origGen != "" && targetGen == "" {
			out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP EXPRESSION", tableRef, colQ))
			if tc.DefaultValue != nil && strings.TrimSpace(*tc.DefaultValue) != "" {
				out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tableRef, colQ, formatDefaultValue(*tc.DefaultValue)))
			}
		} else if origGen != "" && targetGen != "" {
			if origGen != targetGen {
				out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET EXPRESSION AS (%s)", tableRef, colQ, targetGen))
				warns = append(warns, fmt.Sprintf("列 %s 使用了 SET EXPRESSION AS (...) 语法（需 PostgreSQL 17+）；若为 PG 12~16，请先删除该列再重新添加。", colQ))
			}
		} else {
			origDef := ""
			if oc.DefaultValue != nil {
				origDef = strings.TrimSpace(*oc.DefaultValue)
			}
			targetDef := ""
			if tc.DefaultValue != nil {
				targetDef = strings.TrimSpace(*tc.DefaultValue)
			}
			if origDef != targetDef {
				if targetDef != "" {
					out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tableRef, colQ, formatDefaultValue(targetDef)))
				} else {
					out = append(out, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", tableRef, colQ))
				}
			}
		}
		return out, warns, nil
	case EngineSQLServer:
		nullKW := "NOT NULL"
		if tc.Nullable {
			nullKW = "NULL"
		}
		return []string{fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s %s", tableRef, d.QuoteIdent(tc.Name), colType, nullKW)}, nil, nil
	case EngineOracle, EngineDameng:
		nullKW := "NOT NULL"
		if tc.Nullable {
			nullKW = "NULL"
		}
		defClause := ""
		if tc.DefaultValue != nil && strings.TrimSpace(*tc.DefaultValue) != "" {
			defClause = " DEFAULT " + formatDefaultValue(*tc.DefaultValue)
		}
		return []string{fmt.Sprintf("ALTER TABLE %s MODIFY (%s %s%s %s)", tableRef, d.QuoteIdent(tc.Name), colType, defClause, nullKW)}, nil, nil
	default:
		return nil, []string{"当前引擎不支持原地修改列定义。"}, nil
	}
}

func buildDropIndexStmt(d Dialect, database, schema, table, indexName string) string {
	tableRef := QualifiedTable(d, database, schema, table)
	switch d.Engine() {
	case EngineMySQL:
		return fmt.Sprintf("DROP INDEX %s ON %s", d.QuoteIdent(indexName), tableRef)
	case EngineSQLServer:
		return fmt.Sprintf("DROP INDEX %s ON %s", d.QuoteIdent(indexName), tableRef)
	default:
		return fmt.Sprintf("DROP INDEX %s", QualifiedTable(d, database, schema, indexName))
	}
}

func needsSQLiteRebuild(orig, target TableSchema) bool {
	if !pkSetsEqual(orig.Columns, target.Columns) {
		return true
	}
	origByName := make(map[string]ColumnDef, len(orig.Columns))
	for _, c := range orig.Columns {
		origByName[c.Name] = c
	}
	for _, tc := range target.Columns {
		lookup := tc.OriginalName
		if lookup == "" {
			lookup = tc.Name
		}
		oc, ok := origByName[lookup]
		if ok && columnDefinitionChanged(oc, tc) {
			return true
		}
	}
	return false
}

func buildSQLiteRebuild(d Dialect, orig, target TableSchema, finalTableName string) ([]string, error) {
	tmpName := "__webdb_tmp_" + finalTableName
	tmpSchema := target
	tmpSchema.Table = tmpName

	createStmts, err := BuildCreateTableStatements(d, tmpSchema)
	if err != nil {
		return nil, err
	}

	origSet := make(map[string]bool, len(orig.Columns))
	for _, c := range orig.Columns {
		origSet[c.Name] = true
	}

	var insertCols []string
	var selectCols []string
	for _, tc := range target.Columns {
		src := tc.OriginalName
		if src == "" {
			src = tc.Name
		}
		if origSet[src] {
			insertCols = append(insertCols, d.QuoteIdent(tc.Name))
			selectCols = append(selectCols, d.QuoteIdent(src))
		}
	}

	var stmts []string
	stmts = append(stmts, "PRAGMA foreign_keys = OFF")
	stmts = append(stmts, createStmts[0])
	if len(insertCols) > 0 {
		stmts = append(stmts, fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
			d.QuoteIdent(tmpName),
			strings.Join(insertCols, ", "),
			strings.Join(selectCols, ", "),
			d.QuoteIdent(finalTableName),
		))
	}
	stmts = append(stmts, fmt.Sprintf("DROP TABLE %s", d.QuoteIdent(finalTableName)))
	stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", d.QuoteIdent(tmpName), d.QuoteIdent(finalTableName)))
	for _, extra := range createStmts[1:] {
		stmts = append(stmts, strings.ReplaceAll(extra, d.QuoteIdent(tmpName), d.QuoteIdent(finalTableName)))
	}
	stmts = append(stmts, "PRAGMA foreign_keys = ON")
	return stmts, nil
}

func columnDefinitionChanged(a, b ColumnDef) bool {
	if !strings.EqualFold(strings.TrimSpace(a.Type), strings.TrimSpace(b.Type)) {
		return true
	}
	if a.Nullable != b.Nullable || a.PrimaryKey != b.PrimaryKey || a.AutoIncrement != b.AutoIncrement {
		return true
	}
	aGen := strings.TrimSpace(a.GeneratedExpr)
	bGen := strings.TrimSpace(b.GeneratedExpr)
	if aGen != bGen {
		return true
	}
	if aGen != "" && normalizeGeneratedKind(a.GeneratedKind) != normalizeGeneratedKind(b.GeneratedKind) {
		return true
	}
	aDef := ""
	if a.DefaultValue != nil {
		aDef = strings.TrimSpace(*a.DefaultValue)
	}
	bDef := ""
	if b.DefaultValue != nil {
		bDef = strings.TrimSpace(*b.DefaultValue)
	}
	return aDef != bDef
}

func pkSetsEqual(a, b []ColumnDef) bool {
	var pka, pkb []string
	for _, c := range a {
		if c.PrimaryKey {
			pka = append(pka, c.Name)
		}
	}
	for _, c := range b {
		if c.PrimaryKey {
			pkb = append(pkb, c.Name)
		}
	}
	if len(pka) != len(pkb) {
		return false
	}
	for i := range pka {
		if pka[i] != pkb[i] {
			return false
		}
	}
	return true
}

func indexEqual(a, b IndexDef) bool {
	if a.Unique != b.Unique || len(a.Columns) != len(b.Columns) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i] != b.Columns[i] {
			return false
		}
	}
	return true
}

func quoteList(d Dialect, items []string) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = d.QuoteIdent(item)
	}
	return out
}
