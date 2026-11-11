package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"web-db-client/internal/config"
	"web-db-client/internal/crypto"
	"web-db-client/internal/db"
	"web-db-client/internal/store"
)

type Server struct {
	cfg       config.Config
	store     *store.ConnectionStore
	pools     *db.PoolManager
	auth      *AuthHandler
	staticDir string
	terminals terminalSessions
}

func NewServer(cfg config.Config, st *store.ConnectionStore, pools *db.PoolManager, cipher *crypto.Cipher, staticDir string) *Server {
	return &Server{
		cfg:       cfg,
		store:     st,
		pools:     pools,
		auth:      NewAuthHandler(cfg, cipher),
		staticDir: staticDir,
	}
}

func (s *Server) Handler() http.Handler {
	var routes apiRoutes

	routes.add("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	routes.add("GET /api/auth/status", s.auth.HandleStatus)
	routes.add("POST /api/auth/login", s.auth.HandleLogin)
	routes.add("POST /api/auth/logout", s.auth.HandleLogout)

	routes.add("GET /api/capabilities", s.handleCapabilities)
	routes.add("GET /api/connections", s.handleListConnections)
	routes.add("POST /api/connections", s.handleCreateConnection)
	routes.add("PUT /api/connections/{id}", s.handleUpdateConnection)
	routes.add("DELETE /api/connections/{id}", s.handleDeleteConnection)
	routes.add("POST /api/connections/test", s.handleTestConnection)
	routes.add("POST /api/connections/{id}/disconnect", s.handleDisconnect)

	routes.add("GET /api/sqlite/files", s.handleListSQLiteFiles)
	routes.add("POST /api/sqlite/create", s.handleCreateSQLiteFile)
	routes.add("POST /api/sqlite/upload", s.handleUploadSQLiteFile)
	routes.add("GET /api/sqlite/download", s.handleDownloadSQLiteFile)

	routes.add("GET /api/connections/{id}/databases", s.handleListDatabases)
	routes.add("GET /api/connections/{id}/database/describe", s.handleDescribeDatabase)
	routes.add("GET /api/connections/{id}/schemas", s.handleListSchemas)
	routes.add("GET /api/connections/{id}/objects", s.handleListObjects)
	routes.add("GET /api/connections/{id}/table/describe", s.handleDescribeTable)
	routes.add("GET /api/connections/{id}/autocomplete", s.handleAutocomplete)

	routes.add("POST /api/table/data", s.handleTableData)
	routes.add("POST /api/table/mutate", s.handleTableMutate)
	routes.add("POST /api/query/execute", s.handleExecuteSQL)

	routes.add("POST /api/table/ddl/preview", s.handlePreviewDDL)
	routes.add("POST /api/table/ddl/execute", s.handleExecuteDDL)
	routes.add("POST /api/table/action", s.handleTableAction)

	routes.add("POST /api/export", s.handleExport)
	routes.add("POST /api/import", s.handleImport)
	s.addTerminalRoutes(&routes)

	mux := http.NewServeMux()
	mux.Handle("/", routes.withFallback(s.spaHandler()))

	return s.auth.Middleware(mux)
}

func (s *Server) spaHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not_found", "API route not found")
			return
		}
		if s.staticDir == "" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Frontend static assets directory not configured."))
			return
		}
		cleaned := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if cleaned == "." {
			cleaned = "index.html"
		}
		target := filepath.Join(s.staticDir, cleaned)
		rel, err := filepath.Rel(s.staticDir, target)
		if err == nil && !strings.HasPrefix(rel, "..") {
			if info, statErr := os.Stat(target); statErr == nil && !info.IsDir() {
				http.ServeFile(w, r, target)
				return
			}
		}
		indexFile := filepath.Join(s.staticDir, "index.html")
		if _, err := os.Stat(indexFile); err == nil {
			http.ServeFile(w, r, indexFile)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Web UI build not found. Build frontend into static directory."))
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error":   code,
		"message": message,
	})
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) bool {
	if maxBytes <= 0 {
		maxBytes = 2 * 1024 * 1024
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求 JSON 解析失败: "+err.Error())
		return false
	}
	return true
}
