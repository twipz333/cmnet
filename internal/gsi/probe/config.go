package probe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CfgFileName is the GSI config filename Dota loads from gamestate_integration.
// It is deliberately distinct from the live advisor's gamestate_integration_advisor.cfg:
// the probe asks for every known data category, which we do not want to silently
// widen on the live endpoint.
const CfgFileName = "gamestate_integration_advisor_probe.cfg"

// DefaultListen is the loopback endpoint the probe binds when --listen is omitted.
// Not 0.0.0.0: a capture tool has no reason to be reachable off-machine unless
// the user says so.
const DefaultListen = "127.0.0.1:32123"

// DataCategories is the widest set of Dota 2 GSI data blocks catalogued by
// antonpup/Dota2GSI (the most complete public source). Probe requests all of
// them because its job is discovery, not a curated live-advisor subset.
//
// "previously" / "added" are not config keys: Dota attaches them on its own
// when something changes. They arrive inside the raw payload and are stored
// as-is.
var DataCategories = []string{
	"auth",
	"provider",
	"map",
	"player",
	"hero",
	"abilities",
	"items",
	"events",
	"buildings",
	"league",
	"draft",
	"wearables",
	"minimap",
	"roshan",
	"couriers",
	"neutralitems",
}

// URI returns the GSI endpoint URL for a listen address.
func URI(listen string) string {
	return "http://" + listen + "/"
}

// ConfigContent renders the Valve KeyValues config that points Dota at uri.
//
// buffer/throttle are 0.1s so change-driven updates are as frequent as GSI
// allows. heartbeat is 5s rather than the live advisor's 1s so a quiet stretch
// of a match still produces samples we can tell apart from real diffs.
func ConfigContent(uri string) string {
	var b strings.Builder
	b.WriteString("\"Advisor GSI Probe\"\n")
	b.WriteString("{\n")
	fmt.Fprintf(&b, "    \"uri\"           \"%s\"\n", uri)
	b.WriteString("    \"timeout\"       \"5.0\"\n")
	b.WriteString("    \"buffer\"        \"0.1\"\n")
	b.WriteString("    \"throttle\"      \"0.1\"\n")
	b.WriteString("    \"heartbeat\"     \"5.0\"\n")
	b.WriteString("    \"data\"\n")
	b.WriteString("    {\n")
	for _, cat := range DataCategories {
		fmt.Fprintf(&b, "        %-18s \"1\"\n", `"`+cat+`"`)
	}
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

// WriteConfig writes the probe GSI config to path. If path is a directory
// (existing, or ending with a path separator), the canonical filename is
// placed inside it.
//
// Trailing quotes and backslashes are stripped: PowerShell turns
// `--write-config "C:\dir\"` into a path that ends with a quote because
// `\"` escapes the closing quote.
func WriteConfig(path, listen string) (string, error) {
	path = sanitizeConfigPath(path)
	if path == "" {
		return "", fmt.Errorf("write-config: path is empty")
	}
	if asDir(path) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return "", fmt.Errorf("create config dir: %w", err)
		}
		path = filepath.Join(path, CfgFileName)
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(ConfigContent(URI(listen))), 0o644); err != nil {
		return "", fmt.Errorf("write GSI config: %w", err)
	}
	return path, nil
}

func sanitizeConfigPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"'`)
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func asDir(path string) bool {
	info, err := os.Stat(path)
	if err == nil {
		return info.IsDir()
	}
	// No extension and not an existing file: treat as the GSI cfg directory
	// even if the leaf folder does not exist yet.
	return filepath.Ext(path) == ""
}
