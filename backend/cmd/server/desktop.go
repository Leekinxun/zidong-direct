package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"web-db-client/internal/api"
	"web-db-client/internal/config"
	"web-db-client/internal/crypto"
)

type desktopBootstrap struct {
	Token     string `json:"token"`
	MasterKey string `json:"masterKey"`
}

type desktopReady struct {
	Type string `json:"type"`
	Port int    `json:"port"`
}

type desktopFailure struct {
	Type string `json:"type"`
	Code string `json:"code"`
}

func runDesktop(ctx context.Context, input io.Reader, output io.Writer) (runErr error) {
	startupCode := "bootstrap"
	events := json.NewEncoder(output)
	defer func() {
		if runErr != nil && startupCode != "" {
			_ = events.Encode(desktopFailure{Type: "error", Code: startupCode})
		}
	}()

	control := bufio.NewScanner(input)
	control.Buffer(make([]byte, 256), 1024)
	bootstrap, err := readDesktopBootstrap(control)
	if err != nil {
		return err
	}

	startupCode = "configuration"
	cfg := config.Load()
	staticDir := os.Getenv("STATIC_DIR")
	for _, dir := range []struct{ name, path string }{
		{"DATA_DIR", os.Getenv("DATA_DIR")},
		{"SQLITE_DIR", os.Getenv("SQLITE_DIR")},
		{"STATIC_DIR", staticDir},
	} {
		if !filepath.IsAbs(dir.path) {
			return fmt.Errorf("desktop %s must be an explicit absolute path", dir.name)
		}
	}
	if index, err := os.Stat(filepath.Join(staticDir, "index.html")); err != nil || !index.Mode().IsRegular() {
		return errors.New("desktop index.html is missing")
	}

	startupCode = "profile"
	cfg.DesktopMode = true
	cfg.AuthEnabled = false
	cfg.AllowArbitrarySQLitePaths = false
	cipher, err := desktopCipher(bootstrap.MasterKey, filepath.Join(cfg.MetaDir, ".master.key"))
	if err != nil {
		return fmt.Errorf("init desktop cipher: %w", err)
	}
	handler, pools, err := newHandler(cfg, cipher, staticDir)
	if err != nil {
		return err
	}
	defer pools.CloseAll()

	startupCode = "listen"
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on desktop loopback: %w", err)
	}
	defer listener.Close()
	handler = api.DesktopHandler(listener.Addr().String(), bootstrap.Token, handler)
	startupCode = ""
	if err := events.Encode(desktopReady{
		Type: "ready",
		Port: listener.Addr().(*net.TCPAddr).Port,
	}); err != nil {
		return fmt.Errorf("report desktop readiness: %w", err)
	}

	stop := make(chan struct{})
	go func() {
		defer close(stop)
		for control.Scan() {
			if control.Text() == "shutdown" {
				return
			}
		}
	}()
	return serve(ctx, listener, handler, stop)
}

func readDesktopBootstrap(control *bufio.Scanner) (desktopBootstrap, error) {
	var bootstrap desktopBootstrap
	if !control.Scan() || json.Unmarshal(control.Bytes(), &bootstrap) != nil {
		return desktopBootstrap{}, errors.New("invalid desktop bootstrap message")
	}
	if len(bootstrap.Token) != 64 || strings.ToLower(bootstrap.Token) != bootstrap.Token {
		return desktopBootstrap{}, errors.New("invalid desktop bootstrap token")
	}
	if _, err := hex.DecodeString(bootstrap.Token); err != nil {
		return desktopBootstrap{}, errors.New("invalid desktop bootstrap token")
	}
	return bootstrap, nil
}

func desktopCipher(masterKey, keyFile string) (*crypto.Cipher, error) {
	if masterKey == "" {
		if _, err := os.Stat(keyFile); errors.Is(err, os.ErrNotExist) {
			for _, name := range []string{"connections.json", "ssh_hosts.json"} {
				if _, err := os.Stat(filepath.Join(filepath.Dir(keyFile), name)); !errors.Is(err, os.ErrNotExist) {
					if err != nil {
						return nil, err
					}
					return nil, errors.New("desktop master key is missing for existing connection data")
				}
			}
		}
		return crypto.NewCipher("", keyFile)
	}
	raw, err := hex.DecodeString(masterKey)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("invalid desktop master key")
	}
	return crypto.NewCipherFromKey(raw)
}
