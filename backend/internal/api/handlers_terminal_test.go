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
	"net/url"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/net/websocket"

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
			handler := NewServer(config.Config{DesktopMode: tc.desktop}, nil, nil, nil, "", nil).Handler()
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
	handler := DesktopHandler(host, token, NewServer(config.Config{DesktopMode: true}, nil, nil, nil, "", nil).Handler())
	for _, tc := range []struct {
		name, payload, code string
	}{
		{"missing source", `{"rows":24,"cols":80}`, "invalid_terminal_source"},
		{"ambiguous source", `{"connectionId":"saved","ssh":{},"rows":24,"cols":80}`, "invalid_terminal_source"},
		{"ambiguous host source", `{"sshHostId":"host","connectionId":"saved","rows":24,"cols":80}`, "invalid_terminal_source"},
		{"embedded port", `{"ssh":{"host":"127.0.0.1:22","port":22,"username":"fixture","authType":"password"}}`, "invalid_ssh_config"},
		{"out of range port", `{"ssh":{"host":"127.0.0.1","port":65536,"username":"fixture","authType":"password"}}`, "invalid_ssh_config"},
		{"unsupported auth", `{"ssh":{"host":"127.0.0.1","username":"fixture","authType":"other"}}`, "invalid_ssh_config"},
		{"host key required", `{"ssh":{"host":"127.0.0.1","username":"fixture","authType":"password","password":"fixture"},"rows":24,"cols":80}`, "terminal_connect_failed"},
		{"unsupported encoding", `{"ssh":{"host":"127.0.0.1","username":"fixture","authType":"password","password":"fixture","insecureIgnoreHostKey":true},"rows":24,"cols":80,"encoding":"other"}`, "terminal_connect_failed"},
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

func TestTerminalSessionLimit(t *testing.T) {
	const host, token = "127.0.0.1:8080", "fixture-token"
	// No dimensions means requests admitted by the limiter fail validation
	// before making any SSH connection.
	const payload = `{"ssh":{"host":"127.0.0.1","username":"fixture","authType":"password"}}`

	for _, tc := range []struct {
		name, env string
		limit     int
	}{
		{name: "default", limit: 16},
		{name: "configured", env: "2", limit: 2},
		{name: "invalid setting", env: "-1", limit: 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAX_TERMINAL_SESSIONS", tc.env)
			cfg := config.Load()
			cfg.DesktopMode = true
			cfg.AuthEnabled = false
			if cfg.MaxTerminalSessions != tc.limit {
				t.Fatalf("session limit = %d, want %d", cfg.MaxTerminalSessions, tc.limit)
			}
			service := NewServer(cfg, nil, nil, nil, "", nil)
			handler := DesktopHandler(host, token, service.Handler())
			request := func(method, route, body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, "http://"+host+route, strings.NewReader(body))
				req.Header.Set(DesktopTokenHeader, token)
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, req)
				return recorder
			}

			for i := 0; i < tc.limit; i++ {
				if !service.terminals.reserve(fmt.Sprint(i), tc.limit) {
					t.Fatalf("could not reserve slot %d of %d", i+1, tc.limit)
				}
			}
			open := request(http.MethodPost, "/api/terminal/sessions", payload)
			if open.Code != http.StatusTooManyRequests ||
				!strings.Contains(open.Body.String(), fmt.Sprintf("（%d 个）", tc.limit)) {
				t.Fatalf("session limit response = HTTP %d %s", open.Code, open.Body.String())
			}

			closeResponse := request(http.MethodDelete, "/api/terminal/sessions/0", "")
			if closeResponse.Code != http.StatusNoContent {
				t.Fatalf("closing session = HTTP %d", closeResponse.Code)
			}
			open = request(http.MethodPost, "/api/terminal/sessions", payload)
			if open.Code != http.StatusBadRequest || !strings.Contains(open.Body.String(), "terminal_connect_failed") {
				t.Fatalf("opening after release = HTTP %d %s", open.Code, open.Body.String())
			}
			if !service.terminals.reserve("reused", tc.limit) {
				t.Fatal("failed startup did not release its slot")
			}
		})
	}
}

func TestDirectTerminalOpensShellWithoutDatabase(t *testing.T) {
	testDirectTerminalOpensShell(t, false, "")
}

func TestDirectTerminalWebSocketOutput(t *testing.T) {
	testDirectTerminalOpensShell(t, true, "")
}

func TestDirectTerminalGBKOverWebSocket(t *testing.T) {
	testDirectTerminalOpensShell(t, true, "gbk")
}

func testDirectTerminalOpensShell(t *testing.T, useWebSocket bool, charset string) {
	t.Helper()
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
	var expectedInput string
	outputChunks := [][]byte{[]byte("second fixture output")}
	if useWebSocket {
		expectedInput = "echo"
	}
	if charset == "gbk" {
		expectedInput = "\xd6\xd0\xce\xc4"
		outputChunks = [][]byte{{0xd6}, {0xd0, 0xce}, {0xc4}}
	}
	go func() {
		defer close(closed)
		serveTerminalFixture(listener, signer, expectedInput, outputChunks)
	}()

	const token = "fixture-token"
	server := httptest.NewUnstartedServer(nil)
	service := NewServer(config.Config{DesktopMode: true}, nil, nil, nil, "", nil)
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

	payload := fmt.Sprintf(`{"ssh":{"host":"127.0.0.1","port":%d,"username":"fixture","authType":"password","password":"fixture-password","hostKeyFingerprint":%q},"rows":24,"cols":80,"encoding":%q}`,
		listener.Addr().(*net.TCPAddr).Port, gossh.FingerprintSHA256(signer.PublicKey()), charset)
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

	if useWebSocket {
		address := "ws" + strings.TrimPrefix(server.URL, "http") + sessionURL + "/stream"
		dial := func(origin, authToken string) (*websocket.Conn, error) {
			cfg, err := websocket.NewConfig(address, server.URL)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Origin, err = url.Parse(origin)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Header.Set(DesktopTokenHeader, authToken)
			cfg.Dialer = &net.Dialer{Timeout: time.Second}
			return websocket.DialConfig(cfg)
		}
		for _, invalid := range []struct{ origin, token string }{
			{server.URL, ""},
			{"http://other.invalid", token},
			{"", token},
		} {
			if socket, err := dial(invalid.origin, invalid.token); err == nil {
				_ = socket.Close()
				t.Fatalf("accepted invalid terminal socket origin %q, token present %v", invalid.origin, invalid.token != "")
			}
		}
		socket, err := dial(server.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		defer socket.Close()
		_ = socket.SetDeadline(time.Now().Add(2 * time.Second))
		if duplicate, err := dial(server.URL, token); err == nil {
			_ = duplicate.Close()
			t.Fatal("duplicate socket attached")
		}
		var prompt []byte
		if err := websocket.Message.Receive(socket, &prompt); err != nil || string(prompt) != "fixture shell $ " {
			t.Fatalf("standalone terminal socket output %q, error %v", prompt, err)
		}
		if err := websocket.Message.Send(socket, []byte{1}); err != nil {
			t.Fatal(err)
		}
		input, expectedOutput := "echo", "second fixture output"
		if charset == "gbk" {
			invalid := request(http.MethodPost, sessionURL+"/input", "echo 😀")
			details, err := io.ReadAll(invalid.Body)
			_ = invalid.Body.Close()
			if err != nil || invalid.StatusCode != http.StatusBadRequest || !strings.Contains(string(details), "terminal_input_encoding") {
				t.Fatalf("unrepresentable terminal input: HTTP %d %s (%v)", invalid.StatusCode, details, err)
			}
			input, expectedOutput = "中文", "中文"
		}
		response := request(http.MethodPost, sessionURL+"/input", input)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("HTTP input while socket is active: %d", response.StatusCode)
		}
		var output strings.Builder
		for output.Len() < len(expectedOutput) {
			if err := websocket.Message.Receive(socket, &prompt); err != nil {
				t.Fatalf("next terminal socket output %q, error %v", prompt, err)
			}
			output.Write(prompt)
			if output.Len() < len(expectedOutput) {
				if err := websocket.Message.Send(socket, []byte{1}); err != nil {
					t.Fatal(err)
				}
			}
		}
		if output.String() != expectedOutput {
			t.Fatalf("terminal socket output %q, want %q", output.String(), expectedOutput)
		}
		// Disconnect the output socket without a DELETE request. The SSH
		// transport must close even while the server awaits an ACK.
		if err := socket.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
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
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("SSH connection did not close")
	}
}

func serveDirectTerminalFixture(listener net.Listener, signer gossh.Signer) {
	serveTerminalFixture(listener, signer, "", nil)
}

func serveTerminalFixture(listener net.Listener, signer gossh.Signer, expectedInput string, outputChunks [][]byte) {
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
				if expectedInput != "" {
					input := make([]byte, len(expectedInput))
					if _, err := io.ReadFull(channel, input); err == nil && string(input) == expectedInput {
						for _, chunk := range outputChunks {
							_, _ = channel.Write(chunk)
						}
					}
				}
			default:
				_ = request.Reply(false, nil)
			}
		}
		_ = channel.Close()
	}
}
