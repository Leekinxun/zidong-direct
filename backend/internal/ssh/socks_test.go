package ssh

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestSOCKSTunnelDirectTCPIPAndCleanup(t *testing.T) {
	listener, signer := terminalFixture(t)
	forwarded := make(chan socksDirectTCPIP, 1)
	closed := make(chan struct{})
	go serveSOCKSFixture(listener, signer, forwarded, closed)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	tunnel, err := OpenSOCKSTunnel(ctx, terminalFixtureSpec(listener, signer))
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	cancel() // The authenticated tunnel lifetime is separate from startup.

	socket, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(tunnel.Port())), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	_ = socket.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := socket.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	assertSOCKSBytes(t, socket, []byte{5, 0})
	request := append([]byte{5, 1, 0, 3, byte(len("remote.test"))}, []byte("remote.test")...)
	request = append(request, 0x1f, 0x90) // 8080
	if _, err := socket.Write(request); err != nil {
		t.Fatal(err)
	}
	assertSOCKSBytes(t, socket, []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	select {
	case target := <-forwarded:
		if target.Host != "remote.test" || target.Port != 8080 {
			t.Fatalf("unexpected SSH target: %+v", target)
		}
	case <-time.After(time.Second):
		t.Fatal("SSH server did not receive the original hostname")
	}
	if _, err := socket.Write([]byte("fixture body")); err != nil {
		t.Fatal(err)
	}
	assertSOCKSBytes(t, socket, []byte("fixture body"))

	tunnel.Close()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("SSH transport did not close")
	}
	if _, err := socket.Read(make([]byte, 1)); err == nil {
		t.Fatal("forwarded socket still active after closing the proxy")
	}
	if reopened, err := net.DialTimeout("tcp", socket.RemoteAddr().String(), 100*time.Millisecond); err == nil {
		_ = reopened.Close()
		t.Fatal("SOCKS listener still open after closing the proxy")
	}
}

func assertSOCKSBytes(t *testing.T, input io.Reader, want []byte) {
	t.Helper()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(input, got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("read %v, wanted %v; error %v", got, want, err)
	}
}

type socksDirectTCPIP struct {
	Host       string
	Port       uint32
	OriginHost string
	OriginPort uint32
}

func serveSOCKSFixture(listener net.Listener, signer gossh.Signer, forwarded chan<- socksDirectTCPIP, closed chan<- struct{}) {
	defer close(closed)
	raw, err := listener.Accept()
	if err != nil {
		return
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(4 * time.Second))
	config := &gossh.ServerConfig{PasswordCallback: func(meta gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
		if meta.User() != "fixture" || string(password) != "fixture-password" {
			return nil, errors.New("fixture credentials mismatch")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	server, channels, requests, err := gossh.NewServerConn(raw, config)
	if err != nil {
		return
	}
	defer server.Close()
	go gossh.DiscardRequests(requests)
	for channel := range channels {
		var target socksDirectTCPIP
		if channel.ChannelType() != "direct-tcpip" || gossh.Unmarshal(channel.ExtraData(), &target) != nil {
			_ = channel.Reject(gossh.UnknownChannelType, "unexpected fixture channel")
			continue
		}
		stream, requests, err := channel.Accept()
		if err != nil {
			continue
		}
		forwarded <- target
		go gossh.DiscardRequests(requests)
		go func() {
			defer stream.Close()
			_, _ = io.Copy(stream, stream)
		}()
	}
}

func TestSOCKSRejectsUnsupportedCommand(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	errs := make(chan error, 1)
	go func() {
		_, err := readSOCKSDestination(server)
		errs <- err
	}()
	if _, err := client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	assertSOCKSBytes(t, client, []byte{5, 0})
	if _, err := client.Write([]byte{5, 3, 0, 1}); err != nil {
		t.Fatal(err)
	}
	assertSOCKSBytes(t, client, []byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
	if err := <-errs; err == nil {
		t.Fatal("SOCKS accepted a command other than CONNECT")
	}
}
