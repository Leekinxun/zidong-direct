package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"web-db-client/internal/db"
	"web-db-client/internal/ssh"
	"web-db-client/internal/store"
)

const maxBrowserProxies = 8

type sshSource struct {
	ConnectionID string        `json:"connectionId"`
	SSHHostID    string        `json:"sshHostId"`
	SSH          *db.SSHConfig `json:"ssh"`
}

func (s *Server) resolveSSHSource(w http.ResponseWriter, source sshSource, invalidSourceCode string) *db.SSHConfig {
	sourceCount := 0
	for _, present := range []bool{source.ConnectionID != "", source.SSHHostID != "", source.SSH != nil} {
		if present {
			sourceCount++
		}
	}
	if sourceCount != 1 {
		writeError(w, http.StatusBadRequest, invalidSourceCode, "请选择一个已保存的 SSH 来源或填写 SSH 服务器信息")
		return nil
	}
	switch {
	case source.SSH != nil:
		if err := normalizeDirectTerminalSSH(source.SSH); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_ssh_config", err.Error())
			return nil
		}
		return source.SSH
	case source.SSHHostID != "":
		if s.sshHosts == nil {
			writeError(w, http.StatusServiceUnavailable, "ssh_hosts_unavailable", "SSH 主机存储不可用")
			return nil
		}
		host, err := s.sshHosts.Get(source.SSHHostID)
		if errors.Is(err, store.ErrSSHHostNotFound) {
			writeError(w, http.StatusNotFound, "ssh_host_not_found", "找不到所选 SSH 主机")
			return nil
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ssh_host_read_failed", "无法读取已保存的 SSH 主机")
			return nil
		}
		return &host
	default:
		return s.savedConnectionSSH(w, source.ConnectionID)
	}
}

type browserProxyRegistry struct {
	mu      sync.Mutex
	tunnels map[string]*ssh.SOCKSTunnel
}

func (p *browserProxyRegistry) reserve(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tunnels) >= maxBrowserProxies {
		return false
	}
	if p.tunnels == nil {
		p.tunnels = make(map[string]*ssh.SOCKSTunnel)
	}
	p.tunnels[id] = nil
	return true
}

func (p *browserProxyRegistry) start(id string, tunnel *ssh.SOCKSTunnel) {
	p.mu.Lock()
	p.tunnels[id] = tunnel
	p.mu.Unlock()
	go func() {
		<-tunnel.Done()
		p.close(id)
	}()
}

func (p *browserProxyRegistry) close(id string) {
	p.mu.Lock()
	tunnel := p.tunnels[id]
	delete(p.tunnels, id)
	p.mu.Unlock()
	if tunnel != nil {
		tunnel.Close()
	}
}

func (s *Server) addBrowserProxyRoutes(routes *apiRoutes) {
	if !s.cfg.DesktopMode {
		return
	}
	routes.add("POST /api/browser-proxies", s.handleOpenBrowserProxy)
	routes.add("DELETE /api/browser-proxies/{id}", s.handleCloseBrowserProxy)
}

func (s *Server) handleOpenBrowserProxy(w http.ResponseWriter, r *http.Request) {
	var source sshSource
	if !decodeJSONBody(w, r, maxTerminalOpenBody, &source) {
		return
	}
	cfg := s.resolveSSHSource(w, source, "invalid_browser_proxy_source")
	if cfg == nil {
		return
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		writeError(w, http.StatusInternalServerError, "browser_proxy_failed", "无法分配浏览器代理")
		return
	}
	id := hex.EncodeToString(token[:])
	if !s.browserProxies.reserve(id) {
		writeError(w, http.StatusTooManyRequests, "browser_proxy_limit", "浏览器代理已达 8 个，请关闭部分代理 Chrome 窗口")
		return
	}
	started := false
	defer func() {
		if !started {
			s.browserProxies.close(id)
		}
	}()
	tunnel, err := ssh.OpenSOCKSTunnel(r.Context(), ssh.Spec{
		Host: cfg.Host, Port: cfg.Port, Username: cfg.Username, AuthType: cfg.AuthType,
		Password: cfg.Password, PrivateKey: cfg.PrivateKey, Passphrase: cfg.Passphrase,
		HostKeyFingerprint: cfg.HostKeyFingerprint, InsecureIgnoreHostKey: cfg.InsecureIgnoreHostKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "browser_proxy_connect_failed", "无法启用 Chrome 代理："+err.Error())
		return
	}
	if r.Context().Err() != nil {
		tunnel.Close()
		return
	}
	s.browserProxies.start(id, tunnel)
	started = true
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(struct {
		ProxyID string `json:"proxyId"`
		Port    int    `json:"port"`
	}{id, tunnel.Port()}); err != nil {
		s.browserProxies.close(id)
	}
}

func (s *Server) handleCloseBrowserProxy(w http.ResponseWriter, r *http.Request) {
	s.browserProxies.close(routeConnectionID(r))
	w.WriteHeader(http.StatusNoContent)
}
