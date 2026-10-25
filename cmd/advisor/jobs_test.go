package main

import (
	"testing"

	"cmnet/internal/cache"
)

// The operations have prefixed their log lines by area since before the job log
// existed ("cm: ...", "stratz: ...", "warn: ..."). Reading that convention is what
// gives every line a phase and a level without touching forty call sites — so it
// has to hold for the shapes those call sites actually produce.
func TestClassifyLine(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		phase string
		level string
	}{
		{"collection band", "cm: 120/500 drafts stored", "cm", cache.LogInfo},
		{"source prefix", "stratz: fetching position meta (7 weeks)…", "stratz", cache.LogInfo},
		{"hero reference", "heroes: 126 known", "heroes", cache.LogInfo},
		{"warning maps to a level, not a phase", "warn: rank filter dropped 40%", "", cache.LogWarn},
		{"error maps to a level", "error: source unreachable", "", cache.LogError},
		{"unprefixed prose keeps no phase", "saving bundle…", "", cache.LogInfo},

		// The guards. A colon inside prose is not a prefix, and neither is a long
		// one — "training: 12000 samples, 3 features: ok" must not be chopped at its
		// second colon, and a sentence must not become a phase.
		{"prose with a colon", "collected 3 drafts: two were pro", "", cache.LogInfo},
		{"long prefix is prose", "this is a very long prefix: value", "", cache.LogInfo},
		{"colon with no space", "ratio 3:1 held", "", cache.LogInfo},
		{"empty line", "", "", cache.LogInfo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phase, level := classifyLine(tt.line)
			if phase != tt.phase {
				t.Errorf("phase = %q, want %q", phase, tt.phase)
			}
			if level != tt.level {
				t.Errorf("level = %q, want %q", level, tt.level)
			}
		})
	}
}

// The first line of a phase carries the label; the detail lines under it do not,
// and must not blank it. Checked here because the rule lives in two places — the
// classifier and the queue's tail cache — and only their combination is the
// behaviour a user sees.
func TestClassifyKeepsDetailLinesUnlabelled(t *testing.T) {
	if phase, _ := classifyLine("cm: band 7 of 20"); phase != "cm" {
		t.Fatalf("phase = %q, want cm", phase)
	}
	if phase, _ := classifyLine("  stored 43, skipped 7"); phase != "" {
		t.Errorf("phase = %q for a detail line, want empty so the label persists", phase)
	}
}
