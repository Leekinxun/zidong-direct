package db

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	xlsMaxColumns = 256
	xlsMaxRows    = 65536
	xlsMaxText    = 32767
	xlsMaxRecord  = 8224
	xlsMaxBIFF    = 31 << 20 // Leave space for the compound-file container.
	xlsCellXF     = 15
)

var xlsOrder = binary.LittleEndian

var errXLSSize = errors.New("XLS 工作簿过大，请改用 XLSX")

// writeXLSExport writes a BIFF8 Workbook stream inside a version 3 compound
// file. Shared strings hold text as literal values, including formula-like text.
func writeXLSExport(w io.Writer, columns []exportColumn, next exportNextRow) error {
	if len(columns) == 0 || len(columns) > xlsMaxColumns {
		return errors.New("XLS 仅支持 1 至 256 列，请改用 XLSX")
	}
	book := &xlsBook{columns: len(columns), strings: make(map[string]uint32)}
	header := make([]any, len(columns))
	for i, column := range columns {
		header[i] = column.Name
	}
	if err := book.addRow(header); err != nil {
		return err
	}
	for {
		row, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if len(row) != len(columns) {
			return errors.New("XLS 导出行的列数与表头不一致")
		}
		if err := book.addRow(row); err != nil {
			return err
		}
	}
	book.finishBlock()
	globals := book.globals()
	sheetHeader := book.sheetHeader(globals.Len())
	var sheetFooter bytes.Buffer
	// Window2 selects the only sheet and places its cursor in A1.
	xlsRecord(&sheetFooter, 0x023e, []byte{0xb6, 0x06, 0, 0, 0, 0, 0x40, 0, 0, 0, 0x3c, 0, 0x64, 0, 0, 0, 0, 0})
	xlsRecord(&sheetFooter, 0x001d, []byte{3, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0})
	xlsRecord(&sheetFooter, 0x000a, nil) // EOF
	return writeXLSCompound(w, globals.Bytes(), sheetHeader.Bytes(), book.cells.Bytes(), sheetFooter.Bytes())
}

type xlsBook struct {
	columns      int
	strings      map[string]uint32
	unique       []string
	stringCount  uint32
	stringBudget int
	rows         int
	cells        bytes.Buffer
	block        []bytes.Buffer
	blockOffsets []uint32
}

func (b *xlsBook) addRow(row []any) error {
	if b.rows >= xlsMaxRows {
		return errors.New("XLS 每张工作表最多 65,536 行（含表头），请改用 XLSX")
	}
	var cells bytes.Buffer
	for col, value := range row {
		if err := b.cell(&cells, uint16(b.rows), uint16(col), value); err != nil {
			return fmt.Errorf("XLS 第 %d 行第 %d 列: %w", b.rows+1, col+1, err)
		}
	}
	b.block = append(b.block, cells)
	b.rows++
	if len(b.block) == 32 {
		b.finishBlock()
	}
	if b.stringBudget+b.cells.Len() > xlsMaxBIFF {
		return errXLSSize
	}
	return nil
}

func (b *xlsBook) cell(w *bytes.Buffer, row, col uint16, value any) error {
	header := xlsUint16s(row, col, xlsCellXF)
	switch v := value.(type) {
	case nil:
		xlsRecord(w, 0x0201, header) // Blank
		return nil
	case bool:
		data := byte(0)
		if v {
			data = 1
		}
		xlsRecord(w, 0x0205, append(header, data, 0)) // BoolErr, Boolean value
		return nil
	}

	if number, ok := xlsNumericValue(value); ok {
		xlsRecord(w, 0x0203, xlsOrder.AppendUint64(header, math.Float64bits(number))) // Number
		return nil
	}
	var label string
	switch v := value.(type) {
	case time.Time:
		label = v.Format(time.RFC3339Nano)
	case []byte:
		if len(v) > (xlsMaxText-len("[HEX] "))/2 {
			return errors.New("二进制值的十六进制文本超过 XLS 单元格限制，请改用 Parquet")
		}
		label = "[HEX] " + hex.EncodeToString(v)
	default:
		label = fmt.Sprint(v)
	}
	index, err := b.addString(label)
	if err != nil {
		return err
	}
	xlsRecord(w, 0x00fd, xlsOrder.AppendUint32(header, index)) // LabelSst
	return nil
}

func xlsNumericValue(value any) (float64, bool) {
	// Excel preserves only 15 decimal digits in a numeric cell. Longer
	// integers are written as literal text to preserve the original value.
	const maxExactDigits = 999999999999999
	switch v := reflect.ValueOf(value); v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := v.Int()
		return float64(n), n >= -maxExactDigits && n <= maxExactDigits
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := v.Uint()
		return float64(n), n <= maxExactDigits
	case reflect.Float32, reflect.Float64:
		n := v.Float()
		return n, !math.IsInf(n, 0) && !math.IsNaN(n)
	}
	return 0, false
}

func (b *xlsBook) addString(label string) (uint32, error) {
	if index, ok := b.strings[label]; ok {
		b.stringCount++
		return index, nil
	}
	if len(label) > 4*xlsMaxText || !utf8.ValidString(label) {
		return 0, errors.New("文本无效或超过 XLS 单元格字符限制，请改用 XLSX")
	}
	units := 0
	for _, r := range label {
		units++
		if r > 0xffff {
			units++
		}
	}
	if units > xlsMaxText {
		return 0, errors.New("文本超过 XLS 单元格 32,767 字符限制")
	}
	b.stringBudget += 2*units + 3
	if b.stringBudget+b.cells.Len() > xlsMaxBIFF {
		return 0, errXLSSize
	}
	index := uint32(len(b.unique))
	b.unique = append(b.unique, label)
	b.strings[label] = index
	b.stringCount++
	return index, nil
}

func (b *xlsBook) finishBlock() {
	if len(b.block) == 0 {
		return
	}
	start := b.cells.Len()
	for i := range b.block {
		rowIndex := b.rows - len(b.block) + i
		row := xlsUint16s(uint16(rowIndex), 0, uint16(b.columns), 0x00ff, 0, 0, 0x0100, xlsCellXF)
		xlsRecord(&b.cells, 0x0208, row) // Row
	}
	previous := start + 20 // End of the first Row record.
	offsets := make([]byte, 0, len(b.block)*2)
	for i := range b.block {
		offset := b.cells.Len()
		offsets = xlsOrder.AppendUint16(offsets, uint16(offset-previous))
		previous = offset
		b.cells.Write(b.block[i].Bytes())
	}
	offset := b.cells.Len()
	b.blockOffsets = append(b.blockOffsets, uint32(offset))
	data := xlsOrder.AppendUint32(nil, uint32(offset-start))
	xlsRecord(&b.cells, 0x00d7, append(data, offsets...)) // DBCell
	b.block = b.block[:0]
}

func (b *xlsBook) globals() *bytes.Buffer {
	var stream bytes.Buffer
	xlsBOF(&stream, 0x0005)                      // Workbook globals
	xlsRecord(&stream, 0x00e1, xlsUint16s(1200)) // InterfaceHdr
	xlsRecord(&stream, 0x00c1, xlsUint16s(0))    // Mms
	xlsRecord(&stream, 0x00e2, nil)              // InterfaceEnd
	writeAccess := bytes.Repeat([]byte{' '}, 112)
	writeAccess[0], writeAccess[1], writeAccess[2] = 0, 0, 0
	xlsRecord(&stream, 0x005c, writeAccess)      // WriteAccess
	xlsRecord(&stream, 0x0042, xlsUint16s(1200)) // CodePage
	xlsRecord(&stream, 0x0161, xlsUint16s(0))    // DSF
	xlsRecord(&stream, 0x013d, xlsUint16s(1))    // RRTabId
	xlsRecord(&stream, 0x009c, xlsUint16s(14))   // FnGroupCount
	xlsRecord(&stream, 0x0012, xlsUint16s(0))    // Protect
	xlsRecord(&stream, 0x0013, xlsUint16s(0))    // Password
	xlsRecord(&stream, 0x003d, xlsUint16s(0, 0, 16384, 8192, 0x0038, 0, 0, 1, 600))
	xlsRecord(&stream, 0x0040, xlsUint16s(0)) // Backup
	xlsRecord(&stream, 0x008d, xlsUint16s(0)) // HideObj
	xlsRecord(&stream, 0x0022, xlsUint16s(0)) // Date1904
	xlsRecord(&stream, 0x000e, xlsUint16s(1)) // CalcPrecision
	xlsRecord(&stream, 0x01b7, xlsUint16s(0)) // RefreshAll
	xlsRecord(&stream, 0x00da, xlsUint16s(0)) // BookBool
	font := append(xlsUint16s(200, 0, 0x7fff, 400, 0), 0, 0, 0, 0, 5, 0)
	xlsRecord(&stream, 0x0031, append(font, "Arial"...))
	for i := 0; i <= xlsCellXF; i++ {
		xf := make([]byte, 20)
		xlsOrder.PutUint16(xf[4:], 0xfff5) // Built-in style XF with no parent.
		if i == xlsCellXF {
			xlsOrder.PutUint16(xf[4:], 1) // Cell XF based on style 0.
		}
		xf[6] = 0x20 // Vertical alignment at the bottom.
		xlsOrder.PutUint16(xf[18:], 0x20c0)
		xlsRecord(&stream, 0x00e0, xf)
	}
	xlsRecord(&stream, 0x0293, []byte{0x00, 0x80, 0x00, 0xff}) // Normal style
	xlsRecord(&stream, 0x0160, xlsUint16s(0))                  // UsesELFs
	boundSheet := stream.Len()
	xlsRecord(&stream, 0x0085, append(make([]byte, 6), 4, 0, 'D', 'a', 't', 'a'))
	b.writeSST(&stream)
	xlsRecord(&stream, 0x000a, nil)
	xlsOrder.PutUint32(stream.Bytes()[boundSheet+4:], uint32(stream.Len()))
	return &stream
}

func (b *xlsBook) sheetHeader(globalsBytes int) *bytes.Buffer {
	var header bytes.Buffer
	xlsBOF(&header, 0x0010) // Worksheet
	index := make([]byte, 16+4*len(b.blockOffsets))
	xlsOrder.PutUint32(index[8:], uint32(b.rows))
	indexOffset := header.Len()
	xlsRecord(&header, 0x020b, index)
	xlsRecord(&header, 0x000c, xlsUint16s(100)) // CalcCount
	xlsRecord(&header, 0x000f, xlsUint16s(1))   // CalcMode
	xlsRecord(&header, 0x0011, xlsUint16s(0))   // Iteration
	xlsRecord(&header, 0x005f, xlsUint16s(1))   // SaveRecalc
	xlsRecord(&header, 0x0225, xlsUint16s(0, 0x00ff))
	xlsRecord(&header, 0x0081, xlsUint16s(0x04c1)) // WsBool
	xlsRecord(&header, 0x0055, xlsUint16s(8))      // DefColWidth
	dimensions := make([]byte, 14)
	xlsOrder.PutUint32(dimensions[4:], uint32(b.rows))
	xlsOrder.PutUint16(dimensions[10:], uint16(b.columns))
	xlsRecord(&header, 0x0200, dimensions)
	for i, offset := range b.blockOffsets {
		xlsOrder.PutUint32(header.Bytes()[indexOffset+4+16+4*i:], uint32(globalsBytes+header.Len())+offset)
	}
	return &header
}

func (b *xlsBook) writeSST(w *bytes.Buffer) {
	payload := make([]byte, 0, xlsMaxRecord)
	payload = xlsOrder.AppendUint32(payload, b.stringCount)
	payload = xlsOrder.AppendUint32(payload, uint32(len(b.unique)))
	record := uint16(0x00fc)
	flush := func() {
		xlsRecord(w, record, payload)
		payload = payload[:0]
		record = 0x003c // Continue
	}
	stride := (len(b.unique) + 127) / 128
	if stride < 8 {
		stride = 8
	}
	buckets := xlsUint16s(uint16(stride))
	for i, label := range b.unique {
		// The bundled XLS reader only looks for a continuation after a short
		// SST record when at least two string references remain unread.
		if record == 0x00fc && i == len(b.unique)-2 && b.stringCount == uint32(len(b.unique)) && 8+b.stringBudget > xlsMaxRecord {
			flush()
		}
		units := utf16.Encode([]rune(label))
		width := 1
		for _, unit := range units {
			if unit > 255 {
				width = 2
				break
			}
		}
		if len(payload)+3+width > xlsMaxRecord {
			flush()
		}
		if i%stride == 0 {
			offset := 4 + len(payload)
			buckets = xlsOrder.AppendUint32(buckets, uint32(w.Len()+offset))
			buckets = append(buckets, xlsUint16s(uint16(offset), 0)...)
		}
		payload = xlsOrder.AppendUint16(payload, uint16(len(units)))
		payload = append(payload, byte(width-1))
		for _, unit := range units {
			if len(payload)+width > xlsMaxRecord {
				flush()
				payload = append(payload, byte(width-1))
			}
			if width == 2 {
				payload = xlsOrder.AppendUint16(payload, unit)
			} else {
				payload = append(payload, byte(unit))
			}
		}
	}
	flush()
	xlsRecord(w, 0x00ff, buckets) // ExtSST
}

func xlsRecord(w *bytes.Buffer, record uint16, data []byte) {
	w.Write(xlsUint16s(record, uint16(len(data))))
	w.Write(data)
}

func xlsBOF(w *bytes.Buffer, kind uint16) {
	xlsRecord(w, 0x0809, xlsUint16s(0x0600, kind, 0x0dbb, 0x07cc, 0, 0, 6, 0))
}

func xlsUint16s(values ...uint16) []byte {
	data := make([]byte, 0, len(values)*2)
	for _, value := range values {
		data = xlsOrder.AppendUint16(data, value)
	}
	return data
}
