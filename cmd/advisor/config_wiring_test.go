package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"cmnet/internal/config"
)

// writeExample drops the shipped example config into a temp dir.
func writeExample(t *testing.T) *config.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), config.DefaultName)
	if err := os.WriteFile(path, []byte(config.Example), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := config.Load(path)
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	if f == nil {
		t.Fatal("example config parsed to nil")
	}
	return f
}

// The example config must apply cleanly to EVERY real command. This is the test
// that would have caught the example shipping a `sync:` key that sync has no flag
// for, or a section named after a command that does not exist — mistakes that are
// invisible until a user runs that one subcommand.
func TestExampleAppliesToEveryCommand(t *testing.T) {
	root := newRootCmd()
	f := writeExample(t)

	var check func(cmd *cobra.Command)
	check = func(cmd *cobra.Command) {
		if err := f.Apply(cmd); err != nil {
			t.Errorf("example config rejected by %q: %v", cmd.Name(), err)
		}
		for _, sub := range cmd.Commands() {
			check(sub)
		}
	}
	check(root)
}

// Every section in the example must name a command that actually exists,
// otherwise the settings in it silently never apply.
func TestExampleSectionsNameRealCommands(t *testing.T) {
	root := newRootCmd()
	known := map[string]bool{root.Name(): true}
	for _, sub := range root.Commands() {
		known[sub.Name()] = true
	}

	f := writeExample(t)
	for name := range f.Commands {
		if !known[name] {
			t.Errorf("example has a %q section, but no such command exists", name)
		}
	}
}

// GSI is configurable from the file, not just from flags — the reason this test
// exists is that the example originally omitted it, so a user would reasonably
// conclude it was unsupported.
func TestGSIConfigurableFromFile(t *testing.T) {
	root := newRootCmd()
	var serve *cobra.Command
	for _, sub := range root.Commands() {
		if sub.Name() == "serve" {
			serve = sub
		}
	}
	if serve == nil {
		t.Fatal("serve command not found")
	}

	path := filepath.Join(t.TempDir(), config.DefaultName)
	body := "serve:\n  gsi: true\n  gsi-addr: 127.0.0.1:9999\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Apply(serve); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := serve.Flags().GetBool("gsi"); !got {
		t.Error("gsi = false, want true from the config file")
	}
	if got, _ := serve.Flags().GetString("gsi-addr"); got != "127.0.0.1:9999" {
		t.Errorf("gsi-addr = %q, want the config value", got)
	}
}

// The root command's own section is named after the command, and must reach the
// TUI home's flags.
func TestRootSectionAppliesToHome(t *testing.T) {
	root := newRootCmd()
	path := filepath.Join(t.TempDir(), config.DefaultName)
	body := "advisor:\n  gsi: true\n  side: dire\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Apply(root); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := root.Flags().GetBool("gsi"); !got {
		t.Error("gsi = false, want true from the advisor section")
	}
	if got, _ := root.Flags().GetString("side"); got != "dire" {
		t.Errorf("side = %q, want dire", got)
	}
}
