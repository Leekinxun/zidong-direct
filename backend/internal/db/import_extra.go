package db

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type htmlTableData struct {
	name string
	rows [][]string
}

func htmlImport(ctx context.Context, reader io.Reader, opts ImportOptions) (importSource, error) {
	decoder := xml.NewDecoder(reader)
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity

	var tables []htmlTableData
	var currentTable *htmlTableData
	var currentRow []string
	var inRow bool
	var inCell bool
	var inCaption bool
	var cellBuf strings.Builder
	var captionBuf strings.Builder

	for {
		if err := ctx.Err(); err != nil {
			return importSource{}, err
		}
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// HTML fragments in the wild may have trailing unclosed tags; if we already parsed rows, use them.
			if len(tables) > 0 || (currentTable != nil && len(currentTable.rows) > 0) {
				break
			}
			return importSource{}, fmt.Errorf("解析 HTML 表格失败: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			tag := strings.ToLower(t.Name.Local)
			switch tag {
			case "table":
				name := fmt.Sprintf("Table %d", len(tables)+1)
				for _, attr := range t.Attr {
					k := strings.ToLower(attr.Name.Local)
					if (k == "id" || k == "data-name" || k == "title") && strings.TrimSpace(attr.Value) != "" {
						name = strings.TrimSpace(attr.Value)
						break
					}
				}
				currentTable = &htmlTableData{name: name}
			case "caption":
				if currentTable != nil {
					inCaption = true
					captionBuf.Reset()
				}
			case "tr":
				if currentTable != nil {
					inRow = true
					currentRow = nil
				}
			case "td", "th":
				if inRow {
					inCell = true
					cellBuf.Reset()
				}
			case "br":
				if inCell {
					cellBuf.WriteByte('\n')
				}
			}
		case xml.EndElement:
			tag := strings.ToLower(t.Name.Local)
			switch tag {
			case "caption":
				if inCaption && currentTable != nil {
					if capText := strings.TrimSpace(captionBuf.String()); capText != "" {
						currentTable.name = capText
					}
					inCaption = false
				}
			case "td", "th":
				if inCell {
					currentRow = append(currentRow, strings.TrimSpace(cellBuf.String()))
					if len(currentRow) > maxImportColumns {
						return importSource{}, errors.New("HTML 表格列数过多")
					}
					inCell = false
				}
			case "tr":
				if inRow && currentTable != nil {
					if len(currentRow) > 0 {
						currentTable.rows = append(currentTable.rows, currentRow)
					}
					inRow = false
				}
			case "table":
				if currentTable != nil {
					if len(currentTable.rows) > 0 {
						tables = append(tables, *currentTable)
					}
					currentTable = nil
				}
			}
		case xml.CharData:
			if inCell {
				cellBuf.Write(t)
			} else if inCaption {
				captionBuf.Write(t)
			}
		}
	}
	if currentTable != nil && len(currentTable.rows) > 0 {
		tables = append(tables, *currentTable)
	}
	if len(tables) == 0 {
		return importSource{}, errors.New("HTML 文件中未找到包含数据的 <table> 表格")
	}

	sheets := make([]string, len(tables))
	selectedIdx := 0
	for i, tbl := range tables {
		sheets[i] = tbl.name
		if opts.Sheet != "" && strings.EqualFold(tbl.name, opts.Sheet) {
			selectedIdx = i
		}
	}
	chosen := tables[selectedIdx]
	rowIdx := 0
	src, err := tabularImport(func() ([]any, error) {
		if rowIdx >= len(chosen.rows) {
			return nil, io.EOF
		}
		r := chosen.rows[rowIdx]
		rowIdx++
		out := make([]any, len(r))
		for i, v := range r {
			out[i] = v
		}
		return out, nil
	}, opts)
	if err != nil {
		return importSource{}, err
	}
	src.sheets = sheets
	src.sheet = chosen.name
	return src, nil
}

func openSQLiteImport(ctx context.Context, file io.ReaderAt, size int64, opts ImportOptions) (importSource, error) {
	if size <= 0 {
		return importSource{}, errors.New("SQLite 文件为空")
	}
	tmp, err := os.CreateTemp("", "zidong-sqlite-import-*.db")
	if err != nil {
		return importSource{}, err
	}
	cleanup := func() error {
		_ = tmp.Close()
		return os.Remove(tmp.Name())
	}
	if _, err := io.Copy(tmp, io.NewSectionReader(file, 0, size)); err != nil {
		_ = cleanup()
		return importSource{}, fmt.Errorf("读取 SQLite 文件失败: %w", err)
	}
	_ = tmp.Close()

	conn, err := sql.Open("sqlite", tmp.Name())
	if err != nil {
		_ = os.Remove(tmp.Name())
		return importSource{}, fmt.Errorf("打开 SQLite 源数据库失败: %w", err)
	}

	rows, err := conn.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		_ = conn.Close()
		_ = os.Remove(tmp.Name())
		return importSource{}, fmt.Errorf("读取 SQLite 源表列表失败: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr == nil && strings.TrimSpace(name) != "" {
			tables = append(tables, name)
		}
	}
	_ = rows.Close()
	if len(tables) == 0 {
		_ = conn.Close()
		_ = os.Remove(tmp.Name())
		return importSource{}, errors.New("SQLite 文件中没有可导入的数据表或视图")
	}

	targetTable := strings.TrimSpace(opts.Sheet)
	if targetTable == "" {
		targetTable = strings.TrimSpace(opts.SourceTable)
	}
	selected := tables[0]
	if targetTable != "" {
		found := false
		for _, t := range tables {
			if strings.EqualFold(t, targetTable) {
				selected = t
				found = true
				break
			}
		}
		if !found {
			_ = conn.Close()
			_ = os.Remove(tmp.Name())
			return importSource{}, fmt.Errorf("SQLite 文件中不存在表 %q", targetTable)
		}
	}

	quoted := `"` + strings.ReplaceAll(selected, `"`, `""`) + `"`
	dataRows, err := conn.QueryContext(ctx, "SELECT * FROM "+quoted)
	if err != nil {
		_ = conn.Close()
		_ = os.Remove(tmp.Name())
		return importSource{}, fmt.Errorf("查询 SQLite 源表 %s 失败: %w", selected, err)
	}
	cols, err := dataRows.Columns()
	if err != nil {
		_ = dataRows.Close()
		_ = conn.Close()
		_ = os.Remove(tmp.Name())
		return importSource{}, err
	}

	return importSource{
		columns: cols,
		sheets:  tables,
		sheet:   selected,
		next: func() ([]any, error) {
			if !dataRows.Next() {
				if err := dataRows.Err(); err != nil {
					return nil, err
				}
				return nil, io.EOF
			}
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := dataRows.Scan(ptrs...); err != nil {
				return nil, err
			}
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					values[i] = string(b)
				}
			}
			return values, nil
		},
		close: func() error {
			err1 := dataRows.Close()
			err2 := conn.Close()
			err3 := os.Remove(tmp.Name())
			return errors.Join(err1, err2, err3)
		},
	}, nil
}
