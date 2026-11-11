package db

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

func quoteODBCSourceTable(name, driver string, accessFile bool) (string, error) {
	parts := strings.Split(name, ".")
	if len(parts) > 3 || (accessFile && len(parts) != 1) {
		return "", errors.New("源表名无效，请填写表名或模式.表名")
	}
	driver = strings.ToLower(driver)
	open, close := `"`, `"`
	switch {
	case accessFile, strings.Contains(driver, "access"), strings.Contains(driver, "sql server"), strings.Contains(driver, "microsoft excel"):
		open, close = "[", "]"
	case strings.Contains(driver, "mysql"), strings.Contains(driver, "mariadb"):
		open, close = "`", "`"
	}
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || len(part) > 512 || !utf8.ValidString(part) ||
			strings.ContainsAny(part, "`\"'[];\\") || strings.IndexFunc(part, unicode.IsControl) >= 0 {
			return "", errors.New("源表名无效，请填写未加引号的表名")
		}
		parts[i] = open + part + close
	}
	return strings.Join(parts, "."), nil
}

func odbcAttribute(value string) (string, error) {
	if len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsAny(value, "{}") ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", errors.New("ODBC 连接属性过长，或含有花括号、控制字符")
	}
	if value != strings.TrimSpace(value) || strings.ContainsAny(value, "[](),;?*=!@") {
		return "{" + value + "}", nil
	}
	return value, nil
}

func odbcCredentials(user, password string) (string, error) {
	var credentials strings.Builder
	for _, item := range []struct{ key, value string }{{"UID", user}, {"PWD", password}} {
		if item.value == "" && (item.key != "PWD" || user == "") {
			continue
		}
		value, err := odbcAttribute(item.value)
		if err != nil {
			return "", err
		}
		credentials.WriteString(item.key + "=" + value + ";")
	}
	return credentials.String(), nil
}

func copyAccessImport(ctx context.Context, input io.ReaderAt, size, maxBytes int64, dest string) error {
	if maxBytes <= 0 {
		maxBytes = 256 * 1024 * 1024
	}
	if input == nil || size <= 0 || size > maxBytes {
		return errors.New("Access 文件为空或超过导入大小限制")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("无法创建 Access 导入临时文件")
	}
	reader := accessCopyReader{ctx, io.NewSectionReader(input, 0, size)}
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if copyErr != nil || closeErr != nil || written != size {
		return errors.New("无法完整读取 Access 导入文件")
	}
	return nil
}

type accessCopyReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r accessCopyReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
