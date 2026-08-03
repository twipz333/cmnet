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
		`"draft"    "1"`,
		`"token" "tok"`,
	} {
		if !strings.Contains(c, want) {
			t.Errorf("config missing %q\n%s", want, c)
		}
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

	// Second install must overwrite with identical content (idempotent).
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
