package api

import (
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	gossh "golang.org/x/crypto/ssh"

	"web-db-client/internal/db"
	"web-db-client/internal/store"
)

func (s *Server) addSSHHostRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ssh/hosts", s.handleListSSHHosts)
	mux.HandleFunc("POST /api/ssh/hosts", s.handleCreateSSHHost)
	mux.HandleFunc("DELETE /api/ssh/hosts/{id}", s.handleDeleteSSHHost)
}

func (s *Server) handleListSSHHosts(w http.ResponseWriter, _ *http.Request) {
	if s.sshHosts == nil {
		writeError(w, http.StatusServiceUnavailable, "ssh_hosts_unavailable", "SSH 主机存储不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]store.SSHHostView{"hosts": s.sshHosts.List()})
}

func (s *Server) handleCreateSSHHost(w http.ResponseWriter, r *http.Request) {
	if s.sshHosts == nil {
		writeError(w, http.StatusServiceUnavailable, "ssh_hosts_unavailable", "SSH 主机存储不可用")
		return
	}
	var req struct {
		Name         string        `json:"name"`
		ConnectionID string        `json:"connectionId"`
		SSH          *db.SSHConfig `json:"ssh"`
	}
	if !decodeJSONBody(w, r, maxTerminalOpenBody, &req) {
		return
	}
	if (req.ConnectionID != "") == (req.SSH != nil) {
		writeError(w, http.StatusBadRequest, "invalid_ssh_host_source", "请填写 SSH 主机设置或选择一个已保存的数据库 SSH 设置")
		return
	}

	cfg := req.SSH
	if cfg == nil {
		cfg = s.savedConnectionSSH(w, req.ConnectionID)
		if cfg == nil {
			return
		}
		if cfg.AuthType == "" {
			cfg.AuthType = "password"
		}
	}
	if err := validateSSHHost(cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_ssh_config", err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if len(name) > 512 {
		writeError(w, http.StatusBadRequest, "invalid_ssh_host_name", "SSH 主机名称过长")
		return
	}
	if name == "" {
		name = cfg.Username + "@" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	}
	view, err := s.sshHosts.Create(name, *cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ssh_host_save_failed", "无法保存 SSH 主机")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]store.SSHHostView{"host": view})
}

func (s *Server) handleDeleteSSHHost(w http.ResponseWriter, r *http.Request) {
	if s.sshHosts == nil {
		writeError(w, http.StatusServiceUnavailable, "ssh_hosts_unavailable", "SSH 主机存储不可用")
		return
	}
	if err := s.sshHosts.Delete(r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "ssh_host_delete_failed", "无法删除 SSH 主机")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) savedConnectionSSH(w http.ResponseWriter, id string) *db.SSHConfig {
	if s.store == nil {
		writeError(w, http.StatusNotFound, "connection_not_found", "找不到所选连接")
		return nil
	}
	conn, err := s.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "connection_not_found", "找不到所选连接")
		return nil
	}
	if !conn.SSH.Enabled {
		writeError(w, http.StatusBadRequest, "ssh_required", "请在所选连接中启用 SSH 并保存设置")
		return nil
	}
	return &conn.SSH
}

func validateSSHHost(cfg *db.SSHConfig) error {
	if err := normalizeDirectTerminalSSH(cfg); err != nil {
		return err
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	cfg.HostKeyFingerprint = strings.TrimSpace(cfg.HostKeyFingerprint)
	if cfg.HostKeyFingerprint == "" {
		if !cfg.InsecureIgnoreHostKey {
			return errors.New("请填写服务器的 SHA256 指纹，或选择跳过主机验证")
		}
	} else {
		encoded, ok := strings.CutPrefix(cfg.HostKeyFingerprint, "SHA256:")
		decoded, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
		if !ok || err != nil || len(decoded) != 32 {
			return errors.New("请输入有效的 SSH 主机 SHA256 指纹")
		}
	}
	if cfg.AuthType == "password" {
		if cfg.Password == "" {
			return errors.New("请输入 SSH 密码")
		}
		return nil
	}

	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return errors.New("请输入 SSH 私钥")
	}
	var err error
	if cfg.Passphrase == "" {
		_, err = gossh.ParsePrivateKey([]byte(cfg.PrivateKey))
	} else {
		_, err = gossh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKey), []byte(cfg.Passphrase))
	}
	if err != nil {
		return errors.New("无法读取 SSH 私钥，请检查私钥格式和口令")
	}
	return nil
}
