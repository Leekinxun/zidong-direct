package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web-db-client/internal/api"
)

func TestDesktopStartupFailureEvents(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("SQLITE_DIR", filepath.Join(root, "data", "sqlite"))
	t.Setenv("STATIC_DIR", filepath.Join(root, "missing"))

	for _, fixture := range []struct {
		input string
		code  string
	}{
		{"invalid JSON\n", "bootstrap"},
		{fmt.Sprintf("{\"token\":%q}\n", strings.Repeat("ab", 32)), "configuration"},
	} {
		var output bytes.Buffer
		if err := runDesktop(context.Background(), strings.NewReader(fixture.input), &output); err == nil {
			t.Fatal("desktop startup unexpectedly succeeded")
		}
		want := fmt.Sprintf("{\"type\":\"error\",\"code\":%q}\n", fixture.code)
		if output.String() != want {
			t.Fatalf("event = %q, want %q", output.String(), want)
		}
	}
}

func TestDesktopServerControl(t *testing.T) {
	for _, closeInput := range []bool{false, true} {
		t.Run(fmt.Sprintf("eof=%t", closeInput), func(t *testing.T) {
			root := t.TempDir()
			staticDir := filepath.Join(root, "static")
			if err := os.Mkdir(staticDir, 0o700); err != nil {
				t.Fatal(err)
			}
			const page = "<html><title>Desktop fixture</title></html>"
			if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(page), 0o600); err != nil {
				t.Fatal(err)
			}
			dataDir := filepath.Join(root, "data")
			t.Setenv("DATA_DIR", dataDir)
			t.Setenv("SQLITE_DIR", filepath.Join(dataDir, "sqlite"))
			t.Setenv("STATIC_DIR", staticDir)
			t.Setenv("APP_ADMIN_PASSWORD", "web-test-password")
			t.Setenv("ALLOW_ARBITRARY_SQLITE_PATHS", "true")

			input, commands := io.Pipe()
			defer commands.Close()
			output, readiness := io.Pipe()
			defer output.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			done := make(chan error, 1)
			go func() {
				defer input.Close()
				err := runDesktop(ctx, input, readiness)
				_ = readiness.CloseWithError(err)
				done <- err
			}()

			token := strings.Repeat("ab", 32)
			if err := json.NewEncoder(commands).Encode(desktopBootstrap{Token: token, MasterKey: strings.Repeat("cd", 32)}); err != nil {
				t.Fatal(err)
			}
			var ready desktopReady
			if err := json.NewDecoder(output).Decode(&ready); err != nil || ready.Type != "ready" || ready.Port == 0 {
				t.Fatalf("invalid readiness: %+v (%v)", ready, err)
			}
			origin := fmt.Sprintf("http://127.0.0.1:%d", ready.Port)
			client := &http.Client{Timeout: 3 * time.Second}
			check := func(method, path, suppliedToken, originHeader, host string, status int, contains string) {
				t.Helper()
				req, err := http.NewRequestWithContext(ctx, method, origin+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set(api.DesktopTokenHeader, suppliedToken)
				if originHeader != "" {
					req.Header.Set("Origin", originHeader)
				}
				if host != "" {
					req.Host = host
				}
				res, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				body, err := io.ReadAll(res.Body)
				if err != nil || res.StatusCode != status || !strings.Contains(string(body), contains) {
					t.Fatalf("%s %s = %d %s (%v), want %d containing %q", method, path, res.StatusCode, body, err, status, contains)
				}
				if res.Header.Get("Access-Control-Allow-Origin") != "" || res.Header.Get("Cache-Control") != "no-store" {
					t.Fatalf("unexpected desktop response headers: %v", res.Header)
				}
			}

			check("GET", "/", "", "", "", 401, "unauthorized")
			check("GET", "/api/health", "wrong", "", "", 401, "unauthorized")
			check("GET", "/api/health", token, "https://example.invalid", "", 403, "invalid_origin")
			check("GET", "/api/health", token, "", "localhost", 403, "invalid_origin")
			check("OPTIONS", "/api/connections", token, origin, "", 403, "invalid_origin")
			check("GET", "/", token, "", "", 200, page)
			check("GET", "/api/health", token, origin, "", 200, `"status":"ok"`)
			check("GET", "/api/auth/status", token, "", "", 200, `"authEnabled":false`)
			check("GET", "/api/capabilities", token, "", "", 200, `"allowArbitrarySqlitePaths":false`)
			if _, err := os.Stat(filepath.Join(dataDir, "meta", ".master.key")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected plaintext key file: %v", err)
			}

			if closeInput {
				_ = commands.Close()
			} else if _, err := fmt.Fprintln(commands, "shutdown"); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("desktop server did not shut down")
			}
		})
	}
}
