package db

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shakinm/xlsReader/cfb"
)

func TestXLSExportRoundTrip(t *testing.T) {
	columns := []exportColumn{{Name: "文本"}, {Name: "整数"}, {Name: "数值"}, {Name: "布尔"}, {Name: "日期"}, {Name: "二进制"}, {Name: "空值"}}
	datetime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.FixedZone("UTC+8", 8*3600))
	values := []any{"=literal\n中文😀", uint64(1234567890123456789), 12.5, true, datetime, []byte{0, 255}, nil}
	buf := exportXLSRowsForTest(t, columns, [][]any{values})
	source, err := openXLSImport(context.Background(), bytes.NewReader(buf), int64(len(buf)), ImportOptions{Format: "xls"})
	if err != nil {
		t.Fatal(err)
	}
	defer source.finish()
	wantColumns := []string{"文本", "整数", "数值", "布尔", "日期", "二进制", "空值"}
	if !reflect.DeepEqual(source.columns, wantColumns) {
		t.Fatalf("columns = %#v", source.columns)
	}
	row, err := source.read()
	want := []any{values[0], "1234567890123456789", "12.5", "TRUE", "2025-01-02T03:04:05+08:00", "[HEX] 00ff", nil}
	if err != nil || !reflect.DeepEqual(row, want) {
		t.Fatalf("row = %#v, err = %v", row, err)
	}
	if _, err := source.read(); err != io.EOF {
		t.Fatalf("EOF = %v", err)
	}
}

func TestXLSExportContinuedStringsAndRowBlocks(t *testing.T) {
	columns := []exportColumn{{Name: "长文本"}, {Name: "索引"}}
	rows := make([][]any, 67)
	for i := range rows {
		rows[i] = []any{fmt.Sprintf("记录 %d", i), i}
	}
	rows[31][0] = strings.Repeat("多😀行\n", 3000)
	buf := exportXLSRowsForTest(t, columns, rows)
	source, err := openXLSImport(context.Background(), bytes.NewReader(buf), int64(len(buf)), ImportOptions{Format: "xls"})
	if err != nil {
		t.Fatal(err)
	}
	defer source.finish()
	for i := range rows {
		row, err := source.read()
		if err != nil || !reflect.DeepEqual(row, []any{rows[i][0], fmt.Sprint(i)}) {
			t.Fatalf("row %d did not round-trip, len = %d, err = %v", i, len(row), err)
		}
	}
}

func TestXLSCompoundAdditionalFATSectors(t *testing.T) {
	workbook := bytes.Repeat([]byte{0x76, 0x17, 0x83}, 3_000_000)
	var buf bytes.Buffer
	if err := writeXLSCompound(&buf, workbook); err != nil {
		t.Fatal(err)
	}
	container, err := cfb.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	entries := container.GetDirs()
	stream, err := container.OpenObject(entries[1], entries[0])
	if err != nil {
		t.Fatal(err)
	}
	output := make([]byte, len(workbook))
	if _, err := io.ReadFull(stream, output); err != nil || !bytes.Equal(workbook, output) {
		t.Fatalf("additional FAT stream did not round-trip: %v", err)
	}
}

func TestXLSExportLastStringContinuation(t *testing.T) {
	columns := []exportColumn{{Name: "x"}}
	label := strings.Repeat("中", 12000)
	buf := exportXLSRowsForTest(t, columns, [][]any{{label}})
	source, err := openXLSImport(context.Background(), bytes.NewReader(buf), int64(len(buf)), ImportOptions{Format: "xls"})
	if err != nil {
		t.Fatal(err)
	}
	defer source.finish()
	row, err := source.read()
	if err != nil || !reflect.DeepEqual(row, []any{label}) {
		t.Fatalf("last string did not round-trip, len = %d, err = %v", len(row), err)
	}
}

func exportXLSRowsForTest(t *testing.T, columns []exportColumn, rows [][]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := writeXLSExport(&buf, columns, func() ([]any, error) {
		if len(rows) == 0 {
			return nil, io.EOF
		}
		row := rows[0]
		rows = rows[1:]
		return row, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
