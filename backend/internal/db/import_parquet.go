package db

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

func openParquetImport(ctx context.Context, reader io.ReaderAt, size int64) (_ importSource, err error) {
	defer recoverImportReader("Parquet", &err)
	if size < 12 {
		return importSource{}, errors.New("Parquet 文件不完整")
	}
	var footer [8]byte
	if _, err := reader.ReadAt(footer[:], size-8); err != nil {
		return importSource{}, err
	}
	length := int64(binary.LittleEndian.Uint32(footer[:4]))
	if string(footer[4:]) != "PAR1" || length <= 0 || length > size-12 || length > 8<<20 {
		return importSource{}, errors.New("Parquet 文件元数据无效或过大")
	}
	if err := validateParquetLayout(ctx, reader, size-8-length, length); err != nil {
		return importSource{}, err
	}
	file, err := parquet.OpenFile(reader, size, parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if err != nil {
		return importSource{}, err
	}
	paths := file.Schema().Columns()
	if len(paths) == 0 || len(paths) > maxImportColumns {
		return importSource{}, errors.New("Parquet 文件列数无效")
	}
	columns := make([]string, len(paths))
	types := make([]*format.LogicalType, len(paths))
	for i, path := range paths {
		leaf, ok := file.Schema().Lookup(path...)
		if !ok || len(path) != 1 || leaf.Node.Repeated() {
			return importSource{}, errors.New("Parquet 导入支持平面列，请先展开嵌套对象和数组")
		}
		columns[i] = path[0]
		types[i] = leaf.Node.Type().LogicalType()
	}
	rows := parquet.MultiRowGroup(file.RowGroups()...).Rows()
	buffer := make([]parquet.Row, 1)
	return importSource{
		columns: columns,
		close:   rows.Close,
		next: func() (_ []any, err error) {
			defer recoverImportReader("Parquet", &err)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			n, err := rows.ReadRows(buffer)
			if n == 0 && err == nil {
				return nil, io.ErrNoProgress
			}
			if n == 0 || (err != nil && !errors.Is(err, io.EOF)) {
				return nil, err
			}
			values := make([]any, len(columns))
			buffer[0].Range(func(i int, cells []parquet.Value) bool {
				if i < 0 || i >= len(values) || len(cells) != 1 {
					err = errors.New("Parquet 数据行的列结构无效")
					return false
				}
				values[i], err = parquetImportValue(cells[0], types[i])
				return err == nil
			})
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			return values, nil
		},
	}, nil
}

func parquetImportValue(value parquet.Value, logical *format.LogicalType) (any, error) {
	if value.IsNull() {
		return nil, nil
	}
	if logical != nil {
		switch {
		case logical.Decimal != nil:
			return parquetDecimal(value, logical.Decimal.Scale)
		case logical.Date != nil:
			return time.Unix(int64(value.Int32())*86400, 0).UTC().Format("2006-01-02"), nil
		case logical.Timestamp != nil:
			return parquetTimestamp(value.Int64(), logical.Timestamp.Unit, logical.Timestamp.IsAdjustedToUTC)
		case logical.Time != nil:
			count := value.Int64()
			if value.Kind() == parquet.Int32 {
				count = int64(value.Int32())
			}
			return parquetClock(count, logical.Time.Unit)
		case logical.UTF8 != nil || logical.Enum != nil || logical.Json != nil:
			return string(value.ByteArray()), nil
		case logical.UUID != nil:
			b := value.ByteArray()
			if len(b) != 16 {
				return nil, errors.New("Parquet UUID 长度无效")
			}
			return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
		case logical.Integer != nil && !logical.Integer.IsSigned:
			if value.Kind() == parquet.Int32 {
				return int64(value.Uint32()), nil
			}
			if n := value.Uint64(); n <= math.MaxInt64 {
				return int64(n), nil
			}
			return strconv.FormatUint(value.Uint64(), 10), nil
		}
	}
	switch value.Kind() {
	case parquet.Boolean:
		return value.Boolean(), nil
	case parquet.Int32:
		return int64(value.Int32()), nil
	case parquet.Int64:
		return value.Int64(), nil
	case parquet.Int96:
		v := value.Int96()
		ns := uint64(v[0]) | uint64(v[1])<<32
		if ns >= 86400*1e9 {
			return nil, errors.New("Parquet INT96 时间戳无效")
		}
		sec := (int64(v[2])-2440588)*86400 + int64(ns/1e9)
		return time.Unix(sec, int64(ns%1e9)).UTC().Format("2006-01-02 15:04:05.999999999"), nil
	case parquet.Float:
		return importFloat(float64(value.Float()), 32), nil
	case parquet.Double:
		return importFloat(value.Double(), 64), nil
	case parquet.ByteArray, parquet.FixedLenByteArray:
		return append([]byte{}, value.ByteArray()...), nil
	default:
		return nil, fmt.Errorf("Parquet 不支持的数据类型 %s", value.Kind())
	}
}

func importFloat(value float64, precision int) any {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return strconv.FormatFloat(value, 'g', -1, precision)
	}
	return value
}

func parquetDecimal(value parquet.Value, scale int32) (string, error) {
	if scale < 0 || scale > 76 {
		return "", errors.New("Parquet 小数精度无效")
	}
	var n big.Int
	switch value.Kind() {
	case parquet.Int32:
		n.SetInt64(int64(value.Int32()))
	case parquet.Int64:
		n.SetInt64(value.Int64())
	case parquet.ByteArray, parquet.FixedLenByteArray:
		b := value.ByteArray()
		if len(b) == 0 || len(b) > 32 {
			return "", errors.New("Parquet 小数长度无效")
		}
		n.SetBytes(b)
		if b[0]&0x80 != 0 {
			n.Sub(&n, new(big.Int).Lsh(big.NewInt(1), uint(len(b)*8)))
		}
	default:
		return "", errors.New("Parquet 小数类型无效")
	}
	sign := ""
	if n.Sign() < 0 {
		sign = "-"
	}
	digits := n.Abs(&n).String()
	if scale == 0 {
		return sign + digits, nil
	}
	if padding := int(scale) + 1 - len(digits); padding > 0 {
		digits = strings.Repeat("0", padding) + digits
	}
	split := len(digits) - int(scale)
	return sign + digits[:split] + "." + digits[split:], nil
}

func parquetNanosPerUnit(unit format.TimeUnit) int64 {
	switch {
	case unit.Millis != nil:
		return 1e6
	case unit.Micros != nil:
		return 1e3
	case unit.Nanos != nil:
		return 1
	}
	return 0
}

func parquetTimestamp(count int64, unit format.TimeUnit, utc bool) (string, error) {
	nanos := parquetNanosPerUnit(unit)
	if nanos == 0 {
		return "", errors.New("Parquet 时间戳单位无效")
	}
	perSecond := 1e9 / nanos
	timestamp := time.Unix(count/perSecond, (count%perSecond)*nanos).UTC()
	if utc {
		return timestamp.Format(time.RFC3339Nano), nil
	}
	return timestamp.Format("2006-01-02 15:04:05.999999999"), nil
}

func parquetClock(count int64, unit format.TimeUnit) (string, error) {
	nanos := parquetNanosPerUnit(unit)
	if nanos == 0 || count < 0 || count >= 86400*1e9/nanos {
		return "", errors.New("Parquet 时间值无效")
	}
	return time.Unix(0, count*nanos).UTC().Format("15:04:05.999999999"), nil
}
