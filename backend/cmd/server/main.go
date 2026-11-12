package main

import (
	"context"
	"errors"
	"log"
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
	cfg := config.Load()

	if err := os.MkdirAll(cfg.SQLiteDir, 0o750); err != nil {
		log.Fatalf("create sqlite dir: %v", err)
	}

	cipher, err := crypto.NewCipher(cfg.SecretKey, filepath.Join(cfg.MetaDir, ".master.key"))
	if err != nil {
		log.Fatalf("init cipher: %v", err)
	}

	connStore, err := store.NewConnectionStore(cfg.MetaDir, cipher)
	if err != nil {
		log.Fatalf("init connection store: %v", err)
	}
	if err := connStore.ValidateCredentials(); err != nil {
		log.Fatalf("validate stored connection credentials: %v", err)
	}

	sshHosts, err := store.NewSSHHostStore(cfg.MetaDir, cipher)
	if err != nil {
		log.Fatalf("init SSH host store: %v", err)
	}

	pools := db.NewPoolManager(func(inputPath string) (string, error) {
		return store.ResolveSQLitePath(cfg.SQLiteDir, cfg.AllowArbitrarySQLitePaths, inputPath)
	})

	staticDir := resolveStaticDir()
	srv := api.NewServer(cfg, connStore, pools, cipher, staticDir, sshHosts)

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("WebDB Studio listening on :%s (auth=%v, sqliteDir=%s, staticDir=%s)",
			cfg.Port, cfg.AuthEnabled, cfg.SQLiteDir, staticDir)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down WebDB Studio...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	pools.CloseAll()
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
