package api

import (
	"crypto/subtle"
	"net/http"
)

const DesktopTokenHeader = "X-Zidong-Desktop-Token"

func DesktopHandler(host, token string, next http.Handler) http.Handler {
	origin := "http://" + host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		// Keep same-origin form requests from sending an opaque Origin.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; object-src 'none'; frame-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")

		origins := r.Header.Values("Origin")
		if r.Host != host || len(origins) > 1 || (len(origins) == 1 && origins[0] != origin) {
			writeError(w, http.StatusForbidden, "invalid_origin", "桌面请求来源无效")
			return
		}
		tokens := r.Header.Values(DesktopTokenHeader)
		if len(tokens) != 1 || subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "桌面会话无效")
			return
		}
		if r.Method == http.MethodOptions {
			writeError(w, http.StatusForbidden, "invalid_origin", "桌面接口仅支持同源请求")
			return
		}
		next.ServeHTTP(w, r)
	})
}
