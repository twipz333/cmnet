package gsi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigContent(t *testing.T) {
	c := ConfigContent("http://127.0.0.1:53000/", "tok")
	for _, want := range []string{
		`"uri"       "http://127.0.0.1:53000/"`,
		`"draft"`,
		`"token" "tok"`,
		`"heartbeat" "1.0"`,
	} {
		if !strings.Contains(c, want) {
			t.Errorf("config missing %q\n%s", want, c)
		}
	}
}

func TestConfigContentProductionSubscriptions(t *testing.T) {
	c := ConfigContent("http://127.0.0.1:54001/", "")
	for _, cat := range []string{
		"map", "player", "hero", "abilities", "items",
		"buildings", "minimap", "events", "draft", "provider",
	} {
		needle := `"` + cat + `"`
		if !strings.Contains(c, needle) {
			t.Errorf("production config missing subscription %s\n%s", cat, c)
		}
	}
	if strings.Contains(c, "gamestate_integration_advisor_probe") {
		t.Error("production config must not be the probe file")
	}
}

func TestConfigContentNoTokenOmitsAuth(t *testing.T) {
	c := ConfigContent("http://x/", "")
	if strings.Contains(c, "auth") {
		t.Errorf("auth block should be absent without a token:\n%s", c)
	}
}

func TestInstallWritesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path, err := Install(dir, "127.0.0.1:53000", "")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if filepath.Base(path) != CfgFileName {
		t.Errorf("filename = %s, want %s", filepath.Base(path), CfgFileName)
	}
	first, _ := os.ReadFile(path)

	if _, err := Install(dir, "127.0.0.1:53000", ""); err != nil {
		t.Fatalf("second install: %v", err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Error("install not idempotent")
	}
	if !strings.Contains(string(first), "http://127.0.0.1:53000/") {
		t.Error("uri not rendered from addr")
	}
	if !strings.Contains(string(first), `"minimap"`) {
		t.Error("reinstall must upgrade subscriptions to include minimap")
	}
}

func TestInstallCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gamestate_integration")
	if _, err := Install(dir, "127.0.0.1:1234", ""); err != nil {
		t.Fatalf("install into missing dir: %v", err)
	}
	if !dirExists(dir) {
		t.Error("install should have created the cfg dir")
	}
}

func TestInstallOverwritesLegacyNarrowConfig(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`"Dota 2 Integration Configuration"
{
    "uri"       "http://127.0.0.1:54001/"
    "data"
    {
        "provider" "1"
        "map"      "1"
        "player"   "1"
        "hero"     "1"
        "draft"    "1"
    }
}
`)
	path := filepath.Join(dir, CfgFileName)
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir, "127.0.0.1:54001", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `"minimap"`) || !strings.Contains(string(got), `"events"`) {
		t.Fatalf("legacy cfg was not upgraded:\n%s", got)
	}
}
