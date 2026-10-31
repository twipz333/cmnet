// Package datamigrate copies an existing Electron (or prior advisor) data folder
// into the native data directory with a timestamped backup and an honest rollback
// path. It never invents cache rows, bundles, or scout evidence.
package datamigrate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Known sidecar files next to cmnet.db that a captain expects to keep.
// Only files that actually exist are copied — absence is reported, never invented.
var artifactFiles = []string{
	"bundle.json",
	"snapshot.json",
	"config.yaml",
	"team.json",
	"heroes.json",
}

const (
	dbFileName       = "cmnet.db"
	backupRootName   = "migration-backup"
	manifestName     = "MANIFEST.json"
	assetsDirName    = "assets"
	desktopJSONName  = "desktop.json"
)

// AppDataRoot returns %APPDATA%/cmnet on Windows (and the XDG-equivalent elsewhere),
// matching desktop/datadirs.js and native/DesktopSettings.cs.
func AppDataRoot() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			return "", errors.New("APPDATA is unset")
		}
		return filepath.Join(base, "cmnet"), nil
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "cmnet"), nil
}

// DefaultNativeDataDir is %APPDATA%/cmnet/data — the native/Electron default.
func DefaultNativeDataDir() (string, error) {
	root, err := AppDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "data"), nil
}

// DesktopSettingsPath is the shared Electron/native settings file.
func DesktopSettingsPath() (string, error) {
	root, err := AppDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, desktopJSONName), nil
}

// ReadDesktopDataDir returns the dataDir from desktop.json when present and real.
func ReadDesktopDataDir() (string, bool, error) {
	path, err := DesktopSettingsPath()
	if err != nil {
		return "", false, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	var s struct {
		DataDir string `json:"dataDir"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false, nil
	}
	dir := strings.TrimSpace(s.DataDir)
	if dir == "" {
		return "", false, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false, err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return abs, false, nil
	}
	return abs, true, nil
}

// WriteDesktopDataDir updates dataDir + setupDone in desktop.json (create if needed).
func WriteDesktopDataDir(dataDir string) error {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	path, err := DesktopSettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	settings := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	settings["dataDir"] = abs
	settings["setupDone"] = true
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DescribeDir reports whether a folder looks like a cmnet data directory.
type DescribeDir struct {
	Dir       string    `json:"dir"`
	HasDB     bool      `json:"hasDb"`
	DBBytes   int64     `json:"dbBytes,omitempty"`
	Modified  time.Time `json:"modified,omitempty"`
	HasBundle bool      `json:"hasBundle"`
	Artifacts []string  `json:"artifacts,omitempty"`
}

// Describe inspects dir for cmnet.db and known artifacts. Missing files are omitted.
func Describe(dir string) (DescribeDir, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return DescribeDir{}, err
	}
	out := DescribeDir{Dir: abs}
	db := filepath.Join(abs, dbFileName)
	if st, err := os.Stat(db); err == nil && st.Mode().IsRegular() {
		out.HasDB = true
		out.DBBytes = st.Size()
		out.Modified = st.ModTime().UTC()
	}
	out.HasBundle = fileExists(filepath.Join(abs, "bundle.json"))
	for _, name := range artifactFiles {
		if fileExists(filepath.Join(abs, name)) {
			out.Artifacts = append(out.Artifacts, name)
		}
	}
	if dirExists(filepath.Join(abs, assetsDirName)) {
		out.Artifacts = append(out.Artifacts, assetsDirName+"/")
	}
	return out, nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}
