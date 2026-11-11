//go:build !windows

package db

import (
	"context"
	"errors"
	"io"
)

func openODBCImport(_ context.Context, _ io.ReaderAt, _ int64, _ ImportOptions) (importSource, error) {
	return importSource{}, errors.New("ODBC DSN 和 Microsoft Access 导入需要 Windows 桌面客户端")
}
