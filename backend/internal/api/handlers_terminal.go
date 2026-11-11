package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"web-db-client/internal/ssh"
)

const (
	maxTerminalSessions = 4
	terminalAttachTTL   = 15 * time.Second
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

func (sessions *terminalSessions) reserve(id string) bool {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.sessions) >= maxTerminalSessions {
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
	routes.add("POST /api/terminal/sessions/{id}/input", s.handleTerminalInput)
	routes.add("POST /api/terminal/sessions/{id}/resize", s.handleTerminalResize)
	routes.add("DELETE /api/terminal/sessions/{id}", s.handleCloseTerminal)
}

type terminalDimensions struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

func (s *Server) handleOpenTerminal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConnectionID string `json:"connectionId"`
		terminalDimensions
	}
	if !decodeJSONBody(w, r, 4096, &req) {
		return
	}
	conn, err := s.store.Get(req.ConnectionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connection_not_found", "找不到所选连接")
		return
	}
	if !conn.SSH.Enabled {
		writeError(w, http.StatusBadRequest, "ssh_required", "请在所选连接中启用 SSH 并保存设置")
		return
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		writeError(w, http.StatusInternalServerError, "terminal_failed", "无法分配终端会话")
		return
	}
	id := hex.EncodeToString(token[:])
	if !s.terminals.reserve(id) {
		writeError(w, http.StatusTooManyRequests, "terminal_limit", "终端会话已达上限，请先断开其他终端")
		return
	}
	success := false
	defer func() {
		if !success {
			s.terminals.close(id, false)
		}
	}()
	terminal, err := ssh.OpenTerminal(r.Context(), ssh.Spec{
		Host:                  conn.SSH.Host,
		Port:                  conn.SSH.Port,
		Username:              conn.SSH.Username,
		AuthType:              conn.SSH.AuthType,
		Password:              conn.SSH.Password,
		PrivateKey:            conn.SSH.PrivateKey,
		Passphrase:            conn.SSH.Passphrase,
		HostKeyFingerprint:    conn.SSH.HostKeyFingerprint,
		InsecureIgnoreHostKey: conn.SSH.InsecureIgnoreHostKey,
	}, req.Rows, req.Cols)
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
	buf := make([]byte, 16*1024)
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
	writeTerminalCommand(w, terminal.Send(data))
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
	default:
		writeError(w, http.StatusBadRequest, "invalid_terminal_command", err.Error())
	}
}
