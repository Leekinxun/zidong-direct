package db

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	dm "gitee.com/chunanyong/dm"
)

type ExportOptions struct {
	Format    string
	WithBOM   bool
	Delimiter string
	Widths    string
	Table     string
	Database  string
	Schema    string
	MaxRows   int
}

type exportColumn struct {
	Name         string
	DatabaseType string
	ScanType     reflect.Type
}

// The returned slice is valid until the next call. A writer that batches rows
// should copy the slice before reading another row.
type exportNextRow func() ([]any, error)

func ExportContentType(format string) (string, bool) {
	switch format {
	case "csv", "":
		return "text/csv; charset=utf-8", true
	case "txt":
		return "text/plain; charset=utf-8", true
	case "tsv":
		return "text/tab-separated-values; charset=utf-8", true
	case "json":
		return "application/json; charset=utf-8", true
	case "jsonl":
		return "application/x-ndjson; charset=utf-8", true
	case "xml":
		return "application/xml; charset=utf-8", true
	case "html":
		return "text/html; charset=utf-8", true
	case "sql":
		return "application/sql; charset=utf-8", true
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true
	case "xls":
		return "application/vnd.ms-excel", true
	case "ods":
		return "application/vnd.oasis.opendocument.spreadsheet", true
	case "dbf":
		return "application/x-dbf", true
	case "parquet":
		return "application/vnd.apache.parquet", true
	default:
		return "", false
	}
}

func ExportRowsToWriter(ctx context.Context, conn *sql.DB, d Dialect, querySQL string, args []any, opts ExportOptions, w io.Writer) error {
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "csv"
	}
	if _, ok := ExportContentType(format); !ok {
		return fmt.Errorf("不支持的导出格式 %q", opts.Format)
	}

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
	columns := make([]exportColumn, len(colNames))
	raw := make([]any, len(colNames))
	ptrs := make([]any, len(colNames))
	values := make([]any, len(colNames))
	for i, name := range colNames {
		columns[i] = exportColumn{Name: name, DatabaseType: safeColType(colTypes, i)}
		if i < len(colTypes) && colTypes[i] != nil {
			columns[i].ScanType = colTypes[i].ScanType()
		}
		ptrs[i] = &raw[i]
	}

	count := 0
	legacy := format == "csv" || format == "json" || format == "sql"
	next := func() ([]any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if opts.MaxRows > 0 && count >= opts.MaxRows {
			return nil, io.EOF
		}
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return nil, err
			}
			return nil, io.EOF
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		count++
		for i, value := range raw {
			if legacy {
				values[i] = NormalizeCellValue(value, columns[i].DatabaseType)
			} else {
				normalized, err := normalizeExportValue(value, columns[i].DatabaseType)
				if err != nil {
					return nil, fmt.Errorf("第 %d 行、列 %q: %w", count, columns[i].Name, err)
				}
				values[i] = normalized
			}
		}
		return values, nil
	}

	switch format {
	case "csv", "txt", "tsv":
		return writeTextExport(w, format, colNames, opts, next)
	case "json", "jsonl":
		return writeJSONExport(w, format, colNames, next)
	case "xml":
		return writeXMLExport(w, colNames, next)
	case "html":
		return writeHTMLExport(w, colNames, next)
	case "sql":
		return writeSQLExport(w, d, colNames, opts, next)
	case "xlsx", "ods":
		return writeWorkbookExport(w, format, columns, next)
	case "xls":
		return writeXLSExport(w, columns, next)
	case "dbf":
		return writeDBFExport(w, columns, next)
	case "parquet":
		return writeParquetExport(w, columns, next)
	}
	return fmt.Errorf("不支持的导出格式 %q", opts.Format)
}

func safeColType(colTypes []*sql.ColumnType, i int) string {
	if i < len(colTypes) && colTypes[i] != nil {
		return colTypes[i].DatabaseTypeName()
	}
	return ""
}

func normalizeExportValue(value any, colType string) (any, error) {
	switch val := value.(type) {
	case time.Time:
		return val, nil
	case []byte:
		// database/sql supplies an owned copy when scanning into *any.
		if isBinaryType(colType) || !utf8.Valid(val) {
			return val, nil
		}
		return string(val), nil
	case *dm.DmClob:
		if val == nil || !val.Valid {
			return nil, nil
		}
		length, err := val.GetLength()
		if err != nil {
			return nil, err
		}
		if length == 0 {
			return "", nil
		}
		if length < 0 || length > 8<<20 {
			return nil, fmt.Errorf("CLOB 长度超过单列导出限制")
		}
		return val.ReadString(1, int(length))
	case *dm.DmBlob:
		if val == nil || !val.Valid {
			return nil, nil
		}
		length, err := val.GetLength()
		if err != nil {
			return nil, err
		}
		if length < 0 || length > 32<<20 {
			return nil, fmt.Errorf("BLOB 长度超过单列导出限制")
		}
		data := make([]byte, int(length))
		if length == 0 {
			return data, nil
		}
		n, err := val.ReadAt(0, data)
		if n != len(data) {
			return nil, io.ErrUnexpectedEOF
		}
		if err != nil && err != io.EOF {
			return nil, err
		}
		return data, nil
	default:
		return NormalizeCellValue(value, colType), nil
	}
}
