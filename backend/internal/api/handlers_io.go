package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
		Format       string               `json:"format"` // csv | json | sql
		WithBOM      bool                 `json:"withBom"`
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

	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "csv"
	}
	ext := format
	contentType := "text/csv; charset=utf-8"
	switch format {
	case "json":
		contentType = "application/json; charset=utf-8"
	case "sql":
		contentType = "application/sql; charset=utf-8"
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
	lw := &lazyExportWriter{
		w:           w,
		contentType: contentType,
		filename:    fmt.Sprintf("%s.%s", safeName, ext),
	}
	if err := db.ExportRowsToWriter(ctx, handle, dialect, querySQL, args, db.ExportOptions{
		Format:   format,
		WithBOM:  req.WithBOM,
		Table:    req.Table,
		Database: req.Database,
		Schema:   req.Schema,
		MaxRows:  maxRows,
	}, lw); err != nil {
		if !lw.wrote {
			writeError(w, http.StatusBadRequest, "export_failed", err.Error())
		}
		return
	}
	if !lw.wrote {
		lw.ensureHeaders()
	}
}

type lazyExportWriter struct {
	w           http.ResponseWriter
	contentType string
	filename    string
	wrote       bool
}

func (l *lazyExportWriter) ensureHeaders() {
	if !l.wrote {
		l.w.Header().Set("Content-Type", l.contentType)
		l.w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, l.filename))
		l.wrote = true
	}
}

func (l *lazyExportWriter) Write(p []byte) (int, error) {
	l.ensureHeaders()
	return l.w.Write(p)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "import_error", "解析上传表单失败: "+err.Error())
		return
	}
	connID := r.FormValue("connectionId")
	database := r.FormValue("database")
	schema := r.FormValue("schema")
	table := r.FormValue("table")
	format := strings.ToLower(strings.TrimSpace(r.FormValue("format")))
	emptyAsNull := r.FormValue("emptyAsNull") != "false"
	continueOnError := r.FormValue("continueOnError") == "true"

	if connID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "connectionId is required")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "import_error", "缺少上传文件")
		return
	}
	defer file.Close()

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

	if format == "sql" {
		rawBytes, err := io.ReadAll(file)
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
		defer func() {
			_ = tx.Rollback()
		}()
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

	if table == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "CSV/JSON 导入必须指定目标表")
		return
	}

	summary, err := db.ImportCSVOrJSON(ctx, handle, dialect, database, schema, table, format, file, emptyAsNull, continueOnError)
	if err != nil {
		writeError(w, http.StatusBadRequest, "import_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
