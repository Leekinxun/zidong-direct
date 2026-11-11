package api

import (
	"context"
	"net/http"

	"web-db-client/internal/db"
)

func (s *Server) handlePreviewDDL(w http.ResponseWriter, r *http.Request) {
	var diff db.TableDesignDiff
	if !decodeJSONBody(w, r, 2*1024*1024, &diff) {
		return
	}
	if diff.ConnectionID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "connectionId is required")
		return
	}
	cfg, err := s.store.Get(diff.ConnectionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	dialect, err := db.GetDialect(cfg.Engine)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_engine", err.Error())
		return
	}
	if diff.Target.Database == "" {
		diff.Target.Database = diff.Database
	}
	if diff.Target.Schema == "" {
		diff.Target.Schema = diff.Schema
	}
	if diff.Target.Table == "" {
		diff.Target.Table = diff.TableName
	}

	preview, err := db.BuildDDLPreview(dialect, diff)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ddl_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleExecuteDDL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConnectionID string   `json:"connectionId"`
		Database     string   `json:"database"`
		Statements   []string `json:"statements"`
	}
	if !decodeJSONBody(w, r, 2*1024*1024, &req) {
		return
	}
	if req.ConnectionID == "" || len(req.Statements) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "connectionId and statements are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.QueryTimeout)
	defer cancel()

	_, handle, _, err := s.acquireDB(ctx, req.ConnectionID, req.Database)
	if err != nil {
		writeError(w, http.StatusBadRequest, "connect_failed", err.Error())
		return
	}

	tx, err := handle.BeginTx(ctx, nil)
	if err != nil {
		// Some engines auto-commit DDL; run directly if tx fails
		for _, stmt := range req.Statements {
			if _, execErr := handle.ExecContext(ctx, stmt); execErr != nil {
				writeError(w, http.StatusBadRequest, "ddl_exec_failed", execErr.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "executed": len(req.Statements)})
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for _, stmt := range req.Statements {
		if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
			writeError(w, http.StatusBadRequest, "ddl_exec_failed", execErr.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusBadRequest, "ddl_commit_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "executed": len(req.Statements)})
}

func (s *Server) handleTableAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConnectionID string `json:"connectionId"`
		Database     string `json:"database"`
		Schema       string `json:"schema"`
		Table        string `json:"table"`
		Action       string `json:"action"` // "drop_table" | "drop_view" | "truncate_table" | "empty_table"
	}
	if !decodeJSONBody(w, r, 64*1024, &req) {
		return
	}
	if req.ConnectionID == "" || req.Table == "" || req.Action == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "connectionId, table, and action are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.QueryTimeout)
	defer cancel()

	cfg, handle, dialect, err := s.acquireDB(ctx, req.ConnectionID, req.Database)
	if err != nil {
		writeError(w, http.StatusBadRequest, "connect_failed", err.Error())
		return
	}
	dbName := req.Database
	if dbName == "" {
		dbName = cfg.Database
	}
	target := db.QualifiedTable(dialect, dbName, req.Schema, req.Table)

	var stmt string
	switch req.Action {
	case "drop_table":
		stmt = "DROP TABLE " + target
	case "drop_view":
		stmt = "DROP VIEW " + target
	case "empty_table":
		stmt = "DELETE FROM " + target
	case "truncate_table":
		if dialect.Engine() == db.EngineSQLite {
			stmt = "DELETE FROM " + target
		} else {
			stmt = "TRUNCATE TABLE " + target
		}
	default:
		writeError(w, http.StatusBadRequest, "bad_action", "unsupported table action: "+req.Action)
		return
	}

	if _, err := handle.ExecContext(ctx, stmt); err != nil {
		writeError(w, http.StatusBadRequest, "action_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "statement": stmt})
}
