package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupSQLiteIncludesCommittedWAL(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "测试 source #.db")
	sourceURL := sqliteFileURL(sourcePath)
	source, err := sql.Open("sqlite", sourceURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA wal_autocheckpoint=0;
		CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT);
		INSERT INTO notes VALUES (1, 'committed in WAL');
	`); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(sourcePath + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("expected active WAL: %v", err)
	}

	dest := filepath.Join(dir, "测试 backup #.db")
	if err := BackupSQLite(context.Background(), sourcePath, dest); err != nil {
		t.Fatal(err)
	}
	// A download includes only the main snapshot file.
	download := filepath.Join(t.TempDir(), "download.db")
	if err := os.Rename(dest, download); err != nil {
		t.Fatal(err)
	}
	backupURL := sqliteFileURL(download)
	backupURL.RawQuery = "mode=ro"
	backup, err := sql.Open("sqlite", backupURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var title string
	if err := backup.QueryRow("SELECT title FROM notes WHERE id = 1").Scan(&title); err != nil || title != "committed in WAL" {
		t.Fatalf("unexpected backup row %q: %v", title, err)
	}
}
