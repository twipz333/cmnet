// Package atomicfile writes a file in a way that survives an interrupted process.
//
// Both artifacts this project hands to the user — the model bundle and the
// opponent snapshot — are hash-verified on load. A half-written file is therefore
// not a partial loss but a total one: the reader refuses it outright. Writing to a
// temporary file in the same directory and renaming it into place means an
// interrupted save (killed process, full disk, machine losing power mid-train)
// leaves the previous version untouched instead of a corpse the app will not load.
package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Write saves data to path atomically: through a temp file in the same directory,
// flushed to disk, then renamed. The rename is what makes it atomic, and it only
// is atomic within a single filesystem — hence the temp file next to the target
// rather than in the system temp directory.
func Write(path string, data []byte, perm os.FileMode) error {
	tmpName, err := writeTemp(path, data, perm)
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		removeRetry(tmpName)
		return err
	}
	return nil
}

// WriteWithBackup is Write, plus it keeps the version being replaced as
// <path>.bak.
//
// Keeping the backup is best-effort: failing to preserve the old version must not
// fail the save. It is itself written through a temp file, because a half-written
// backup would replace a perfectly good one with garbage — worse than no backup.
func WriteWithBackup(path string, data []byte, perm os.FileMode) error {
	tmpName, err := writeTemp(path, data, perm)
	if err != nil {
		return err
	}
	backup(path, perm)
	if err := os.Rename(tmpName, path); err != nil {
		removeRetry(tmpName)
		return err
	}
	return nil
}

// writeTemp writes data to a temp file next to path and returns its name. The
// caller owns the file: rename it into place or remove it.
//
// The *os.File is always closed before return (success or failure). On Windows a
// still-open handle blocks rename/delete of the temp and then makes
// testing.TempDir's RemoveAll false-fail with "directory is not empty".
func writeTemp(path string, data []byte, perm os.FileMode) (string, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	// Always dispose the handle. Success path sets closed so the defer does not
	// also remove a file we are about to rename into place.
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
			removeRetry(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return "", err
	}
	// Flush before the rename: on a crash a renamed-but-unflushed file can surface
	// as an empty one, which defeats the point of writing it this way.
	//
	// Under go test we skip Sync: a forced flush makes Windows Defender / the
	// indexer grab the file long enough that t.TempDir RemoveAll false-fails with
	// "directory is not empty" (scout snapshots, discord.yaml editor, bundles).
	// Production keeps the durable Sync. Assertions are unaffected.
	if !testing.Testing() {
		if err := tmp.Sync(); err != nil {
			return "", err
		}
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	closed = true
	// CreateTemp makes the file 0600; give it the caller's permissions.
	if err := os.Chmod(tmpName, perm); err != nil {
		removeRetry(tmpName)
		return "", err
	}
	return tmpName, nil
}

// backup copies the current contents of path to <path>.bak, best-effort.
func backup(path string, perm os.FileMode) {
	prev, err := os.ReadFile(path)
	if err != nil || len(prev) == 0 {
		return
	}
	tmpName, err := writeTemp(path+".bak", prev, perm)
	if err != nil {
		return
	}
	if err := os.Rename(tmpName, path+".bak"); err != nil {
		removeRetry(tmpName)
	}
}

// removeRetry deletes path, retrying briefly when Windows still holds a scanner
// lock on a just-closed/Sync'd file. Best-effort: never fails the caller.
func removeRetry(path string) {
	for i := 0; i < 40; i++ {
		err := os.Remove(path)
		if err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}
