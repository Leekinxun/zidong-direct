package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"web-db-client/internal/crypto"
	"web-db-client/internal/db"
)

var ErrSSHHostNotFound = errors.New("SSH host not found")

type SSHHostView struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	AuthType              string `json:"authType"`
	HostKeyFingerprint    string `json:"hostKeyFingerprint,omitempty"`
	InsecureIgnoreHostKey bool   `json:"insecureIgnoreHostKey"`
	HasPassword           bool   `json:"hasPassword"`
	HasPrivateKey         bool   `json:"hasPrivateKey"`
	HasPassphrase         bool   `json:"hasPassphrase"`
}

type persistedSSHHost struct {
	ID   string       `json:"id"`
	Name string       `json:"name"`
	SSH  persistedSSH `json:"ssh"`
}

func (host persistedSSHHost) view() SSHHostView {
	return SSHHostView{
		ID:                    host.ID,
		Name:                  host.Name,
		Host:                  host.SSH.Host,
		Port:                  host.SSH.Port,
		Username:              host.SSH.Username,
		AuthType:              host.SSH.AuthType,
		HostKeyFingerprint:    host.SSH.HostKeyFingerprint,
		InsecureIgnoreHostKey: host.SSH.InsecureIgnoreHostKey,
		HasPassword:           host.SSH.PasswordEnc != "",
		HasPrivateKey:         host.SSH.PrivateKeyEnc != "",
		HasPassphrase:         host.SSH.PassphraseEnc != "",
	}
}

type SSHHostStore struct {
	mu       sync.RWMutex
	filePath string
	cipher   *crypto.Cipher
	hosts    map[string]persistedSSHHost
}

func NewSSHHostStore(metaDir string, cipher *crypto.Cipher) (*SSHHostStore, error) {
	if err := os.MkdirAll(metaDir, 0o700); err != nil {
		return nil, err
	}
	s := &SSHHostStore{
		filePath: filepath.Join(metaDir, "ssh_hosts.json"),
		cipher:   cipher,
		hosts:    make(map[string]persistedSSHHost),
	}
	raw, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var hosts []persistedSSHHost
	if err := json.Unmarshal(raw, &hosts); err != nil {
		return nil, err
	}
	for _, host := range hosts {
		if host.ID == "" {
			return nil, errors.New("SSH host profile has no ID")
		}
		if _, exists := s.hosts[host.ID]; exists {
			return nil, errors.New("duplicate SSH host ID")
		}
		if _, err := s.decrypt(host); err != nil {
			return nil, fmt.Errorf("validate SSH host credentials: %w", err)
		}
		s.hosts[host.ID] = host
	}
	return s, nil
}

func (s *SSHHostStore) List() []SSHHostView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	views := make([]SSHHostView, 0, len(s.hosts))
	for _, host := range s.hosts {
		views = append(views, host.view())
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Name == views[j].Name {
			return views[i].ID < views[j].ID
		}
		return views[i].Name < views[j].Name
	})
	return views
}

func (s *SSHHostStore) Get(id string) (db.SSHConfig, error) {
	s.mu.RLock()
	host, ok := s.hosts[id]
	s.mu.RUnlock()
	if !ok {
		return db.SSHConfig{}, ErrSSHHostNotFound
	}
	return s.decrypt(host)
}

func (s *SSHHostStore) decrypt(host persistedSSHHost) (db.SSHConfig, error) {
	data := host.SSH
	password, err := s.cipher.Decrypt(data.PasswordEnc)
	if err != nil {
		return db.SSHConfig{}, err
	}
	privateKey, err := s.cipher.Decrypt(data.PrivateKeyEnc)
	if err != nil {
		return db.SSHConfig{}, err
	}
	passphrase, err := s.cipher.Decrypt(data.PassphraseEnc)
	if err != nil {
		return db.SSHConfig{}, err
	}
	return db.SSHConfig{
		Enabled:               true,
		Host:                  data.Host,
		Port:                  data.Port,
		Username:              data.Username,
		AuthType:              data.AuthType,
		Password:              password,
		PrivateKey:            privateKey,
		Passphrase:            passphrase,
		HostKeyFingerprint:    data.HostKeyFingerprint,
		InsecureIgnoreHostKey: data.InsecureIgnoreHostKey,
	}, nil
}

func (s *SSHHostStore) Create(name string, cfg db.SSHConfig) (SSHHostView, error) {
	host := persistedSSHHost{
		Name: name,
		SSH: persistedSSH{
			Enabled:               true,
			Host:                  cfg.Host,
			Port:                  cfg.Port,
			Username:              cfg.Username,
			AuthType:              cfg.AuthType,
			HostKeyFingerprint:    cfg.HostKeyFingerprint,
			InsecureIgnoreHostKey: cfg.InsecureIgnoreHostKey,
		},
	}
	var err error
	switch cfg.AuthType {
	case "password":
		host.SSH.PasswordEnc, err = s.cipher.Encrypt(cfg.Password)
	case "privateKey":
		host.SSH.PrivateKeyEnc, err = s.cipher.Encrypt(cfg.PrivateKey)
		if err == nil {
			host.SSH.PassphraseEnc, err = s.cipher.Encrypt(cfg.Passphrase)
		}
	default:
		return SSHHostView{}, errors.New("invalid SSH authentication method")
	}
	if err != nil {
		return SSHHostView{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	host.ID = newID()
	for s.hosts[host.ID].ID != "" {
		host.ID = newID()
	}
	s.hosts[host.ID] = host
	if err := s.saveLocked(); err != nil {
		delete(s.hosts, host.ID)
		return SSHHostView{}, err
	}
	return host.view(), nil
}

func (s *SSHHostStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	host, ok := s.hosts[id]
	if !ok {
		return nil
	}
	delete(s.hosts, id)
	if err := s.saveLocked(); err != nil {
		s.hosts[id] = host
		return err
	}
	return nil
}

func (s *SSHHostStore) saveLocked() error {
	hosts := make([]persistedSSHHost, 0, len(s.hosts))
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].ID < hosts[j].ID })
	file, err := os.CreateTemp(filepath.Dir(s.filePath), "ssh_hosts-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(hosts); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.filePath)
}
