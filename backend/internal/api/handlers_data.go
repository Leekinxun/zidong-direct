package api

import (
	"context"
	"net/http"
	"time"

	"web-db-client/internal/db"
	"web-db-client/internal/sqlutil"
)

func (s *Server) handleTableData(w http.ResponseWriter, r *http.Request) {
	var req db.TableDataRequest
	if !decodeJSONBody(w, r, 1024*1024, &req) {
		return
	}
	if req.ConnectionID == "" || req.Table == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "connectionId and table are required")
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
	res, err := db.QueryTableData(ctx, handle, dialect, req, s.cfg.MaxQueryRows)
	if err != nil {
		writeError(w, http.StatusBadRequest, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleTableMutate(w http.ResponseWriter, r *http.Request) {
	var req db.RowMutationRequest
	if !decodeJSONBody(w, r, 4*1024*1024, &req) {
		return
	}
	if req.ConnectionID == "" || req.Table == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "connectionId and table are required")
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
	res, err := db.ApplyRowMutations(ctx, handle, dialect, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "mutation_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleExecuteSQL(w http.ResponseWriter, r *http.Request) {
	var req db.SQLBatchRequest
	if !decodeJSONBody(w, r, 8*1024*1024, &req) {
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

	maxRows := req.MaxRows
	if maxRows <= 0 || maxRows > s.cfg.MaxQueryRows {
		maxRows = s.cfg.MaxQueryRows
	}

	stmts := sqlutil.SplitStatements(req.SQL, string(cfg.Engine))
	if len(stmts) == 0 {
		writeError(w, http.StatusBadRequest, "empty_sql", "没有可执行的 SQL 语句")
		return
	}

	results := make([]db.QueryResult, 0, len(stmts))
	isMySQL := cfg.Engine == db.EngineMySQL

	conn, err := handle.Conn(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, "connect_failed", err.Error())
		return
	}
	defer conn.Close()

	if cfg.Engine == db.EnginePostgres && req.Schema != "" {
		if _, setErr := conn.ExecContext(ctx, "SET search_path TO "+dialect.QuoteIdent(req.Schema)+", public"); setErr == nil {
			defer func() {
				_, _ = conn.ExecContext(context.Background(), "RESET search_path")
			}()
		}
	} else if cfg.Engine == db.EngineDameng && req.Schema != "" {
		var prevSchema string
		_ = conn.QueryRowContext(ctx, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL").Scan(&prevSchema)
		if _, setErr := conn.ExecContext(ctx, "SET SCHEMA "+dialect.QuoteIdent(req.Schema)); setErr == nil && prevSchema != "" && prevSchema != req.Schema {
			defer func() {
				_, _ = conn.ExecContext(context.Background(), "SET SCHEMA "+dialect.QuoteIdent(prevSchema))
			}()
		}
	}

	for _, stmt := range stmts {
		start := time.Now()
		if sqlutil.IsReadLikeQuery(stmt, isMySQL) {
			rows, qErr := conn.QueryContext(ctx, stmt)
			if qErr != nil {
				results = append(results, db.QueryResult{
					Statement:  stmt,
					DurationMs: time.Since(start).Milliseconds(),
					Error:      qErr.Error(),
				})
				break
			}
			cols, data, truncated, scanErr := db.ScanRows(rows, maxRows)
			_ = rows.Close()
			if scanErr != nil {
				results = append(results, db.QueryResult{
					Statement:  stmt,
					DurationMs: time.Since(start).Milliseconds(),
					Error:      scanErr.Error(),
				})
				break
			}
			results = append(results, db.QueryResult{
				Columns:    cols,
				Rows:       data,
				RowCount:   len(data),
				Truncated:  truncated,
				DurationMs: time.Since(start).Milliseconds(),
				Statement:  stmt,
			})
		} else {
			execRes, execErr := conn.ExecContext(ctx, stmt)
			if execErr != nil {
				results = append(results, db.QueryResult{
					Statement:  stmt,
					DurationMs: time.Since(start).Milliseconds(),
					Error:      execErr.Error(),
				})
				break
			}
			affected, _ := execRes.RowsAffected()
			results = append(results, db.QueryResult{
				AffectedRows: &affected,
				DurationMs:   time.Since(start).Milliseconds(),
				Statement:    stmt,
			})
		}
	}

	writeJSON(w, http.StatusOK, db.SQLBatchResponse{Results: results})
}
