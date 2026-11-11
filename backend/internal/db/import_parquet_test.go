package db

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestParquetImportReadsNamedTypedColumns(t *testing.T) {
	type record struct {
		Name  string  `parquet:"name"`
		Count int64   `parquet:"count"`
		Note  *string `parquet:"note,optional"`
	}
	for _, fixture := range []struct {
		name   string
		option parquet.WriterOption
	}{
		{"uncompressed", parquet.Compression(&parquet.Uncompressed)},
		{"snappy", parquet.Compression(&parquet.Snappy)},
		{"gzip", parquet.Compression(&parquet.Gzip)},
		{"zstd", parquet.Compression(&parquet.Zstd)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var data bytes.Buffer
			if err := parquet.Write(&data, []record{{Name: "中文", Count: 42}}, fixture.option); err != nil {
				t.Fatal(err)
			}
			source, err := openParquetImport(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()))
			if err != nil {
				t.Fatal(err)
			}
			defer source.finish()
			if !reflect.DeepEqual(source.columns, []string{"name", "count", "note"}) {
				t.Fatalf("columns = %#v", source.columns)
			}
			row, err := source.read()
			if err != nil || !reflect.DeepEqual(row, []any{"中文", int64(42), nil}) {
				t.Fatalf("row = %#v, error = %v", row, err)
			}
			if _, err := source.read(); !errors.Is(err, io.EOF) {
				t.Fatalf("expected EOF, got %v", err)
			}
		})
	}
}

func TestParquetImportRejectsOutOfBoundsFooter(t *testing.T) {
	fixture := []byte("PAR1....PAR1")
	binary.LittleEndian.PutUint32(fixture[4:8], 0xffffffff)
	if _, err := openParquetImport(context.Background(), bytes.NewReader(fixture), int64(len(fixture))); err == nil {
		t.Fatal("accepted a footer larger than the file")
	}
}
