package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func editorTree() *cobra.Command {
	root, sub := testCmd()
	root.Short = "root"
	root.Flags().String("bundle", "bundle.json", "model bundle path")
	root.Flags().Bool("gsi", false, "use GSI")
	sub.Short = "sync data"
	return root
}

func TestBuildViewListsFlagsFromTree(t *testing.T) {
	root := editorTree()
	path := write(t, "token: abc\nsync:\n  window-days: 7\n")
	v, err := BuildView(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Exists || v.Path != path {
		t.Fatalf("view meta: %+v", v)
	}
	if len(v.Sections) < 2 {
		t.Fatalf("sections = %d, want global+sync", len(v.Sections))
	}
	if v.Sections[0].Name != "" {
		t.Fatalf("first section name = %q, want global", v.Sections[0].Name)
	}
	var token, window *Field
	for i := range v.Sections[0].Fields {
		if v.Sections[0].Fields[i].Key == "token" {
			token = &v.Sections[0].Fields[i]
		}
	}
	for _, sec := range v.Sections {
		if sec.Name != "sync" {
			continue
		}
		for i := range sec.Fields {
			if sec.Fields[i].Key == "window-days" {
				window = &sec.Fields[i]
			}
		}
	}
	if token == nil || !token.Set || token.Value != "abc" || !token.Secret {
		t.Fatalf("token field = %+v", token)
	}
	if window == nil || !window.Set {
		t.Fatalf("window-days field = %+v", window)
	}
}

func TestSaveViewRoundTripAndClear(t *testing.T) {
	root := editorTree()
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultName)
	if err := os.WriteFile(path, []byte("token: old\norphan: keep-me\nsync:\n  window-days: 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := SaveView(root, path, SaveRequest{Sections: []SaveSection{
		{Name: "", Fields: []SaveField{
			{Key: "token", Set: true, Value: "new-token"},
			{Key: "db", Set: true, Value: "x.db"},
		}},
		{Name: "sync", Fields: []SaveField{
			{Key: "window-days", Set: false},
			{Key: "public-cm", Set: true, Value: 100},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "new-token") || !strings.Contains(body, "x.db") {
		t.Fatalf("missing globals:\n%s", body)
	}
	if !strings.Contains(body, "orphan: keep-me") {
		t.Fatalf("unknown key dropped:\n%s", body)
	}
	if strings.Contains(body, "window-days") {
		t.Fatalf("cleared key still present:\n%s", body)
	}
	if !strings.Contains(body, "public-cm: 100") {
		t.Fatalf("missing public-cm:\n%s", body)
	}
	// New flag on the tree must appear in a fresh BuildView even if unset in file.
	root.Flags().Int("brand-new-flag", 3, "appears automatically")
	v, err := BuildView(root, path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range v.Sections[0].Fields {
		if f.Key == "brand-new-flag" {
			found = true
			if f.Set {
				t.Fatal("new flag should not be set in file")
			}
		}
	}
	if !found {
		t.Fatal("new flag missing from schema view")
	}
}

func TestBuildViewNilRoot(t *testing.T) {
	if _, err := BuildView(nil, "x"); err == nil {
		t.Fatal("want error")
	}
}

func TestBuildViewReadsRootSection(t *testing.T) {
	root := editorTree()
	path := write(t, "advisor:\n  gsi: true\n")
	v, err := BuildView(root, path)
	if err != nil {
		t.Fatal(err)
	}
	var gsi *Field
	for i := range v.Sections[0].Fields {
		if v.Sections[0].Fields[i].Key == "gsi" {
			gsi = &v.Sections[0].Fields[i]
		}
	}
	if gsi == nil || !gsi.Set || gsi.Value != true {
		t.Fatalf("gsi from advisor section = %+v", gsi)
	}
}
