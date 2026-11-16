package api

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"
)

const (
	terminalOutputChunk = 16 * 1024
	terminalWriteLimit  = 5 * time.Second
	terminalAckTimeout  = 30 * time.Second
)

func webTerminalOriginAllowed(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	if len(origins) != 1 || r.Host == "" {
		return false
	}
	// Reverse proxies that terminate TLS should preserve the browser-facing
	// Host. Compare the authority rather than trusting forwarded headers.
	return strings.EqualFold(origins[0], "https://"+r.Host) ||
		(r.TLS == nil && strings.EqualFold(origins[0], "http://"+r.Host))
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
	if !webTerminalOriginAllowed(r) {
		writeError(w, http.StatusForbidden, "invalid_origin", "终端请求来源无效")
		return
	}
	if _, ok := w.(http.Hijacker); !ok {
		writeError(w, http.StatusInternalServerError, "terminal_stream_failed", "无法打开终端输出流")
		return
	}

	id := r.PathValue("id")
	var session *terminalSession
	defer func() {
		if session != nil {
			s.terminals.close(id, false)
		}
	}()
	server := websocket.Server{
		Handshake: func(config *websocket.Config, _ *http.Request) error {
			session, _ = s.terminals.attach(id)
			if session == nil {
				return errors.New("terminal is closed or already attached")
			}
			config.Protocol = nil
			return nil
		},
		Handler: func(socket *websocket.Conn) {
			streamTerminalOutput(socket, session)
		},
	}
	server.ServeHTTP(w, r)
}

func streamTerminalOutput(socket *websocket.Conn, session *terminalSession) {
	// SSH output alone uses WebSocket; input and resize stay on their HTTP
	// routes. The only accepted data frame acknowledges one rendered chunk.
	terminal := session.terminal
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
		case <-session.done:
			_ = socket.SetDeadline(time.Now())
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
			case <-session.done:
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
