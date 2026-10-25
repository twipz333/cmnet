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
		os.Remove(tmpName)
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
		os.Remove(tmpName)
		return err
	}
	return nil
}

// writeTemp writes data to a temp file next to path and returns its name. The
// caller owns the file: rename it into place or remove it.
func writeTemp(path string, data []byte, perm os.FileMode) (string, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	// Flush before the rename: on a crash a renamed-but-unflushed file can surface
	// as an empty one, which defeats the point of writing it this way.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	// CreateTemp makes the file 0600; give it the caller's permissions.
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
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
		os.Remove(tmpName)
	}
}
