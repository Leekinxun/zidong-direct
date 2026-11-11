package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"web-db-client/internal/db"
	"web-db-client/internal/store"
)

func (s *Server) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"engines":                   db.AllCapabilities(),
		"sqliteDir":                 s.cfg.SQLiteDir,
		"allowArbitrarySqlitePaths": s.cfg.AllowArbitrarySQLitePaths,
		"maxQueryRows":              s.cfg.MaxQueryRows,
	})
}

func (s *Server) handleListConnections(w http.ResponseWriter, _ *http.Request) {
	list, err := s.store.ListViews()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": list})
}

func (s *Server) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	var in db.ConnectionInput
	if !decodeJSONBody(w, r, 1024*1024, &in) {
		return
	}
	if err := validateConnectionInput(in); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	view, err := s.store.Create(in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"connection": view})
}

func (s *Server) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in db.ConnectionInput
	if !decodeJSONBody(w, r, 1024*1024, &in) {
		return
	}
	if err := validateConnectionInput(in); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	view, err := s.store.Update(id, in)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	s.pools.InvalidateConnection(id)
	writeJSON(w, http.StatusOK, map[string]any{"connection": view})
}

func (s *Server) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	s.pools.InvalidateConnection(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.pools.InvalidateConnection(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var in db.ConnectionInput
	if !decodeJSONBody(w, r, 1024*1024, &in) {
		return
	}
	if err := validateConnectionInput(in); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	cfg, err := s.store.ResolveRuntimeConfig(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, "config_error", err.Error())
		return
	}
	latencyMs, err := s.pools.TestConnection(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "connect_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"latencyMs": latencyMs,
	})
}

func validateConnectionInput(in db.ConnectionInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("连接名称不能为空")
	}
	if _, err := db.GetDialect(in.Engine); err != nil {
		return err
	}
	if in.Engine == db.EngineSQLite {
		if strings.TrimSpace(in.SQLitePath) == "" {
			return fmt.Errorf("SQLite 文件路径不能为空")
		}
		return nil
	}
	if strings.TrimSpace(in.Host) == "" {
		return fmt.Errorf("主机地址不能为空")
	}
	if in.Engine == db.EngineOracle && strings.TrimSpace(in.Database) == "" {
		return fmt.Errorf("Oracle 连接需要填写 Service Name 或 SID")
	}
	return nil
}

func (s *Server) handleListSQLiteFiles(w http.ResponseWriter, _ *http.Request) {
	files, err := store.ListSQLiteFiles(s.cfg.SQLiteDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sqlite_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files":     files,
		"sqliteDir": s.cfg.SQLiteDir,
	})
}

func (s *Server) handleCreateSQLiteFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSONBody(w, r, 4096, &req) {
		return
	}
	name := filepath.Base(strings.TrimSpace(req.Name))
	if name == "" || name == "." || name == ".." {
		writeError(w, http.StatusBadRequest, "invalid_name", "文件名无效")
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".db" && ext != ".sqlite" && ext != ".sqlite3" {
		name += ".db"
	}
	resolved, err := store.ResolveSQLitePath(s.cfg.SQLiteDir, false, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}
	if _, err := os.Stat(resolved); err == nil {
		writeError(w, http.StatusConflict, "already_exists", "该 SQLite 文件已存在")
		return
	}
	f, err := os.OpenFile(resolved, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	_ = f.Close()
	writeJSON(w, http.StatusCreated, map[string]any{
		"relativePath": filepath.ToSlash(name),
	})
}

func (s *Server) handleUploadSQLiteFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "upload_error", "解析上传文件失败: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "upload_error", "缺少 file 字段")
		return
	}
	defer file.Close()

	base := filepath.Base(strings.TrimSpace(header.Filename))
	if base == "" || base == "." || base == ".." {
		writeError(w, http.StatusBadRequest, "invalid_filename", "文件名无效")
		return
	}
	ext := strings.ToLower(filepath.Ext(base))
	if ext != ".db" && ext != ".sqlite" && ext != ".sqlite3" {
		base += ".db"
	}
	uploadsDir := filepath.Join(s.cfg.SQLiteDir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "mkdir_failed", err.Error())
		return
	}
	relPath := filepath.ToSlash(filepath.Join("uploads", base))
	resolved, err := store.ResolveSQLitePath(s.cfg.SQLiteDir, false, relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}

	tmpPath := resolved + ".uploading"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "write_failed", err.Error())
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, "write_failed", err.Error())
		return
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, "write_failed", err.Error())
		return
	}
	if err := os.Rename(tmpPath, resolved); err != nil {
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, "write_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"relativePath": relPath,
		"name":         base,
	})
}

func (s *Server) handleDownloadSQLiteFile(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	resolved, err := store.ResolveSQLitePath(s.cfg.SQLiteDir, s.cfg.AllowArbitrarySQLitePaths, relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "not_found", "SQLite 文件不存在")
		return
	}
	safeName := filepath.Base(resolved)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(safeName, `"`, "")))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, resolved)
}
