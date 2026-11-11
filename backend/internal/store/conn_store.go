package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"web-db-client/internal/crypto"
	"web-db-client/internal/db"
)

type persistedConnection struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Engine         db.EngineType `json:"engine"`
	Host           string        `json:"host"`
	Port           int           `json:"port"`
	Database       string        `json:"database"`
	Username       string        `json:"username"`
	PasswordEnc    string        `json:"passwordEnc,omitempty"`
	SSLMode        string        `json:"sslMode,omitempty"`
	TrustServerCer bool          `json:"trustServerCert,omitempty"`
	OracleMode     string        `json:"oracleMode,omitempty"`
	SQLitePath     string        `json:"sqlitePath,omitempty"`
	SSH            persistedSSH  `json:"ssh"`
	Color          string        `json:"color,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

type persistedSSH struct {
	Enabled               bool   `json:"enabled"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	AuthType              string `json:"authType"`
	PasswordEnc           string `json:"passwordEnc,omitempty"`
	PrivateKeyEnc         string `json:"privateKeyEnc,omitempty"`
	PassphraseEnc         string `json:"passphraseEnc,omitempty"`
	HostKeyFingerprint    string `json:"hostKeyFingerprint,omitempty"`
	InsecureIgnoreHostKey bool   `json:"insecureIgnoreHostKey"`
}

type ConnectionStore struct {
	mu          sync.RWMutex
	filePath    string
	cipher      *crypto.Cipher
	connections map[string]persistedConnection
}

func NewConnectionStore(metaDir string, c *crypto.Cipher) (*ConnectionStore, error) {
	if err := os.MkdirAll(metaDir, 0o700); err != nil {
		return nil, fmt.Errorf("create meta dir: %w", err)
	}
	s := &ConnectionStore{
		filePath:    filepath.Join(metaDir, "connections.json"),
		cipher:      c,
		connections: make(map[string]persistedConnection),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ConnectionStore) load() error {
	raw, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read connections store: %w", err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var list []persistedConnection
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("parse connections store: %w", err)
	}
	for _, item := range list {
		s.connections[item.ID] = item
	}
	return nil
}

func (s *ConnectionStore) saveLocked() error {
	list := make([]persistedConnection, 0, len(s.connections))
	for _, item := range s.connections {
		list = append(list, item)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].Name < list[j].Name
		}
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.filePath)
}

func (s *ConnectionStore) decryptConnection(p persistedConnection) (db.ConnectionConfig, error) {
	pass, err := s.cipher.Decrypt(p.PasswordEnc)
	if err != nil {
		return db.ConnectionConfig{}, fmt.Errorf("decrypt password for %s: %w", p.ID, err)
	}
	sshPass, err := s.cipher.Decrypt(p.SSH.PasswordEnc)
	if err != nil {
		return db.ConnectionConfig{}, fmt.Errorf("decrypt ssh password for %s: %w", p.ID, err)
	}
	sshKey, err := s.cipher.Decrypt(p.SSH.PrivateKeyEnc)
	if err != nil {
		return db.ConnectionConfig{}, fmt.Errorf("decrypt ssh private key for %s: %w", p.ID, err)
	}
	sshPhrase, err := s.cipher.Decrypt(p.SSH.PassphraseEnc)
	if err != nil {
		return db.ConnectionConfig{}, fmt.Errorf("decrypt ssh passphrase for %s: %w", p.ID, err)
	}
	return db.ConnectionConfig{
		ID:             p.ID,
		Name:           p.Name,
		Engine:         p.Engine,
		Host:           p.Host,
		Port:           p.Port,
		Database:       p.Database,
		Username:       p.Username,
		Password:       pass,
		SSLMode:        p.SSLMode,
		TrustServerCer: p.TrustServerCer,
		OracleMode:     p.OracleMode,
		SQLitePath:     p.SQLitePath,
		SSH: db.SSHConfig{
			Enabled:               p.SSH.Enabled,
			Host:                  p.SSH.Host,
			Port:                  p.SSH.Port,
			Username:              p.SSH.Username,
			AuthType:              p.SSH.AuthType,
			Password:              sshPass,
			PrivateKey:            sshKey,
			Passphrase:            sshPhrase,
			HostKeyFingerprint:    p.SSH.HostKeyFingerprint,
			InsecureIgnoreHostKey: p.SSH.InsecureIgnoreHostKey,
		},
		Color:     p.Color,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}, nil
}

func (s *ConnectionStore) encryptFromInput(in db.ConnectionInput, existing *persistedConnection) (persistedConnection, error) {
	now := time.Now().UTC()
	id := strings.TrimSpace(in.ID)
	createdAt := now
	if existing != nil {
		id = existing.ID
		createdAt = existing.CreatedAt
	} else if id == "" {
		id = newID()
	}

	var err error
	passEnc := ""
	if existing != nil && !in.ClearPassword && in.Password == "" {
		passEnc = existing.PasswordEnc
	} else if in.Password != "" {
		if passEnc, err = s.cipher.Encrypt(in.Password); err != nil {
			return persistedConnection{}, err
		}
	}

	sshPassEnc := ""
	if existing != nil && !in.SSH.ClearPassword && in.SSH.Password == "" {
		sshPassEnc = existing.SSH.PasswordEnc
	} else if in.SSH.Password != "" {
		if sshPassEnc, err = s.cipher.Encrypt(in.SSH.Password); err != nil {
			return persistedConnection{}, err
		}
	}

	sshKeyEnc := ""
	if existing != nil && !in.SSH.ClearPrivateKey && in.SSH.PrivateKey == "" {
		sshKeyEnc = existing.SSH.PrivateKeyEnc
	} else if in.SSH.PrivateKey != "" {
		if sshKeyEnc, err = s.cipher.Encrypt(in.SSH.PrivateKey); err != nil {
			return persistedConnection{}, err
		}
	}

	sshPhraseEnc := ""
	if existing != nil && !in.SSH.ClearPassphrase && in.SSH.Passphrase == "" {
		sshPhraseEnc = existing.SSH.PassphraseEnc
	} else if in.SSH.Passphrase != "" {
		if sshPhraseEnc, err = s.cipher.Encrypt(in.SSH.Passphrase); err != nil {
			return persistedConnection{}, err
		}
	}

	sshPort := in.SSH.Port
	if sshPort <= 0 {
		sshPort = 22
	}
	authType := in.SSH.AuthType
	if authType == "" {
		authType = "password"
	}

	return persistedConnection{
		ID:             id,
		Name:           strings.TrimSpace(in.Name),
		Engine:         in.Engine,
		Host:           strings.TrimSpace(in.Host),
		Port:           in.Port,
		Database:       strings.TrimSpace(in.Database),
		Username:       strings.TrimSpace(in.Username),
		PasswordEnc:    passEnc,
		SSLMode:        strings.TrimSpace(in.SSLMode),
		TrustServerCer: in.TrustServerCer,
		OracleMode:     strings.TrimSpace(in.OracleMode),
		SQLitePath:     strings.TrimSpace(in.SQLitePath),
		SSH: persistedSSH{
			Enabled:               in.SSH.Enabled,
			Host:                  strings.TrimSpace(in.SSH.Host),
			Port:                  sshPort,
			Username:              strings.TrimSpace(in.SSH.Username),
			AuthType:              authType,
			PasswordEnc:           sshPassEnc,
			PrivateKeyEnc:         sshKeyEnc,
			PassphraseEnc:         sshPhraseEnc,
			HostKeyFingerprint:    strings.TrimSpace(in.SSH.HostKeyFingerprint),
			InsecureIgnoreHostKey: in.SSH.InsecureIgnoreHostKey,
		},
		Color:     strings.TrimSpace(in.Color),
		CreatedAt: createdAt,
		UpdatedAt: now,
	}, nil
}

func (s *ConnectionStore) ListViews() ([]db.ConnectionView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	views := make([]db.ConnectionView, 0, len(s.connections))
	for _, item := range s.connections {
		views = append(views, db.ConnectionView{
			ID:             item.ID,
			Name:           item.Name,
			Engine:         item.Engine,
			Host:           item.Host,
			Port:           item.Port,
			Database:       item.Database,
			Username:       item.Username,
			HasPassword:    item.PasswordEnc != "",
			SSLMode:        item.SSLMode,
			TrustServerCer: item.TrustServerCer,
			OracleMode:     item.OracleMode,
			SQLitePath:     item.SQLitePath,
			SSH: db.SSHView{
				Enabled:               item.SSH.Enabled,
				Host:                  item.SSH.Host,
				Port:                  item.SSH.Port,
				Username:              item.SSH.Username,
				AuthType:              item.SSH.AuthType,
				HostKeyFingerprint:    item.SSH.HostKeyFingerprint,
				InsecureIgnoreHostKey: item.SSH.InsecureIgnoreHostKey,
				HasPassword:           item.SSH.PasswordEnc != "",
				HasPrivateKey:         item.SSH.PrivateKeyEnc != "",
				HasPassphrase:         item.SSH.PassphraseEnc != "",
			},
			Color:     item.Color,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].CreatedAt.Equal(views[j].CreatedAt) {
			return views[i].Name < views[j].Name
		}
		return views[i].CreatedAt.Before(views[j].CreatedAt)
	})
	return views, nil
}

func (s *ConnectionStore) Get(id string) (db.ConnectionConfig, error) {
	s.mu.RLock()
	item, ok := s.connections[id]
	s.mu.RUnlock()
	if !ok {
		return db.ConnectionConfig{}, fmt.Errorf("connection %q not found", id)
	}
	return s.decryptConnection(item)
}

func (s *ConnectionStore) ValidateCredentials() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.connections {
		if _, err := s.decryptConnection(item); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConnectionStore) Create(in db.ConnectionInput) (db.ConnectionView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.encryptFromInput(in, nil)
	if err != nil {
		return db.ConnectionView{}, err
	}
	s.connections[p.ID] = p
	if err := s.saveLocked(); err != nil {
		delete(s.connections, p.ID)
		return db.ConnectionView{}, err
	}
	cfg, err := s.decryptConnection(p)
	if err != nil {
		return db.ConnectionView{}, err
	}
	return cfg.ToView(), nil
}

func (s *ConnectionStore) Update(id string, in db.ConnectionInput) (db.ConnectionView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.connections[id]
	if !ok {
		return db.ConnectionView{}, fmt.Errorf("connection %q not found", id)
	}
	p, err := s.encryptFromInput(in, &existing)
	if err != nil {
		return db.ConnectionView{}, err
	}
	s.connections[id] = p
	if err := s.saveLocked(); err != nil {
		s.connections[id] = existing
		return db.ConnectionView{}, err
	}
	cfg, err := s.decryptConnection(p)
	if err != nil {
		return db.ConnectionView{}, err
	}
	return cfg.ToView(), nil
}

func (s *ConnectionStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.connections[id]
	if !ok {
		return fmt.Errorf("connection %q not found", id)
	}
	delete(s.connections, id)
	if err := s.saveLocked(); err != nil {
		s.connections[id] = existing
		return err
	}
	return nil
}

// ResolveRuntimeConfig converts a ConnectionInput into a complete ConnectionConfig,
// reusing saved secrets when testing edits without re-entering passwords.
func (s *ConnectionStore) ResolveRuntimeConfig(in db.ConnectionInput) (db.ConnectionConfig, error) {
	if strings.TrimSpace(in.ID) != "" {
		existing, err := s.Get(in.ID)
		if err == nil {
			cfg := existing
			cfg.Name = strings.TrimSpace(in.Name)
			cfg.Engine = in.Engine
			cfg.Host = strings.TrimSpace(in.Host)
			cfg.Port = in.Port
			cfg.Database = strings.TrimSpace(in.Database)
			cfg.Username = strings.TrimSpace(in.Username)
			cfg.SSLMode = strings.TrimSpace(in.SSLMode)
			cfg.TrustServerCer = in.TrustServerCer
			cfg.OracleMode = strings.TrimSpace(in.OracleMode)
			cfg.SQLitePath = strings.TrimSpace(in.SQLitePath)
			if in.ClearPassword {
				cfg.Password = ""
			} else if in.Password != "" {
				cfg.Password = in.Password
			}
			cfg.SSH.Enabled = in.SSH.Enabled
			cfg.SSH.Host = strings.TrimSpace(in.SSH.Host)
			if in.SSH.Port > 0 {
				cfg.SSH.Port = in.SSH.Port
			} else if cfg.SSH.Port <= 0 {
				cfg.SSH.Port = 22
			}
			cfg.SSH.Username = strings.TrimSpace(in.SSH.Username)
			if in.SSH.AuthType != "" {
				cfg.SSH.AuthType = in.SSH.AuthType
			}
			cfg.SSH.HostKeyFingerprint = strings.TrimSpace(in.SSH.HostKeyFingerprint)
			cfg.SSH.InsecureIgnoreHostKey = in.SSH.InsecureIgnoreHostKey
			if in.SSH.ClearPassword {
				cfg.SSH.Password = ""
			} else if in.SSH.Password != "" {
				cfg.SSH.Password = in.SSH.Password
			}
			if in.SSH.ClearPrivateKey {
				cfg.SSH.PrivateKey = ""
			} else if in.SSH.PrivateKey != "" {
				cfg.SSH.PrivateKey = in.SSH.PrivateKey
			}
			if in.SSH.ClearPassphrase {
				cfg.SSH.Passphrase = ""
			} else if in.SSH.Passphrase != "" {
				cfg.SSH.Passphrase = in.SSH.Passphrase
			}
			return cfg, nil
		}
	}

	sshPort := in.SSH.Port
	if sshPort <= 0 {
		sshPort = 22
	}
	return db.ConnectionConfig{
		ID:             strings.TrimSpace(in.ID),
		Name:           strings.TrimSpace(in.Name),
		Engine:         in.Engine,
		Host:           strings.TrimSpace(in.Host),
		Port:           in.Port,
		Database:       strings.TrimSpace(in.Database),
		Username:       strings.TrimSpace(in.Username),
		Password:       in.Password,
		SSLMode:        strings.TrimSpace(in.SSLMode),
		TrustServerCer: in.TrustServerCer,
		OracleMode:     strings.TrimSpace(in.OracleMode),
		SQLitePath:     strings.TrimSpace(in.SQLitePath),
		SSH: db.SSHConfig{
			Enabled:               in.SSH.Enabled,
			Host:                  strings.TrimSpace(in.SSH.Host),
			Port:                  sshPort,
			Username:              strings.TrimSpace(in.SSH.Username),
			AuthType:              in.SSH.AuthType,
			Password:              in.SSH.Password,
			PrivateKey:            in.SSH.PrivateKey,
			Passphrase:            in.SSH.Passphrase,
			HostKeyFingerprint:    strings.TrimSpace(in.SSH.HostKeyFingerprint),
			InsecureIgnoreHostKey: in.SSH.InsecureIgnoreHostKey,
		},
	}, nil
}

// ResolveSQLitePath enforces strict containment within sqliteDir unless allowArbitrary is true.
func ResolveSQLitePath(sqliteDir string, allowArbitrary bool, inputPath string) (string, error) {
	cleaned := strings.TrimSpace(inputPath)
	if cleaned == "" {
		return "", errors.New("SQLite file path cannot be empty")
	}
	if strings.ContainsRune(cleaned, '\x00') {
		return "", errors.New("invalid SQLite file path")
	}

	baseAbs, err := filepath.Abs(sqliteDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(baseAbs, 0o750); err != nil {
		return "", err
	}
	baseReal, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return "", err
	}

	var candidate string
	if filepath.IsAbs(cleaned) {
		candidate = filepath.Clean(cleaned)
	} else {
		if !allowArbitrary && !filepath.IsLocal(cleaned) {
			return "", errors.New("invalid relative SQLite file path")
		}
		candidate = filepath.Join(baseReal, filepath.Clean(cleaned))
	}

	if allowArbitrary {
		return filepath.Abs(candidate)
	}

	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}

	// Resolve the file itself when it exists, including symlinks and junctions.
	// A new file is allowed only under an existing, resolved parent directory.
	if _, err := os.Lstat(absCandidate); errors.Is(err, os.ErrNotExist) {
		parent, err := filepath.EvalSymlinks(filepath.Dir(absCandidate))
		if err != nil {
			return "", err
		}
		absCandidate = filepath.Join(parent, filepath.Base(absCandidate))
	} else if err != nil {
		return "", err
	} else {
		absCandidate, err = filepath.EvalSymlinks(absCandidate)
		if err != nil {
			return "", err
		}
	}

	rel, err := filepath.Rel(baseReal, absCandidate)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("SQLite path %q must stay inside configured SQLITE_DIR (%s)", inputPath, baseReal)
	}
	return absCandidate, nil
}

func ListSQLiteFiles(sqliteDir string) ([]db.SQLiteFileEntry, error) {
	baseAbs, err := filepath.Abs(sqliteDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(baseAbs, 0o750); err != nil {
		return nil, err
	}
	var entries []db.SQLiteFileEntry
	err = filepath.WalkDir(baseAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".db" && ext != ".sqlite" && ext != ".sqlite3" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(baseAbs, path)
		if err != nil {
			return nil
		}
		entries = append(entries, db.SQLiteFileEntry{
			Name:         d.Name(),
			RelativePath: filepath.ToSlash(rel),
			Size:         info.Size(),
			ModifiedAt:   info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RelativePath < entries[j].RelativePath
	})
	return entries, nil
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
