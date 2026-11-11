package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"web-db-client/internal/config"
	"web-db-client/internal/crypto"
)

const sessionCookieName = "webdb_session"

type AuthHandler struct {
	cfg    config.Config
	cipher *crypto.Cipher
}

func NewAuthHandler(cfg config.Config, cipher *crypto.Cipher) *AuthHandler {
	return &AuthHandler{cfg: cfg, cipher: cipher}
}

func (a *AuthHandler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.cfg.AuthEnabled {
			next.ServeHTTP(w, r)
			return
		}
		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") ||
			path == "/api/health" ||
			path == "/api/auth/status" ||
			path == "/api/auth/login" ||
			path == "/api/auth/logout" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录工作台")
			return
		}
		if _, ok := a.cipher.VerifySession(cookie.Value); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "登录会话已过期，请重新登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *AuthHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	authenticated := !a.cfg.AuthEnabled
	username := ""
	if a.cfg.AuthEnabled {
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			if user, ok := a.cipher.VerifySession(cookie.Value); ok {
				authenticated = true
				username = user
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authEnabled":   a.cfg.AuthEnabled,
		"authenticated": authenticated,
		"username":      username,
	})
}

func (a *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.AuthEnabled {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": "guest"})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求参数格式无效")
		return
	}
	userMatch := subtle.ConstantTimeCompare([]byte(req.Username), []byte(a.cfg.AdminUser)) == 1
	passMatch := subtle.ConstantTimeCompare([]byte(req.Password), []byte(a.cfg.AdminPassword)) == 1
	if !userMatch || !passMatch {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	}
	token := a.cipher.SignSession(req.Username, 24*time.Hour)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"username": req.Username,
	})
}

func (a *AuthHandler) HandleLogout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
