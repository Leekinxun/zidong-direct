package db

import (
	"archive/zip"
	"bufio"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	workbookMaxRows    = 1048576
	xlsxMaxCellUnits   = 32767
	odsMIMEType        = "application/vnd.oasis.opendocument.spreadsheet"
	spreadsheetMLNS    = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	officeRelationsNS  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	packageRelationsNS = "http://schemas.openxmlformats.org/package/2006/relationships"
)

type workbookPart struct {
	path string
	xml  string
}

var xlsxParts = []workbookPart{
	{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`},
	{"_rels/.rels", `<Relationships xmlns="` + packageRelationsNS + `">
<Relationship Id="rId1" Type="` + officeRelationsNS + `/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`},
	{"xl/workbook.xml", `<workbook xmlns="` + spreadsheetMLNS + `" xmlns:r="` + officeRelationsNS + `">
<sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets>
</workbook>`},
	{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="` + packageRelationsNS + `">
<Relationship Id="rId1" Type="` + officeRelationsNS + `/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`},
}

var odsParts = []workbookPart{
	{"META-INF/manifest.xml", `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.2">
<manifest:file-entry manifest:full-path="/" manifest:version="1.2" manifest:media-type="` + odsMIMEType + `"/>
<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>
</manifest:manifest>`},
}

const odsContentStart = `<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
 xmlns:table="` + odsTableNS + `" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" office:version="1.2">
<office:body><office:spreadsheet><table:table table:name="Data">`

func writeWorkbookExport(w io.Writer, format string, columns []exportColumn, next exportNextRow) error {
	if len(columns) == 0 || len(columns) > maxImportColumns {
		return fmt.Errorf("工作簿导出支持 1 至 %d 列", maxImportColumns)
	}
	archive := zip.NewWriter(w)
	parts := xlsxParts
	entryName := "xl/worksheets/sheet1.xml"
	if format == "ods" {
		parts, entryName = odsParts, "content.xml"
		// ODF readers locate this uncompressed first entry by its byte offset.
		mime := []byte(odsMIMEType)
		entry, err := archive.CreateRaw(&zip.FileHeader{
			Name: "mimetype", Method: zip.Store, CreatorVersion: 20, ReaderVersion: 20,
			CRC32: crc32.ChecksumIEEE(mime), CompressedSize64: uint64(len(mime)), UncompressedSize64: uint64(len(mime)),
		})
		if err != nil {
			return err
		}
		if _, err := entry.Write(mime); err != nil {
			return err
		}
	} else if format != "xlsx" {
		return fmt.Errorf("不支持的工作簿导出格式 %q", format)
	}
	for _, part := range parts {
		entry, err := archive.Create(part.path)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(entry, xml.Header+part.xml); err != nil {
			return err
		}
	}
	entry, err := archive.Create(entryName)
	if err != nil {
		return err
	}
	buffer := bufio.NewWriter(&workbookEntryLimit{writer: entry, remaining: maxWorkbookEntryBytes})
	out := &workbookXMLWriter{writer: buffer}
	out.raw(xml.Header)
	columnRefs := make([]string, len(columns))
	for i := range columnRefs {
		columnRefs[i] = workbookColumnName(i)
	}
	if format == "ods" {
		out.raw(odsContentStart)
		out.raw(`<table:table-column table:number-columns-repeated="` + strconv.Itoa(len(columns)) + `"/>`)
	} else {
		out.raw(`<worksheet xmlns="` + spreadsheetMLNS + `"><sheetData>`)
	}
	header := make([]any, len(columns))
	for i, column := range columns {
		header[i] = column.Name
	}
	if err := out.row(format, columnRefs, 1, header); err != nil {
		return err
	}
	for rowIndex := 2; ; rowIndex++ {
		row, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if rowIndex > workbookMaxRows {
			return fmt.Errorf("工作簿最多容纳 %d 行数据（另有一行表头）", workbookMaxRows-1)
		}
		if len(row) != len(columns) {
			return fmt.Errorf("第 %d 行的数据列数与表头不符", rowIndex)
		}
		if err := out.row(format, columnRefs, rowIndex, row); err != nil {
			return err
		}
	}
	if format == "ods" {
		out.raw("</table:table></office:spreadsheet></office:body></office:document-content>")
	} else {
		out.raw("</sheetData></worksheet>")
	}
	if out.err != nil {
		return out.err
	}
	if err := buffer.Flush(); err != nil {
		return err
	}
	return archive.Close()
}

type workbookEntryLimit struct {
	writer    io.Writer
	remaining int
}

func (w *workbookEntryLimit) Write(data []byte) (int, error) {
	if len(data) > w.remaining {
		return 0, errors.New("工作簿数据过大，请减少导出行数")
	}
	n, err := w.writer.Write(data)
	w.remaining -= n
	return n, err
}

type workbookXMLWriter struct {
	writer io.Writer
	err    error
}

func (w *workbookXMLWriter) raw(value string) {
	if w.err == nil {
		_, w.err = io.WriteString(w.writer, value)
	}
}

func (w *workbookXMLWriter) text(value string) {
	if w.err == nil {
		w.err = xml.EscapeText(w.writer, []byte(value))
	}
}

func (w *workbookXMLWriter) row(format string, refs []string, index int, values []any) error {
	row := strconv.Itoa(index)
	if format == "ods" {
		w.raw("<table:table-row>")
	} else {
		w.raw(`<row r="` + row + `">`)
	}
	for i, value := range values {
		if w.err != nil {
			return w.err
		}
		if value == nil {
			if format == "ods" {
				w.raw("<table:table-cell/>")
			} else {
				w.raw(`<c r="` + refs[i] + row + `"/>`)
			}
			continue
		}
		kind, text, err := workbookValue(format, value)
		if err != nil {
			return fmt.Errorf("单元格 %s%s: %w", refs[i], row, err)
		}
		if format == "ods" {
			w.odsCell(kind, text)
		} else {
			w.xlsxCell(refs[i]+row, kind, text)
		}
	}
	if format == "ods" {
		w.raw("</table:table-row>")
	} else {
		w.raw("</row>")
	}
	return w.err
}

func (w *workbookXMLWriter) xlsxCell(reference string, kind byte, text string) {
	start := `<c r="` + reference + `"`
	switch kind {
	case 'n':
		w.raw(start + ` t="n"><v>` + text + "</v></c>")
	case 'b':
		value := "0"
		if text == "true" {
			value = "1"
		}
		w.raw(start + ` t="b"><v>` + value + "</v></c>")
	default:
		w.raw(start + ` t="inlineStr"><is><t xml:space="preserve">`)
		w.text(text)
		w.raw("</t></is></c>")
	}
}

func (w *workbookXMLWriter) odsCell(kind byte, text string) {
	switch kind {
	case 'n':
		w.raw(`<table:table-cell office:value-type="float" office:value="` + text + `"><text:p>` + text + "</text:p></table:table-cell>")
	case 'b':
		w.raw(`<table:table-cell office:value-type="boolean" office:boolean-value="` + text + `"><text:p>` + strings.ToUpper(text) + "</text:p></table:table-cell>")
	default:
		w.raw(`<table:table-cell office:value-type="string"><text:p>`)
		w.odsText(text)
		w.raw("</text:p></table:table-cell>")
	}
}

func (w *workbookXMLWriter) odsText(value string) {
	for len(value) > 0 && w.err == nil {
		i := strings.IndexAny(value, " \t\n")
		if i < 0 {
			w.text(value)
			return
		}
		w.text(value[:i])
		value = value[i:]
		switch value[0] {
		case ' ':
			n := len(value) - len(strings.TrimLeft(value, " "))
			if n == 1 {
				w.raw("<text:s/>")
			} else {
				w.raw(`<text:s text:c="` + strconv.Itoa(n) + `"/>`)
			}
			value = value[n:]
		case '\t':
			w.raw("<text:tab/>")
			value = value[1:]
		case '\n':
			w.raw("<text:line-break/>")
			value = value[1:]
		}
	}
}

func workbookValue(format string, value any) (byte, string, error) {
	var text string
	switch value := value.(type) {
	case bool:
		return 'b', strconv.FormatBool(value), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		text = fmt.Sprint(value)
		if len(strings.TrimPrefix(text, "-")) <= 15 {
			return 'n', text, nil
		}
		// Preserve large identifiers exactly when opened in a spreadsheet.
	case float32:
		return workbookFloat(float64(value), 32)
	case float64:
		return workbookFloat(value, 64)
	case time.Time:
		text = value.Format(time.RFC3339Nano)
	case []byte:
		const prefix = "[HEX] "
		limit := maxWorkbookEntryBytes
		if format == "xlsx" {
			limit = xlsxMaxCellUnits
		}
		if len(value) > (limit-len(prefix))/2 {
			return 0, "", errors.New("二进制值超过工作簿单元格大小限制")
		}
		text = prefix + hex.EncodeToString(value)
	case string:
		text = value
	default:
		text = fmt.Sprint(value)
	}
	if !utf8.ValidString(text) {
		return 0, "", errors.New("工作簿单元格包含无效的 UTF-8")
	}
	if format == "xlsx" {
		encoded, err := encodeXLSXText(text)
		return 's', encoded, err
	}
	for _, r := range text {
		if !isWorkbookXMLRune(r) {
			return 0, "", errors.New("ODS 单元格包含 XML 不支持的字符")
		}
	}
	return 's', text, nil
}

func workbookFloat(value float64, bitSize int) (byte, string, error) {
	text := strconv.FormatFloat(value, 'g', -1, bitSize)
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 9.99999999999999e307 ||
		(value != 0 && math.Abs(value) < 2.2250738585072014e-308) {
		return 's', text, nil
	}
	// Excel's numeric cells preserve at most 15 significant decimal digits.
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(value, 'g', 15, bitSize), bitSize)
	if rounded != value {
		return 's', text, nil
	}
	return 'n', text, nil
}

func workbookColumnName(index int) string {
	var letters [8]byte
	end := len(letters)
	for index++; index > 0; index = (index - 1) / 26 {
		end--
		letters[end] = byte('A' + (index-1)%26)
	}
	return string(letters[end:])
}

func isWorkbookXMLRune(r rune) bool {
	return r == '\t' || r == '\n' || r == '\r' ||
		r >= 0x20 && r <= 0xd7ff || r >= 0xe000 && r <= 0xfffd || r >= 0x10000 && r <= 0x10ffff
}

func encodeXLSXText(text string) (string, error) {
	var escaped strings.Builder
	start, units := 0, 0
	for i, r := range text {
		units++
		if r > 0xffff {
			units++
		}
		if units > xlsxMaxCellUnits {
			return "", fmt.Errorf("XLSX 单元格超过 %d 个 UTF-16 字符单元", xlsxMaxCellUnits)
		}
		_, literalEscape := xlsxEscapeAt(text[i:])
		if r == '\r' || !isWorkbookXMLRune(r) || literalEscape {
			escaped.WriteString(text[start:i])
			fmt.Fprintf(&escaped, "_x%04X_", r)
			start = i + utf8.RuneLen(r)
		}
	}
	if start == 0 {
		return text, nil
	}
	escaped.WriteString(text[start:])
	return escaped.String(), nil
}

func xlsxEscapeAt(text string) (rune, bool) {
	if len(text) < 7 || text[:2] != "_x" || text[6] != '_' {
		return 0, false
	}
	// ParseUint accepts a leading +, which is not part of an OOXML escape.
	for _, digit := range text[2:6] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", digit) {
			return 0, false
		}
	}
	code, _ := strconv.ParseUint(text[2:6], 16, 16)
	return rune(code), true
}
