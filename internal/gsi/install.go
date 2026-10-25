package gsi

import (
	"fmt"
	"os"
	"path/filepath"
)

// CfgFileName is the GSI config file Dota loads from its gamestate_integration dir.
const CfgFileName = "gamestate_integration_advisor.cfg"

// candidateCfgDirs lists common Windows Steam locations for the Dota 2
// gamestate_integration config directory.
func candidateCfgDirs() []string {
	const tail = `steamapps\common\dota 2 beta\game\dota\cfg\gamestate_integration`
	roots := []string{
		`C:\Program Files (x86)\Steam`,
		`C:\Program Files\Steam`,
		`D:\Steam`,
		`D:\SteamLibrary`,
		`E:\SteamLibrary`,
	}
	dirs := make([]string, 0, len(roots))
	for _, r := range roots {
		dirs = append(dirs, filepath.Join(r, tail))
	}
	return dirs
}

// DetectCfgDir returns the first existing Dota gamestate_integration directory, or
// an error listing the locations searched.
func DetectCfgDir() (string, error) {
	for _, d := range candidateCfgDirs() {
		// The gamestate_integration leaf may not exist yet; accept its parent (cfg)
		// so we can create the leaf.
		if dirExists(d) {
			return d, nil
		}
		if parent := filepath.Dir(d); dirExists(parent) {
			return d, nil
		}
	}
	return "", fmt.Errorf("could not autodetect Dota cfg dir; pass --cfg-dir explicitly")
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// ConfigContent renders the GSI config (Valve KeyValues) pointing Dota at uri.
// token, when non-empty, is echoed back by Dota in the "auth" block so the server
// can verify events originate from the local client.
func ConfigContent(uri, token string) string {
	auth := ""
	if token != "" {
		auth = fmt.Sprintf("    \"auth\"\n    {\n        \"token\" \"%s\"\n    }\n", token)
	}
	return fmt.Sprintf(`"Dota 2 Integration Configuration"
{
    "uri"       "%s"
    "timeout"   "5.0"
    "buffer"    "0.1"
    "throttle"  "0.1"
    "heartbeat" "30.0"
%s    "data"
    {
        "provider" "1"
        "map"      "1"
        "player"   "1"
        "hero"     "1"
        "draft"    "1"
    }
}
`, uri, auth)
}

// Install writes the GSI config into cfgDir (autodetected when empty) so Dota
// POSTs draft state to the advisor at addr. It is idempotent. Returns the written
// path.
func Install(cfgDir, addr, token string) (string, error) {
	if cfgDir == "" {
		detected, err := DetectCfgDir()
		if err != nil {
			return "", err
		}
		cfgDir = detected
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return "", fmt.Errorf("create cfg dir: %w", err)
	}
	uri := fmt.Sprintf("http://%s/", addr)
	path := filepath.Join(cfgDir, CfgFileName)
	if err := os.WriteFile(path, []byte(ConfigContent(uri, token)), 0o644); err != nil {
		return "", fmt.Errorf("write cfg: %w", err)
	}
	return path, nil
}
