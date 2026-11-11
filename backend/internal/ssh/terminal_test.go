package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestTerminalPTYStreamsInputAndResize(t *testing.T) {
	listener, signer := terminalFixture(t)
	events := make(chan [2]uint32, 2)
	closed := make(chan struct{})
	go serveTerminalFixture(listener, signer, events, closed)
	spec := terminalFixtureSpec(listener, signer)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	terminal, err := OpenTerminal(ctx, spec, 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	cancel() // The startup request can end while the terminal stays attached.
	assertTerminalDimensions(t, events, [2]uint32{24, 80})

	prompt := []byte("\x1b[32mfixture $ \x1b[0m")
	output := make([]byte, len(prompt))
	if _, err := io.ReadFull(terminal.Output, output); err != nil || string(output) != string(prompt) {
		t.Fatalf("terminal prompt %q, %v", output, err)
	}
	input := []byte("echo 世界\r\x03")
	if err := terminal.Send(input); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, len(input))
	if _, err := io.ReadFull(terminal.Output, echo); err != nil || string(echo) != string(input) {
		t.Fatalf("terminal echo %q, %v", echo, err)
	}
	if err := terminal.Resize(40, 120); err != nil {
		t.Fatal(err)
	}
	assertTerminalDimensions(t, events, [2]uint32{40, 120})

	terminal.Close()
	if err := terminal.Send([]byte("after close")); !errors.Is(err, ErrTerminalClosed) {
		t.Fatalf("expected terminal closed, got %v", err)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal did not close the SSH transport")
	}
}

func TestTerminalChecksHostFingerprint(t *testing.T) {
	listener, signer := terminalFixture(t)
	go serveTerminalFixture(listener, signer, nil, make(chan struct{}))
	spec := terminalFixtureSpec(listener, signer)
	spec.HostKeyFingerprint = "SHA256:fixture-mismatch"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if terminal, err := OpenTerminal(ctx, spec, 24, 80); err == nil {
		terminal.Close()
		t.Fatal("opened terminal with mismatched host key")
	} else if !strings.Contains(err.Error(), "SSH host key mismatch") {
		t.Fatalf("expected host key error, got %v", err)
	}
}

func TestTerminalCancelsStalledHandshake(t *testing.T) {
	listener, signer := terminalFixture(t)
	accepted := make(chan struct{})
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			return
		}
		defer raw.Close()
		close(accepted)
		_ = raw.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.Copy(io.Discard, raw)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-accepted:
			cancel()
		case <-ctx.Done():
		}
	}()
	start := time.Now()
	if terminal, err := OpenTerminal(ctx, terminalFixtureSpec(listener, signer), 24, 80); err == nil {
		terminal.Close()
		t.Fatal("opened terminal on a silent peer")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("SSH handshake cancellation took %v", elapsed)
	}
}

func terminalFixture(t *testing.T) (net.Listener, gossh.Signer) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener, signer
}

func terminalFixtureSpec(listener net.Listener, signer gossh.Signer) Spec {
	return Spec{
		Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port,
		Username: "fixture", AuthType: "password", Password: "fixture-password",
		HostKeyFingerprint: gossh.FingerprintSHA256(signer.PublicKey()),
	}
}

func serveTerminalFixture(listener net.Listener, signer gossh.Signer, events chan<- [2]uint32, closed chan<- struct{}) {
	defer close(closed)
	raw, err := listener.Accept()
	if err != nil {
		return
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(4 * time.Second))
	config := &gossh.ServerConfig{
		PasswordCallback: func(meta gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			if meta.User() != "fixture" || string(password) != "fixture-password" {
				return nil, errors.New("fixture credentials mismatch")
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)
	conn, channels, requests, err := gossh.NewServerConn(raw, config)
	if err != nil {
		return
	}
	defer conn.Close()
	go gossh.DiscardRequests(requests)
	for next := range channels {
		if next.ChannelType() != "session" {
			_ = next.Reject(gossh.UnknownChannelType, "fixture only accepts sessions")
			continue
		}
		channel, reqs, err := next.Accept()
		if err != nil {
			return
		}
		for req := range reqs {
			switch req.Type {
			case "pty-req":
				var pty struct {
					Term                      string
					Cols, Rows, Width, Height uint32
					Modes                     string
				}
				err := gossh.Unmarshal(req.Payload, &pty)
				_ = req.Reply(err == nil && pty.Term == "xterm-256color", nil)
				if events != nil {
					events <- [2]uint32{pty.Rows, pty.Cols}
				}
			case "shell":
				_ = req.Reply(true, nil)
				go func() {
					_, _ = io.WriteString(channel, "\x1b[32mfixture $ \x1b[0m")
					_, _ = io.Copy(channel, channel)
					_ = channel.Close()
				}()
			case "window-change":
				var size struct{ Cols, Rows, Width, Height uint32 }
				if gossh.Unmarshal(req.Payload, &size) == nil && events != nil {
					events <- [2]uint32{size.Rows, size.Cols}
				}
			default:
				_ = req.Reply(false, nil)
			}
		}
	}
}

func assertTerminalDimensions(t *testing.T, events <-chan [2]uint32, expected [2]uint32) {
	t.Helper()
	select {
	case got := <-events:
		if got != expected {
			t.Fatalf("PTY dimensions %v, want %v", got, expected)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no PTY dimensions received")
	}
}
