package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"web-db-client/internal/crypto"
	"web-db-client/internal/db"
	"web-db-client/internal/store"
)

func TestResolveSQLitePathRejectsEscapingFileLink(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	if err := os.Mkdir(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.db")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(managed, "link.db")); err != nil {
		t.Skipf("test user cannot create a symlink: %v", err)
	}
	if _, err := store.ResolveSQLitePath(managed, false, "link.db"); err == nil {
		t.Fatal("SQLite file symlink escapes the managed directory")
	}
}

func TestConnectionStoreEncryptsSecretsAndPreservesOnUpdate(t *testing.T) {
	tmp := t.TempDir()
	c, err := crypto.NewCipher("", filepath.Join(tmp, ".master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.NewConnectionStore(tmp, c)
	if err != nil {
		t.Fatal(err)
	}

	view, err := st.Create(db.ConnectionInput{
		Name:     "Prod PG",
		Engine:   db.EnginePostgres,
		Host:     "10.0.0.5",
		Port:     5432,
		Database: "app",
		Username: "readonly",
		Password: "db-secret-password",
		SSH: db.SSHInput{
			Enabled:               true,
			Host:                  "bastion.internal",
			Port:                  22,
			Username:              "ops",
			AuthType:              "privateKey",
			PrivateKey:            "PRIVATE-KEY-MATERIAL",
			Passphrase:            "key-passphrase",
			InsecureIgnoreHostKey: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasPassword || !view.SSH.HasPrivateKey || !view.SSH.HasPassphrase {
		t.Fatalf("expected secret presence booleans true, got %+v", view)
	}

	// Update host without re-entering password or SSH key
	updated, err := st.Update(view.ID, db.ConnectionInput{
		Name:     "Prod PG Renamed",
		Engine:   db.EnginePostgres,
		Host:     "10.0.0.6",
		Port:     5432,
		Database: "app",
		Username: "readonly",
		SSH: db.SSHInput{
			Enabled:               true,
			Host:                  "bastion.internal",
			Port:                  22,
			Username:              "ops",
			AuthType:              "privateKey",
			InsecureIgnoreHostKey: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.HasPassword || !updated.SSH.HasPrivateKey || !updated.SSH.HasPassphrase {
		t.Fatalf("expected preserved secrets on update, got %+v", updated)
	}

	full, err := st.Get(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Password != "db-secret-password" || full.SSH.PrivateKey != "PRIVATE-KEY-MATERIAL" || full.SSH.Passphrase != "key-passphrase" {
		t.Fatalf("decrypted secrets mismatch: %+v", full)
	}
}
