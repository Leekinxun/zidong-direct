package db

import (
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	maxDBFFields      = 255
	maxDBFRecordBytes = 4000
	maxDBFTextBytes   = 254
	maxDBFNumberBytes = 20
	dbfFieldNameBytes = 10
)

type dbfExportField struct {
	name     string
	kind     fileExportKind
	dbfType  byte
	width    int
	whole    int
	decimals int
}

func writeDBFExport(w io.Writer, columns []exportColumn, next exportNextRow) error {
	if len(columns) == 0 || len(columns) > maxDBFFields {
		return fmt.Errorf("DBF 导出支持 1 至 %d 列", maxDBFFields)
	}
	fields := dbfExportFields(columns)

	// DBF writes the row count and field widths before its first record.
	// Spool normalized cells while measuring widths so large exports don't
	// require holding every row in memory or truncating to guessed widths.
	spool, err := os.CreateTemp("", "zidong-export-dbf-*.tmp")
	if err != nil {
		return fmt.Errorf("创建 DBF 临时文件失败：%w", err)
	}
	defer os.Remove(spool.Name())
	defer spool.Close()
	encoder := gob.NewEncoder(spool)
	values := make([]string, len(fields))
	recordBytes := 1
	for _, field := range fields {
		recordBytes += field.width
	}
	var count uint32
	for {
		row, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if count == math.MaxUint32 {
			return errors.New("导出行数超过 DBF 格式限制")
		}
		if len(row) != len(fields) {
			return fmt.Errorf("第 %d 行的列数与 DBF 表头不符", count+1)
		}
		for i, raw := range row {
			previousWidth := fields[i].width
			values[i], err = fields[i].measure(raw)
			if err != nil {
				return fmt.Errorf("第 %d 行的 DBF 字段 %q 无法导出：%w", count+1, columns[i].Name, err)
			}
			recordBytes += fields[i].width - previousWidth
			if recordBytes > maxDBFRecordBytes {
				return fmt.Errorf("DBF 每行宽度超过 %d 字节限制", maxDBFRecordBytes)
			}
		}
		if err := encoder.Encode(values); err != nil {
			return err
		}
		count++
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := writeDBFHeader(w, fields, recordBytes, count); err != nil {
		return err
	}
	return writeDBFRecords(w, gob.NewDecoder(spool), fields, values, recordBytes, count)
}

func dbfExportFields(columns []exportColumn) []dbfExportField {
	fields := make([]dbfExportField, len(columns))
	reserved := make(map[string]bool, len(columns))
	for i, column := range columns {
		field := dbfExportField{
			name:    dbfBaseFieldName(column.Name, i),
			kind:    columnFileExportKind(column),
			dbfType: 'C',
			width:   1,
		}
		switch field.kind {
		case fileExportInteger, fileExportUnsigned, fileExportFloat, fileExportDecimal:
			field.dbfType, field.whole = 'N', 1
		case fileExportBool:
			field.dbfType = 'L'
		case fileExportDate:
			field.dbfType, field.width = 'D', 8
		}
		fields[i] = field
		reserved[field.name] = true
	}

	used := make(map[string]bool, len(columns))
	for i := range fields {
		base := fields[i].name
		if used[base] {
			for n := 2; ; n++ {
				suffix := "_" + strconv.Itoa(n)
				prefix := base
				if len(prefix)+len(suffix) > dbfFieldNameBytes {
					prefix = prefix[:dbfFieldNameBytes-len(suffix)]
				}
				candidate := prefix + suffix
				if !used[candidate] && !reserved[candidate] {
					fields[i].name = candidate
					break
				}
			}
		}
		used[fields[i].name] = true
	}
	return fields
}

func dbfBaseFieldName(name string, index int) string {
	var safe strings.Builder
	for _, ch := range strings.ToUpper(strings.TrimSpace(name)) {
		if safe.Len() == dbfFieldNameBytes {
			break
		}
		if ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' {
			safe.WriteByte(byte(ch))
		} else {
			safe.WriteByte('_')
		}
	}
	result := strings.Trim(safe.String(), "_")
	if result == "" {
		return "FIELD" + strconv.Itoa(index+1)
	}
	if result[0] >= '0' && result[0] <= '9' {
		result = "F" + result
		if len(result) > dbfFieldNameBytes {
			result = result[:dbfFieldNameBytes]
		}
	}
	return result
}

func (field *dbfExportField) measure(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	switch field.dbfType {
	case 'C':
		if field.kind == fileExportBinary {
			return "", errors.New("DBF 不支持二进制字段，请选择 Parquet")
		}
		text := fileExportTextValue(value)
		if !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
			return "", errors.New("DBF 文本必须是有效的 UTF-8 且不能含 NUL 字符")
		}
		encoded, err := simplifiedchinese.GBK.NewEncoder().String(text)
		if err != nil {
			return "", errors.New("文字无法使用 DBF 的 GBK 编码表示，请选择 XLSX 或 Parquet")
		}
		if len(encoded) > maxDBFTextBytes {
			return "", fmt.Errorf("文本超过 DBF 字段 %d 字节限制", maxDBFTextBytes)
		}
		if len(encoded) > field.width {
			field.width = len(encoded)
		}
		return encoded, nil
	case 'D':
		date, err := fileExportDateValue(value)
		if err != nil {
			return "", err
		}
		return date.Format("20060102"), nil
	case 'L':
		flag, err := fileExportBoolean(value)
		if err != nil {
			return "", err
		}
		if flag {
			return "T", nil
		}
		return "F", nil
	default:
		text, err := dbfExportNumber(value, field.kind)
		if err != nil {
			return "", err
		}
		whole, fractional, _ := strings.Cut(text, ".")
		if len(whole) > field.whole {
			field.whole = len(whole)
		}
		if len(fractional) > field.decimals {
			field.decimals = len(fractional)
		}
		field.width = field.whole
		if field.decimals > 0 {
			field.width += 1 + field.decimals
		}
		if field.width > maxDBFNumberBytes {
			return "", fmt.Errorf("数字超过 DBF 数值字段 %d 字节限制", maxDBFNumberBytes)
		}
		return text, nil
	}
}

func dbfExportNumber(value any, kind fileExportKind) (string, error) {
	text := strings.TrimSpace(fileExportTextValue(value))
	switch kind {
	case fileExportInteger:
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return "", errors.New("无效的 64 位整数")
		}
		return strconv.FormatInt(number, 10), nil
	case fileExportUnsigned:
		number, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return "", errors.New("无效的无符号 64 位整数")
		}
		return strconv.FormatUint(number, 10), nil
	case fileExportFloat:
		number, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			return "", errors.New("DBF 数值必须是有限数字")
		}
		return strconv.FormatFloat(number, 'f', -1, 64), nil
	}

	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	} else {
		text = strings.TrimPrefix(text, "+")
	}
	whole, fractional, _ := strings.Cut(text, ".")
	if (whole == "" && fractional == "") ||
		strings.IndexFunc(whole, nonDBFDigit) >= 0 || strings.IndexFunc(fractional, nonDBFDigit) >= 0 {
		return "", errors.New("无效的定点小数")
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	if fractional != "" {
		return sign + whole + "." + fractional, nil
	}
	return sign + whole, nil
}

func nonDBFDigit(ch rune) bool { return ch < '0' || ch > '9' }

func writeDBFHeader(w io.Writer, fields []dbfExportField, recordBytes int, count uint32) error {
	const descriptorBytes = 32
	header := make([]byte, descriptorBytes*(len(fields)+1)+1)
	now := time.Now().UTC()
	header[0] = 0x03 // dBASE III without memo files
	header[1], header[2], header[3] = byte(now.Year()-1900), byte(now.Month()), byte(now.Day())
	binary.LittleEndian.PutUint32(header[4:8], count)
	binary.LittleEndian.PutUint16(header[8:10], uint16(len(header)))
	binary.LittleEndian.PutUint16(header[10:12], uint16(recordBytes))
	header[29] = 0x7a // Windows code page 936 (GBK)
	offset := 1
	for i, field := range fields {
		descriptor := header[descriptorBytes*(i+1):][:descriptorBytes]
		copy(descriptor[:11], field.name)
		descriptor[11] = field.dbfType
		binary.LittleEndian.PutUint32(descriptor[12:16], uint32(offset))
		descriptor[16], descriptor[17] = byte(field.width), byte(field.decimals)
		offset += field.width
	}
	header[len(header)-1] = 0x0d
	return writeDBFBytes(w, header)
}

func writeDBFRecords(w io.Writer, decoder *gob.Decoder, fields []dbfExportField, values []string, recordBytes int, count uint32) error {
	record := make([]byte, recordBytes)
	for i := uint32(0); i < count; i++ {
		if err := decoder.Decode(&values); err != nil {
			return err
		}
		for i := range record {
			record[i] = ' '
		}
		offset := 1
		for i, field := range fields {
			text := values[i]
			cell := record[offset : offset+field.width]
			if field.dbfType == 'N' && text != "" {
				if field.decimals > 0 {
					whole, fractional, _ := strings.Cut(text, ".")
					text = whole + "." + fractional + strings.Repeat("0", field.decimals-len(fractional))
				}
				copy(cell[len(cell)-len(text):], text)
			} else {
				copy(cell, text)
			}
			offset += field.width
		}
		if err := writeDBFBytes(w, record); err != nil {
			return err
		}
	}
	return writeDBFBytes(w, []byte{0x1a})
}

func writeDBFBytes(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}
