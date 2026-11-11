package db

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"sort"

	"github.com/andybalholm/brotli"
	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"
	"github.com/parquet-go/parquet-go/format"
	"github.com/pierrec/lz4/v4"
	"github.com/segmentio/encoding/thrift"
)

const (
	maxParquetHeader = 1 << 20
	maxParquetPage   = 32 << 20
	maxParquetValues = 1 << 20
)

var errParquetLayout = errors.New("Parquet 文件的页或元数据无效或超过导入限制")

type parquetThriftReader interface{ thrift.Reader }

// The Win7 decoder reads lengths from the file before allocating. Check those
// lengths with a bounded Thrift reader before handing it the same data.
type boundedParquetThrift struct {
	parquetThriftReader
	input  *bufio.Reader
	budget *io.LimitedReader
	fields int
}

func (r *boundedParquetThrift) remaining() int64 {
	return r.budget.N + int64(r.input.Buffered())
}

func (r *boundedParquetThrift) ReadField() (thrift.Field, error) {
	r.fields++
	if r.fields > 1<<17 {
		return thrift.Field{}, errParquetLayout
	}
	return r.parquetThriftReader.ReadField()
}

func (r *boundedParquetThrift) ReadLength() (int, error) {
	length, err := r.parquetThriftReader.ReadLength()
	if err == nil && (length < 0 || length > maxParquetHeader || int64(length) > r.remaining()) {
		return 0, errParquetLayout
	}
	return length, err
}

func (r *boundedParquetThrift) ReadBytes() ([]byte, error) {
	length, err := r.ReadLength()
	if err != nil {
		return nil, err
	}
	value := make([]byte, length)
	_, err = io.ReadFull(r.input, value)
	return value, err
}

func (r *boundedParquetThrift) ReadString() (string, error) {
	value, err := r.ReadBytes()
	return string(value), err
}

func (r *boundedParquetThrift) checkCollection(size int32, err error) error {
	if err == nil && (size < 0 || size > 1<<15 || int64(size) > r.remaining()) {
		return errParquetLayout
	}
	return err
}

func (r *boundedParquetThrift) ReadList() (thrift.List, error) {
	list, err := r.parquetThriftReader.ReadList()
	return list, r.checkCollection(list.Size, err)
}

func (r *boundedParquetThrift) ReadSet() (thrift.Set, error) {
	set, err := r.parquetThriftReader.ReadSet()
	return set, r.checkCollection(set.Size, err)
}

func (r *boundedParquetThrift) ReadMap() (thrift.Map, error) {
	values, err := r.parquetThriftReader.ReadMap()
	return values, r.checkCollection(values.Size, err)
}

func decodeParquetHeader(file io.ReaderAt, offset, size int64, target any) (int64, error) {
	budget := &io.LimitedReader{R: io.NewSectionReader(file, offset, size), N: size}
	input := bufio.NewReader(budget)
	protocol := &thrift.CompactProtocol{}
	reader := &boundedParquetThrift{parquetThriftReader: protocol.NewReader(input), input: input, budget: budget}
	err := thrift.NewDecoder(reader).Decode(target)
	return size - reader.remaining(), err
}

type parquetColumnSpan struct {
	start, end int64
	codec      format.CompressionCodec
}

func validateParquetLayout(ctx context.Context, file io.ReaderAt, footerOffset, footerSize int64) error {
	var metadata format.FileMetaData
	if _, err := decodeParquetHeader(file, footerOffset, footerSize, &metadata); err != nil {
		return errParquetLayout
	}
	if len(metadata.Schema) < 2 || len(metadata.Schema) > maxImportColumns+1 ||
		int(metadata.Schema[0].NumChildren) != len(metadata.Schema)-1 {
		return errors.New("Parquet 导入支持平面列，请先展开嵌套对象和数组")
	}
	var spans []parquetColumnSpan
	for _, group := range metadata.RowGroups {
		if len(group.Columns) != len(metadata.Schema)-1 {
			return errParquetLayout
		}
		for _, column := range group.Columns {
			meta := column.MetaData
			start := meta.DataPageOffset
			if meta.DictionaryPageOffset != 0 {
				start = meta.DictionaryPageOffset
			}
			if column.FilePath != "" || start < 4 || start > meta.DataPageOffset ||
				start > footerOffset || meta.TotalCompressedSize < 0 ||
				meta.TotalCompressedSize > footerOffset-start ||
				meta.TotalUncompressedSize < 0 || meta.TotalUncompressedSize > 256<<20 {
				return errParquetLayout
			}
			if meta.TotalCompressedSize != 0 {
				spans = append(spans, parquetColumnSpan{start, start + meta.TotalCompressedSize, meta.Codec})
			}
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	end := int64(4)
	for _, span := range spans {
		if span.start < end {
			return errParquetLayout
		}
		end = span.end
		if err := validateParquetPages(ctx, file, span); err != nil {
			return err
		}
	}
	return nil
}

func validateParquetPages(ctx context.Context, file io.ReaderAt, span parquetColumnSpan) error {
	for offset := span.start; offset < span.end; {
		if err := ctx.Err(); err != nil {
			return err
		}
		headerLimit := span.end - offset
		if headerLimit > maxParquetHeader {
			headerLimit = maxParquetHeader
		}
		var header format.PageHeader
		headerSize, err := decodeParquetHeader(file, offset, headerLimit, &header)
		if err != nil || headerSize <= 0 {
			return errParquetLayout
		}
		offset += headerSize
		physical := int64(header.CompressedPageSize)
		logical := int64(header.UncompressedPageSize)
		if physical < 0 || physical > maxParquetPage || physical > span.end-offset ||
			logical < 0 || logical > maxParquetPage {
			return errParquetLayout
		}
		var count int32
		var prefix int64
		codec := span.codec
		switch header.Type {
		case format.DataPage:
			if header.DataPageHeader == nil {
				return errParquetLayout
			}
			count = header.DataPageHeader.NumValues
		case format.DataPageV2:
			page := header.DataPageHeaderV2
			if page == nil || page.DefinitionLevelsByteLength < 0 || page.RepetitionLevelsByteLength < 0 ||
				page.NumRows < 0 || page.NumRows > maxParquetValues || page.NumNulls < 0 || page.NumNulls > page.NumValues {
				return errParquetLayout
			}
			count = page.NumValues
			prefix = int64(page.DefinitionLevelsByteLength) + int64(page.RepetitionLevelsByteLength)
			if page.IsCompressed != nil && !*page.IsCompressed {
				codec = format.Uncompressed
			}
		case format.DictionaryPage:
			if header.DictionaryPageHeader == nil {
				return errParquetLayout
			}
			count = header.DictionaryPageHeader.NumValues
		case format.IndexPage:
		default:
			return errParquetLayout
		}
		if count < 0 || count > maxParquetValues || prefix > physical || prefix > logical {
			return errParquetLayout
		}
		if err := validateParquetCompression(file, offset+prefix, physical-prefix, logical-prefix, codec); err != nil {
			return err
		}
		offset += physical
	}
	return nil
}

func validateParquetCompression(file io.ReaderAt, offset, physical, logical int64, codec format.CompressionCodec) error {
	if codec == format.Uncompressed || (physical == 0 && logical == 0) {
		if physical != logical {
			return errParquetLayout
		}
		return nil
	}
	compressed := io.NewSectionReader(file, offset, physical)
	switch codec {
	case format.Snappy, format.Lz4Raw:
		data, err := io.ReadAll(compressed)
		if err != nil {
			return err
		}
		var size int
		if codec == format.Snappy {
			size, err = snappy.DecodedLen(data)
		} else {
			size, err = lz4.UncompressBlock(data, make([]byte, logical))
		}
		if err != nil || int64(size) != logical {
			return errParquetLayout
		}
		return nil
	}
	var decoded io.Reader
	switch codec {
	case format.Gzip:
		gzipReader, err := gzip.NewReader(compressed)
		if err != nil {
			return errParquetLayout
		}
		defer gzipReader.Close()
		decoded = gzipReader
	case format.Brotli:
		decoded = brotli.NewReader(compressed)
	case format.Zstd:
		zstdReader, err := zstd.NewReader(compressed, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(64<<20))
		if err != nil {
			return errParquetLayout
		}
		defer zstdReader.Close()
		decoded = zstdReader
	default:
		return errors.New("Parquet 文件使用了不支持的压缩编码")
	}
	var buffer [4096]byte
	n, err := io.CopyBuffer(io.Discard, io.LimitReader(decoded, logical+1), buffer[:])
	if err != nil || n != logical {
		return errParquetLayout
	}
	return nil
}
