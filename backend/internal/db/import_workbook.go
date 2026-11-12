package db

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const maxWorkbookEntryBytes = 128 << 20

type workbookArchive map[string]*zip.File

func openWorkbook(file io.ReaderAt, size int64, opts ImportOptions) (importSource, error) {
	archive, err := zip.NewReader(file, size)
	if err != nil {
		return importSource{}, fmt.Errorf("读取工作簿: %w", err)
	}
	files := make(workbookArchive, len(archive.File))
	for _, entry := range archive.File {
		files[entry.Name] = entry
	}
	if opts.Format == "ods" {
		return files.ods(opts)
	}
	return files.xlsx(opts)
}

func (archive workbookArchive) open(name string) (io.ReadCloser, error) {
	file, ok := archive[name]
	if !ok {
		return nil, fmt.Errorf("工作簿缺少 %s", name)
	}
	if file.UncompressedSize64 > maxWorkbookEntryBytes {
		return nil, fmt.Errorf("工作簿内部文件 %s 超过解压大小限制", name)
	}
	return file.Open()
}

func (archive workbookArchive) decode(name string, value any) error {
	file, err := archive.open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	return xml.NewDecoder(file).Decode(value)
}

type xlsxText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (value xlsxText) String() string {
	var text strings.Builder
	text.WriteString(decodeXLSXText(value.Text))
	for _, run := range value.Runs {
		text.WriteString(decodeXLSXText(run.Text))
	}
	return text.String()
}

// Each Excel inline/shared text element can contain OOXML ST_Xstring escapes.
func decodeXLSXText(value string) string {
	if !strings.Contains(value, "_x") {
		return value
	}
	var text strings.Builder
	for {
		i := strings.Index(value, "_x")
		if i == -1 {
			text.WriteString(value)
			return text.String()
		}
		text.WriteString(value[:i])
		value = value[i:]
		r, escaped := xlsxEscapeAt(value)
		if !escaped {
			text.WriteString(value[:2])
			value = value[2:]
			continue
		}
		value = value[7:]
		if r >= 0xd800 && r <= 0xdbff {
			if low, ok := xlsxEscapeAt(value); ok && low >= 0xdc00 && low <= 0xdfff {
				r = utf16.DecodeRune(r, low)
				value = value[7:]
			}
		}
		text.WriteRune(r)
	}
}

type xlsxCell struct {
	Reference string   `xml:"r,attr"`
	Type      string   `xml:"t,attr"`
	Style     int      `xml:"s,attr"`
	Value     *string  `xml:"v"`
	Inline    xlsxText `xml:"is"`
}

func (archive workbookArchive) xlsx(opts ImportOptions) (importSource, error) {
	var workbook struct {
		Properties struct {
			Date1904 bool `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := archive.decode("xl/workbook.xml", &workbook); err != nil {
		return importSource{}, err
	}
	var names []string
	for _, sheet := range workbook.Sheets {
		names = append(names, sheet.Name)
	}
	selected, err := selectSheet(names, opts.Sheet)
	if err != nil {
		return importSource{}, err
	}
	var relationships struct {
		Links []struct {
			ID         string `xml:"Id,attr"`
			Target     string `xml:"Target,attr"`
			TargetMode string `xml:"TargetMode,attr"`
			Type       string `xml:"Type,attr"`
		} `xml:"Relationship"`
	}
	if err := archive.decode("xl/_rels/workbook.xml.rels", &relationships); err != nil {
		return importSource{}, err
	}
	var location string
	for _, relationship := range relationships.Links {
		if relationship.ID == workbook.Sheets[selected].ID &&
			relationship.TargetMode != "External" && strings.HasSuffix(relationship.Type, "/worksheet") {
			location = path.Join("xl", relationship.Target)
			if strings.HasPrefix(relationship.Target, "/") {
				location = path.Clean(strings.TrimPrefix(relationship.Target, "/"))
			}
			break
		}
	}
	if !strings.HasPrefix(location, "xl/") {
		return importSource{}, errors.New("找不到所选 Excel 工作表")
	}
	var shared struct {
		Items []xlsxText `xml:"si"`
	}
	if archive["xl/sharedStrings.xml"] != nil {
		if err := archive.decode("xl/sharedStrings.xml", &shared); err != nil {
			return importSource{}, err
		}
	}
	styles, err := archive.xlsxDateStyles()
	if err != nil {
		return importSource{}, err
	}
	data, err := archive.open(location)
	if err != nil {
		return importSource{}, err
	}
	decoder := xml.NewDecoder(data)
	src, err := tabularImport(func() ([]any, error) {
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			element, ok := token.(xml.StartElement)
			if !ok || element.Name.Local != "row" {
				continue
			}
			var row struct {
				Cells []xlsxCell `xml:"c"`
			}
			if err := decoder.DecodeElement(&row, &element); err != nil {
				return nil, err
			}
			cells := make([]any, 0, len(row.Cells))
			for _, cell := range row.Cells {
				column := len(cells)
				if cell.Reference != "" {
					column = excelColumnIndex(cell.Reference)
				}
				if column < len(cells) || column >= maxImportColumns {
					return nil, fmt.Errorf("无效或过宽的 Excel 单元格引用 %q", cell.Reference)
				}
				value, err := cellValue(cell, shared.Items, styles, workbook.Properties.Date1904)
				if err != nil {
					return nil, err
				}
				for len(cells) <= column {
					cells = append(cells, missingImportValue{})
				}
				cells[column] = value
			}
			for len(cells) > 0 {
				value := cells[len(cells)-1]
				if _, missing := value.(missingImportValue); !missing && value != nil {
					break
				}
				cells = cells[:len(cells)-1]
			}
			return cells, nil
		}
	}, opts)
	if err != nil {
		data.Close()
		return importSource{}, err
	}
	src.sheets, src.sheet, src.close = names, names[selected], data.Close
	return src, nil
}

func selectSheet(names []string, name string) (int, error) {
	if len(names) == 0 {
		return 0, errors.New("工作簿没有工作表")
	}
	if name == "" {
		return 0, nil
	}
	for index, candidate := range names {
		if name == candidate {
			return index, nil
		}
	}
	return 0, fmt.Errorf("找不到工作表 %q", name)
}

func excelColumnIndex(reference string) int {
	index := 0
	for _, letter := range reference {
		if letter < 'A' || letter > 'Z' {
			break
		}
		index = index*26 + int(letter-'A'+1)
		if index > maxImportColumns {
			return maxImportColumns
		}
	}
	return index - 1
}

func cellValue(cell xlsxCell, shared []xlsxText, styles []string, date1904 bool) (any, error) {
	if cell.Type == "inlineStr" {
		return cell.Inline.String(), nil
	}
	if cell.Value == nil {
		return nil, nil
	}
	value := *cell.Value
	switch cell.Type {
	case "s":
		index, err := strconv.Atoi(value)
		if err != nil || index < 0 || index >= len(shared) {
			return nil, fmt.Errorf("无效的 Excel 共享字符串索引 %q", value)
		}
		return shared[index].String(), nil
	case "b":
		return value == "1", nil
	case "", "n":
		if cell.Style >= 0 && cell.Style < len(styles) && styles[cell.Style] != "" {
			if serial, err := strconv.ParseFloat(value, 64); err == nil && serial >= 0 && serial <= 3000000 {
				return excelDate(serial, date1904, styles[cell.Style]), nil
			}
		}
	}
	return value, nil
}

func (archive workbookArchive) xlsxDateStyles() ([]string, error) {
	if archive["xl/styles.xml"] == nil {
		return nil, nil
	}
	var styles struct {
		Formats []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		Cells []struct {
			ID int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if err := archive.decode("xl/styles.xml", &styles); err != nil {
		return nil, err
	}
	custom := make(map[int]string)
	for _, f := range styles.Formats {
		code := strings.ToLower(f.Code)
		// Ignore literal text, escaped characters, and bracketed locale or color codes.
		var pattern strings.Builder
		var quoted, bracketed, escape bool
		for _, r := range code {
			switch {
			case escape:
				escape = false
			case r == '\\':
				escape = true
			case r == '"' && !bracketed:
				quoted = !quoted
			case r == '[' && !quoted:
				bracketed = true
			case r == ']' && bracketed:
				bracketed = false
			case !quoted && !bracketed:
				pattern.WriteRune(r)
			}
		}
		date := strings.ContainsAny(pattern.String(), "yd")
		clock := strings.ContainsAny(pattern.String(), "hs")
		switch {
		case date && clock:
			custom[f.ID] = "2006-01-02 15:04:05"
		case date:
			custom[f.ID] = "2006-01-02"
		case clock && !strings.Contains(code, "[h]") && !strings.Contains(code, "[m]") && !strings.Contains(code, "[s]"):
			custom[f.ID] = "15:04:05"
		}
	}
	formats := make([]string, len(styles.Cells))
	for i, cell := range styles.Cells {
		switch id := cell.ID; {
		case id >= 14 && id <= 17, id >= 27 && id <= 36, id >= 50 && id <= 58:
			formats[i] = "2006-01-02"
		case id >= 18 && id <= 21, id == 45, id == 47:
			formats[i] = "15:04:05"
		case id == 22:
			formats[i] = "2006-01-02 15:04:05"
		default:
			formats[i] = custom[id]
		}
	}
	return formats, nil
}

func excelDate(serial float64, date1904 bool, format string) string {
	days, fractional := math.Modf(serial)
	epoch := time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
	if date1904 {
		epoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	} else if days >= 60 {
		days-- // Excel's 1900 calendar includes a fictitious leap day.
	}
	value := epoch.AddDate(0, 0, int(days)).Add(time.Duration(math.Round(fractional*86400)) * time.Second)
	return value.Format(format)
}

const odsTableNS = "urn:oasis:names:tc:opendocument:xmlns:table:1.0"

func (archive workbookArchive) ods(opts ImportOptions) (importSource, error) {
	names, err := archive.odsSheets()
	if err != nil {
		return importSource{}, err
	}
	index, err := selectSheet(names, opts.Sheet)
	if err != nil {
		return importSource{}, err
	}
	data, err := archive.open("content.xml")
	if err != nil {
		return importSource{}, err
	}
	decoder := xml.NewDecoder(data)
	for {
		token, err := decoder.Token()
		if err != nil {
			data.Close()
			return importSource{}, err
		}
		element, ok := token.(xml.StartElement)
		if ok && element.Name == (xml.Name{Space: odsTableNS, Local: "table"}) && xmlAttribute(element, "name") == names[index] {
			break
		}
	}
	var savedRow []any
	remaining := 0
	src, err := tabularImport(func() ([]any, error) {
		if remaining > 0 {
			remaining--
			return savedRow, nil
		}
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			if end, ok := token.(xml.EndElement); ok && end.Name == (xml.Name{Space: odsTableNS, Local: "table"}) {
				return nil, io.EOF
			}
			element, ok := token.(xml.StartElement)
			if !ok || element.Name != (xml.Name{Space: odsTableNS, Local: "table-row"}) {
				continue
			}
			row, err := readODSRow(decoder, element)
			if err != nil {
				return nil, err
			}
			if len(row) == 0 {
				continue
			}
			repeat, err := parseODSRepeat(xmlAttribute(element, "number-rows-repeated"))
			if err != nil || repeat > 100000 {
				return nil, errors.New("无效或过大的 ODS 重复行计数")
			}
			savedRow, remaining = row, repeat-1
			return row, nil
		}
	}, opts)
	if err != nil {
		data.Close()
		return importSource{}, err
	}
	src.sheets, src.sheet, src.close = names, names[index], data.Close
	return src, nil
}

func (archive workbookArchive) odsSheets() ([]string, error) {
	data, err := archive.open("content.xml")
	if err != nil {
		return nil, err
	}
	defer data.Close()
	decoder := xml.NewDecoder(data)
	var names []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
		if element, ok := token.(xml.StartElement); ok && element.Name == (xml.Name{Space: odsTableNS, Local: "table"}) {
			names = append(names, xmlAttribute(element, "name"))
			if err := decoder.Skip(); err != nil {
				return nil, err
			}
		}
	}
}

func readODSRow(decoder *xml.Decoder, rowStart xml.StartElement) ([]any, error) {
	var row []any
	column := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if end, ok := token.(xml.EndElement); ok && end.Name == rowStart.Name {
			return row, nil
		}
		cell, ok := token.(xml.StartElement)
		if !ok || cell.Name.Space != odsTableNS || (cell.Name.Local != "table-cell" && cell.Name.Local != "covered-table-cell") {
			continue
		}
		var text struct {
			Paragraphs []odsParagraph `xml:"p"`
		}
		if err := decoder.DecodeElement(&text, &cell); err != nil {
			return nil, err
		}
		repeat, err := parseODSRepeat(xmlAttribute(cell, "number-columns-repeated"))
		if err != nil || repeat > 1000000-column {
			return nil, errors.New("无效或过大的 ODS 重复列计数")
		}
		var value any
		switch xmlAttribute(cell, "value-type") {
		case "float", "currency", "percentage":
			value = xmlAttribute(cell, "value")
		case "date":
			value = xmlAttribute(cell, "date-value")
		case "time":
			value = xmlAttribute(cell, "time-value")
		case "boolean":
			value = xmlAttribute(cell, "boolean-value") == "true"
		default:
			if len(text.Paragraphs) > 0 {
				lines := make([]string, len(text.Paragraphs))
				for i, paragraph := range text.Paragraphs {
					lines[i] = string(paragraph)
				}
				value = strings.Join(lines, "\n")
			} else if stringValue := xmlAttribute(cell, "string-value"); stringValue != "" {
				value = stringValue
			}
		}
		if value != nil {
			if column+repeat > maxImportColumns {
				return nil, errors.New("ODS 数据列数过多")
			}
			for len(row) < column {
				row = append(row, missingImportValue{})
			}
			for i := 0; i < repeat; i++ {
				row = append(row, value)
			}
		}
		column += repeat
	}
}

type odsParagraph string

func (p *odsParagraph) UnmarshalXML(decoder *xml.Decoder, _ xml.StartElement) error {
	var content strings.Builder
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.CharData:
			content.Write(value)
		case xml.StartElement:
			depth++
			switch value.Name.Local {
			case "s":
				count, err := parseODSRepeat(xmlAttribute(value, "c"))
				if err != nil || count > 1000000 {
					return errors.New("无效或过大的 ODS 连续空格计数")
				}
				content.WriteString(strings.Repeat(" ", count))
			case "tab":
				content.WriteByte('\t')
			case "line-break":
				content.WriteByte('\n')
			}
		case xml.EndElement:
			depth--
		}
		if content.Len() > maxWorkbookEntryBytes {
			return errors.New("ODS 单元格文本过长")
		}
	}
	*p = odsParagraph(content.String())
	return nil
}

func xmlAttribute(element xml.StartElement, name string) string {
	for _, attr := range element.Attr {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func parseODSRepeat(value string) (int, error) {
	if value == "" {
		return 1, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 1 {
		return 0, fmt.Errorf("invalid repetition count %q", value)
	}
	return count, nil
}
