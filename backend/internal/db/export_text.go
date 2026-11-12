package db

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const exportXMLNamespace = "urn:zidong-direct:export:1"

func exportText(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case time.Time:
		return NormalizeCellValue(value, "").(string)
	case []byte:
		return "[HEX] " + hex.EncodeToString(value)
	default:
		return fmt.Sprint(value)
	}
}

func writeTextExport(w io.Writer, format string, columns []string, opts ExportOptions, next exportNextRow) error {
	var widths []int
	if format == "txt" && strings.TrimSpace(opts.Widths) != "" {
		var err error
		widths, _, err = parseFixedWidths(opts.Widths)
		if err != nil {
			return err
		}
		if len(widths) != len(columns) {
			return fmt.Errorf("定宽导出需要为 %d 列分别指定宽度", len(columns))
		}
	}
	delimiter := ','
	if format == "tsv" || format == "txt" {
		delimiter = '\t'
	}
	if format == "txt" && len(widths) == 0 && opts.Delimiter != "" {
		r, n := utf8.DecodeRuneInString(opts.Delimiter)
		if n != len(opts.Delimiter) || r == utf8.RuneError || strings.ContainsRune("\x00\r\n\"", r) {
			return fmt.Errorf("字段分隔符必须是有效的单个字符")
		}
		delimiter = r
	}
	if opts.WithBOM {
		if _, err := w.Write([]byte{0xef, 0xbb, 0xbf}); err != nil {
			return err
		}
	}
	if len(widths) > 0 {
		return writeFixedWidthExport(w, columns, widths, next)
	}

	writer := csv.NewWriter(w)
	writer.Comma = delimiter
	if err := writer.Write(columns); err != nil {
		return err
	}
	record := make([]string, len(columns))
	for {
		values, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		for i, value := range values {
			record[i] = exportText(value)
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func writeFixedWidthExport(w io.Writer, columns []string, widths []int, next exportNextRow) error {
	write := func(record []string, rowNumber int) error {
		var line strings.Builder
		for i, text := range record {
			size := utf8.RuneCountInString(text)
			if size > widths[i] || strings.ContainsAny(text, "\r\n") {
				return fmt.Errorf("第 %d 行、列 %q 超出定宽限制或包含换行", rowNumber, columns[i])
			}
			line.WriteString(text)
			line.WriteString(strings.Repeat(" ", widths[i]-size))
		}
		line.WriteByte('\n')
		_, err := io.WriteString(w, line.String())
		return err
	}
	if err := write(columns, 1); err != nil {
		return err
	}
	record := make([]string, len(columns))
	for rowNumber := 2; ; rowNumber++ {
		values, err := next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		for i, value := range values {
			record[i] = exportText(value)
		}
		if err := write(record, rowNumber); err != nil {
			return err
		}
	}
}

func uniqueExportNames(columns []string) []string {
	reserved := make(map[string]bool, len(columns))
	for _, name := range columns {
		reserved[name] = true
	}
	used := make(map[string]bool, len(columns))
	names := make([]string, len(columns))
	for i, name := range columns {
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
			for reserved[name] || used[name] {
				name += "_"
			}
		}
		if used[name] {
			base := name
			for suffix := 2; ; suffix++ {
				name = base + "_" + strconv.Itoa(suffix)
				if !reserved[name] && !used[name] {
					break
				}
			}
		}
		names[i], used[name] = name, true
	}
	return names
}

func writeJSONExport(w io.Writer, format string, columns []string, next exportNextRow) error {
	array := format == "json"
	if array {
		if _, err := io.WriteString(w, "[\n"); err != nil {
			return err
		}
	}
	names := uniqueExportNames(columns)
	first := true
	encoder := json.NewEncoder(w)
	for {
		values, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		object := make(map[string]any, len(names))
		for i, name := range names {
			object[name] = values[i]
		}
		if array {
			data, err := json.Marshal(object)
			if err != nil {
				return err
			}
			if !first {
				if _, err := io.WriteString(w, ",\n"); err != nil {
					return err
				}
			}
			if _, err := w.Write(data); err != nil {
				return err
			}
			first = false
		} else if err := encoder.Encode(object); err != nil {
			return err
		}
	}
	if array {
		_, err := io.WriteString(w, "\n]\n")
		return err
	}
	return nil
}

func writeXMLExport(w io.Writer, columns []string, next exportNextRow) error {
	names := uniqueExportNames(columns)
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	root := xml.StartElement{Name: xml.Name{Local: "rows"}}
	row := xml.StartElement{Name: xml.Name{Local: "row"}}
	if err := encoder.EncodeToken(root); err != nil {
		return err
	}
	for {
		values, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := encoder.EncodeToken(row); err != nil {
			return err
		}
		for i, value := range values {
			if err := writeXMLExportField(encoder, names[i], value); err != nil {
				return err
			}
		}
		if err := encoder.EncodeToken(row.End()); err != nil {
			return err
		}
	}
	if err := encoder.EncodeToken(root.End()); err != nil {
		return err
	}
	return encoder.Flush()
}

func writeXMLExportField(encoder *xml.Encoder, name string, value any) error {
	data, binary := value.([]byte)
	var text string
	if !binary {
		text = exportText(value)
	}
	if value != nil && !binary && xmlColumnName(name) && validXMLText(text) {
		return encoder.EncodeElement(text, xml.StartElement{Name: xml.Name{Local: name}})
	}
	if !validXMLText(name) {
		return fmt.Errorf("列名包含 XML 不支持的字符")
	}
	field := xml.StartElement{Name: xml.Name{Space: exportXMLNamespace, Local: "field"}, Attr: []xml.Attr{
		{Name: xml.Name{Local: "name"}, Value: name},
	}}
	switch {
	case value == nil:
		field.Attr = append(field.Attr, xml.Attr{Name: xml.Name{Local: "null"}, Value: "true"})
	case binary:
		field.Attr = append(field.Attr, xml.Attr{Name: xml.Name{Local: "encoding"}, Value: "base64"})
		text = base64.StdEncoding.EncodeToString(data)
	case !validXMLText(text):
		field.Attr = append(field.Attr, xml.Attr{Name: xml.Name{Local: "encoding"}, Value: "base64-text"})
		text = base64.StdEncoding.EncodeToString([]byte(text))
	}
	return encoder.EncodeElement(text, field)
}

func xmlColumnName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || unicode.IsLetter(r) || i > 0 && (unicode.IsDigit(r) || r == '-' || r == '.') {
			continue
		}
		return false
	}
	return true
}

func validXMLText(text string) bool {
	return utf8.ValidString(text) && strings.IndexFunc(text, func(r rune) bool {
		return r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0xfffe || r == 0xffff
	}) < 0
}

func writeHTMLExport(w io.Writer, columns []string, next exportNextRow) error {
	if _, err := io.WriteString(w, "<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>Export</title></head><body>\n<table><thead><tr>"); err != nil {
		return err
	}
	for _, name := range columns {
		if _, err := fmt.Fprintf(w, "<th>%s</th>", html.EscapeString(name)); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "</tr></thead><tbody>\n"); err != nil {
		return err
	}
	for {
		values, err := next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, "<tr>"); err != nil {
			return err
		}
		for _, value := range values {
			if _, err := fmt.Fprintf(w, "<td>%s</td>", html.EscapeString(exportText(value))); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, "</tr>\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "</tbody></table>\n</body></html>\n")
	return err
}

func writeSQLExport(w io.Writer, d Dialect, columns []string, opts ExportOptions, next exportNextRow) error {
	tableName := opts.Table
	if tableName == "" {
		tableName = "exported_table"
	}
	tableRef := QualifiedTable(d, opts.Database, opts.Schema, tableName)
	quoted := strings.Join(quoteList(d, columns), ", ")
	record := make([]string, len(columns))
	for {
		values, err := next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		for i, value := range values {
			record[i] = formatSQLValue(value)
		}
		if _, err := fmt.Fprintf(w, "INSERT INTO %s (%s) VALUES (%s);\n", tableRef, quoted, strings.Join(record, ", ")); err != nil {
			return err
		}
	}
}

func formatSQLValue(v any) string {
	if v == nil {
		return "NULL"
	}
	switch x := v.(type) {
	case bool:
		if x {
			return "TRUE"
		}
		return "FALSE"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", x)
	case float32, float64:
		s := fmt.Sprintf("%v", x)
		if _, err := strconv.ParseFloat(s, 64); err == nil && s != "NaN" && s != "+Inf" && s != "-Inf" {
			return s
		}
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	default:
		s := fmt.Sprintf("%v", x)
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
}
