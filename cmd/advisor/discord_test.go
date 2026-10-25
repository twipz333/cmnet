package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLookDiscordBinFindsCWD(t *testing.T) {
	dir := t.TempDir()
	name := discordBinaryName()
	dummy := filepath.Join(dir, name)
	if err := os.WriteFile(dummy, []byte("not-a-real-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	got, err := lookDiscordBin()
	if err != nil {
		t.Fatalf("lookDiscordBin: %v", err)
	}
	want, err := filepath.Abs(dummy)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		// On Windows Stat may return a path that isn't Abs-normalized the same way.
		if filepath.Clean(got) != filepath.Clean(want) {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestLookDiscordBinMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	if runtime.GOOS == "windows" {
		t.Setenv("PATH", t.TempDir())
	} else {
		t.Setenv("PATH", t.TempDir())
	}
	if _, err := lookDiscordBin(); err == nil {
		t.Fatal("expected error when binary is absent")
	}
}

func TestDiscordBinaryName(t *testing.T) {
	got := discordBinaryName()
	if runtime.GOOS == "windows" {
		if got != "advisor-discord.exe" {
			t.Fatalf("got %q", got)
		}
		return
	}
	if got != "advisor-discord" {
		t.Fatalf("got %q", got)
	}
}
