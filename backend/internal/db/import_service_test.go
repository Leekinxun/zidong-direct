package db

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestImportPreviewAndWriteModes(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("CREATE TABLE demo (id INTEGER PRIMARY KEY, name TEXT UNIQUE)"); err != nil {
		t.Fatal(err)
	}
	dialect, err := GetDialect(EngineSQLite)
	if err != nil {
		t.Fatal(err)
	}
	mapping := []ImportMapping{{Source: 1, Target: "id"}, {Source: 0, Target: "name"}}
	source := "名称;编号\n新名称;1\n其他;2\n"
	gb, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	options := ImportOptions{Format: "csv", Encoding: "gb18030", Delimiter: ";"}
	preview, err := PreviewImport(ctx, conn, dialect, "", "", "demo", bytes.NewReader(gb), int64(len(gb)), options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Columns, []string{"名称", "编号"}) ||
		!reflect.DeepEqual(preview.Rows[0], []any{"新名称", "1"}) || !preview.TargetColumns[0].PrimaryKey {
		t.Fatalf("preview: %#v", preview)
	}

	run := func(content string, opts ImportOptions) (ImportSummary, error) {
		return ImportRows(ctx, conn, dialect, "", "", "demo", strings.NewReader(content), int64(len(content)), opts)
	}
	summary, err := ImportRows(ctx, conn, dialect, "", "", "demo", bytes.NewReader(gb), int64(len(gb)),
		ImportOptions{Format: "csv", Encoding: "gb18030", Delimiter: ";", Mappings: mapping})
	if err != nil || summary.InsertedRows != 2 {
		t.Fatalf("append: %+v %v", summary, err)
	}
	for _, tt := range []struct {
		mode, content string
		expect        ImportSummary
	}{
		{"upsert", "id,name\n1,变更\n3,新建", ImportSummary{InsertedRows: 1, UpdatedRows: 1}},
		{"update", "id,name\n1,再次变更\n4,无匹配", ImportSummary{UpdatedRows: 1, SkippedRows: 1}},
		{"skip", "id,name\n1,忽略\n4,追加", ImportSummary{InsertedRows: 1, SkippedRows: 1}},
		{"delete", "id\n2\n20", ImportSummary{DeletedRows: 1, SkippedRows: 1}},
	} {
		got, err := run(tt.content, ImportOptions{Format: "csv", Mode: tt.mode})
		if err != nil || !reflect.DeepEqual(got, tt.expect) {
			t.Fatalf("%s: %+v %v", tt.mode, got, err)
		}
	}
	got, err := run("id,name\n5,追加\n6,未冲突", ImportOptions{Format: "csv", Mode: "append", ContinueOnError: true})
	if err != nil || got.InsertedRows != 1 || got.FailedRows != 1 || len(got.Errors) != 1 {
		t.Fatalf("row savepoint: %+v %v", got, err)
	}
	for _, tt := range []struct{ format, source string }{
		{"csv", "id,name\n7,短暂\n8,短暂"},
		{"csv", "id,name\n"},
		{"json", `[{"id":7,"name":"truncated"}`},
	} {
		if _, err := run(tt.source, ImportOptions{Format: tt.format, Mode: "replace"}); err == nil {
			t.Fatalf("replace should fail for %q", tt.source)
		}
	}
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM demo").Scan(&count); err != nil || count != 4 {
		t.Fatalf("replace should preserve existing rows, got %d (%v)", count, err)
	}

	if _, err := run(`[{"id":1},{"id":3,"name":null}]`, ImportOptions{Format: "json", Mode: "update"}); err == nil {
		t.Fatal("update should reject missing mapped cells")
	}
	if _, err := run(`[{"id":3,"name":null}]`, ImportOptions{Format: "json", Mode: "update"}); err != nil {
		t.Fatalf("explicit NULL update: %v", err)
	}
	var name sql.NullString
	if err := conn.QueryRow("SELECT name FROM demo WHERE id=3").Scan(&name); err != nil || name.Valid {
		t.Fatalf("name should be NULL: %+v (%v)", name, err)
	}
	summary, err = run("id,name\n8,替换", ImportOptions{Format: "csv", Mode: "replace"})
	if err != nil || summary.DeletedRows != 4 || summary.InsertedRows != 1 {
		t.Fatalf("replace: %+v %v", summary, err)
	}
}

func TestImportSourceFormats(t *testing.T) {
	workbook := importZipFixture(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="一" r:id="rId1"/><sheet name="二" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><r><t>中文</t></r><r><t>表格</t></r></si></sst>`,
		"xl/worksheets/sheet2.xml":   `<worksheet><sheetData><row><c r="A1" t="inlineStr"><is><t>id</t></is></c><c r="B1" t="inlineStr"><is><t>name</t></is></c></row><row><c r="A2"><v>2</v></c><c r="B2" t="s"><v>0</v></c></row></sheetData></worksheet>`,
	})
	ods := importZipFixture(t, map[string]string{
		"content.xml": `<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0">
<office:body><office:spreadsheet><table:table table:name="工作表">
<table:table-row><table:table-cell><text:p>id</text:p></table:table-cell><table:table-cell><text:p>name</text:p></table:table-cell></table:table-row>
<table:table-row><table:table-cell office:value-type="float" office:value="2"/><table:table-cell><text:p><text:span>跨样式</text:span><text:s text:c="2"/>文本</text:p></table:table-cell><table:table-cell table:number-columns-repeated="16382"/></table:table-row>
</table:table></office:spreadsheet></office:body></office:document-content>`,
	})
	for _, tt := range []struct {
		name    string
		data    []byte
		opts    ImportOptions
		columns []string
		row     []any
	}{
		{"fixed-width", []byte("id名称    \n02中文    "), ImportOptions{Format: "txt", Widths: "2,6"}, []string{"id", "名称"}, []any{"02", "中文"}},
		{"tsv-no-header", []byte("1\t中文\n"), ImportOptions{Format: "tsv", NoHeader: true}, []string{"列 1", "列 2"}, []any{"1", "中文"}},
		{"json-lines", []byte("{\"id\":1,\"name\":\"中文\"}\n"), ImportOptions{Format: "jsonl"}, []string{"id", "name"}, []any{int64(1), "中文"}},
		{"xml", []byte(`<root><record id="2"><name>中文</name></record></root>`), ImportOptions{Format: "xml", XMLRow: "record"}, []string{"id", "name"}, []any{"2", "中文"}},
		{"xlsx", workbook, ImportOptions{Format: "xlsx", Sheet: "二"}, []string{"id", "name"}, []any{"2", "中文表格"}},
		{"ods", ods, ImportOptions{Format: "ods"}, []string{"id", "name"}, []any{"2", "跨样式  文本"}},
		{"dbf", importDBFFixture(), ImportOptions{Format: "dbf"}, []string{"ID", "NAME"}, []any{"2", "Café"}},
		{"html", []byte(`<html><body><table><tr><th>id</th><th>name</th></tr><tr><td>2</td><td>网页表格</td></tr></table></body></html>`), ImportOptions{Format: "html"}, []string{"id", "name"}, []any{"2", "网页表格"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src, err := openImportSource(context.Background(), bytes.NewReader(tt.data), int64(len(tt.data)), tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer src.finish()
			row, err := src.read()
			if err != nil || !reflect.DeepEqual(src.columns, tt.columns) || !reflect.DeepEqual(row, tt.row) {
				t.Fatalf("columns=%v row=%v err=%v", src.columns, row, err)
			}
			if _, err := src.read(); !errors.Is(err, io.EOF) {
				t.Fatalf("expected EOF, got %v", err)
			}
		})
	}
}

func importZipFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, contents := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func importDBFFixture() []byte {
	header := make([]byte, 32+32+32+1)
	header[0], header[29] = 0x03, 0x03
	binary.LittleEndian.PutUint32(header[4:], 1)
	binary.LittleEndian.PutUint16(header[8:], uint16(len(header)))
	binary.LittleEndian.PutUint16(header[10:], 1+3+6)
	copy(header[32:], "ID")
	header[32+11], header[32+16] = 'N', 3
	copy(header[64:], "NAME")
	header[64+11], header[64+16] = 'C', 6
	header[len(header)-1] = 0x0d
	return append(header, []byte("   2Caf\xe9  ")...)
}
