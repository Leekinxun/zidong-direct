package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopDoesNotReplaceMissingKeyForExistingProfiles(t *testing.T) {
	for _, name := range []string{"connections.json", "ssh_hosts.json"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			keyFile := filepath.Join(dir, ".master.key")
			if err := os.WriteFile(filepath.Join(dir, name), []byte("[]"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := desktopCipher("", keyFile); err == nil {
				t.Fatal("missing master key accepted for existing profile")
			}
			if _, err := os.Stat(keyFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected replacement key: %v", err)
			}
		})
	}
}
