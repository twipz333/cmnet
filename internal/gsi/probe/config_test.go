package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigContentRequestsAllKnownCategories(t *testing.T) {
	c := ConfigContent("http://127.0.0.1:32123/")
	if !strings.Contains(c, `"uri"           "http://127.0.0.1:32123/"`) {
		t.Errorf("uri missing:\n%s", c)
	}
	required := []string{
		"provider", "map", "player", "hero", "abilities", "items",
		"buildings", "draft", "minimap", "roshan", "couriers",
		"neutralitems", "events", "league", "wearables",
	}
	for _, cat := range required {
		needle := `"` + cat + `"`
		if !strings.Contains(c, needle) {
			t.Errorf("config missing category %s\n%s", cat, c)
		}
	}
	if !strings.Contains(c, `"heartbeat"     "5.0"`) {
		t.Errorf("heartbeat missing:\n%s", c)
	}
}

func TestWriteConfigToFileAndDirectory(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "custom.cfg")
	got, err := WriteConfig(filePath, "127.0.0.1:32123")
	if err != nil {
		t.Fatal(err)
	}
	if got != filePath {
		t.Errorf("path = %s, want %s", got, filePath)
	}
	data, _ := os.ReadFile(filePath)
	if !strings.Contains(string(data), "http://127.0.0.1:32123/") {
		t.Errorf("uri not in written file:\n%s", data)
	}

	outDir := filepath.Join(dir, "cfg")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = WriteConfig(outDir, DefaultListen)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != CfgFileName {
		t.Errorf("filename = %s, want %s", filepath.Base(got), CfgFileName)
	}
}

func TestWriteConfigEmptyPath(t *testing.T) {
	if _, err := WriteConfig("", DefaultListen); err == nil {
		t.Fatal("empty path should fail")
	}
}

func TestWarnIfNonLoopback(t *testing.T) {
	if msg := WarnIfNonLoopback("127.0.0.1:32123"); msg != "" {
		t.Errorf("loopback warned: %s", msg)
	}
	if msg := WarnIfNonLoopback("0.0.0.0:32123"); msg == "" {
		t.Error("0.0.0.0 should warn")
	}
	if msg := WarnIfNonLoopback("192.168.1.5:32123"); msg == "" {
		t.Error("LAN address should warn")
	}
}

func TestWriteConfigStripsPowerShellJunk(t *testing.T) {
	dir := t.TempDir()
	// PowerShell `--write-config "C:\dir\"` leaves a trailing quote on the
	// argument because the backslash escapes the closing quote.
	got, err := WriteConfig(dir+`\"`, DefaultListen)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != CfgFileName {
		t.Errorf("filename = %s, want %s", filepath.Base(got), CfgFileName)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeConfigPath(t *testing.T) {
	got := sanitizeConfigPath(`E:\Steam\gamestate_integration"`)
	if strings.ContainsAny(got, `"'`) {
		t.Errorf("quote survived: %q", got)
	}
	if strings.HasSuffix(got, `\`) || strings.HasSuffix(got, `/`) {
		t.Errorf("trailing slash survived: %q", got)
	}
}
