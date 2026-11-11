package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
)

func TestImportURLRejectsLocalEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("local endpoint received an import request")
	}))
	defer server.Close()
	for _, raw := range []string{server.URL, "http://127.0.0.1/file.csv", "http://[::1]/file.csv"} {
		if file, _, err := openImportURL(context.Background(), raw, 1024); err == nil {
			file.Close()
			t.Errorf("accepted local URL %s", raw)
		}
	}
	for _, address := range []string{"169.254.169.254", "100.100.100.200", "192.168.1.1", "::ffff:127.0.0.1", "64:ff9b::a9fe:a9fe"} {
		if publicImportAddress(netip.MustParseAddr(address)) {
			t.Errorf("accepted non-public address %s", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicImportAddress(netip.MustParseAddr(address)) {
			t.Errorf("rejected public address %s", address)
		}
	}
}

func TestImportURLValidation(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/file.csv", "https://user:pass@example.com/file.csv", "https://example.com:80/file.csv"} {
		target, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if validateImportURL(target) == nil {
			t.Errorf("accepted invalid URL %s", raw)
		}
	}
	target, err := url.Parse("https://example.com:443/data.csv?download=1")
	if err != nil || validateImportURL(target) != nil {
		t.Fatalf("rejected valid source: %v", err)
	}
}
