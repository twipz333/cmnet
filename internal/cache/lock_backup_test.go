package cache

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestProbeLockAllowsIdleDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cmnet.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := ProbeLock(path); err != nil {
		t.Fatalf("idle db should be free: %v", err)
	}
}

func TestProbeLockDetectsHeldWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cmnet.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	holder, err := openRaw(path, busyTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = holder.Exec(`ROLLBACK`)
		_ = releaseDB(holder)
	}()
	if _, err := holder.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	err = ProbeLock(path)
	if !errors.Is(err, ErrDatabaseBusy) {
		t.Fatalf("expected ErrDatabaseBusy, got %v", err)
	}
}

func TestBackupPathNoMigrateAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "cmnet.db")
	s, err := Open(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("marker", "electron"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	dst := filepath.Join(dir, "backup", "cmnet.db")
	if err := BackupPath(context.Background(), src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	v, ok, err := got.GetMeta("marker")
	if err != nil || !ok || v != "electron" {
		t.Fatalf("backup lost meta: %q %v %v", v, ok, err)
	}
}
