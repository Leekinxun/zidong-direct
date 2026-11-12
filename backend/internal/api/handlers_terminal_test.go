package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"web-db-client/internal/config"
)

func TestTerminalRoutesRequireDesktopAndLocalAuthorization(t *testing.T) {
	const host = "127.0.0.1:8080"
	const token = "fixture-token"
	for _, tc := range []struct {
		name, token string
		desktop     bool
		status      int
	}{
		{name: "web", status: http.StatusNotFound},
		{name: "desktop without token", desktop: true, status: http.StatusUnauthorized},
		{name: "authorized desktop", desktop: true, token: token, status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewServer(config.Config{DesktopMode: tc.desktop}, nil, nil, nil, "").Handler()
			if tc.desktop {
				handler = DesktopHandler(host, token, handler)
			}
			req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/terminal/sessions", strings.NewReader("{"))
			if tc.token != "" {
				req.Header.Set(DesktopTokenHeader, tc.token)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("got HTTP %d, want %d", recorder.Code, tc.status)
			}
		})
	}
}

func TestTerminalRejectsInvalidDirectRequests(t *testing.T) {
	const host, token = "127.0.0.1:8080", "fixture-token"
	handler := DesktopHandler(host, token, NewServer(config.Config{DesktopMode: true}, nil, nil, nil, "").Handler())
	for _, tc := range []struct {
		name, payload, code string
	}{
		{"missing source", `{"rows":24,"cols":80}`, "invalid_terminal_source"},
		{"ambiguous source", `{"connectionId":"saved","ssh":{},"rows":24,"cols":80}`, "invalid_terminal_source"},
		{"embedded port", `{"ssh":{"host":"127.0.0.1:22","port":22,"username":"fixture","authType":"password"}}`, "invalid_ssh_config"},
		{"out of range port", `{"ssh":{"host":"127.0.0.1","port":65536,"username":"fixture","authType":"password"}}`, "invalid_ssh_config"},
		{"unsupported auth", `{"ssh":{"host":"127.0.0.1","username":"fixture","authType":"other"}}`, "invalid_ssh_config"},
		{"host key required", `{"ssh":{"host":"127.0.0.1","username":"fixture","authType":"password","password":"fixture"},"rows":24,"cols":80}`, "terminal_connect_failed"},
		{"oversized payload", `{"ssh":{"privateKey":"` + strings.Repeat("x", maxTerminalOpenBody) + `"}}`, "bad_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/terminal/sessions", strings.NewReader(tc.payload))
			req.Header.Set(DesktopTokenHeader, token)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			var result struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusBadRequest || result.Error != tc.code {
				t.Fatalf("got HTTP %d, error %q, want HTTP 400, error %q", recorder.Code, result.Error, tc.code)
			}
		})
	}
}

func TestDirectTerminalOpensShellWithoutDatabase(t *testing.T) {
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

	const token = "fixture-token"
	server := httptest.NewUnstartedServer(nil)
	service := NewServer(config.Config{DesktopMode: true}, nil, nil, nil, "")
	server.Config.Handler = DesktopHandler(server.Listener.Addr().String(), token, service.Handler())
	server.Start()
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	request := func(method, route, body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+route, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(DesktopTokenHeader, token)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	payload := fmt.Sprintf(`{"ssh":{"host":"127.0.0.1","port":%d,"username":"fixture","authType":"password","password":"fixture-password","hostKeyFingerprint":%q},"rows":24,"cols":80}`,
		listener.Addr().(*net.TCPAddr).Port, gossh.FingerprintSHA256(signer.PublicKey()))
	response := request(http.MethodPost, "/api/terminal/sessions", payload)
	var session struct {
		ID string `json:"sessionId"`
	}
	err = json.NewDecoder(response.Body).Decode(&session)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusCreated || session.ID == "" {
		t.Fatalf("opening standalone terminal: HTTP %d, decode error %v", response.StatusCode, err)
	}
	sessionURL := "/api/terminal/sessions/" + session.ID
	defer service.terminals.close(session.ID, false)

	output := request(http.MethodGet, sessionURL+"/output", "")
	defer output.Body.Close()
	prompt := make([]byte, len("fixture shell $ "))
	if _, err := io.ReadFull(output.Body, prompt); err != nil || string(prompt) != "fixture shell $ " {
		t.Fatalf("standalone terminal output %q, error %v", prompt, err)
	}
	response = request(http.MethodDelete, sessionURL, "")
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("closing terminal: HTTP %d", response.StatusCode)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("SSH connection did not close")
	}
}

func serveDirectTerminalFixture(listener net.Listener, signer gossh.Signer) {
	raw, err := listener.Accept()
	if err != nil {
		return
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
	server := &gossh.ServerConfig{
		PasswordCallback: func(meta gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			if meta.User() != "fixture" || string(password) != "fixture-password" {
				return nil, errors.New("invalid fixture credentials")
			}
			return nil, nil
		},
	}
	server.AddHostKey(signer)
	conn, channels, requests, err := gossh.NewServerConn(raw, server)
	if err != nil {
		return
	}
	defer conn.Close()
	go gossh.DiscardRequests(requests)
	for next := range channels {
		if next.ChannelType() != "session" {
			_ = next.Reject(gossh.UnknownChannelType, "fixture accepts SSH sessions only")
			continue
		}
		channel, requests, err := next.Accept()
		if err != nil {
			return
		}
		for request := range requests {
			switch request.Type {
			case "pty-req":
				_ = request.Reply(true, nil)
			case "shell":
				_ = request.Reply(true, nil)
				_, _ = io.WriteString(channel, "fixture shell $ ")
			default:
				_ = request.Reply(false, nil)
			}
		}
		_ = channel.Close()
	}
}
