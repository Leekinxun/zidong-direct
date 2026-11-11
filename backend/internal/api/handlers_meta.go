package api

import (
	"context"
	"database/sql"
	"net/http"

	"web-db-client/internal/db"
)

func (s *Server) acquireDB(ctx context.Context, connID, database string) (db.ConnectionConfig, *sql.DB, db.Dialect, error) {
	cfg, err := s.store.Get(connID)
	if err != nil {
		return db.ConnectionConfig{}, nil, nil, err
	}
	handle, dialect, err := s.pools.Acquire(ctx, cfg, database)
	if err != nil {
		return db.ConnectionConfig{}, nil, nil, err
	}
	return cfg, handle, dialect, nil
}

func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.QueryTimeout)
	defer cancel()

	cfg, handle, dialect, err := s.acquireDB(ctx, connID, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, "connect_failed", err.Error())
		return
	}
	dbs, err := dialect.ListDatabases(ctx, handle, cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"databases": dbs})
}

func (s *Server) handleListSchemas(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("id")
	database := r.URL.Query().Get("database")
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.QueryTimeout)
	defer cancel()

	cfg, handle, dialect, err := s.acquireDB(ctx, connID, database)
	if err != nil {
		writeError(w, http.StatusBadRequest, "connect_failed", err.Error())
		return
	}
	schemas, err := dialect.ListSchemas(ctx, handle, cfg, database)
	if err != nil {
		writeError(w, http.StatusBadRequest, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schemas": schemas})
}

func (s *Server) handleListObjects(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("id")
	database := r.URL.Query().Get("database")
	schema := r.URL.Query().Get("schema")
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
	objs, err := dialect.ListObjects(ctx, handle, database, schema)
	if err != nil {
		writeError(w, http.StatusBadRequest, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"objects": objs})
}

func (s *Server) handleDescribeTable(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("id")
	database := r.URL.Query().Get("database")
	schema := r.URL.Query().Get("schema")
	table := r.URL.Query().Get("table")
	if table == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "table parameter is required")
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
	desc, err := dialect.DescribeTable(ctx, handle, database, schema, table)
	if err != nil {
		writeError(w, http.StatusBadRequest, "describe_failed", err.Error())
		return
	}
	if desc.DDL == "" && len(desc.Columns) > 0 {
		desc.DDL = db.BuildCreateTableSQL(dialect, desc) + ";"
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema": desc})
}

func (s *Server) handleAutocomplete(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("id")
	database := r.URL.Query().Get("database")
	schema := r.URL.Query().Get("schema")
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
	objs, err := dialect.ListObjects(ctx, handle, database, schema)
	if err != nil {
		writeError(w, http.StatusBadRequest, "query_failed", err.Error())
		return
	}

	limit := len(objs)
	if limit > 80 {
		limit = 80
	}
	tables := make([]db.AutocompleteTable, 0, limit)
	for i := 0; i < limit; i++ {
		obj := objs[i]
		entry := db.AutocompleteTable{
			Schema: obj.Schema,
			Name:   obj.Name,
			Type:   obj.Type,
		}
		if desc, descErr := dialect.DescribeTable(ctx, handle, database, obj.Schema, obj.Name); descErr == nil {
			cols := make([]string, len(desc.Columns))
			for j, c := range desc.Columns {
				cols[j] = c.Name
			}
			entry.Columns = cols
		}
		tables = append(tables, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": tables})
}
