package alerts

import (
	"testing"

	"cmnet/internal/objectives"
)

func TestUpcomingIncludesCustom(t *testing.T) {
	f := DefaultFile()
	f.DefaultLead = 30
	f.Custom = []Custom{{
		ID: "mid-smoke", At: 900, Label: "Смок", TTS: "Смок через {{nextIn}}", Lead: 60, Enabled: true,
	}}
	out := Upcoming(f, objectives.DefaultObjectives(), 870)
	found := false
	for _, a := range out {
		if a.Kind == "custom:mid-smoke" {
			found = true
			if a.NextIn != 30 || !a.Due || a.TTS == "" {
				t.Fatalf("custom alert = %+v", a)
			}
		}
		if a.Kind == objectives.Lotus && a.TTS == "" {
			t.Fatalf("built-in should carry TTS template")
		}
	}
	if !found {
		t.Fatal("custom alert missing")
	}
}

func TestCustomPastSkipped(t *testing.T) {
	f := DefaultFile()
	f.Custom = []Custom{{ID: "x", At: 100, Label: "X", Enabled: true, Lead: 20}}
	out := Upcoming(f, nil, 101)
	for _, a := range out {
		if a.Kind == "custom:x" {
			t.Fatal("past custom should be omitted")
		}
	}
}
