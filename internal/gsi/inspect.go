package gsi

import (
	"os"
	"path/filepath"
)

// Inspect reports whether the advisor GSI cfg is present in cfgDir (or the
// first autodected Dota gamestate_integration directory when cfgDir is empty).
// It never invents a successful install: Detected/Installed are independent.
func Inspect(cfgDir string) (dir string, cfgPath string, detected, installed bool, detectErr error) {
	if cfgDir == "" {
		detectedDir, err := DetectCfgDir()
		if err != nil {
			return "", "", false, false, err
		}
		cfgDir = detectedDir
	}
	detected = dirExists(cfgDir) || dirExists(filepath.Dir(cfgDir))
	cfgPath = filepath.Join(cfgDir, CfgFileName)
	if info, err := os.Stat(cfgPath); err == nil && !info.IsDir() {
		installed = true
	}
	return cfgDir, cfgPath, detected, installed, nil
}

// CandidateCfgDirs exposes the search list for UI instructions when autodetect fails.
func CandidateCfgDirs() []string {
	return candidateCfgDirs()
}
