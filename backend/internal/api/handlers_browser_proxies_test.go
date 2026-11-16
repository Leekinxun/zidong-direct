package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"web-db-client/internal/config"
)

func TestBrowserProxyDesktopAuthorization(t *testing.T) {
	const host = "127.0.0.1:12345"
	const token = "fixture-token"
	for _, fixture := range []struct {
		name         string
		desktop      bool
		token        string
		origin       string
		body         string
		expectedCode int
	}{
		{"disabled in Web mode", false, token, "", "{}", http.StatusNotFound},
		{"missing desktop token", true, "", "", "{}", http.StatusUnauthorized},
		{"foreign origin", true, token, "https://not-the-workbench.test", "{}", http.StatusForbidden},
		{"a single SSH source required", true, token, "", `{"sshHostId":"one","connectionId":"two"}`, http.StatusBadRequest},
		{"invalid direct source", true, token, "", `{"ssh":{"host":"example.test:22","port":22,"username":"fixture","authType":"password"}}`, http.StatusBadRequest},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			service := NewServer(config.Config{DesktopMode: fixture.desktop}, nil, nil, nil, "", nil)
			handler := DesktopHandler(host, token, service.Handler())
			req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/browser-proxies", strings.NewReader(fixture.body))
			req.Header.Set(DesktopTokenHeader, fixture.token)
			if fixture.origin != "" {
				req.Header.Set("Origin", fixture.origin)
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != fixture.expectedCode {
				t.Fatalf("status %d, expected %d: %s", res.Code, fixture.expectedCode, res.Body.String())
			}
		})
	}
}

func TestBrowserProxyRegistryLimitAndRelease(t *testing.T) {
	var proxies browserProxyRegistry
	for i := byte(0); i < maxBrowserProxies; i++ {
		if !proxies.reserve(string([]byte{i})) {
			t.Fatal("unable to reserve available browser proxy")
		}
	}
	if proxies.reserve("extra") {
		t.Fatal("reserved beyond the browser proxy limit")
	}
	proxies.close(string([]byte{0}))
	if !proxies.reserve("extra") {
		t.Fatal("closing the proxy did not release its slot")
	}
}
