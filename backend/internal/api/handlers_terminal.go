package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"golang.org/x/net/websocket"

	"web-db-client/internal/config"
	"web-db-client/internal/db"
	"web-db-client/internal/ssh"
)

const (
	terminalAttachTTL   = 15 * time.Second
	maxTerminalOpenBody = 64 * 1024
	terminalOutputChunk = 16 * 1024
	terminalWriteLimit  = 5 * time.Second
	terminalAckTimeout  = 30 * time.Second
)

type terminalSession struct {
	terminal *ssh.Terminal
	timer    *time.Timer
	attached bool
}

type terminalSessions struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
}

func (sessions *terminalSessions) reserve(id string, limit int) bool {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.sessions) >= limit {
		return false
	}
	if sessions.sessions == nil {
		sessions.sessions = make(map[string]*terminalSession)
	}
	if _, exists := sessions.sessions[id]; exists {
		return false
	}
	sessions.sessions[id] = &terminalSession{}
	return true
}

func (sessions *terminalSessions) start(id string, terminal *ssh.Terminal) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	entry := sessions.sessions[id]
	entry.terminal = terminal
	entry.timer = time.AfterFunc(terminalAttachTTL, func() { sessions.close(id, true) })
}

func (sessions *terminalSessions) attach(id string) (terminal *ssh.Terminal, alreadyAttached bool) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	entry := sessions.sessions[id]
	if entry == nil || entry.terminal == nil {
		return nil, false
	}
	if entry.attached {
		return nil, true
	}
	entry.attached = true
	entry.timer.Stop()
	return entry.terminal, false
}

func (sessions *terminalSessions) active(id string) *ssh.Terminal {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if entry := sessions.sessions[id]; entry != nil && entry.attached {
		return entry.terminal
	}
	return nil
}

func (sessions *terminalSessions) close(id string, unattachedOnly bool) {
	sessions.mu.Lock()
	entry := sessions.sessions[id]
	if entry == nil || (unattachedOnly && entry.attached) {
		sessions.mu.Unlock()
		return
	}
	delete(sessions.sessions, id)
	sessions.mu.Unlock()
	if entry.timer != nil {
		entry.timer.Stop()
	}
	if entry.terminal != nil {
		entry.terminal.Close()
	}
}

func (s *Server) addTerminalRoutes(routes *apiRoutes) {
	if !s.cfg.DesktopMode {
		return
	}
	routes.add("POST /api/terminal/sessions", s.handleOpenTerminal)
	routes.add("GET /api/terminal/sessions/{id}/output", s.handleTerminalOutput)
	routes.add("GET /api/terminal/sessions/{id}/stream", s.handleTerminalStream)
	routes.add("POST /api/terminal/sessions/{id}/input", s.handleTerminalInput)
	routes.add("POST /api/terminal/sessions/{id}/resize", s.handleTerminalResize)
	routes.add("DELETE /api/terminal/sessions/{id}", s.handleCloseTerminal)
}

type terminalDimensions struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

func normalizeDirectTerminalSSH(cfg *db.SSHConfig) error {
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)
	if cfg.Host == "" || len(cfg.Host) > 255 || strings.ContainsAny(cfg.Host, "/\\") ||
		strings.IndexFunc(cfg.Host, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("请输入有效的 SSH 主机地址")
	}
	if strings.HasPrefix(cfg.Host, "[") && strings.HasSuffix(cfg.Host, "]") {
		cfg.Host = cfg.Host[1 : len(cfg.Host)-1]
		if !strings.Contains(cfg.Host, ":") {
			return errors.New("请输入有效的 SSH 主机地址")
		}
	}
	if strings.Contains(cfg.Host, ":") {
		if _, err := netip.ParseAddr(cfg.Host); err != nil {
			return errors.New("请将 SSH 主机和端口分开填写")
		}
	} else if strings.ContainsAny(cfg.Host, "[]") {
		return errors.New("请输入有效的 SSH 主机地址")
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return errors.New("SSH 端口必须介于 1 和 65535 之间")
	}
	if cfg.Username == "" || len(cfg.Username) > 255 {
		return errors.New("请输入有效的 SSH 用户名")
	}
	if cfg.AuthType != "password" && cfg.AuthType != "privateKey" {
		return errors.New("请选择 SSH 密码或私钥认证")
	}
	return nil
}

func (s *Server) handleOpenTerminal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		sshSource
		Encoding string `json:"encoding"`
		terminalDimensions
	}
	if !decodeJSONBody(w, r, maxTerminalOpenBody, &req) {
		return
	}
	cfg := s.resolveSSHSource(w, req.sshSource, "invalid_terminal_source")
	if cfg == nil {
		return
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		writeError(w, http.StatusInternalServerError, "terminal_failed", "无法分配终端会话")
		return
	}
	id := hex.EncodeToString(token[:])
	limit := s.cfg.MaxTerminalSessions
	if limit <= 0 {
		limit = config.DefaultMaxTerminalSessions
	}
	if !s.terminals.reserve(id, limit) {
		writeError(w, http.StatusTooManyRequests, "terminal_limit", fmt.Sprintf("终端会话已达上限（%d 个），请先断开其他终端", limit))
		return
	}
	success := false
	defer func() {
		if !success {
			s.terminals.close(id, false)
		}
	}()
	terminal, err := ssh.OpenTerminalWithEncoding(r.Context(), ssh.Spec{
		Host:                  cfg.Host,
		Port:                  cfg.Port,
		Username:              cfg.Username,
		AuthType:              cfg.AuthType,
		Password:              cfg.Password,
		PrivateKey:            cfg.PrivateKey,
		Passphrase:            cfg.Passphrase,
		HostKeyFingerprint:    cfg.HostKeyFingerprint,
		InsecureIgnoreHostKey: cfg.InsecureIgnoreHostKey,
	}, req.Rows, req.Cols, req.Encoding)
	if err != nil {
		writeError(w, http.StatusBadRequest, "terminal_connect_failed", "无法打开 SSH 终端："+err.Error())
		return
	}
	if r.Context().Err() != nil {
		terminal.Close()
		return
	}
	s.terminals.start(id, terminal)
	success = true
	writeJSON(w, http.StatusCreated, map[string]string{"sessionId": id})
}

func (s *Server) handleTerminalOutput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "终端输出需要 GET 请求")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "terminal_stream_failed", "无法打开终端输出流")
		return
	}
	id := routeConnectionID(r)
	terminal, alreadyAttached := s.terminals.attach(id)
	if alreadyAttached {
		writeError(w, http.StatusConflict, "terminal_already_attached", "终端已连接到另一个输出流")
		return
	}
	if terminal == nil {
		writeTerminalClosed(w)
		return
	}
	defer s.terminals.close(id, false)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-r.Context().Done():
			terminal.Close()
		case <-done:
		}
	}()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	buf := make([]byte, terminalOutputChunk)
	for {
		n, readErr := terminal.Output.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			return
		}
	}
}

func (s *Server) handleTerminalStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "终端输出需要 GET 请求")
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		w.Header().Set("Upgrade", "websocket")
		writeError(w, http.StatusUpgradeRequired, "terminal_upgrade_required", "终端输出需要 WebSocket")
		return
	}
	origins := r.Header.Values("Origin")
	if len(origins) != 1 || origins[0] != "http://"+r.Host {
		writeError(w, http.StatusForbidden, "invalid_origin", "桌面请求来源无效")
		return
	}
	if _, ok := w.(http.Hijacker); !ok {
		writeError(w, http.StatusInternalServerError, "terminal_stream_failed", "无法打开终端输出流")
		return
	}

	id := routeConnectionID(r)
	var terminal *ssh.Terminal
	defer func() {
		if terminal != nil {
			s.terminals.close(id, false)
		}
	}()
	server := websocket.Server{
		Handshake: func(config *websocket.Config, _ *http.Request) error {
			terminal, _ = s.terminals.attach(id)
			if terminal == nil {
				return errors.New("terminal is closed or already attached")
			}
			config.Protocol = nil
			return nil
		},
		Handler: func(socket *websocket.Conn) {
			streamTerminalOutput(socket, terminal)
		},
	}
	server.ServeHTTP(w, r)
}

func streamTerminalOutput(socket *websocket.Conn, terminal *ssh.Terminal) {
	// SSH output alone uses WebSocket; input and resize stay on their HTTP
	// routes. The only accepted data frame acknowledges one rendered chunk.
	socket.PayloadType = websocket.BinaryFrame
	socket.MaxPayloadBytes = 1
	_ = socket.SetWriteDeadline(time.Now().Add(terminalWriteLimit))
	done := make(chan struct{})
	defer close(done)
	defer func() {
		_ = socket.SetWriteDeadline(time.Now().Add(terminalWriteLimit))
		_ = socket.Close()
	}()
	var awaitingAck atomic.Bool
	acks := make(chan struct{}, 1)
	peerClosed := make(chan struct{})
	go func() {
		defer close(peerClosed)
		defer terminal.Close()
		for {
			var ack []byte
			if err := terminalAckCodec.Receive(socket, &ack); err != nil || !awaitingAck.CompareAndSwap(true, false) {
				return
			}
			acks <- struct{}{}
		}
	}()
	go func() {
		select {
		case <-socket.Request().Context().Done():
			_ = socket.SetDeadline(time.Now())
			terminal.Close()
		case <-done:
		}
	}()

	buf := make([]byte, terminalOutputChunk)
	for {
		n, readErr := terminal.Output.Read(buf)
		if n > 0 {
			awaitingAck.Store(true)
			_ = socket.SetWriteDeadline(time.Now().Add(terminalWriteLimit))
			if _, err := socket.Write(buf[:n]); err != nil {
				return
			}
			timeout := time.NewTimer(terminalAckTimeout)
			select {
			case <-acks:
				timeout.Stop()
			case <-peerClosed:
				timeout.Stop()
				return
			case <-socket.Request().Context().Done():
				timeout.Stop()
				return
			case <-timeout.C:
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}

var terminalAckCodec = websocket.Codec{Unmarshal: func(payload []byte, kind byte, _ any) error {
	if kind != websocket.BinaryFrame || len(payload) != 1 || payload[0] != 1 {
		return errors.New("invalid terminal output acknowledgment")
	}
	return nil
}}

func (s *Server) handleTerminalInput(w http.ResponseWriter, r *http.Request) {
	terminal := s.terminals.active(routeConnectionID(r))
	if terminal == nil {
		writeTerminalClosed(w)
		return
	}
	defer r.Body.Close()
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, ssh.MaxTerminalInput))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "terminal_input_too_large", "终端输入过长")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_terminal_input", "无法读取终端输入")
		return
	}
	switch r.Header.Get("X-Zidong-Terminal-Input") {
	case "", "text":
		writeTerminalCommand(w, terminal.Send(data))
	case "binary":
		writeTerminalCommand(w, terminal.SendRaw(data))
	default:
		writeError(w, http.StatusBadRequest, "invalid_terminal_input", "无法识别终端输入类型")
	}
}

func (s *Server) handleTerminalResize(w http.ResponseWriter, r *http.Request) {
	terminal := s.terminals.active(routeConnectionID(r))
	if terminal == nil {
		writeTerminalClosed(w)
		return
	}
	var req terminalDimensions
	if !decodeJSONBody(w, r, 512, &req) {
		return
	}
	writeTerminalCommand(w, terminal.Resize(req.Rows, req.Cols))
}

func (s *Server) handleCloseTerminal(w http.ResponseWriter, r *http.Request) {
	s.terminals.close(routeConnectionID(r), false)
	w.WriteHeader(http.StatusNoContent)
}

func writeTerminalClosed(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "terminal_closed", "终端会话已断开")
}

func writeTerminalCommand(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ssh.ErrTerminalClosed):
		writeTerminalClosed(w)
	case errors.Is(err, ssh.ErrTerminalBusy):
		writeError(w, http.StatusTooManyRequests, "terminal_busy", "终端输入缓冲已满")
	case errors.Is(err, ssh.ErrTerminalInputEncoding):
		writeError(w, http.StatusBadRequest, "terminal_input_encoding", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "invalid_terminal_command", err.Error())
	}
}
