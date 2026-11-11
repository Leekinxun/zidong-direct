package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"web-db-client/internal/ssh"
)

type pooledEntry struct {
	poolKey     string
	connID      string
	fingerprint string
	db          *sql.DB
	tunnel      *ssh.Tunnel
	lastUsed    time.Time
}

type PoolManager struct {
	mu             sync.Mutex
	entries        map[string]*pooledEntry
	sqliteResolver func(inputPath string) (string, error)
}

func NewPoolManager(sqliteResolver func(inputPath string) (string, error)) *PoolManager {
	return &PoolManager{
		entries:        make(map[string]*pooledEntry),
		sqliteResolver: sqliteResolver,
	}
}

func (m *PoolManager) Acquire(ctx context.Context, cfg ConnectionConfig, databaseOverride string) (*sql.DB, Dialect, error) {
	d, err := GetDialect(cfg.Engine)
	if err != nil {
		return nil, nil, err
	}

	resolvedCfg := cfg
	if cfg.Engine == EngineSQLite && m.sqliteResolver != nil {
		resolvedPath, err := m.sqliteResolver(cfg.SQLitePath)
		if err != nil {
			return nil, nil, err
		}
		resolvedCfg.SQLitePath = resolvedPath
	}

	dbTarget := strings.TrimSpace(databaseOverride)
	if dbTarget == "" {
		dbTarget = resolvedCfg.Database
	}

	fp := configFingerprint(resolvedCfg, dbTarget)
	key := resolvedCfg.ID + "::" + dbTarget

	if resolvedCfg.ID != "" {
		m.mu.Lock()
		existing, ok := m.entries[key]
		if ok && existing.fingerprint == fp {
			existing.lastUsed = time.Now()
			dbHandle := existing.db
			m.mu.Unlock()
			return dbHandle, d, nil
		}
		if ok {
			delete(m.entries, key)
			m.mu.Unlock()
			closeEntry(existing)
		} else {
			m.mu.Unlock()
		}
	}

	entry, err := m.openNew(ctx, resolvedCfg, d, dbTarget, key, fp)
	if err != nil {
		return nil, nil, err
	}

	if resolvedCfg.ID == "" {
		// Ad-hoc test connection: caller is responsible for closing, or use TestConnection.
		return entry.db, d, nil
	}

	m.mu.Lock()
	if prev, ok := m.entries[key]; ok {
		delete(m.entries, key)
		go closeEntry(prev)
	}
	m.entries[key] = entry
	m.mu.Unlock()
	return entry.db, d, nil
}

func (m *PoolManager) TestConnection(ctx context.Context, cfg ConnectionConfig) (int64, error) {
	d, err := GetDialect(cfg.Engine)
	if err != nil {
		return 0, err
	}
	resolvedCfg := cfg
	if cfg.Engine == EngineSQLite && m.sqliteResolver != nil {
		resolvedPath, err := m.sqliteResolver(cfg.SQLitePath)
		if err != nil {
			return 0, err
		}
		resolvedCfg.SQLitePath = resolvedPath
	}

	start := time.Now()
	entry, err := m.openNew(ctx, resolvedCfg, d, resolvedCfg.Database, "", "")
	if err != nil {
		return 0, err
	}
	defer closeEntry(entry)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := entry.db.PingContext(pingCtx); err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

func (m *PoolManager) InvalidateConnection(connID string) {
	m.mu.Lock()
	var toClose []*pooledEntry
	for k, e := range m.entries {
		if e.connID == connID {
			toClose = append(toClose, e)
			delete(m.entries, k)
		}
	}
	m.mu.Unlock()
	for _, e := range toClose {
		closeEntry(e)
	}
}

func (m *PoolManager) CloseAll() {
	m.mu.Lock()
	toClose := make([]*pooledEntry, 0, len(m.entries))
	for k, e := range m.entries {
		toClose = append(toClose, e)
		delete(m.entries, k)
	}
	m.mu.Unlock()
	for _, e := range toClose {
		closeEntry(e)
	}
}

func (m *PoolManager) openNew(ctx context.Context, cfg ConnectionConfig, d Dialect, dbOverride, key, fp string) (*pooledEntry, error) {
	targetHost := cfg.Host
	targetPort := cfg.Port
	if targetPort <= 0 {
		targetPort = d.Capabilities().DefaultPort
	}

	var tunnel *ssh.Tunnel
	if cfg.SSH.Enabled && cfg.Engine != EngineSQLite {
		var err error
		tunnel, err = ssh.OpenTunnel(ctx, ssh.Spec{
			Host:                  cfg.SSH.Host,
			Port:                  cfg.SSH.Port,
			Username:              cfg.SSH.Username,
			AuthType:              cfg.SSH.AuthType,
			Password:              cfg.SSH.Password,
			PrivateKey:            cfg.SSH.PrivateKey,
			Passphrase:            cfg.SSH.Passphrase,
			HostKeyFingerprint:    cfg.SSH.HostKeyFingerprint,
			InsecureIgnoreHostKey: cfg.SSH.InsecureIgnoreHostKey,
			TargetHost:            targetHost,
			TargetPort:            targetPort,
		})
		if err != nil {
			return nil, err
		}
		targetHost = tunnel.LocalHost()
		targetPort = tunnel.LocalPort()
	}

	dsn, err := d.BuildDSN(cfg, targetHost, targetPort, dbOverride)
	if err != nil {
		if tunnel != nil {
			_ = tunnel.Close()
		}
		return nil, err
	}

	handle, err := sql.Open(d.DriverName(), dsn)
	if err != nil {
		if tunnel != nil {
			_ = tunnel.Close()
		}
		return nil, err
	}

	if cfg.Engine == EngineSQLite {
		handle.SetMaxOpenConns(1)
		handle.SetMaxIdleConns(1)
	} else {
		handle.SetMaxOpenConns(5)
		handle.SetMaxIdleConns(2)
		handle.SetConnMaxLifetime(10 * time.Minute)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := handle.PingContext(pingCtx); err != nil {
		_ = handle.Close()
		if tunnel != nil {
			_ = tunnel.Close()
		}
		return nil, fmt.Errorf("connect %s: %w", cfg.Engine, err)
	}

	return &pooledEntry{
		poolKey:     key,
		connID:      cfg.ID,
		fingerprint: fp,
		db:          handle,
		tunnel:      tunnel,
		lastUsed:    time.Now(),
	}, nil
}

func closeEntry(e *pooledEntry) {
	if e == nil {
		return
	}
	if e.db != nil {
		_ = e.db.Close()
	}
	if e.tunnel != nil {
		_ = e.tunnel.Close()
	}
}

func configFingerprint(cfg ConnectionConfig, dbOverride string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, strings.Join([]string{
		string(cfg.Engine),
		cfg.Host,
		strconv.Itoa(cfg.Port),
		dbOverride,
		cfg.Username,
		cfg.Password,
		cfg.SSLMode,
		strconv.FormatBool(cfg.TrustServerCer),
		cfg.OracleMode,
		cfg.SQLitePath,
		strconv.FormatBool(cfg.SSH.Enabled),
		cfg.SSH.Host,
		strconv.Itoa(cfg.SSH.Port),
		cfg.SSH.Username,
		cfg.SSH.AuthType,
		cfg.SSH.Password,
		cfg.SSH.PrivateKey,
		cfg.SSH.Passphrase,
		cfg.SSH.HostKeyFingerprint,
		strconv.FormatBool(cfg.SSH.InsecureIgnoreHostKey),
	}, "\x00"))
	return hex.EncodeToString(h.Sum(nil))
}
