package ssh

import (
	"context"
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

var (
	ErrTerminalClosed = errors.New("SSH terminal closed")
	ErrTerminalBusy   = errors.New("SSH terminal input queue is full")
)

const MaxTerminalInput = 128 * 1024

type terminalCommand struct {
	input      []byte
	rows, cols int
}

// Terminal owns one SSH client and interactive PTY.
type Terminal struct {
	Output    io.Reader
	client    *gossh.Client
	session   *gossh.Session
	output    *io.PipeWriter
	commands  chan terminalCommand
	done      chan struct{}
	closeOnce sync.Once
}

func OpenTerminal(ctx context.Context, spec Spec, rows, cols int) (*Terminal, error) {
	if strings.TrimSpace(spec.Host) == "" || strings.TrimSpace(spec.Username) == "" {
		return nil, errors.New("SSH host and username are required")
	}
	if spec.Port <= 0 {
		spec.Port = 22
	}
	if !validTerminalSize(rows, cols) {
		return nil, errors.New("invalid SSH terminal size")
	}
	auth, err := buildAuthMethods(spec)
	if err != nil {
		return nil, err
	}
	hostKey, err := buildHostKeyCallback(spec)
	if err != nil {
		return nil, err
	}

	const timeout = 12 * time.Second
	addr := net.JoinHostPort(spec.Host, strconv.Itoa(spec.Port))
	dialer := net.Dialer{Timeout: timeout}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial SSH server: %w", err)
	}
	started := false
	defer func() {
		if !started {
			_ = raw.Close()
		}
	}()
	if err := raw.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	stopWatching := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			_ = raw.Close()
		case <-stopWatching:
		}
	}()
	defer func() {
		close(stopWatching)
		<-watcherDone
	}()

	conn, channels, requests, err := gossh.NewClientConn(raw, addr, &gossh.ClientConfig{
		User:            spec.Username,
		Auth:            auth,
		HostKeyCallback: hostKey,
	})
	if err != nil {
		return nil, fmt.Errorf("SSH handshake: %w", err)
	}
	client := gossh.NewClient(conn, channels, requests)
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open SSH session: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}
	reader, writer := io.Pipe()
	session.Stdout, session.Stderr = writer, writer
	defer func() {
		if !started {
			_ = writer.Close()
			_ = reader.Close()
		}
	}()
	if err := session.RequestPty("xterm-256color", rows, cols, gossh.TerminalModes{gossh.ECHO: 1}); err != nil {
		return nil, fmt.Errorf("request SSH PTY: %w", err)
	}
	if err := session.Shell(); err != nil {
		return nil, fmt.Errorf("start SSH shell: %w", err)
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	terminal := &Terminal{
		Output: reader, client: client, session: session, output: writer,
		commands: make(chan terminalCommand, 8), done: make(chan struct{}),
	}
	started = true
	go terminal.run(stdin)
	go func() {
		_ = session.Wait()
		terminal.Close()
	}()
	return terminal, nil
}

func (t *Terminal) run(stdin io.Writer) {
	defer t.Close()
	for {
		select {
		case <-t.done:
			return
		case command := <-t.commands:
			if command.input == nil {
				if err := t.session.WindowChange(command.rows, command.cols); err != nil {
					return
				}
			} else if _, err := stdin.Write(command.input); err != nil {
				return
			}
		}
	}
}

func (t *Terminal) Send(input []byte) error {
	if len(input) == 0 || len(input) > MaxTerminalInput {
		return errors.New("invalid SSH terminal input length")
	}
	return t.enqueue(terminalCommand{input: input})
}

func (t *Terminal) Resize(rows, cols int) error {
	if !validTerminalSize(rows, cols) {
		return errors.New("invalid SSH terminal size")
	}
	return t.enqueue(terminalCommand{rows: rows, cols: cols})
}

func validTerminalSize(rows, cols int) bool {
	return rows > 0 && rows <= 512 && cols > 0 && cols <= 512
}

func (t *Terminal) enqueue(command terminalCommand) error {
	select {
	case <-t.done:
		return ErrTerminalClosed
	default:
	}
	select {
	case <-t.done:
		return ErrTerminalClosed
	case t.commands <- command:
		return nil
	default:
		return ErrTerminalBusy
	}
}

func (t *Terminal) Close() {
	t.closeOnce.Do(func() {
		close(t.done)
		_ = t.output.Close()
		_ = t.client.Close()
	})
}
