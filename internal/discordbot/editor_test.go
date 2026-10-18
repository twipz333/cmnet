package discordbot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAndSaveEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discord.yaml")
	v, err := BuildEditorView(path)
	if err != nil {
		t.Fatal(err)
	}
	if v.Exists {
		t.Fatal("new path should not exist")
	}
	if len(v.Sections) < 3 {
		t.Fatalf("sections=%d", len(v.Sections))
	}
	req := EditorSaveRequest{Sections: []EditorSaveSection{
		{Name: "", Fields: []EditorSaveField{
			{Key: "token", Set: true, Value: "bot-tok"},
			{Key: "guild-id", Set: true, Value: "g1"},
			{Key: "role-id", Set: true, Value: "r1"},
			{Key: "advisor-token", Set: true, Value: "sec"},
		}},
		{Name: "ntts", Fields: []EditorSaveField{
			{Key: "token", Set: true, Value: "ntts-tok"},
			{Key: "speaker", Set: true, Value: "kleiner"},
		}},
		{Name: "templates", Fields: []EditorSaveField{
			{Key: "power", Set: true, Value: "Power {{nextIn}}"},
		}},
	}}
	if err := SaveEditor(path, req); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{"bot-tok", "kleiner", "Power {{nextIn}}", "ntts-tok"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NTTS.Speaker != "kleiner" || cfg.NTTS.Templates["power"] != "Power {{nextIn}}" {
		t.Fatalf("%+v", cfg.NTTS)
	}
}