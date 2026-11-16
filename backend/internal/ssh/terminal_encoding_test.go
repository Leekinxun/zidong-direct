package ssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/iotest"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestTerminalCharsetsDecodeSplitCharacters(t *testing.T) {
	for _, test := range []struct {
		charset string
		text    string
		wire    []byte
	}{
		{"utf-8", "中文", []byte("中文")},
		{"gbk", "中文", []byte{0xd6, 0xd0, 0xce, 0xc4}},
		{"gb18030", "😀", []byte{0x94, 0x39, 0xfc, 0x36}},
	} {
		t.Run(test.charset, func(t *testing.T) {
			charset, err := terminalEncoding(test.charset)
			if err != nil {
				t.Fatal(err)
			}
			prefix, suffix := "\x1b[31m", "\x1b[0m\r\n\x03"
			wire := append(append([]byte(prefix), test.wire...), []byte(suffix)...)
			expected := prefix + test.text + suffix
			result, err := io.ReadAll(decodeTerminalOutput(iotest.OneByteReader(bytes.NewReader(wire)), charset))
			if err != nil || string(result) != expected {
				t.Fatalf("decoded %q, want %q (error %v)", result, expected, err)
			}
			result, err = encodeTerminalInput([]byte(expected), charset)
			if err != nil || !bytes.Equal(result, wire) {
				t.Fatalf("encoded %x, want %x (error %v)", result, wire, err)
			}
		})
	}
	if _, err := terminalEncoding("bogus"); err == nil {
		t.Fatal("accepted unknown terminal charset")
	}
}

func TestGBKTerminalRoundTripPreservesRawReports(t *testing.T) {
	listener, signer := terminalFixture(t)
	closed := make(chan struct{})
	received := make(chan []byte, 1)
	want := []byte{0xd6, 0xd0, 0xce, 0xc4, 0x03, 0xff, 0x81}
	go serveTerminalFixtureWithShell(listener, signer, nil, closed, func(channel gossh.Channel) {
		for _, octet := range []byte{0xd6, 0xd0, 0xce, 0xc4, '\n'} {
			if _, err := channel.Write([]byte{octet}); err != nil {
				return
			}
		}
		wire := make([]byte, len(want))
		if _, err := io.ReadFull(channel, wire); err == nil {
			received <- wire
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	terminal, err := OpenTerminalWithEncoding(ctx, terminalFixtureSpec(listener, signer), 24, 80, "gbk")
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	output := make([]byte, len("中文\n"))
	if _, err := io.ReadFull(terminal.Output, output); err != nil || string(output) != "中文\n" {
		t.Fatalf("decoded output %q, error %v", output, err)
	}
	if err := terminal.Send([]byte("partial-prefix😀")); !errors.Is(err, ErrTerminalInputEncoding) {
		t.Fatalf("unrepresentable GBK input returned %v", err)
	}
	if err := terminal.Send([]byte("中文\x03")); err != nil {
		t.Fatal(err)
	}
	if err := terminal.SendRaw([]byte{0xff, 0x81}); err != nil {
		t.Fatal(err)
	}
	select {
	case wire := <-received:
		if !bytes.Equal(wire, want) {
			t.Fatalf("SSH received %x, want %x", wire, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SSH did not receive encoded terminal input")
	}
}
