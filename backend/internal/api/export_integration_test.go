package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"web-db-client/internal/api"
	"web-db-client/internal/config"
	"web-db-client/internal/crypto"
	"web-db-client/internal/db"
	"web-db-client/internal/store"
)

func TestExportAttachmentsUseTheSameTableAndQueryRoutes(t *testing.T) {
	dir := t.TempDir()
	fixture, err := sql.Open("sqlite", filepath.Join(dir, "export.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	if _, err := fixture.Exec(`CREATE TABLE sample(id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO sample VALUES (1, '甲'), (2, '乙');`); err != nil {
		t.Fatal(err)
	}

	cipher, err := crypto.NewCipher("", filepath.Join(dir, ".master.key"))
	if err != nil {
		t.Fatal(err)
	}
	connections, err := store.NewConnectionStore(dir, cipher)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := connections.Create(db.ConnectionInput{
		Name: "Export fixture", Engine: db.EngineSQLite, SQLitePath: "export.db",
	})
	if err != nil {
		t.Fatal(err)
	}
	pools := db.NewPoolManager(func(name string) (string, error) {
		return store.ResolveSQLitePath(dir, false, name)
	})
	defer pools.CloseAll()
	handler := api.NewServer(config.Config{SQLiteDir: dir, QueryTimeout: 10 * time.Second}, connections, pools, cipher, "", nil).Handler()
	post := func(fields map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		fields["connectionId"] = connection.ID
		data, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/export", bytes.NewReader(data)))
		return recorder
	}

	for _, format := range []struct {
		name, contentType, prefix string
	}{
		{"csv", "text/csv", "id,name\n"},
		{"tsv", "text/tab-separated-values", "id\tname\n"},
		{"txt", "text/plain", "id|name\n"},
		{"json", "application/json", "["},
		{"jsonl", "application/x-ndjson", "{"},
		{"xml", "application/xml", "<?xml"},
		{"html", "text/html", "<!doctype html>"},
		{"sql", "application/sql", "INSERT INTO "},
		{"xls", "application/vnd.ms-excel", "\xd0\xcf\x11\xe0"},
		{"xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "PK"},
		{"ods", "application/vnd.oasis.opendocument.spreadsheet", "PK"},
		{"dbf", "application/x-dbf", "\x03"},
		{"parquet", "application/vnd.apache.parquet", "PAR1"},
	} {
		t.Run(format.name, func(t *testing.T) {
			got := post(map[string]any{"format": format.name, "sql": "SELECT id, name FROM sample ORDER BY id", "delimiter": "|", "maxRows": 1})
			if got.Code != http.StatusOK || !strings.HasPrefix(got.Body.String(), format.prefix) {
				t.Fatalf("unexpected %s response: HTTP %d (%d bytes)", format.name, got.Code, got.Body.Len())
			}
			if !strings.HasPrefix(got.Header().Get("Content-Type"), format.contentType) ||
				got.Header().Get("Content-Disposition") != `attachment; filename="query_result.`+format.name+`"` ||
				got.Header().Get("Content-Length") != strconv.Itoa(got.Body.Len()) {
				t.Fatalf("attachment metadata: %v", got.Header())
			}
			if format.name == "jsonl" && bytes.Count(got.Body.Bytes(), []byte("\n")) != 1 {
				t.Fatalf("expected maxRows=1, got %q", got.Body.String())
			}
		})
	}

	filtered := post(map[string]any{
		"table": "sample", "format": "tsv", "sortColumn": "id", "sortDir": "DESC",
		"filters": []db.FilterCondition{{Column: "id", Operator: ">", Value: "1"}},
	})
	if filtered.Code != http.StatusOK || filtered.Body.String() != "id\tname\n2\t乙\n" {
		t.Fatalf("filtered table export: %d %s", filtered.Code, filtered.Body)
	}
	failure := post(map[string]any{
		"sql": "SELECT 1 AS id, 'good' AS name UNION ALL SELECT 2, 'too long'", "format": "txt", "widths": "2,4",
	})
	if failure.Code != http.StatusBadRequest || failure.Header().Get("Content-Disposition") != "" {
		t.Fatalf("failed export sent an attachment: %d %v", failure.Code, failure.Header())
	}
}
