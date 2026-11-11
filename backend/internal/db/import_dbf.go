package db

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

func openDBF(reader io.Reader, opts ImportOptions) (importSource, error) {
	const descriptorSize = 32
	var header [descriptorSize]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return importSource{}, fmt.Errorf("读取 DBF 文件头: %w", err)
	}
	switch header[0] {
	case 0x03, 0x04, 0x05, 0x30, 0x31, 0x32, 0x43, 0x63, 0x83, 0x8b, 0xcb, 0xf5:
	default:
		return importSource{}, fmt.Errorf("不支持的 DBF 版本 0x%02x", header[0])
	}
	headerSize := int(binary.LittleEndian.Uint16(header[8:10]))
	recordSize := int(binary.LittleEndian.Uint16(header[10:12]))
	count := binary.LittleEndian.Uint32(header[4:8])
	if headerSize < descriptorSize+1 || recordSize < 2 {
		return importSource{}, errors.New("无效的 DBF 头长度或记录长度")
	}
	descriptors := make([]byte, headerSize-descriptorSize)
	if _, err := io.ReadFull(reader, descriptors); err != nil {
		return importSource{}, fmt.Errorf("读取 DBF 字段定义: %w", err)
	}
	charset := opts.Encoding
	if charset == "" || charset == "auto" {
		switch header[29] {
		case 0x01:
			charset = "cp437"
		case 0x02:
			charset = "cp850"
		case 0x4d, 0x7a:
			charset = "gb18030"
		case 0x4f, 0x78:
			charset = "big5"
		default:
			charset = "windows-1252"
		}
	}
	codec, err := importEncoding(charset)
	if err != nil {
		return importSource{}, err
	}
	decode := func(raw []byte) (string, error) { return string(raw), nil }
	if codec != nil {
		decoder := codec.NewDecoder()
		decode = func(raw []byte) (string, error) {
			decoded, err := decoder.Bytes(raw)
			return string(decoded), err
		}
	}

	type field struct {
		kind   byte
		offset int
		size   int
	}
	var fields []field
	var columns []string
	offset := 1
	for len(descriptors) > 0 && descriptors[0] != 0x0d {
		if len(descriptors) < descriptorSize || len(fields) >= maxImportColumns {
			return importSource{}, errors.New("无效的 DBF 字段描述")
		}
		name := descriptors[:11]
		if i := bytes.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}
		label, err := decode(bytes.TrimSpace(name))
		if err != nil {
			return importSource{}, err
		}
		kind, size := descriptors[11], int(descriptors[16])
		switch kind {
		case 'C', 'N', 'F', 'D', 'L':
		case 'I':
			if size != 4 {
				return importSource{}, fmt.Errorf("DBF 整数字段 %q 的长度无效", label)
			}
		default:
			return importSource{}, fmt.Errorf("不支持的 DBF 字段 %q 类型 %q（例如外部备注文件）", label, kind)
		}
		if size == 0 || offset+size > recordSize {
			return importSource{}, fmt.Errorf("DBF 字段 %q 的长度无效", label)
		}
		fields = append(fields, field{kind, offset, size})
		columns = append(columns, label)
		offset += size
		descriptors = descriptors[descriptorSize:]
	}
	if len(descriptors) == 0 || len(fields) == 0 {
		return importSource{}, errors.New("DBF 没有完整字段描述")
	}
	record := make([]byte, recordSize)
	var current uint32
	return importSource{columns: columns, next: func() ([]any, error) {
		for current < count {
			current++
			if _, err := io.ReadFull(reader, record); err != nil {
				return nil, fmt.Errorf("读取第 %d 条 DBF 记录: %w", current, err)
			}
			if record[0] == '*' {
				continue
			}
			if record[0] != ' ' {
				return nil, fmt.Errorf("DBF 记录 %d 的删除标记无效", current)
			}
			row := make([]any, len(fields))
			for index, f := range fields {
				raw := record[f.offset : f.offset+f.size]
				switch f.kind {
				case 'C':
					text, err := decode(bytes.TrimRight(raw, " \x00"))
					if err != nil {
						return nil, err
					}
					row[index] = text
				case 'N', 'F':
					if text := strings.TrimSpace(string(raw)); text != "" {
						row[index] = text
					}
				case 'D':
					if text := strings.TrimSpace(string(raw)); text != "" && text != "00000000" {
						date, err := time.Parse("20060102", text)
						if err != nil {
							return nil, fmt.Errorf("无效的 DBF 日期 %q", text)
						}
						row[index] = date.Format("2006-01-02")
					}
				case 'I':
					row[index] = int64(int32(binary.LittleEndian.Uint32(raw)))
				case 'L':
					switch raw[0] {
					case 'Y', 'y', 'T', 't':
						row[index] = true
					case 'N', 'n', 'F', 'f':
						row[index] = false
					}
				}
			}
			return row, nil
		}
		return nil, io.EOF
	}}, nil
}
