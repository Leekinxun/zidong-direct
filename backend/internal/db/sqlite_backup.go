package db

import (
	"context"
	"database/sql"
	"errors"

	"modernc.org/sqlite"
)

// BackupSQLite copies a consistent snapshot, including committed WAL pages.
func BackupSQLite(ctx context.Context, sourcePath, destinationPath string) error {
	sourceURL := sqliteFileURL(sourcePath)
	sourceURL.RawQuery = "mode=ro&_pragma=busy_timeout(5000)"
	source, err := sql.Open("sqlite", sourceURL.String())
	if err != nil {
		return err
	}
	defer source.Close()
	conn, err := source.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	destinationURL := sqliteFileURL(destinationPath)
	return conn.Raw(func(driverConn any) (err error) {
		backupSource, ok := driverConn.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		})
		if !ok {
			return errors.New("SQLite driver does not support online backup")
		}
		backup, err := backupSource.NewBackup(destinationURL.String())
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, backup.Finish()) }()

		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(128)
			if err != nil || !more {
				return err
			}
		}
	})
}
