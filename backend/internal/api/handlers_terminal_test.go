package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"web-db-client/internal/config"
)

func TestTerminalAndResetRoutesOnWeb(t *testing.T) {
	handler := NewServer(config.Config{AuthEnabled: false}, nil, nil, nil, "").Handler()

	t.Run("terminal sessions validates json body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/terminal/sessions", strings.NewReader("{"))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("got HTTP %d, want %d", recorder.Code, http.StatusBadRequest)
		}
	})

	t.Run("system reset requires confirmation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/system/reset", strings.NewReader(`{"clearConnections":true,"confirmed":false}`))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("got HTTP %d, want %d", recorder.Code, http.StatusBadRequest)
		}
	})
}
