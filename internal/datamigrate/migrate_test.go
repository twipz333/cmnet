package datamigrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cmnet/internal/cache"
)

func seedDataDir(t *testing.T, dir string, meta string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := cache.Open(filepath.Join(dir, "cmnet.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("who", meta); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.WriteFile(filepath.Join(dir, "bundle.json"), []byte(`{"bundle_version":"`+meta+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), []byte(`{"ok":true,"tag":"`+meta+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "team.json"), []byte(`{"name":"`+meta+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets", "heroes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "heroes", "1.png"), []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMeta(t *testing.T, dir string) string {
	t.Helper()
	s, err := cache.Open(filepath.Join(dir, "cmnet.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, ok, err := s.GetMeta("who")
	if err != nil || !ok {
		t.Fatalf("meta: %v %v", v, err)
	}
	return v
}

func TestMigrateCopyBackupRollback(t *testing.T) {
	root := t.TempDir()
	electron := filepath.Join(root, "electron-data")
	native := filepath.Join(root, "native-data")
	seedDataDir(t, electron, "electron")

	ctx := context.Background()
	res, err := Run(ctx, Options{
		Source:        electron,
		Dest:          native,
		Stamp:         "20260101-120000",
		UpdateDesktop: false,
		SkipAssets:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if readMeta(t, native) != "electron" {
		t.Fatalf("native db not migrated")
	}
	for _, name := range []string{"bundle.json", "snapshot.json", "team.json", "assets/heroes/1.png"} {
		if !fileExists(filepath.Join(native, filepath.FromSlash(name))) {
			t.Fatalf("missing copied artifact %s", name)
		}
	}
	backup := BackupDir(native, "20260101-120000")
	if _, err := ReadManifest(backup); err != nil {
		t.Fatal(err)
	}
	if res.Backup.Stamp != "20260101-120000" {
		t.Fatalf("stamp: %+v", res.Backup)
	}

	// Overwrite native, migrate again (backs up clobbered dest), then rollback that backup.
	seedDataDir(t, native, "clobbered")
	if readMeta(t, native) != "clobbered" {
		t.Fatal("setup clobber failed")
	}
	if _, err := Run(ctx, Options{
		Source:     electron,
		Dest:       native,
		Stamp:      "20260101-130000",
		SkipAssets: true,
	}); err != nil {
		t.Fatal(err)
	}
	if readMeta(t, native) != "electron" {
		t.Fatal("second migrate failed")
	}
	if _, err := Rollback(ctx, BackupDir(native, "20260101-130000"), native); err != nil {
		t.Fatal(err)
	}
	if readMeta(t, native) != "clobbered" {
		t.Fatalf("rollback did not restore clobbered dest, got %q", readMeta(t, native))
	}
}

func TestSameFolderBackupOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	seedDataDir(t, dir, "shared")
	res, err := Run(context.Background(), Options{
		Source: dir,
		Dest:   dir,
		Stamp:  "same-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.SameDir {
		t.Fatal("expected same dir")
	}
	if len(res.Copied) != 0 {
		t.Fatalf("same-folder must not re-copy: %v", res.Copied)
	}
	if !fileExists(filepath.Join(BackupDir(dir, "same-1"), "cmnet.db")) {
		t.Fatal("expected backup db")
	}
	if readMeta(t, dir) != "shared" {
		t.Fatal("source must stay intact")
	}
}

func TestRefuseEmptySource(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty")
	_ = os.MkdirAll(empty, 0o755)
	_, err := Run(context.Background(), Options{Source: empty, Dest: filepath.Join(t.TempDir(), "d"), Stamp: "x"})
	if err == nil {
		t.Fatal("empty source must fail")
	}
}

func TestInspectNeedsMigrate(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "e")
	dst := filepath.Join(root, "n")
	seedDataDir(t, src, "e")
	st, err := Inspect(Options{Source: src, Dest: dst})
	if err != nil {
		t.Fatal(err)
	}
	if !st.NeedsMigrate || st.SameFolder {
		t.Fatalf("status: %+v", st)
	}
}
