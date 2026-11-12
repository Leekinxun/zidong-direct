package db

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/parquet-go/parquet-go"
)

type fileExportKind uint8

const (
	fileExportText fileExportKind = iota
	fileExportInteger
	fileExportUnsigned
	fileExportFloat
	fileExportDecimal
	fileExportBool
	fileExportDate
	fileExportBinary
)

func columnFileExportKind(column exportColumn) fileExportKind {
	typeName, _, _ := strings.Cut(strings.ToUpper(strings.TrimSpace(column.DatabaseType)), "(")
	unsigned := strings.Contains(typeName, "UNSIGNED")
	typeName = strings.TrimSpace(strings.ReplaceAll(typeName, "UNSIGNED", ""))
	switch typeName {
	case "INTEGER", "INT", "INT2", "INT4", "INT8", "TINYINT", "SMALLINT", "MEDIUMINT", "BIGINT",
		"SMALLSERIAL", "SERIAL", "BIGSERIAL":
		if unsigned {
			return fileExportUnsigned
		}
		return fileExportInteger
	case "REAL", "FLOAT", "FLOAT4", "FLOAT8", "DOUBLE", "DOUBLE PRECISION", "BINARY_FLOAT", "BINARY_DOUBLE":
		return fileExportFloat
	case "DECIMAL", "NUMERIC", "NUMBER", "MONEY", "SMALLMONEY", "DEC":
		return fileExportDecimal
	case "BOOLEAN", "BOOL":
		return fileExportBool
	case "DATE":
		return fileExportDate
	case "BIT":
		if column.ScanType != nil && column.ScanType.Kind() == reflect.Bool {
			return fileExportBool
		}
		return fileExportBinary
	}
	if isBinaryType(typeName) {
		return fileExportBinary
	}
	// A declared text, time or complex SQL type stays text, even if the driver
	// represents it as bytes or exposes a broad Go scan type.
	if typeName != "" || column.ScanType == nil {
		return fileExportText
	}
	switch column.ScanType.Kind() {
	case reflect.Bool:
		return fileExportBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fileExportInteger
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fileExportUnsigned
	case reflect.Float32, reflect.Float64:
		return fileExportFloat
	case reflect.Slice:
		if column.ScanType.Elem().Kind() == reflect.Uint8 {
			return fileExportBinary
		}
	}
	return fileExportText
}

func fileExportTextValue(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	case time.Time:
		return value.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(value)
	}
}

func fileExportBoolean(value any) (bool, error) {
	if raw, ok := value.([]byte); ok && len(raw) == 1 && raw[0] <= 1 {
		return raw[0] == 1, nil
	}
	switch strings.ToLower(strings.TrimSpace(fileExportTextValue(value))) {
	case "1", "t", "true", "y", "yes":
		return true, nil
	case "0", "f", "false", "n", "no":
		return false, nil
	default:
		return false, errors.New("无效的布尔值")
	}
}

func fileExportDateValue(value any) (time.Time, error) {
	date, ok := value.(time.Time)
	if !ok {
		text := strings.TrimSpace(fileExportTextValue(value))
		for _, layout := range []string{"2006-01-02", "2006-01-02 15:04:05", time.RFC3339Nano} {
			if parsed, err := time.Parse(layout, text); err == nil {
				date, ok = parsed, true
				break
			}
		}
	}
	if !ok || date.Year() < 1 || date.Year() > 9999 {
		return time.Time{}, errors.New("无效的日期")
	}
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC), nil
}

type orderedParquetGroup struct {
	parquet.Group
	fields []parquet.Field
}

func (g orderedParquetGroup) Fields() []parquet.Field { return g.fields }

func writeParquetExport(w io.Writer, columns []exportColumn, next exportNextRow) error {
	if len(columns) == 0 || len(columns) > maxImportColumns {
		return fmt.Errorf("Parquet 导出支持 1 至 %d 列", maxImportColumns)
	}
	names, err := parquetExportNames(columns)
	if err != nil {
		return err
	}
	kinds := make([]fileExportKind, len(columns))
	group := make(parquet.Group, len(columns))
	for i, column := range columns {
		kinds[i] = columnFileExportKind(column)
		group[names[i]] = parquet.Optional(parquetExportNode(kinds[i]))
	}
	byName := make(map[string]parquet.Field, len(group))
	for _, field := range group.Fields() {
		byName[field.Name()] = field
	}
	fields := make([]parquet.Field, len(names))
	for i, name := range names {
		fields[i] = byName[name]
	}
	schema := parquet.NewSchema("exported_data", orderedParquetGroup{Group: group, fields: fields})
	writer := parquet.NewWriter(w, schema, parquet.Compression(&parquet.Snappy), parquet.DataPageStatistics(false))
	row := make(parquet.Row, len(columns))
	batch := []parquet.Row{row}
	const maxGroupBytes = 32 << 20
	var groupBytes, groupRows int

	for rowNumber := 1; ; rowNumber++ {
		values, err := next()
		if errors.Is(err, io.EOF) {
			return writer.Close()
		}
		if err != nil {
			return err
		}
		if len(values) != len(columns) {
			return fmt.Errorf("第 %d 行的列数与 Parquet 表头不符", rowNumber)
		}
		rowBytes := len(columns) * 8
		for i, raw := range values {
			if raw == nil {
				row[i] = parquet.NullValue().Level(0, 0, i)
				continue
			}
			value, size, err := parquetExportValue(raw, kinds[i])
			if err != nil {
				return fmt.Errorf("第 %d 行的 Parquet 字段 %q 无法导出：%w", rowNumber, names[i], err)
			}
			rowBytes += size
			if rowBytes > maxGroupBytes {
				return fmt.Errorf("第 %d 行超过 Parquet 导出单行 32 MiB 限制", rowNumber)
			}
			row[i] = value.Level(0, 1, i)
		}
		if groupRows == 4096 || groupRows > 0 && groupBytes+rowBytes > maxGroupBytes {
			if err := writer.Flush(); err != nil {
				return err
			}
			groupBytes, groupRows = 0, 0
		}
		if n, err := writer.WriteRows(batch); err != nil {
			return err
		} else if n != len(batch) {
			return io.ErrShortWrite
		}
		groupBytes += rowBytes
		groupRows++
	}
}

func parquetExportNames(columns []exportColumn) ([]string, error) {
	names := make([]string, len(columns))
	reserved := make(map[string]bool, len(columns))
	for i, column := range columns {
		name := column.Name
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}
		if !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') {
			return nil, fmt.Errorf("第 %d 个 Parquet 字段名无效", i+1)
		}
		names[i] = name
		reserved[name] = true
	}
	used := make(map[string]bool, len(columns))
	for i, name := range names {
		if used[name] {
			for suffix := 2; ; suffix++ {
				candidate := name + "_" + strconv.Itoa(suffix)
				if !reserved[candidate] && !used[candidate] {
					names[i] = candidate
					break
				}
			}
		}
		used[names[i]] = true
	}
	return names, nil
}

func parquetExportNode(kind fileExportKind) parquet.Node {
	switch kind {
	case fileExportInteger:
		return parquet.Int(64)
	case fileExportUnsigned:
		return parquet.Uint(64)
	case fileExportFloat:
		return parquet.Leaf(parquet.DoubleType)
	case fileExportBool:
		return parquet.Leaf(parquet.BooleanType)
	case fileExportDate:
		return parquet.Date()
	case fileExportBinary:
		return parquet.Leaf(parquet.ByteArrayType)
	default:
		// Preserve arbitrary SQL decimal precision as text when precision and
		// scale metadata aren't available for the full result.
		return parquet.String()
	}
}

func parquetExportValue(raw any, kind fileExportKind) (parquet.Value, int, error) {
	switch kind {
	case fileExportInteger:
		value, err := strconv.ParseInt(strings.TrimSpace(fileExportTextValue(raw)), 10, 64)
		if err != nil {
			return parquet.Value{}, 0, errors.New("无效的 64 位整数")
		}
		return parquet.Int64Value(value), 0, nil
	case fileExportUnsigned:
		value, err := strconv.ParseUint(strings.TrimSpace(fileExportTextValue(raw)), 10, 64)
		if err != nil {
			return parquet.Value{}, 0, errors.New("无效的无符号 64 位整数")
		}
		return parquet.ValueOf(value), 0, nil
	case fileExportFloat:
		switch value := raw.(type) {
		case float32:
			return parquet.DoubleValue(float64(value)), 0, nil
		case float64:
			return parquet.DoubleValue(value), 0, nil
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(fileExportTextValue(raw)), 64)
		if err != nil {
			return parquet.Value{}, 0, errors.New("无效的浮点数")
		}
		return parquet.DoubleValue(value), 0, nil
	case fileExportBool:
		value, err := fileExportBoolean(raw)
		return parquet.BooleanValue(value), 0, err
	case fileExportDate:
		value, err := fileExportDateValue(raw)
		if err != nil {
			return parquet.Value{}, 0, err
		}
		return parquet.Int32Value(int32(value.Unix() / 86400)), 0, nil
	case fileExportBinary:
		value, ok := raw.([]byte)
		if !ok {
			value = []byte(fileExportTextValue(raw))
		}
		return parquetExportBytes(value)
	default:
		value := fileExportTextValue(raw)
		if !utf8.ValidString(value) {
			return parquet.Value{}, 0, errors.New("文本不是有效的 UTF-8")
		}
		return parquetExportBytes([]byte(value))
	}
}

func parquetExportBytes(value []byte) (parquet.Value, int, error) {
	if len(value) > maxParquetPage/2 {
		return parquet.Value{}, 0, errors.New("单个文本或二进制值超过 16 MiB")
	}
	return parquet.ByteArrayValue(value), len(value), nil
}
