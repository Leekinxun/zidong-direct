package db

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
)

func TestExportXMLRetainsNamesNullsAndBinary(t *testing.T) {
	var output bytes.Buffer
	columns := []string{"id", "显示 名称", "empty", "bytes", "control"}
	expected := []any{"1", "中文 & <文本>", nil, []byte{0, 255}, "left\x01right"}
	read := false
	if err := writeXMLExport(&output, columns, func() ([]any, error) {
		if read {
			return nil, io.EOF
		}
		read = true
		return expected, nil
	}); err != nil {
		t.Fatal(err)
	}
	source, err := xmlImport(context.Background(), bytes.NewReader(output.Bytes()), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	values, err := source.read()
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]any, len(values))
	for i, name := range source.columns {
		result[name] = values[i]
	}
	for i, name := range columns {
		if !reflect.DeepEqual(result[name], expected[i]) {
			t.Errorf("column %q = %#v, want %#v", name, result[name], expected[i])
		}
	}
}

func TestFixedWidthExportRoundTripsChinese(t *testing.T) {
	var output bytes.Buffer
	read := false
	if err := writeTextExport(&output, "txt", []string{"id", "名称"}, ExportOptions{Widths: "4,8"}, func() ([]any, error) {
		if read {
			return nil, io.EOF
		}
		read = true
		return []any{7, "测试"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	source, err := openImportSource(context.Background(), bytes.NewReader(output.Bytes()), int64(output.Len()), ImportOptions{
		Format: "txt", Widths: "4,8",
	})
	if err != nil {
		t.Fatal(err)
	}
	values, err := source.read()
	if err != nil || !reflect.DeepEqual(values, []any{"7", "测试"}) {
		t.Fatalf("row %#v, error %v", values, err)
	}
}
