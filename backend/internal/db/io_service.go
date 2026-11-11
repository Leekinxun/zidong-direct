package db

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type ExportOptions struct {
	Format   string // csv | json | sql
	WithBOM  bool
	Table    string
	Database string
	Schema   string
	MaxRows  int
}

func ExportRowsToWriter(ctx context.Context, conn *sql.DB, d Dialect, querySQL string, args []any, opts ExportOptions, w io.Writer) error {
	rows, err := conn.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return err
	}
	colNames, err := rows.Columns()
	if err != nil {
		return err
	}

	rawVals := make([]any, len(colNames))
	ptrs := make([]any, len(colNames))
	for i := range rawVals {
		ptrs[i] = &rawVals[i]
	}

	switch strings.ToLower(opts.Format) {
	case "csv", "":
		if opts.WithBOM {
			if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
				return err
			}
		}
		cw := csv.NewWriter(w)
		if err := cw.Write(colNames); err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			if opts.MaxRows > 0 && count >= opts.MaxRows {
				break
			}
			for i := range rawVals {
				rawVals[i] = nil
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			record := make([]string, len(colNames))
			for i, v := range rawVals {
				norm := NormalizeCellValue(v, safeColType(colTypes, i))
				if norm == nil {
					record[i] = ""
				} else {
					record[i] = fmt.Sprintf("%v", norm)
				}
			}
			if err := cw.Write(record); err != nil {
				return err
			}
			count++
		}
		cw.Flush()
		return cw.Error()

	case "json":
		if _, err := io.WriteString(w, "[\n"); err != nil {
			return err
		}
		first := true
		count := 0
		for rows.Next() {
			if opts.MaxRows > 0 && count >= opts.MaxRows {
				break
			}
			for i := range rawVals {
				rawVals[i] = nil
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			obj := make(map[string]any, len(colNames))
			for i, name := range colNames {
				obj[name] = NormalizeCellValue(rawVals[i], safeColType(colTypes, i))
			}
			b, err := json.Marshal(obj)
			if err != nil {
				return err
			}
			if !first {
				if _, err := io.WriteString(w, ",\n"); err != nil {
					return err
				}
			}
			first = false
			if _, err := w.Write(b); err != nil {
				return err
			}
			count++
		}
		_, err := io.WriteString(w, "\n]\n")
		return err

	case "sql":
		tableName := opts.Table
		if tableName == "" {
			tableName = "exported_table"
		}
		tableRef := QualifiedTable(d, opts.Database, opts.Schema, tableName)
		quotedCols := strings.Join(quoteList(d, colNames), ", ")
		count := 0
		for rows.Next() {
			if opts.MaxRows > 0 && count >= opts.MaxRows {
				break
			}
			for i := range rawVals {
				rawVals[i] = nil
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			vals := make([]string, len(colNames))
			for i, v := range rawVals {
				norm := NormalizeCellValue(v, safeColType(colTypes, i))
				vals[i] = formatSQLValue(norm)
			}
			line := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);\n", tableRef, quotedCols, strings.Join(vals, ", "))
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
			count++
		}
		return rows.Err()

	default:
		return fmt.Errorf("unsupported export format %q", opts.Format)
	}
}

func safeColType(colTypes []*sql.ColumnType, i int) string {
	if i < len(colTypes) && colTypes[i] != nil {
		return colTypes[i].DatabaseTypeName()
	}
	return ""
}

func formatSQLValue(v any) string {
	if v == nil {
		return "NULL"
	}
	switch x := v.(type) {
	case bool:
		if x {
			return "TRUE"
		}
		return "FALSE"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", x)
	case float32, float64:
		s := fmt.Sprintf("%v", x)
		if _, err := strconv.ParseFloat(s, 64); err == nil && s != "NaN" && s != "+Inf" && s != "-Inf" {
			return s
		}
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	default:
		s := fmt.Sprintf("%v", x)
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
}
