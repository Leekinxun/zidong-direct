package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web-db-client/internal/api"
	"web-db-client/internal/config"
	"web-db-client/internal/crypto"
	"web-db-client/internal/db"
	"web-db-client/internal/store"
)

func TestSQLiteEndToEndWorkflow(t *testing.T) {
	tmp := t.TempDir()
	sqliteDir := filepath.Join(tmp, "sqlite")
	metaDir := filepath.Join(tmp, "meta")

	// Verify path traversal rejection
	if _, err := store.ResolveSQLitePath(sqliteDir, false, "../../etc/passwd"); err == nil {
		t.Fatal("expected traversal rejection")
	}

	cfg := config.Config{
		DataDir:        tmp,
		MetaDir:        metaDir,
		SQLiteDir:      sqliteDir,
		AuthEnabled:    true,
		AdminUser:      "admin",
		AdminPassword:  "secret-pass",
		MaxQueryRows:   500,
		MaxUploadBytes: 10 << 20,
		QueryTimeout:   15 * time.Second,
	}

	cipher, err := crypto.NewCipher("", filepath.Join(metaDir, ".master.key"))
	if err != nil {
		t.Fatal(err)
	}
	connStore, err := store.NewConnectionStore(metaDir, cipher)
	if err != nil {
		t.Fatal(err)
	}
	pools := db.NewPoolManager(func(p string) (string, error) {
		return store.ResolveSQLitePath(cfg.SQLiteDir, false, p)
	})
	defer pools.CloseAll()

	srv := api.NewServer(cfg, connStore, pools, cipher, "", nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Unauthenticated request should return 401
	resp, err := http.Get(ts.URL + "/api/connections")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Login
	loginResp, err := http.Post(ts.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"secret-pass"}`))
	if err != nil {
		t.Fatal(err)
	}
	cookies := loginResp.Cookies()
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK || len(cookies) == 0 {
		t.Fatalf("login failed: status=%d", loginResp.StatusCode)
	}

	client := &http.Client{}
	doJSON := func(method, path string, body any) *http.Response {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		r, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// Create SQLite file
	r := doJSON("POST", "/api/sqlite/create", map[string]any{"name": "demo"})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("create sqlite failed: %d", r.StatusCode)
	}
	r.Body.Close()

	// Create connection
	r = doJSON("POST", "/api/connections", map[string]any{
		"name":       "Local Demo SQLite",
		"engine":     "sqlite",
		"sqlitePath": "demo.db",
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("create conn failed: %d", r.StatusCode)
	}
	var createOut struct {
		Connection db.ConnectionView `json:"connection"`
	}
	_ = json.NewDecoder(r.Body).Decode(&createOut)
	r.Body.Close()
	connID := createOut.Connection.ID
	r = doJSON("GET", "/api/connections/"+connID+"/databases", nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("list databases failed: %d", r.StatusCode)
	}
	r.Body.Close()

	r = doJSON("HEAD", "/api/connections/"+connID+"/databases", nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("HEAD on GET route failed: %d", r.StatusCode)
	}
	r.Body.Close()

	r = doJSON("POST", "/api/connections/"+connID+"/databases", nil)
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected fallback status for unsupported method: %d", r.StatusCode)
	}
	r.Body.Close()

	// Preview & Execute CREATE TABLE DDL
	r = doJSON("POST", "/api/table/ddl/preview", map[string]any{
		"mode":         "create",
		"connectionId": connID,
		"database":     "main",
		"schema":       "main",
		"target": map[string]any{
			"table": "notes",
			"columns": []map[string]any{
				{"name": "id", "type": "INTEGER", "primaryKey": true, "autoIncrement": true, "nullable": false},
				{"name": "title", "type": "TEXT", "nullable": false},
			},
		},
	})
	var previewOut db.DDLPreviewResponse
	_ = json.NewDecoder(r.Body).Decode(&previewOut)
	r.Body.Close()

	r = doJSON("POST", "/api/table/ddl/execute", map[string]any{
		"connectionId": connID,
		"database":     "main",
		"statements":   previewOut.Statements,
	})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("execute ddl failed: %d", r.StatusCode)
	}
	r.Body.Close()

	// Insert & Update via row mutations
	r = doJSON("POST", "/api/table/mutate", map[string]any{
		"connectionId": connID,
		"database":     "main",
		"schema":       "main",
		"table":        "notes",
		"mutations": []map[string]any{
			{"type": "insert", "values": map[string]any{"id": 1, "title": "Win7 Chrome 109"}},
			{"type": "update", "keys": map[string]any{"id": 1}, "values": map[string]any{"title": "Win7 Ready"}},
		},
	})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("mutate failed: %d", r.StatusCode)
	}
	r.Body.Close()

	// Query table data
	r = doJSON("POST", "/api/table/data", map[string]any{
		"connectionId": connID,
		"database":     "main",
		"schema":       "main",
		"table":        "notes",
		"page":         1,
		"pageSize":     50,
	})
	var dataOut db.QueryResult
	_ = json.NewDecoder(r.Body).Decode(&dataOut)
	r.Body.Close()
	if dataOut.RowCount != 1 || dataOut.Rows[0][1] != "Win7 Ready" {
		t.Fatalf("unexpected rows: %#v", dataOut.Rows)
	}

	// Import CSV
	var mpBuf bytes.Buffer
	mw := multipart.NewWriter(&mpBuf)
	_ = mw.WriteField("connectionId", connID)
	_ = mw.WriteField("database", "main")
	_ = mw.WriteField("schema", "main")
	_ = mw.WriteField("table", "notes")
	_ = mw.WriteField("format", "csv")
	fw, _ := mw.CreateFormFile("file", "notes.csv")
	_, _ = fw.Write([]byte("\xEF\xBB\xBFtitle\nSecond Note\n"))
	_ = mw.Close()

	impReq, _ := http.NewRequest("POST", ts.URL+"/api/import", &mpBuf)
	impReq.Header.Set("Content-Type", mw.FormDataContentType())
	for _, c := range cookies {
		impReq.AddCookie(c)
	}
	impResp, err := client.Do(impReq)
	if err != nil {
		t.Fatal(err)
	}
	defer impResp.Body.Close()
	if impResp.StatusCode != http.StatusOK {
		t.Fatalf("csv import failed: %d", impResp.StatusCode)
	}
	var sum db.ImportSummary
	_ = json.NewDecoder(impResp.Body).Decode(&sum)
	if sum.InsertedRows != 1 {
		t.Fatalf("expected 1 imported row, got %+v", sum)
	}
}
