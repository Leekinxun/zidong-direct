package api

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"web-db-client/internal/config"
	"web-db-client/internal/crypto"
	"web-db-client/internal/db"
	"web-db-client/internal/store"
)

func TestSSHHostsRequireAuthenticationWhenAuthEnabled(t *testing.T) {
	const host = "127.0.0.1:8080"
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/ssh/hosts", nil)
	recorder := httptest.NewRecorder()
	NewServer(config.Config{AuthEnabled: true, AdminUser: "admin", AdminPassword: "secret"}, nil, nil, nil, "", nil).Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated SSH host list returned HTTP %d", recorder.Code)
	}
}

func TestSSHHostPersistenceCopyAndTerminal(t *testing.T) {
	const host = "127.0.0.1:8080"
	const password = "fixture-password"
	metaDir := t.TempDir()
	cipher, err := crypto.NewCipher("fixture key", "")
	if err != nil {
		t.Fatal(err)
	}
	connections, err := store.NewConnectionStore(metaDir, cipher)
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := store.NewSSHHostStore(metaDir, cipher)
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		serveDirectTerminalFixture(listener, signer)
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	fingerprint := gossh.FingerprintSHA256(signer.PublicKey())

	connection, err := connections.Create(db.ConnectionInput{
		Name: "Legacy database", Engine: db.EngineMySQL, Host: "unused-database.invalid",
		SSH: db.SSHInput{
			Enabled: true, Host: "127.0.0.1", Port: port,
			Username: "fixture", AuthType: "password", Password: password, HostKeyFingerprint: fingerprint,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	connectionFile := filepath.Join(metaDir, "connections.json")
	original, err := os.ReadFile(connectionFile)
	if err != nil {
		t.Fatal(err)
	}

	handler := NewServer(config.Config{AuthEnabled: false}, connections, nil, cipher, "", hosts).Handler()
	request := func(method, route, body string, status int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, "http://"+host+route, strings.NewReader(body))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != status {
			t.Fatalf("%s %s got HTTP %d, want %d: %s", method, route, recorder.Code, status, recorder.Body.String())
		}
		return recorder.Body.Bytes()
	}
	request(http.MethodPost, "/api/ssh/hosts", `{"ssh":{"host":"127.0.0.1","username":"fixture","authType":"password","insecureIgnoreHostKey":true}}`, http.StatusBadRequest)
	request(http.MethodPost, "/api/ssh/hosts", `{"ssh":{"host":"127.0.0.1","username":"fixture","authType":"password","password":"fixture-password"}}`, http.StatusBadRequest)
	request(http.MethodPost, "/api/ssh/hosts", `{"ssh":{"host":"127.0.0.1","username":"fixture","authType":"privateKey","privateKey":"invalid","insecureIgnoreHostKey":true}}`, http.StatusBadRequest)
	direct := request(http.MethodPost, "/api/ssh/hosts", fmt.Sprintf(
		`{"name":"Direct","ssh":{"host":"127.0.0.1","username":"fixture","authType":"password","password":%q,"hostKeyFingerprint":%q}}`,
		password, fingerprint,
	), http.StatusCreated)
	if bytes.Contains(direct, []byte(password)) {
		t.Fatal("create response contains SSH password")
	}
	raw := request(http.MethodPost, "/api/ssh/hosts", fmt.Sprintf(`{"connectionId":%q}`, connection.ID), http.StatusCreated)
	var copied struct {
		Host store.SSHHostView `json:"host"`
	}
	if err := json.Unmarshal(raw, &copied); err != nil {
		t.Fatal(err)
	}
	expectedName := fmt.Sprintf("fixture@127.0.0.1:%d", port)
	if copied.Host.Name != expectedName || !copied.Host.HasPassword || copied.Host.Port != port || copied.Host.HostKeyFingerprint != fingerprint {
		t.Fatalf("copy returned unexpected host metadata: %+v", copied.Host)
	}
	if bytes.Contains(raw, []byte(password)) {
		t.Fatal("copy response contains SSH password")
	}
	raw = request(http.MethodGet, "/api/ssh/hosts", "", http.StatusOK)
	var list struct {
		Hosts []store.SSHHostView `json:"hosts"`
	}
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Hosts) != 2 {
		t.Fatalf("unexpected SSH host list: count=%d err=%v", len(list.Hosts), err)
	}
	if bytes.Contains(raw, []byte(password)) || bytes.Contains(raw, []byte(connection.Name)) {
		t.Fatal("SSH host list contains credentials or database entries")
	}
	persisted, err := os.ReadFile(filepath.Join(metaDir, "ssh_hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(persisted, []byte(password)) || !bytes.Contains(persisted, []byte("enc:v1:")) {
		t.Fatal("SSH host passwords were not encrypted")
	}

	reloaded, err := store.NewSSHHostStore(metaDir, cipher)
	if err != nil {
		t.Fatal(err)
	}
	// The reloaded standalone host can open a terminal without a database store or pool.
	handler = NewServer(config.Config{AuthEnabled: false}, nil, nil, cipher, "", reloaded).Handler()
	raw = request(http.MethodPost, "/api/terminal/sessions", fmt.Sprintf(`{"sshHostId":%q,"rows":24,"cols":80}`, copied.Host.ID), http.StatusCreated)
	var session struct {
		ID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &session); err != nil || session.ID == "" {
		t.Fatalf("missing SSH session ID: %v", err)
	}
	request(http.MethodDelete, "/api/terminal/sessions/"+session.ID, "", http.StatusNoContent)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("standalone terminal transport did not close")
	}
	request(http.MethodDelete, "/api/ssh/hosts/"+copied.Host.ID, "", http.StatusNoContent)
	request(http.MethodPost, "/api/terminal/sessions", fmt.Sprintf(`{"sshHostId":%q,"rows":24,"cols":80}`, copied.Host.ID), http.StatusNotFound)

	unchanged, err := os.ReadFile(connectionFile)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("standalone host operations rewrote database connections")
	}
}
