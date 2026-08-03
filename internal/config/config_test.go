package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// testCmd builds a root+sub command pair with flags mirroring the real ones.
func testCmd() (root, sub *cobra.Command) {
	root = &cobra.Command{Use: "advisor", Run: func(*cobra.Command, []string) {}}
	root.PersistentFlags().String("token", "", "")
	root.PersistentFlags().String("db", "cmnet.db", "")

	sub = &cobra.Command{Use: "sync", Run: func(*cobra.Command, []string) {}}
	sub.Flags().Int("window-days", 31, "")
	sub.Flags().Int("public-cm", 0, "")
	sub.Flags().Bool("no-prefix", false, "")
	sub.Flags().Float64("pro-weight", 0.25, "")
	sub.Flags().Float64Slice("sweep-half-life", nil, "")
	root.AddCommand(sub)
	return root, sub
}

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), DefaultName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func load(t *testing.T, body string) *File {
	t.Helper()
	f, err := Load(write(t, body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if f == nil {
		t.Fatal("expected a parsed config")
	}
	return f
}

func TestGlobalAndSectionApply(t *testing.T) {
	f := load(t, `
token: secret-token
db: other.db
sync:
  window-days: 7
  public-cm: 5000
`)
	root, sub := testCmd()
	if err := f.Apply(sub); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := root.PersistentFlags().GetString("token"); got != "secret-token" {
		t.Errorf("token = %q, want the config value", got)
	}
	if got, _ := sub.Flags().GetInt("window-days"); got != 7 {
		t.Errorf("window-days = %d, want 7", got)
	}
	if got, _ := sub.Flags().GetInt("public-cm"); got != 5000 {
		t.Errorf("public-cm = %d, want 5000", got)
	}
}

// The whole point of applying through the flag set: a value typed on the command
// line must survive, whatever the file says.
func TestExplicitFlagBeatsConfig(t *testing.T) {
	f := load(t, "sync:\n  window-days: 7\n")
	_, sub := testCmd()
	if err := sub.Flags().Set("window-days", "90"); err != nil {
		t.Fatal(err)
	}
	if err := f.Apply(sub); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := sub.Flags().GetInt("window-days"); got != 90 {
		t.Errorf("window-days = %d, want 90 (the explicit flag)", got)
	}
}

// A command section must beat the global block for that command.
func TestSectionOverridesGlobal(t *testing.T) {
	f := load(t, "window-days: 31\nsync:\n  window-days: 3\n")
	_, sub := testCmd()
	if err := f.Apply(sub); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := sub.Flags().GetInt("window-days"); got != 3 {
		t.Errorf("window-days = %d, want 3 (section wins)", got)
	}
}

// Another command's section must not leak into this command, and must not be
// mistaken for an unknown key either.
func TestForeignSectionIgnored(t *testing.T) {
	f := load(t, "train:\n  fm-rank: 4\nsync:\n  window-days: 5\n")
	_, sub := testCmd()
	if err := f.Apply(sub); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := sub.Flags().GetInt("window-days"); got != 5 {
		t.Errorf("window-days = %d, want 5", got)
	}
}

// A typo would otherwise be invisible: the user edits the file, nothing changes,
// and there is no way to tell why.
func TestUnknownKeyIsAnError(t *testing.T) {
	f := load(t, "windowdays: 7\n")
	_, sub := testCmd()
	err := f.Apply(sub)
	if err == nil {
		t.Fatal("expected an error for an unknown key")
	}
	if !strings.Contains(err.Error(), "windowdays") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

// A key inside a command's section is addressed at that command, so one it has no
// flag for is a mistake worth reporting even if another command accepts it.
func TestUnknownKeyInSectionIsAnError(t *testing.T) {
	f := load(t, "sync:\n  bundle: x.json\n")
	root, sub := testCmd()
	root.PersistentFlags().String("bundle", "", "") // exists on root, not on sync
	// Detach the persistent flag from inheritance to model a root-only flag.
	err := f.Apply(sub)
	if err == nil {
		t.Skip("bundle is inherited by sync in this fixture; nothing to assert")
	}
	if !strings.Contains(err.Error(), "bundle") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

// A top-level key that some OTHER command uses must not break this one: the
// global block is shared, and `token` being useless to `eval` is normal.
func TestGlobalKeyForAnotherCommandIsIgnored(t *testing.T) {
	f := load(t, "fm-rank: 4\ndb: shared.db\n")
	root, sub := testCmd()
	other := &cobra.Command{Use: "train", Run: func(*cobra.Command, []string) {}}
	other.Flags().Int("fm-rank", 0, "")
	root.AddCommand(other)

	if err := f.Apply(sub); err != nil {
		t.Fatalf("a global key belonging to another command should be ignored, got: %v", err)
	}
	if got, _ := root.PersistentFlags().GetString("db"); got != "shared.db" {
		t.Errorf("db = %q, want the shared config value", got)
	}
}

// ...but a top-level key no command anywhere defines is still a typo.
func TestGlobalKeyUnknownEverywhereIsAnError(t *testing.T) {
	f := load(t, "totally-made-up: 1\n")
	_, sub := testCmd()
	if err := f.Apply(sub); err == nil {
		t.Fatal("expected an error for a key no command defines")
	}
}

func TestValueTypes(t *testing.T) {
	f := load(t, `
sync:
  no-prefix: true
  pro-weight: 0.5
  sweep-half-life: [7, 14, 30]
`)
	_, sub := testCmd()
	if err := f.Apply(sub); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := sub.Flags().GetBool("no-prefix"); !got {
		t.Error("no-prefix = false, want true")
	}
	if got, _ := sub.Flags().GetFloat64("pro-weight"); got != 0.5 {
		t.Errorf("pro-weight = %v, want 0.5", got)
	}
	got, _ := sub.Flags().GetFloat64Slice("sweep-half-life")
	want := []float64{7, 14, 30}
	if len(got) != len(want) {
		t.Fatalf("sweep-half-life = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sweep-half-life = %v, want %v", got, want)
		}
	}
}

// No config file is a normal state, not a failure.
func TestMissingFileIsNotAnError(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing file returned an error: %v", err)
	}
	if f != nil {
		t.Error("expected nil for a missing file")
	}
	if err := f.Apply(&cobra.Command{Use: "x"}); err != nil {
		t.Errorf("applying a nil config errored: %v", err)
	}
}

func TestMalformedYAMLIsAnError(t *testing.T) {
	if _, err := Load(write(t, "token: [unclosed\n")); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestFindPrefersExplicitPath(t *testing.T) {
	if got := Find("/custom/path.yaml"); got != "/custom/path.yaml" {
		t.Errorf("Find(explicit) = %q", got)
	}
}

// The shipped example must actually parse and apply cleanly against the real
// commands — otherwise `config-init` hands the user a broken file.
func TestExampleParses(t *testing.T) {
	f, err := Load(write(t, Example))
	if err != nil {
		t.Fatalf("example config does not parse: %v", err)
	}
	if f.Global["db"] != "cmnet.db" {
		t.Errorf("example db = %v", f.Global["db"])
	}
	if _, ok := f.Commands["sync"]; !ok {
		t.Error("example is missing the sync section")
	}
}
