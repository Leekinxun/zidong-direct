package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"web-db-client/internal/api"
	"web-db-client/internal/config"
	"web-db-client/internal/crypto"
	"web-db-client/internal/db"
	"web-db-client/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	run := runWeb
	if os.Getenv("ZIDONG_DESKTOP") == "1" {
		run = func(ctx context.Context) error { return runDesktop(ctx, os.Stdin, os.Stdout) }
	}
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func runWeb(ctx context.Context) error {
	cfg := config.Load()
	cipher, err := crypto.NewCipher(cfg.SecretKey, filepath.Join(cfg.MetaDir, ".master.key"))
	if err != nil {
		return fmt.Errorf("init cipher: %w", err)
	}
	staticDir := resolveStaticDir()
	handler, pools, err := newHandler(cfg, cipher, staticDir)
	if err != nil {
		return err
	}
	defer pools.CloseAll()

	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.Printf("WebDB Studio listening on :%s (auth=%v, sqliteDir=%s, staticDir=%s)",
		cfg.Port, cfg.AuthEnabled, cfg.SQLiteDir, staticDir)
	return serve(ctx, listener, handler, nil)
}

func newHandler(cfg config.Config, cipher *crypto.Cipher, staticDir string) (http.Handler, *db.PoolManager, error) {
	if err := os.MkdirAll(cfg.SQLiteDir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	connStore, err := store.NewConnectionStore(cfg.MetaDir, cipher)
	if err != nil {
		return nil, nil, fmt.Errorf("init connection store: %w", err)
	}
	if cfg.DesktopMode {
		if err := connStore.ValidateCredentials(); err != nil {
			return nil, nil, fmt.Errorf("validate stored connection credentials: %w", err)
		}
	}

	pools := db.NewPoolManager(func(inputPath string) (string, error) {
		return store.ResolveSQLitePath(cfg.SQLiteDir, cfg.AllowArbitrarySQLitePaths, inputPath)
	})
	return api.NewServer(cfg, connStore, pools, cipher, staticDir).Handler(), pools, nil
}

func serve(ctx context.Context, listener net.Listener, handler http.Handler, stop <-chan struct{}) error {
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return requestCtx },
	}

	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.Serve(listener)
	}()

	select {
	case err := <-serveErrors:
		return err
	case <-ctx.Done():
	case <-stop:
	}
	log.Println("Shutting down WebDB Studio...")
	if stop != nil {
		cancelRequests()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	if err := <-serveErrors; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func resolveStaticDir() string {
	candidates := []string{
		os.Getenv("STATIC_DIR"),
		"/app/static",
		"./static",
		"../web/dist",
		"./web/dist",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return ""
}
