package db

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/shakinm/xlsReader/cfb"
	"github.com/shakinm/xlsReader/xls"
)

type xlsReadBudget struct {
	io.ReadSeeker
	ctx       context.Context
	remaining int64
}

func (reader *xlsReadBudget) Read(buf []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(buf)) > reader.remaining {
		return 0, errors.New("XLS 文件数据链无效")
	}
	n, err := reader.ReadSeeker.Read(buf)
	reader.remaining -= int64(n)
	return n, err
}

func recoverImportReader(format string, err *error) {
	if recover() != nil {
		*err = fmt.Errorf("%s 文件结构无效", format)
	}
}

func openXLSImport(ctx context.Context, reader io.ReaderAt, size int64, opts ImportOptions) (_ importSource, err error) {
	defer recoverImportReader("XLS", &err)
	if size < 512 || size > 32<<20 {
		return importSource{}, errors.New("XLS 文件不完整或超过 32 MB，请将大文件另存为 XLSX")
	}
	// Check the workbook stream size before the BIFF decoder allocates it.
	newReader := func() *xlsReadBudget {
		return &xlsReadBudget{ReadSeeker: io.NewSectionReader(reader, 0, size), ctx: ctx, remaining: size*4 + 4096}
	}
	container, err := cfb.OpenReader(newReader())
	if err != nil {
		return importSource{}, err
	}
	workbookFound := false
	for _, entry := range container.GetDirs() {
		if entry.Name() != "Workbook" && entry.Name() != "Book" {
			continue
		}
		if streamSize := int64(entry.GetStreamSize()); streamSize <= 0 || streamSize > size {
			return importSource{}, errors.New("XLS 工作簿流大小无效")
		}
		workbookFound = true
	}
	if !workbookFound {
		return importSource{}, errors.New("XLS 文件没有工作簿")
	}
	workbook, err := xls.OpenReader(newReader())
	if err != nil {
		return importSource{}, err
	}
	sheets := workbook.GetSheets()
	names := make([]string, len(sheets))
	selected := -1
	for i := range sheets {
		names[i] = sheets[i].GetName()
		if names[i] == opts.Sheet || (selected < 0 && opts.Sheet == "") {
			selected = i
		}
	}
	if selected < 0 {
		return importSource{}, errors.New("XLS 文件中没有指定的工作表")
	}
	sheet := &sheets[selected]
	rowIndex, total := 0, sheet.GetNumberRows()
	source, err := tabularImport(func() (_ []any, err error) {
		defer recoverImportReader("XLS", &err)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if rowIndex >= total {
			return nil, io.EOF
		}
		row, err := sheet.GetRow(rowIndex)
		rowIndex++
		if err != nil {
			return nil, err
		}
		cells := row.GetCols()
		if len(cells) > 256 {
			return nil, errors.New("XLS 工作表列数无效")
		}
		values := make([]any, len(cells))
		hasData := false
		for i, cell := range cells {
			if value := cell.GetString(); value != "" {
				values[i] = value
				hasData = true
			}
		}
		if !hasData {
			return nil, nil
		}
		return values, nil
	}, opts)
	if err != nil {
		return importSource{}, err
	}
	source.sheets, source.sheet = names, names[selected]
	return source, nil
}
