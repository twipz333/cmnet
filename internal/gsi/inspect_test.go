package gsi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectReportsMissingInstall(t *testing.T) {
	dir := t.TempDir()
	gotDir, path, detected, installed, err := Inspect(dir)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if gotDir != dir {
		t.Fatalf("dir = %q, want %q", gotDir, dir)
	}
	if filepath.Base(path) != CfgFileName {
		t.Fatalf("path base = %s", filepath.Base(path))
	}
	if !detected {
		t.Fatal("temp dir should count as detected")
	}
	if installed {
		t.Fatal("empty dir must not report installed")
	}
}

func TestInspectReportsInstalledAfterWrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, "127.0.0.1:54001", ""); err != nil {
		t.Fatal(err)
	}
	_, path, detected, installed, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !detected || !installed {
		t.Fatalf("detected=%v installed=%v", detected, installed)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestCandidateCfgDirsNonEmpty(t *testing.T) {
	if len(CandidateCfgDirs()) == 0 {
		t.Fatal("expected Steam candidate paths")
	}
}
