package ssh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type Spec struct {
	Host                  string
	Port                  int
	Username              string
	AuthType              string // password | privateKey
	Password              string
	PrivateKey            string
	Passphrase            string
	HostKeyFingerprint    string
	InsecureIgnoreHostKey bool
	TargetHost            string
	TargetPort            int
}

func (s Spec) Fingerprint() string {
	h := sha256.New()
	_, _ = io.WriteString(h, strings.Join([]string{
		s.Host,
		strconv.Itoa(s.Port),
		s.Username,
		s.AuthType,
		s.Password,
		s.PrivateKey,
		s.Passphrase,
		s.HostKeyFingerprint,
		strconv.FormatBool(s.InsecureIgnoreHostKey),
		s.TargetHost,
		strconv.Itoa(s.TargetPort),
	}, "\x00"))
	return hex.EncodeToString(h.Sum(nil))
}

type Tunnel struct {
	fingerprint string
	localHost   string
	localPort   int
	client      *gossh.Client
	listener    net.Listener
	cancel      context.CancelFunc

	mu        sync.Mutex
	closed    bool
	conns     map[net.Conn]struct{}
	wg        sync.WaitGroup
}

func OpenTunnel(ctx context.Context, spec Spec) (*Tunnel, error) {
	if strings.TrimSpace(spec.Host) == "" {
		return nil, errors.New("SSH host is required")
	}
	if strings.TrimSpace(spec.Username) == "" {
		return nil, errors.New("SSH username is required")
	}
	if spec.Port <= 0 {
		spec.Port = 22
	}
	if strings.TrimSpace(spec.TargetHost) == "" || spec.TargetPort <= 0 {
		return nil, errors.New("target database host and port are required for SSH tunnel")
	}

	authMethods, err := buildAuthMethods(spec)
	if err != nil {
		return nil, err
	}
	hostKeyCB, err := buildHostKeyCallback(spec)
	if err != nil {
		return nil, err
	}

	sshCfg := &gossh.ClientConfig{
		User:            spec.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCB,
		Timeout:         12 * time.Second,
	}

	sshAddr := net.JoinHostPort(spec.Host, strconv.Itoa(spec.Port))
	dialer := &net.Dialer{Timeout: 12 * time.Second}
	rawConn, err := dialer.DialContext(ctx, "tcp", sshAddr)
	if err != nil {
		return nil, fmt.Errorf("dial SSH server %s: %w", sshAddr, err)
	}

	sshConn, chans, reqs, err := gossh.NewClientConn(rawConn, sshAddr, sshCfg)
	if err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("SSH handshake with %s: %w", sshAddr, err)
	}
	client := gossh.NewClient(sshConn, chans, reqs)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("start local tunnel listener: %w", err)
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		_ = client.Close()
		return nil, errors.New("unexpected listener address type")
	}

	runCtx, cancel := context.WithCancel(context.Background())
	t := &Tunnel{
		fingerprint: spec.Fingerprint(),
		localHost:   "127.0.0.1",
		localPort:   tcpAddr.Port,
		client:      client,
		listener:    listener,
		cancel:      cancel,
		conns:       make(map[net.Conn]struct{}),
	}

	t.wg.Add(2)
	go t.acceptLoop(runCtx, net.JoinHostPort(spec.TargetHost, strconv.Itoa(spec.TargetPort)))
	go t.keepaliveLoop(runCtx)

	return t, nil
}

func buildAuthMethods(spec Spec) ([]gossh.AuthMethod, error) {
	switch spec.AuthType {
	case "privateKey":
		if strings.TrimSpace(spec.PrivateKey) == "" {
			return nil, errors.New("SSH private key is required for privateKey auth")
		}
		var signer gossh.Signer
		var err error
		if spec.Passphrase != "" {
			signer, err = gossh.ParsePrivateKeyWithPassphrase([]byte(spec.PrivateKey), []byte(spec.Passphrase))
		} else {
			signer, err = gossh.ParsePrivateKey([]byte(spec.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("parse SSH private key: %w", err)
		}
		return []gossh.AuthMethod{gossh.PublicKeys(signer)}, nil
	case "password", "":
		if spec.Password == "" {
			return nil, errors.New("SSH password is required")
		}
		return []gossh.AuthMethod{gossh.Password(spec.Password)}, nil
	default:
		return nil, fmt.Errorf("unsupported SSH authType %q", spec.AuthType)
	}
}

func buildHostKeyCallback(spec Spec) (gossh.HostKeyCallback, error) {
	expected := strings.TrimSpace(spec.HostKeyFingerprint)
	if expected != "" {
		return func(_ string, _ net.Addr, key gossh.PublicKey) error {
			actual := gossh.FingerprintSHA256(key)
			if actual != expected {
				return fmt.Errorf("SSH host key mismatch: got %s, expected %s", actual, expected)
			}
			return nil
		}, nil
	}
	if spec.InsecureIgnoreHostKey {
		return gossh.InsecureIgnoreHostKey(), nil
	}
	return nil, errors.New("SSH hostKeyFingerprint is required unless insecureIgnoreHostKey is enabled")
}

func (t *Tunnel) LocalHost() string    { return t.localHost }
func (t *Tunnel) LocalPort() int       { return t.localPort }
func (t *Tunnel) Fingerprint() string  { return t.fingerprint }

func (t *Tunnel) acceptLoop(ctx context.Context, remoteAddr string) {
	defer t.wg.Done()
	for {
		localConn, err := t.listener.Accept()
		if err != nil {
			return
		}
		if ctx.Err() != nil {
			_ = localConn.Close()
			return
		}
		t.wg.Add(1)
		go t.forwardConn(localConn, remoteAddr)
	}
}

func (t *Tunnel) forwardConn(localConn net.Conn, remoteAddr string) {
	defer t.wg.Done()
	t.trackConn(localConn, true)
	defer func() {
		t.trackConn(localConn, false)
		_ = localConn.Close()
	}()

	remoteConn, err := t.client.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	t.trackConn(remoteConn, true)
	defer func() {
		t.trackConn(remoteConn, false)
		_ = remoteConn.Close()
	}()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remoteConn, localConn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(localConn, remoteConn)
		done <- struct{}{}
	}()
	<-done
}

func (t *Tunnel) trackConn(c net.Conn, add bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		if add {
			_ = c.Close()
		}
		return
	}
	if add {
		t.conns[c] = struct{}{}
	} else {
		delete(t.conns, c)
	}
}

func (t *Tunnel) keepaliveLoop(ctx context.Context) {
	defer t.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _, err := t.client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				return
			}
		}
	}
}

func (t *Tunnel) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.cancel()
	var errs []error
	if t.listener != nil {
		if err := t.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	for c := range t.conns {
		_ = c.Close()
	}
	if t.client != nil {
		if err := t.client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	t.mu.Unlock()
	t.wg.Wait()
	return errors.Join(errs...)
}
