package db

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWorkbookExportRoundTrip(t *testing.T) {
	columns := []exportColumn{
		{Name: "编号"}, {Name: "启用"}, {Name: "数值"}, {Name: "空单元格"}, {Name: "说明"},
		{Name: "大整数"}, {Name: "二进制"}, {Name: "时间"}, {Name: "公式样式文本"},
	}
	timestamp := time.Date(2026, time.November, 12, 10, 15, 2, 123456, time.FixedZone("fixture", -8*3600))
	literal := "  中文 <&> \t第一行\n第二行\r\n_x000D_  "
	row := []any{int64(42), true, 12.25, nil, literal, uint64(math.MaxUint64), []byte{0, 255}, timestamp, "=1+2"}
	for _, format := range []string{"xlsx", "ods"} {
		t.Run(format, func(t *testing.T) {
			var file bytes.Buffer
			next := workbookFixtureRows(row, []any{int64(43), false, 0.5, nil, "", int64(7), []byte{1}, timestamp, "-1+3"})
			if err := writeWorkbookExport(&file, format, columns, next); err != nil {
				t.Fatal(err)
			}
			reader := bytes.NewReader(file.Bytes())
			archive, err := zip.NewReader(reader, int64(file.Len()))
			if err != nil {
				t.Fatal(err)
			}
			if format == "ods" {
				mimetype := archive.File[0]
				if mimetype.Name != "mimetype" || mimetype.Method != zip.Store || len(mimetype.Extra) != 0 {
					t.Fatalf("first ODS entry is not an uncompressed mimetype: %#v", mimetype.FileHeader)
				}
				data, err := archive.Open("mimetype")
				if err != nil {
					t.Fatal(err)
				}
				mime, err := io.ReadAll(data)
				data.Close()
				if err != nil || string(mime) != odsMIMEType {
					t.Fatalf("bad mimetype %q: %v", mime, err)
				}
			}

			source, err := openWorkbook(reader, int64(file.Len()), ImportOptions{Format: format})
			if err != nil {
				t.Fatal(err)
			}
			defer source.finish()
			if source.sheet != "Data" || len(source.columns) != len(columns) {
				t.Fatalf("sheet %q, header %v", source.sheet, source.columns)
			}
			for i, column := range columns {
				if source.columns[i] != column.Name {
					t.Fatalf("column %d: %q", i, source.columns[i])
				}
			}
			actual, err := source.read()
			if err != nil {
				t.Fatal(err)
			}
			var empty any
			if format == "ods" {
				empty = missingImportValue{}
			}
			want := []any{"42", true, "12.25", empty, literal, "18446744073709551615", "[HEX] 00ff", timestamp.Format(time.RFC3339Nano), "=1+2"}
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("row mismatch\n got: %#v\nwant: %#v", actual, want)
			}
			actual, err = source.read()
			want = []any{"43", false, "0.5", empty, "", "7", "[HEX] 01", timestamp.Format(time.RFC3339Nano), "-1+3"}
			if err != nil || !reflect.DeepEqual(actual, want) {
				t.Fatalf("second row: %#v, %v", actual, err)
			}
			if _, err := source.read(); err != io.EOF {
				t.Fatalf("end of workbook: %v", err)
			}
		})
	}
}

func TestXLSXExportEscapesControlCharacters(t *testing.T) {
	value := "😀\x00\r_x000A_"
	var file bytes.Buffer
	if err := writeWorkbookExport(&file, "xlsx", []exportColumn{{Name: "文本"}}, workbookFixtureRows([]any{value})); err != nil {
		t.Fatal(err)
	}
	source, err := openWorkbook(bytes.NewReader(file.Bytes()), int64(file.Len()), ImportOptions{Format: "xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	defer source.finish()
	row, err := source.read()
	if err != nil || !reflect.DeepEqual(row, []any{value}) {
		t.Fatalf("escaped text: %#v, %v", row, err)
	}
	if actual := decodeXLSXText("_xD83D__xDE00_"); actual != "😀" {
		t.Fatalf("surrogate pair: %q", actual)
	}
}

func TestWorkbookExportValidatesRowsAndText(t *testing.T) {
	columns := []exportColumn{{Name: "text"}}
	if err := writeWorkbookExport(io.Discard, "xlsx", columns, workbookFixtureRows([]any{strings.Repeat("😀", xlsxMaxCellUnits/2+1)})); err == nil {
		t.Fatal("accepted a string over Excel's UTF-16 cell limit")
	}
	if err := writeWorkbookExport(io.Discard, "ods", columns, workbookFixtureRows([]any{"\x00"})); err == nil {
		t.Fatal("accepted text with an XML-invalid control in an ODS file")
	}
	if err := writeWorkbookExport(io.Discard, "xlsx", columns, workbookFixtureRows([]any{1, 2})); err == nil {
		t.Fatal("accepted a row with the wrong column count")
	}
	sourceErr := errors.New("fixture read failed")
	err := writeWorkbookExport(io.Discard, "ods", columns, func() ([]any, error) { return nil, sourceErr })
	if !errors.Is(err, sourceErr) {
		t.Fatalf("source failure: %v", err)
	}
}

func workbookFixtureRows(rows ...[]any) exportNextRow {
	return func() ([]any, error) {
		if len(rows) == 0 {
			return nil, io.EOF
		}
		row := rows[0]
		rows = rows[1:]
		return row, nil
	}
}
