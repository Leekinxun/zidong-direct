package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"

	"web-db-client/internal/db"
	"web-db-client/internal/sqlutil"
)

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConnectionID string               `json:"connectionId"`
		Database     string               `json:"database"`
		Schema       string               `json:"schema"`
		Table        string               `json:"table"`
		SQL          string               `json:"sql"`
		Format       string               `json:"format"`
		WithBOM      bool                 `json:"withBom"`
		Delimiter    string               `json:"delimiter"`
		Widths       string               `json:"widths"`
		Filters      []db.FilterCondition `json:"filters"`
		SortColumn   string               `json:"sortColumn"`
		SortDir      string               `json:"sortDir"`
		MaxRows      int                  `json:"maxRows"`
	}
	if !decodeJSONBody(w, r, 2*1024*1024, &req) {
		return
	}
	if req.ConnectionID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "connectionId is required")
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "csv"
	}
	contentType, ok := db.ExportContentType(format)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_format", "不支持的导出格式")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.QueryTimeout)
	defer cancel()

	cfg, handle, dialect, err := s.acquireDB(ctx, req.ConnectionID, req.Database)
	if err != nil {
		writeError(w, http.StatusBadRequest, "connect_failed", err.Error())
		return
	}
	if req.Database == "" {
		req.Database = cfg.Database
	}

	var querySQL string
	var args []any

	if strings.TrimSpace(req.SQL) != "" {
		stmts := sqlutil.SplitStatements(req.SQL, string(cfg.Engine))
		if len(stmts) != 1 || !sqlutil.IsReadLikeQuery(stmts[0], cfg.Engine == db.EngineMySQL) {
			writeError(w, http.StatusBadRequest, "bad_sql", "导出仅支持单条 SELECT/SHOW 查询语句")
			return
		}
		querySQL = stmts[0]
	} else {
		if req.Table == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "table or sql is required")
			return
		}
		schemaInfo, err := dialect.DescribeTable(ctx, handle, req.Database, req.Schema, req.Table)
		if err != nil {
			writeError(w, http.StatusBadRequest, "describe_failed", err.Error())
			return
		}
		if schemaInfo.Database != "" {
			req.Database = schemaInfo.Database
		}
		if schemaInfo.Schema != "" {
			req.Schema = schemaInfo.Schema
		}
		if schemaInfo.Table != "" {
			req.Table = schemaInfo.Table
		}
		whereSQL, whereArgs, _, err := db.BuildWhereClause(dialect, schemaInfo.Columns, req.Filters, 1)
		if err != nil {
			writeError(w, http.StatusBadRequest, "filter_error", err.Error())
			return
		}
		tableRef := db.QualifiedTable(dialect, req.Database, req.Schema, req.Table)
		querySQL = "SELECT * FROM " + tableRef + whereSQL
		if strings.TrimSpace(req.SortColumn) != "" {
			valid := false
			for _, c := range schemaInfo.Columns {
				if c.Name == req.SortColumn {
					valid = true
					break
				}
			}
			if !valid {
				writeError(w, http.StatusBadRequest, "bad_sort", "invalid sortColumn")
				return
			}
			dir := "ASC"
			if strings.EqualFold(req.SortDir, "DESC") {
				dir = "DESC"
			}
			querySQL += " ORDER BY " + dialect.QuoteIdent(req.SortColumn) + " " + dir
		}
		args = whereArgs
	}

	baseName := req.Table
	if baseName == "" {
		baseName = "query_result"
	}
	safeName := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, baseName)

	maxRows := req.MaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}
	file, err := os.CreateTemp("", "zidong-export-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_failed", "无法创建导出临时文件")
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()

	// Finish the file before sending the attachment headers so a writer or query
	// error cannot leave the user with an apparently successful partial file.
	if err := db.ExportRowsToWriter(ctx, handle, dialect, querySQL, args, db.ExportOptions{
		Format:    format,
		WithBOM:   req.WithBOM,
		Delimiter: req.Delimiter,
		Widths:    req.Widths,
		Table:     req.Table,
		Database:  req.Database,
		Schema:    req.Schema,
		MaxRows:   maxRows,
	}, file); err != nil {
		writeError(w, http.StatusBadRequest, "export_failed", err.Error())
		return
	}
	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_failed", "无法读取导出文件")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "export_failed", "无法读取导出文件")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, safeName, format))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, file)
}

type importFile interface {
	io.ReaderAt
	io.Closer
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "import_error", "解析上传表单失败: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll()

	connID := r.FormValue("connectionId")
	database := r.FormValue("database")
	schema := r.FormValue("schema")
	table := r.FormValue("table")
	preview := r.FormValue("preview") == "true"
	opts := db.ImportOptions{
		Format:          strings.ToLower(strings.TrimSpace(r.FormValue("format"))),
		Encoding:        r.FormValue("encoding"),
		Delimiter:       r.FormValue("delimiter"),
		Widths:          r.FormValue("widths"),
		Sheet:           r.FormValue("sheet"),
		XMLRow:          r.FormValue("xmlRow"),
		Mode:            r.FormValue("mode"),
		SourceDSN:       strings.TrimSpace(r.FormValue("sourceDsn")),
		SourceUser:      r.FormValue("sourceUser"),
		SourcePassword:  r.FormValue("sourcePassword"),
		SourceTable:     strings.TrimSpace(r.FormValue("sourceTable")),
		SourceMaxBytes:  s.cfg.MaxUploadBytes,
		NoHeader:        r.FormValue("noHeader") == "true",
		EmptyAsNull:     r.FormValue("emptyAsNull") != "false",
		ContinueOnError: r.FormValue("continueOnError") == "true",
	}
	if raw := r.FormValue("skipRows"); raw != "" {
		count, err := strconv.Atoi(raw)
		if err != nil || count < 0 || count > 100000 {
			writeError(w, http.StatusBadRequest, "bad_request", "跳过行数必须介于 0 和 100000 之间")
			return
		}
		opts.SkipRows = count
	}
	if raw := r.FormValue("mappings"); raw != "" {
		if len(raw) > 256<<10 || json.Unmarshal([]byte(raw), &opts.Mappings) != nil || opts.Mappings == nil {
			writeError(w, http.StatusBadRequest, "bad_request", "字段映射格式无效")
			return
		}
	}

	if opts.Format == "json" && opts.Mappings == nil && opts.Mode == "" {
		opts.EmptyAsNull = false // Legacy JSON imports preserve explicit empty strings.
	}
	if connID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "connectionId is required")
		return
	}
	if opts.Format != "sql" && strings.TrimSpace(table) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "数据文件导入必须指定目标表")
		return
	}
	if preview && opts.Format == "sql" {
		writeError(w, http.StatusBadRequest, "bad_request", "SQL 脚本不支持预览")
		return
	}
	if !preview && (opts.Mode == "replace" || opts.Mode == "delete") && r.FormValue("confirmed") != "true" {
		writeError(w, http.StatusBadRequest, "confirmation_required", "此模式会删除目标表数据，请先确认")
		return
	}

	sourceURL := strings.TrimSpace(r.FormValue("sourceUrl"))
	if sourceURL != "" && !s.cfg.DesktopMode && !s.cfg.AuthEnabled {
		writeError(w, http.StatusForbidden, "authentication_required", "URL 导入仅在桌面版或已配置登录验证的 Web 部署中可用")
		return
	}
	odbc := opts.Format == "odbc"
	access := opts.Format == "mdb" || opts.Format == "accdb"
	if odbc || access {
		if !s.cfg.DesktopMode || runtime.GOOS != "windows" {
			writeError(w, http.StatusBadRequest, "unsupported_source", "ODBC 和 Access 仅在安装了对应驱动的 Windows 桌面版可用")
			return
		}
		if opts.SourceTable == "" || (odbc && opts.SourceDSN == "") {
			writeError(w, http.StatusBadRequest, "bad_request", "请输入源表名及适用的 ODBC 数据源名称")
			return
		}
		if sourceURL != "" || (odbc && len(r.MultipartForm.File["file"]) > 0) {
			writeError(w, http.StatusBadRequest, "bad_request", "ODBC 请选择系统数据源，Access 请选择本地文件")
			return
		}
	}
	if sourceURL != "" && opts.Format == "sql" {
		writeError(w, http.StatusBadRequest, "bad_request", "请先下载并核对 SQL 脚本，再选择本地文件")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.QueryTimeout)
	defer cancel()
	cfg, handle, dialect, err := s.acquireDB(ctx, connID, database)
	if err != nil {
		writeError(w, http.StatusBadRequest, "connect_failed", err.Error())
		return
	}
	if database == "" {
		database = cfg.Database
	}

	var file importFile
	var size int64
	if sourceURL != "" {
		if len(r.MultipartForm.File["file"]) > 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "请选择文件或输入 URL")
			return
		}
		file, size, err = openImportURL(ctx, sourceURL, s.cfg.MaxUploadBytes)
		if err != nil {
			writeError(w, http.StatusBadRequest, "download_failed", err.Error())
			return
		}
	} else if !odbc {
		if len(r.MultipartForm.File["file"]) != 1 {
			writeError(w, http.StatusBadRequest, "bad_request", "请选择一个本地文件")
			return
		}
		uploaded, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "import_error", "缺少上传文件")
			return
		}
		file, size = uploaded, header.Size
	}
	if file != nil {
		defer file.Close()
	}

	var sourceDigest string
	if sourceURL != "" {
		digest := sha256.New()
		if _, err := io.Copy(digest, io.NewSectionReader(file, 0, size)); err != nil {
			writeError(w, http.StatusBadRequest, "read_failed", err.Error())
			return
		}
		sourceDigest = hex.EncodeToString(digest.Sum(nil))
		if !preview && r.FormValue("sourceDigest") != sourceDigest {
			writeError(w, http.StatusConflict, "source_changed", "URL 数据与预览不同，请重新预览后导入")
			return
		}
	}

	if opts.Format == "sql" {
		reader, err := db.DecodeImportText(io.NewSectionReader(file, 0, size), opts.Encoding)
		if err != nil {
			writeError(w, http.StatusBadRequest, "read_failed", err.Error())
			return
		}
		rawBytes, err := io.ReadAll(reader)
		if err != nil {
			writeError(w, http.StatusBadRequest, "read_failed", err.Error())
			return
		}
		stmts := sqlutil.SplitStatements(string(rawBytes), string(cfg.Engine))
		if len(stmts) == 0 {
			writeError(w, http.StatusBadRequest, "empty_sql", "SQL 文件中没有可执行语句")
			return
		}
		tx, err := handle.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusBadRequest, "tx_failed", err.Error())
			return
		}
		defer tx.Rollback()
		var total int64
		for idx, st := range stmts {
			res, execErr := tx.ExecContext(ctx, st)
			if execErr != nil {
				writeError(w, http.StatusBadRequest, "sql_import_failed", fmt.Sprintf("第 %d 条语句执行失败: %v", idx+1, execErr))
				return
			}
			n, _ := res.RowsAffected()
			total += n
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusBadRequest, "commit_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"insertedRows": total,
			"statements":   len(stmts),
		})
		return
	}

	if preview {
		data, err := db.PreviewImport(ctx, handle, dialect, database, schema, table, file, size, opts)
		if err != nil {
			writeError(w, http.StatusBadRequest, "preview_failed", err.Error())
			return
		}
		data.SourceDigest = sourceDigest
		writeJSON(w, http.StatusOK, data)
		return
	}
	summary, err := db.ImportRows(ctx, handle, dialect, database, schema, table, file, size, opts)
	if err != nil {
		writeError(w, http.StatusBadRequest, "import_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
