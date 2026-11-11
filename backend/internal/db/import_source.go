package db

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

const maxImportColumns = 1024

type ImportOptions struct {
	Format, Encoding, Delimiter, Widths                string
	Sheet, XMLRow, Mode                                string
	SkipRows                                           int
	SourceDSN, SourceUser, SourcePassword, SourceTable string
	SourceMaxBytes                                     int64
	NoHeader, EmptyAsNull                              bool
	ContinueOnError                                    bool
	Mappings                                           []ImportMapping
}

type ImportMapping struct {
	Source int    `json:"source"`
	Target string `json:"target"`
}

type missingImportValue struct{}

type importSource struct {
	columns []string
	sheets  []string
	sheet   string
	next    func() ([]any, error)
	close   func() error
}

func (s importSource) read() ([]any, error) {
	if s.next == nil {
		return nil, io.EOF
	}
	return s.next()
}

func (s importSource) finish() {
	if s.close != nil {
		_ = s.close()
	}
}

// DecodeImportText detects common byte order marks before applying the selected
// character encoding. auto assumes UTF-8 for files without a BOM.
func DecodeImportText(reader io.Reader, name string) (io.Reader, error) {
	input := bufio.NewReader(reader)
	bom, _ := input.Peek(3)
	if bytes.HasPrefix(bom, []byte{0xef, 0xbb, 0xbf}) {
		_, _ = input.Discard(3)
		return input, nil
	}
	if bytes.HasPrefix(bom, []byte{0xff, 0xfe}) {
		name = "utf-16le"
	}
	if bytes.HasPrefix(bom, []byte{0xfe, 0xff}) {
		name = "utf-16be"
	}
	codec, err := importEncoding(name)
	if err != nil {
		return nil, err
	}
	if codec == nil {
		return input, nil
	}
	return codec.NewDecoder().Reader(input), nil
}

func importEncoding(name string) (encoding.Encoding, error) {
	var codec encoding.Encoding
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto", "utf-8", "utf8":
		return nil, nil
	case "utf-16", "utf-16le":
		codec = unicode.UTF16(unicode.LittleEndian, unicode.UseBOM)
	case "utf-16be":
		codec = unicode.UTF16(unicode.BigEndian, unicode.UseBOM)
	case "gb18030", "gbk", "gb2312":
		codec = simplifiedchinese.GB18030
	case "big5":
		codec = traditionalchinese.Big5
	case "windows-1252":
		codec = charmap.Windows1252
	case "iso-8859-1":
		codec = charmap.ISO8859_1
	case "cp437":
		codec = charmap.CodePage437
	case "cp850":
		codec = charmap.CodePage850
	default:
		return nil, fmt.Errorf("不支持的文件编码 %q", name)
	}
	return codec, nil
}

func openImportSource(ctx context.Context, file io.ReaderAt, size int64, opts ImportOptions) (importSource, error) {
	if opts.SkipRows < 0 || opts.SkipRows > 100000 {
		return importSource{}, errors.New("跳过行数必须介于 0 和 100000 之间")
	}
	raw := io.NewSectionReader(file, 0, size)
	switch opts.Format {
	case "xlsx", "ods":
		return openWorkbook(file, size, opts)
	case "xls":
		return openXLSImport(ctx, file, size, opts)
	case "parquet":
		return openParquetImport(ctx, file, size)
	case "odbc", "mdb", "accdb":
		return openODBCImport(ctx, file, size, opts)
	case "sqlite", "db", "sqlite3":
		return openSQLiteImport(ctx, file, size, opts)
	case "dbf":
		return openDBF(raw, opts)
	}
	reader, err := DecodeImportText(raw, opts.Encoding)
	if err != nil {
		return importSource{}, err
	}
	switch opts.Format {
	case "csv", "txt", "tsv":
		if opts.Widths != "" {
			next, err := fixedWidthReader(reader, opts.Widths)
			if err != nil {
				return importSource{}, err
			}
			return tabularImport(next, opts)
		}
		return delimitedImport(reader, opts)
	case "json", "jsonl":
		return jsonImport(ctx, reader, opts.Format == "jsonl")
	case "xml":
		return xmlImport(ctx, reader, opts)
	case "html", "htm":
		return htmlImport(ctx, reader, opts)
	default:
		return importSource{}, fmt.Errorf("不支持的导入格式 %q", opts.Format)
	}
}

func delimitedImport(reader io.Reader, opts ImportOptions) (importSource, error) {
	cr := csv.NewReader(reader)
	cr.FieldsPerRecord = -1
	switch {
	case opts.Delimiter != "":
		r, n := utf8.DecodeRuneInString(opts.Delimiter)
		if n != len(opts.Delimiter) || r == utf8.RuneError || r == '\r' || r == '\n' || r == '"' || r == 0 {
			return importSource{}, errors.New("字段分隔符必须是有效的单个字符")
		}
		cr.Comma = r
	case opts.Format == "tsv" || opts.Format == "txt":
		cr.Comma = '\t'
	}
	next := func() ([]any, error) {
		row, err := cr.Read()
		if err != nil {
			return nil, err
		}
		values := make([]any, len(row))
		for i, value := range row {
			values[i] = value
		}
		return values, nil
	}
	return tabularImport(next, opts)
}

func fixedWidthReader(reader io.Reader, spec string) (func() ([]any, error), error) {
	fields := strings.Split(spec, ",")
	if len(fields) > maxImportColumns {
		return nil, errors.New("固定宽度列过多")
	}
	widths := make([]int, len(fields))
	total := 0
	for i, field := range fields {
		width, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || width <= 0 || width > 100000-total {
			return nil, errors.New("固定宽度必须是逗号分隔的正整数，总宽度不能超过 100000")
		}
		widths[i] = width
		total += width
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4*100000+2)
	return func() ([]any, error) {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, err
			}
			return nil, io.EOF
		}
		line := []rune(scanner.Text())
		if len(line) > total {
			return nil, errors.New("源行长于所设定的固定宽度")
		}
		row := make([]any, len(widths))
		for i, width := range widths {
			if width > len(line) {
				width = len(line)
			}
			row[i] = strings.TrimSpace(string(line[:width]))
			line = line[width:]
		}
		return row, nil
	}, nil
}

func tabularImport(next func() ([]any, error), opts ImportOptions) (importSource, error) {
	for i := 0; i < opts.SkipRows; i++ {
		if _, err := next(); err != nil {
			if err == io.EOF {
				return importSource{}, errors.New("跳过指定行后没有数据")
			}
			return importSource{}, err
		}
	}
	var first []any
	for len(first) == 0 {
		var err error
		first, err = next()
		if err != nil {
			if err == io.EOF {
				return importSource{}, errors.New("源文件没有数据")
			}
			return importSource{}, err
		}
	}
	if len(first) > maxImportColumns {
		return importSource{}, errors.New("源文件列数过多")
	}
	columns := make([]string, len(first))
	for i, value := range first {
		if opts.NoHeader {
			columns[i] = fmt.Sprintf("列 %d", i+1)
		} else if _, missing := value.(missingImportValue); !missing && value != nil {
			columns[i] = strings.TrimSpace(fmt.Sprint(value))
			if len(columns[i]) > 4096 {
				return importSource{}, errors.New("源字段名过长，请检查字段行")
			}
		}
	}
	if !opts.NoHeader {
		first = nil
	}
	return importSource{
		columns: columns,
		next: func() ([]any, error) {
			if first != nil {
				row := first
				first = nil
				return row, nil
			}
			for {
				row, err := next()
				if err != nil {
					return nil, err
				}
				if len(row) == 0 {
					continue
				}
				if len(row) > len(columns) {
					return nil, errors.New("源数据行的列数超过字段行")
				}
				return row, nil
			}
		},
	}, nil
}

// Keyed formats can contain records with different properties. Collect the
// union of their fields with a memory bound before presenting mappings.
func keyedImport(ctx context.Context, next func() (map[string]any, error)) (importSource, error) {
	const maxKeyedRecords = 200000
	const maxKeyedCells = 2000000
	var records []map[string]any
	fields := make(map[string]bool)
	cells := 0
	for {
		if err := ctx.Err(); err != nil {
			return importSource{}, err
		}
		record, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return importSource{}, err
		}
		if len(records) == maxKeyedRecords || cells+len(record) > maxKeyedCells {
			return importSource{}, errors.New("源文件包含过多对象或字段，请拆分文件后导入")
		}
		records = append(records, record)
		cells += len(record)
		for field := range record {
			if len(field) > 4096 {
				return importSource{}, errors.New("源字段名过长")
			}
			fields[field] = true
		}
		if len(fields) > maxImportColumns {
			return importSource{}, errors.New("源文件列数过多")
		}
	}
	columns := make([]string, 0, len(fields))
	for field := range fields {
		columns = append(columns, field)
	}
	sort.Strings(columns)
	return importSource{
		columns: columns,
		next: func() ([]any, error) {
			if len(records) == 0 {
				return nil, io.EOF
			}
			record := records[0]
			records[0] = nil
			records = records[1:]
			row := make([]any, len(columns))
			for i, field := range columns {
				if value, exists := record[field]; exists {
					row[i] = value
				} else {
					row[i] = missingImportValue{}
				}
			}
			return row, nil
		},
	}, nil
}

func jsonImport(ctx context.Context, reader io.Reader, lines bool) (importSource, error) {
	input := bufio.NewReader(reader)
	array := false
	for {
		head, err := input.Peek(1)
		if err != nil {
			if err == io.EOF {
				return importSource{}, errors.New("JSON 文件为空")
			}
			return importSource{}, err
		}
		if bytes.IndexByte([]byte(" \r\n\t"), head[0]) >= 0 {
			_, _ = input.Discard(1)
			continue
		}
		array = head[0] == '[' && !lines
		break
	}
	dec := json.NewDecoder(input)
	dec.UseNumber()
	if array {
		if _, err := dec.Token(); err != nil {
			return importSource{}, err
		}
	}
	finished := false
	return keyedImport(ctx, func() (map[string]any, error) {
		if finished {
			return nil, io.EOF
		}
		if array && !dec.More() {
			end, err := dec.Token()
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			if err != nil {
				return nil, err
			}
			if end != json.Delim(']') {
				return nil, errors.New("JSON 数组未正确结束")
			}
			finished = true
			return nil, checkJSONEnd(dec)
		}
		var record map[string]any
		if err := dec.Decode(&record); err != nil {
			if array && err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if record == nil {
			return nil, errors.New("JSON 数据行必须是对象")
		}
		for key, value := range record {
			switch v := value.(type) {
			case json.Number:
				if number, err := v.Int64(); err == nil {
					record[key] = number
				} else {
					record[key] = v.String()
				}
			case []any, map[string]any:
				encoded, err := json.Marshal(value)
				if err != nil {
					return nil, err
				}
				record[key] = string(encoded)
			}
		}
		if !array && !lines {
			if err := checkJSONEnd(dec); err != io.EOF {
				return nil, err
			}
			finished = true
		}
		return record, nil
	})
}

func checkJSONEnd(dec *json.Decoder) error {
	if _, err := dec.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("JSON 数据后存在多余内容")
	}
	return io.EOF
}

func xmlImport(ctx context.Context, reader io.Reader, opts ImportOptions) (importSource, error) {
	tag := strings.TrimSpace(opts.XMLRow)
	if tag == "" {
		tag = "row"
	}
	dec := xml.NewDecoder(reader)
	dec.CharsetReader = func(name string, input io.Reader) (io.Reader, error) {
		if opts.Encoding != "" && opts.Encoding != "auto" || strings.HasPrefix(strings.ToLower(name), "utf-16") {
			return input, nil // the source reader has already decoded these bytes
		}
		return DecodeImportText(input, name)
	}
	found := false
	src, err := keyedImport(ctx, func() (map[string]any, error) {
		for {
			token, err := dec.Token()
			if err != nil {
				return nil, err
			}
			element, ok := token.(xml.StartElement)
			if !ok || element.Name.Local != tag {
				continue
			}
			found = true
			var row struct {
				Fields []struct {
					XMLName xml.Name
					Value   string `xml:",chardata"`
				} `xml:",any"`
			}
			if err := dec.DecodeElement(&row, &element); err != nil {
				return nil, err
			}
			record := make(map[string]any, len(row.Fields)+len(element.Attr))
			for _, attr := range element.Attr {
				if attr.Name.Space != "xmlns" && attr.Name.Local != "xmlns" {
					record[attr.Name.Local] = attr.Value
				}
			}
			for _, field := range row.Fields {
				if _, exists := record[field.XMLName.Local]; exists {
					return nil, fmt.Errorf("XML 行中的字段 %q 重复", field.XMLName.Local)
				}
				record[field.XMLName.Local] = field.Value
			}
			return record, nil
		}
	})
	if err == nil && !found {
		return importSource{}, fmt.Errorf("XML 中没有 <%s> 行", tag)
	}
	return src, err
}
