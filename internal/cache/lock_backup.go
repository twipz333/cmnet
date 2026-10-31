package cache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrDatabaseBusy means another process (typically Electron or another advisor)
// already holds the SQLite write lock. Callers must refuse concurrent use of the
// same cmnet.db and ask the user to quit the other client.
var ErrDatabaseBusy = errors.New("database is locked by another process")

// ProbeLock tries to take an exclusive SQLite lock with a short busy timeout.
// It does not apply schema migrations and leaves the file unchanged on success.
func ProbeLock(path string) error {
	if path == "" {
		return errors.New("database path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	db, err := openRaw(path, 50*time.Millisecond)
	if err != nil {
		return mapBusy(err)
	}
	defer func() { _ = releaseDB(db) }()

	// BEGIN IMMEDIATE acquires a reserved write lock without committing data.
	if _, err := db.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		return mapBusy(err)
	}
	if _, err := db.ExecContext(context.Background(), `ROLLBACK`); err != nil {
		return mapBusy(err)
	}
	return nil
}

// BackupPath writes a consistent VACUUM INTO snapshot of srcPath to destPath
// without applying schema migrations to the source. Refuses to overwrite destPath.
// Use this for Electron→native migration backups so an older Electron client can
// still open the original file after restore.
func BackupPath(ctx context.Context, srcPath, destPath string) error {
	if srcPath == "" || destPath == "" {
		return errors.New("backup path is empty")
	}
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("backup exists: %s", destPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
		return err
	}
	if err := ProbeLock(srcPath); err != nil {
		return err
	}
	db, err := openRaw(srcPath, busyTimeout)
	if err != nil {
		return mapBusy(err)
	}
	defer func() { _ = releaseDB(db) }()

	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", destPath); err != nil {
		_ = os.Remove(destPath)
		return mapBusy(fmt.Errorf("SQLite consistent backup: %w", err))
	}
	check, err := openRaw(destPath, busyTimeout)
	if err != nil {
		_ = os.Remove(destPath)
		return err
	}
	defer func() { _ = releaseDB(check) }()
	var integrity string
	if err := check.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		_ = os.Remove(destPath)
		return err
	}
	if integrity != "ok" {
		_ = os.Remove(destPath)
		return fmt.Errorf("backup integrity check failed: %s", integrity)
	}
	return nil
}

func openRaw(path string, busy time.Duration) (*sql.DB, error) {
	esc := (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()
	dsn := "file:" + esc +
		"?_pragma=" + url.QueryEscape(fmt.Sprintf("busy_timeout(%d)", busy.Milliseconds())) +
		"&_pragma=" + url.QueryEscape("journal_mode(WAL)")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = releaseDB(db)
		return nil, mapBusy(err)
	}
	return db, nil
}

func mapBusy(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrDatabaseBusy) {
		return err
	}
	if isBusyErr(err) {
		return fmt.Errorf("%w: quit Electron / other cmnet before opening this folder", ErrDatabaseBusy)
	}
	return err
}

func isBusyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "table is locked")
}
