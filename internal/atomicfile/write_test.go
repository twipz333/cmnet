package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := Write(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}

// The whole point: readers see either the old file or the new one, never a
// half-written one. Nothing may be left lying around either.
func TestWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	for range 3 {
		if err := Write(path, []byte("v"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only the target file", names)
	}
}

// A freshly written bundle that turns out worse must be swappable back by hand.
func TestWriteWithBackupKeepsThePreviousVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := Write(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteWithBackup(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(cur) != "new" {
		t.Errorf("file = %q, want the new content", cur)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("no backup written: %v", err)
	}
	if string(bak) != "old" {
		t.Errorf("backup = %q, want the version that was replaced", bak)
	}
}

// Writing where there was nothing must not invent an empty backup that a later
// save would then treat as a real previous version.
func TestWriteWithBackupSkipsWhenThereIsNothingToBackUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteWithBackup(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("a backup was created for a file that did not exist (err = %v)", err)
	}
}

// Failing to write must leave the existing file untouched — that is the property
// the hash-verified artifacts depend on.
func TestWriteToAMissingDirectoryLeavesNothingBehind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "out.json")
	if err := Write(path, []byte("x"), 0o644); err == nil {
		t.Fatal("write into a nonexistent directory should fail")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a file appeared despite the failure (err = %v)", err)
	}
}
