//go:build windows

package db

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/alexbrainman/odbc"
	winregistry "golang.org/x/sys/windows/registry"
)

const accessDriver = "Microsoft Access Driver (*.mdb, *.accdb)"

// Driver Manager calls may block inside a vendor driver. Limit the number of
// native operations that can remain active while their requests are cancelled.
var odbcImports = make(chan struct{}, 4)

type odbcImportEvent struct {
	columns []string
	values  []any
	err     error
}

func openODBCImport(ctx context.Context, file io.ReaderAt, size int64, opts ImportOptions) (importSource, error) {
	select {
	case odbcImports <- struct{}{}:
	case <-ctx.Done():
		return importSource{}, ctx.Err()
	}

	connString, query, tempDir, err := prepareODBCImport(ctx, file, size, opts)
	if err != nil {
		<-odbcImports
		return importSource{}, err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	events := make(chan odbcImportEvent)
	go func() {
		defer func() { <-odbcImports }()
		defer close(events)
		if tempDir != "" {
			defer os.RemoveAll(tempDir)
		}
		streamODBCImport(streamCtx, connString, query, opts.SkipRows, func(event odbcImportEvent) bool {
			select {
			case events <- event:
				return true
			case <-streamCtx.Done():
				return false
			}
		})
	}()

	receive := func() (odbcImportEvent, error) {
		select {
		case <-streamCtx.Done():
			return odbcImportEvent{}, streamCtx.Err()
		case event, ok := <-events:
			if err := streamCtx.Err(); err != nil {
				return odbcImportEvent{}, err
			}
			if !ok {
				return odbcImportEvent{}, io.EOF
			}
			return event, event.err
		}
	}
	first, err := receive()
	if err != nil {
		cancel()
		return importSource{}, err
	}
	return importSource{
		columns: first.columns,
		next: func() ([]any, error) {
			event, err := receive()
			return event.values, err
		},
		close: func() error {
			cancel()
			return nil
		},
	}, nil
}

func prepareODBCImport(ctx context.Context, file io.ReaderAt, size int64, opts ImportOptions) (string, string, string, error) {
	credentials, err := odbcCredentials(opts.SourceUser, opts.SourcePassword)
	if err != nil {
		return "", "", "", err
	}

	isAccess := opts.Format == "mdb" || opts.Format == "accdb"
	driver := accessDriver
	var connString string
	if !isAccess {
		dsn := strings.TrimSpace(opts.SourceDSN)
		if dsn == "" || strings.ContainsRune(dsn, '\\') {
			return "", "", "", errors.New("请填写 Windows ODBC 数据源名称（DSN）")
		}
		value, err := odbcAttribute(dsn)
		if err != nil {
			return "", "", "", err
		}
		driver = registeredODBCDriver(dsn)
		if driver == "" {
			return "", "", "", errors.New("找不到此 ODBC 数据源，请在 Windows 中创建与客户端位数匹配的 DSN")
		}
		connString = "DSN=" + value + ";" + credentials
	}
	table, err := quoteODBCSourceTable(opts.SourceTable, driver, isAccess)
	if err != nil {
		return "", "", "", err
	}
	query := "SELECT * FROM " + table
	if !isAccess {
		return connString, query, "", nil
	}

	dir, err := os.MkdirTemp("", "zidong-import-access-")
	if err != nil {
		return "", "", "", errors.New("无法创建 Access 导入临时目录")
	}
	tempFile := filepath.Join(dir, "source."+opts.Format)
	value, err := odbcAttribute(tempFile)
	if err == nil {
		err = copyAccessImport(ctx, file, size, opts.SourceMaxBytes, tempFile)
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", "", "", err
	}
	return "DRIVER={" + accessDriver + "};DBQ=" + value + ";READONLY=TRUE;" + credentials, query, dir, nil
}

func registeredODBCDriver(dsn string) string {
	const path = `Software\ODBC\ODBC.INI\ODBC Data Sources`
	for _, hive := range []winregistry.Key{winregistry.CURRENT_USER, winregistry.LOCAL_MACHINE} {
		key, err := winregistry.OpenKey(hive, path, winregistry.QUERY_VALUE)
		if err != nil {
			continue
		}
		driver, _, err := key.GetStringValue(dsn)
		_ = key.Close()
		if err == nil && driver != "" {
			return driver
		}
	}
	return ""
}

func streamODBCImport(ctx context.Context, connString, query string, skipRows int, send func(odbcImportEvent) bool) {
	conn, err := sql.Open("odbc", connString)
	if err != nil {
		send(odbcImportEvent{err: errors.New("无法打开 ODBC 数据源")})
		return
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		send(odbcImportEvent{err: errors.New("无法读取 ODBC 源表，请检查驱动、数据源、表名和访问权限")})
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil || len(columns) == 0 || len(columns) > maxImportColumns {
		send(odbcImportEvent{err: errors.New("无法读取 ODBC 源表字段，或字段数量超出限制")})
		return
	}
	if !send(odbcImportEvent{columns: columns}) {
		return
	}
	for ctx.Err() == nil && rows.Next() {
		if skipRows > 0 {
			skipRows--
			continue
		}
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			send(odbcImportEvent{err: errors.New("无法转换 ODBC 源记录")})
			return
		}
		if !send(odbcImportEvent{values: values}) {
			return
		}
	}
	if rows.Err() != nil && ctx.Err() == nil {
		send(odbcImportEvent{err: errors.New("读取 ODBC 源记录失败")})
	}
}
