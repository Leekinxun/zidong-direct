package db

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func exportFormatFixtureRows(rows ...[]any) exportNextRow {
	var buffer []any
	return func() ([]any, error) {
		if len(rows) == 0 {
			return nil, io.EOF
		}
		buffer = append(buffer[:0], rows[0]...)
		rows = rows[1:]
		return buffer, nil
	}
}

func TestExportParquetRoundTrip(t *testing.T) {
	columns := []exportColumn{
		{Name: "订单"},
		{Name: "订单", DatabaseType: "BIGINT"},
		{Name: "订单_2", DatabaseType: "BOOLEAN"},
		{Name: "unsigned", DatabaseType: "BIGINT UNSIGNED"},
		{Name: "float", ScanType: reflect.TypeOf(float64(0))},
		{Name: "amount", DatabaseType: "DECIMAL"},
		{Name: "payload", DatabaseType: "BYTEA"},
		{Name: "day", DatabaseType: "DATE"},
	}
	binaryValue := []byte{0, 0xff, 'x'}
	date := time.Date(2024, time.December, 31, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	rows := exportFormatFixtureRows(
		[]any{"中文\n订单", int64(math.MinInt64), true, uint64(math.MaxUint64), 0.125,
			"987654321.01234567890123456789", binaryValue, date},
		[]any{"", nil, false, uint64(3), -1.5, nil, nil, nil},
	)
	var file bytes.Buffer
	if err := writeParquetExport(&file, columns, rows); err != nil {
		t.Fatal(err)
	}
	src, err := openParquetImport(context.Background(), bytes.NewReader(file.Bytes()), int64(file.Len()))
	if err != nil {
		t.Fatal(err)
	}
	defer src.finish()
	wantColumns := []string{"订单", "订单_3", "订单_2", "unsigned", "float", "amount", "payload", "day"}
	if !reflect.DeepEqual(src.columns, wantColumns) {
		t.Fatalf("Parquet columns = %q, want %q", src.columns, wantColumns)
	}
	wantRows := [][]any{
		{"中文\n订单", int64(math.MinInt64), true, "18446744073709551615", 0.125,
			"987654321.01234567890123456789", binaryValue, "2024-12-31"},
		{"", nil, false, int64(3), -1.5, nil, nil, nil},
	}
	assertExportFormatRows(t, src, wantRows)
}

func TestExportParquetEmptyResult(t *testing.T) {
	var file bytes.Buffer
	if err := writeParquetExport(&file, []exportColumn{{Name: "id", DatabaseType: "INTEGER"}}, exportFormatFixtureRows()); err != nil {
		t.Fatal(err)
	}
	src, err := openParquetImport(context.Background(), bytes.NewReader(file.Bytes()), int64(file.Len()))
	if err != nil {
		t.Fatal(err)
	}
	defer src.finish()
	if !reflect.DeepEqual(src.columns, []string{"id"}) {
		t.Fatalf("empty Parquet columns = %q", src.columns)
	}
	assertExportFormatRows(t, src, nil)
}

func TestExportParquetAcrossRowGroups(t *testing.T) {
	const rowCount = 4098
	var file bytes.Buffer
	var nextID int64
	next := func() ([]any, error) {
		if nextID == rowCount {
			return nil, io.EOF
		}
		value := nextID
		nextID++
		return []any{value}, nil
	}
	if err := writeParquetExport(&file, []exportColumn{{Name: "id", DatabaseType: "INTEGER"}}, next); err != nil {
		t.Fatal(err)
	}
	src, err := openParquetImport(context.Background(), bytes.NewReader(file.Bytes()), int64(file.Len()))
	if err != nil {
		t.Fatal(err)
	}
	defer src.finish()
	for id := int64(0); id < rowCount; id++ {
		row, err := src.read()
		if err != nil || !reflect.DeepEqual(row, []any{id}) {
			t.Fatalf("row %d = %#v, err %v", id, row, err)
		}
	}
	assertExportFormatRows(t, src, nil)
}

func TestExportDBFRoundTrip(t *testing.T) {
	columns := []exportColumn{
		{Name: "中文"},
		{Name: "bad name", DatabaseType: "BIGINT"},
		{Name: "bad-name", DatabaseType: "DECIMAL"},
		{Name: "bad_name_2", DatabaseType: "BOOL"},
		{Name: "entry date", DatabaseType: "DATE"},
		{Name: "price", DatabaseType: "DOUBLE PRECISION"},
	}
	rows := exportFormatFixtureRows(
		[]any{"上海", int64(math.MinInt64), "12.345", true, "2024-12-31", 0.125},
		[]any{"", nil, "-9.1", nil, nil, 2.5},
	)
	var file bytes.Buffer
	if err := writeDBFExport(&file, columns, rows); err != nil {
		t.Fatal(err)
	}
	content := file.Bytes()
	if content[0] != 0x03 || content[29] != 0x7a || content[len(content)-1] != 0x1a {
		t.Fatal("DBF file missing the version, encoding, or end marker")
	}
	count := binary.LittleEndian.Uint32(content[4:8])
	headerBytes := int(binary.LittleEndian.Uint16(content[8:10]))
	recordBytes := int(binary.LittleEndian.Uint16(content[10:12]))
	if count != 2 || headerBytes+int(count)*recordBytes+1 != len(content) {
		t.Fatal("DBF dimensions don't match the file size")
	}
	src, err := openDBF(bytes.NewReader(content), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantColumns := []string{"FIELD1", "BAD_NAME", "BAD_NAME_3", "BAD_NAME_2", "ENTRY_DATE", "PRICE"}
	if !reflect.DeepEqual(src.columns, wantColumns) {
		t.Fatalf("DBF columns = %q, want %q", src.columns, wantColumns)
	}
	wantRows := [][]any{
		{"上海", "-9223372036854775808", "12.345", true, "2024-12-31", "0.125"},
		{"", nil, "-9.100", nil, nil, "2.500"},
	}
	assertExportFormatRows(t, src, wantRows)
}

func TestExportDBFRejectsUnrepresentableValues(t *testing.T) {
	tests := []struct {
		name, typ, errorText string
		value                any
	}{
		{"text width", "TEXT", "254", strings.Repeat("汉", 128)},
		{"unencodable text", "TEXT", "GBK", "🪄"},
		{"binary", "BYTEA", "二进制", []byte{0, 1}},
		{"numeric width", "DECIMAL", "20", "12345678901234567890.01"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var file bytes.Buffer
			err := writeDBFExport(&file, []exportColumn{{Name: "value", DatabaseType: test.typ}},
				exportFormatFixtureRows([]any{test.value}))
			if err == nil || !strings.Contains(err.Error(), test.errorText) || file.Len() != 0 {
				t.Fatalf("expected DBF error containing %q and no partial output, got %v and %d bytes",
					test.errorText, err, file.Len())
			}
		})
	}
}

func TestExportDBFRejectsExcessRecordWidth(t *testing.T) {
	columns := make([]exportColumn, 16)
	row := make([]any, len(columns))
	for i := range columns {
		row[i] = strings.Repeat("x", 254)
	}
	var file bytes.Buffer
	err := writeDBFExport(&file, columns, exportFormatFixtureRows(row))
	if err == nil || !strings.Contains(err.Error(), "4000") || file.Len() != 0 {
		t.Fatalf("expected DBF width error and no partial output, got %v and %d bytes", err, file.Len())
	}
}

func assertExportFormatRows(t *testing.T, src importSource, expected [][]any) {
	t.Helper()
	for i, want := range expected {
		got, err := src.read()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("row %d = %#v, err %v, want %#v", i+1, got, err, want)
		}
	}
	if _, err := src.read(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected end of file, got %v", err)
	}
}
