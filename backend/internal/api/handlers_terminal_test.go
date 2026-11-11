package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
