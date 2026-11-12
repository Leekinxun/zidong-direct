package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"web-db-client/internal/crypto"
	"web-db-client/internal/db"
)

func TestSSHHostEncryptsPrivateKeyAndPassphrase(t *testing.T) {
	dir := t.TempDir()
	cipher, err := crypto.NewCipher("fixture key", "")
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := NewSSHHostStore(dir, cipher)
	if err != nil {
		t.Fatal(err)
	}
	cfg := db.SSHConfig{
		Host: "127.0.0.1", Port: 22, Username: "fixture", AuthType: "privateKey",
		PrivateKey: "fixture-private-key", Passphrase: "fixture-passphrase", Password: "unused-password",
	}
	view, err := hosts.Create("fixture", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasPrivateKey || !view.HasPassphrase || view.HasPassword {
		t.Fatalf("unexpected authentication metadata: %+v", view)
	}
	persisted, err := os.ReadFile(filepath.Join(dir, "ssh_hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{cfg.PrivateKey, cfg.Passphrase, cfg.Password} {
		if bytes.Contains(persisted, []byte(secret)) {
			t.Fatal("found unencrypted credential")
		}
	}
	reloaded, err := NewSSHHostStore(dir, cipher)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := reloaded.Get(view.ID)
	if err != nil || saved.PrivateKey != cfg.PrivateKey || saved.Passphrase != cfg.Passphrase || saved.Password != "" {
		t.Fatalf("unable to recover selected credentials: %v", err)
	}
	other, err := crypto.NewCipher("wrong fixture key", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSSHHostStore(dir, other); err == nil {
		t.Fatal("opened encrypted profiles with a replacement key")
	}
}

func TestSSHHostSaveFailureKeepsExistingProfiles(t *testing.T) {
	cipher, err := crypto.NewCipher("fixture key", "")
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := NewSSHHostStore(t.TempDir(), cipher)
	if err != nil {
		t.Fatal(err)
	}
	cfg := db.SSHConfig{Host: "127.0.0.1", Port: 22, Username: "fixture", AuthType: "password", Password: "fixture"}
	view, err := hosts.Create("original", cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Renaming the temporary file onto a directory fails after writing.
	hosts.filePath = t.TempDir()
	if _, err := hosts.Create("unsaved", cfg); err == nil {
		t.Fatal("unexpected create success")
	}
	if err := hosts.Delete(view.ID); err == nil {
		t.Fatal("unexpected delete success")
	}
	views := hosts.List()
	if len(views) != 1 || views[0].ID != view.ID {
		t.Fatal("failed write changed the visible host list")
	}
}
