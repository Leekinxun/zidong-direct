package ssh

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

const (
	socksConnectTimeout = 15 * time.Second
	maxSOCKSConnections = 128
)

// SOCKSTunnel is an SSH dynamic forward bound to the local IPv4 loopback.
type SOCKSTunnel struct {
	client   *gossh.Client
	listener *net.TCPListener
	cancel   context.CancelFunc
	done     chan struct{}

	mu     sync.Mutex
	closed bool
	conns  map[*net.TCPConn]struct{}
}

func OpenSOCKSTunnel(ctx context.Context, spec Spec) (*SOCKSTunnel, error) {
	port := spec.Port
	if port == 0 {
		port = 22
	}
	if strings.TrimSpace(spec.Host) == "" || strings.TrimSpace(spec.Username) == "" || port < 1 || port > 65535 {
		return nil, errors.New("无效的 SSH 主机、端口或用户名")
	}
	auth, err := buildAuthMethods(spec)
	if err != nil {
		return nil, err
	}
	hostKey, err := buildHostKeyCallback(spec)
	if err != nil {
		return nil, err
	}

	handshake, cancel := context.WithTimeout(ctx, socksConnectTimeout)
	defer cancel()
	addr := net.JoinHostPort(spec.Host, strconv.Itoa(port))
	raw, err := (&net.Dialer{}).DialContext(handshake, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("连接 SSH 服务器：%w", err)
	}
	deadline, _ := handshake.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		_ = raw.Close()
		return nil, err
	}
	stopWatch := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-handshake.Done():
			_ = raw.Close()
		case <-stopWatch:
		}
	}()
	conn, channels, requests, err := gossh.NewClientConn(raw, addr, &gossh.ClientConfig{
		User: spec.Username, Auth: auth, HostKeyCallback: hostKey,
	})
	close(stopWatch)
	<-watcherDone
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("SSH 握手失败：%w", err)
	}
	client := gossh.NewClient(conn, channels, requests)
	if err := handshake.Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		_ = client.Close()
		return nil, err
	}
	return listenSOCKS(client)
}

func listenSOCKS(client *gossh.Client) (*SOCKSTunnel, error) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("启动本地 SOCKS5 代理：%w", err)
	}
	lifetime, cancel := context.WithCancel(context.Background())
	tunnel := &SOCKSTunnel{
		client: client, listener: listener, cancel: cancel, done: make(chan struct{}),
		conns: make(map[*net.TCPConn]struct{}),
	}
	go tunnel.accept(lifetime)
	go func() {
		_ = client.Wait()
		tunnel.Close()
	}()
	return tunnel, nil
}

func (s *SOCKSTunnel) Port() int             { return s.listener.Addr().(*net.TCPAddr).Port }
func (s *SOCKSTunnel) Done() <-chan struct{} { return s.done }

func (s *SOCKSTunnel) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cancel()
	_ = s.listener.Close()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	_ = s.client.Close()
	close(s.done)
}

func (s *SOCKSTunnel) accept(ctx context.Context) {
	defer s.Close()
	for {
		conn, err := s.listener.AcceptTCP()
		if err != nil {
			return
		}
		s.mu.Lock()
		refuse := s.closed || len(s.conns) >= maxSOCKSConnections
		if !refuse {
			s.conns[conn] = struct{}{}
		}
		s.mu.Unlock()
		if refuse {
			_ = conn.Close()
			continue
		}
		go s.forward(ctx, conn)
	}
}

func (s *SOCKSTunnel) forward(ctx context.Context, local *net.TCPConn) {
	defer func() {
		_ = local.Close()
		s.mu.Lock()
		delete(s.conns, local)
		s.mu.Unlock()
	}()
	if err := local.SetDeadline(time.Now().Add(socksConnectTimeout)); err != nil {
		return
	}
	destination, err := readSOCKSDestination(local)
	if err != nil {
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, socksConnectTimeout)
	remote, err := s.client.DialContext(dialCtx, "tcp", destination)
	cancel()
	if err != nil {
		_ = writeSOCKSReply(local, 1)
		return
	}
	defer remote.Close()
	if err := writeSOCKSReply(local, 0); err != nil {
		return
	}
	if err := local.SetDeadline(time.Time{}); err != nil {
		return
	}

	upstreamDone := make(chan struct{})
	go func() {
		defer close(upstreamDone)
		_, err := io.Copy(remote, local)
		if halfClose, ok := remote.(interface{ CloseWrite() error }); err == nil && ok {
			_ = halfClose.CloseWrite()
			return
		}
		_ = remote.Close()
	}()
	_, _ = io.Copy(local, remote)
	_ = local.Close()
	_ = remote.Close()
	<-upstreamDone
}

func readSOCKSDestination(conn io.ReadWriter) (string, error) {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:2]); err != nil {
		return "", err
	}
	if header[0] != 5 || header[1] == 0 {
		return "", errors.New("invalid SOCKS5 greeting")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", err
	}
	if !bytes.Contains(methods, []byte{0}) {
		_, _ = conn.Write([]byte{5, 0xff})
		return "", errors.New("SOCKS5 no-authentication method is unavailable")
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return "", err
	}
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return "", err
	}
	if header[0] != 5 || header[2] != 0 {
		_ = writeSOCKSReply(conn, 1)
		return "", errors.New("invalid SOCKS5 request")
	}
	if header[1] != 1 {
		_ = writeSOCKSReply(conn, 7)
		return "", errors.New("only SOCKS5 CONNECT is supported")
	}

	var host string
	switch header[3] {
	case 1, 4:
		size := net.IPv4len
		if header[3] == 4 {
			size = net.IPv6len
		}
		ip := make(net.IP, size)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", err
		}
		host = ip.String()
	case 3:
		if _, err := io.ReadFull(conn, header[:1]); err != nil {
			return "", err
		}
		domain := make([]byte, int(header[0]))
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", err
		}
		if len(domain) == 0 || bytes.IndexFunc(domain, func(r rune) bool {
			return r <= ' ' || r >= 0x7f || r == ':' || r == '[' || r == ']' || r == '/' || r == '\\'
		}) >= 0 {
			_ = writeSOCKSReply(conn, 4)
			return "", errors.New("invalid SOCKS5 destination name")
		}
		// Pass the name straight to SSH so resolution happens on the server.
		host = string(domain)
	default:
		_ = writeSOCKSReply(conn, 8)
		return "", errors.New("unsupported SOCKS5 address type")
	}
	if _, err := io.ReadFull(conn, header[:2]); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(header[:2])
	if port == 0 {
		_ = writeSOCKSReply(conn, 1)
		return "", errors.New("invalid SOCKS5 destination port")
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

func writeSOCKSReply(writer io.Writer, code byte) error {
	_, err := writer.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
	return err
}
