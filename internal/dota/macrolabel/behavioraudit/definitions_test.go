package behavioraudit

import (
	"strings"
	"testing"

	"cmnet/internal/dota/action"
)

func TestAllDefinitionsCoverSpace(t *testing.T) {
	space := action.DefaultSpaceV0()
	defs := AllDefinitions()
	if len(defs) != len(space.All()) {
		t.Fatalf("defs=%d space=%d", len(defs), len(space.All()))
	}
	banned := []string{"optimal", "correct", "good play", "правильно", "оптимальн"}
	for _, d := range defs {
		if d.Definition == "" {
			t.Fatalf("empty definition for %s/%s", d.Kind, d.Target)
		}
		low := strings.ToLower(d.Definition)
		for _, b := range banned {
			if strings.Contains(low, b) {
				t.Fatalf("banned wording %q in %s", b, d.Definition)
			}
		}
	}
}

func TestVersions(t *testing.T) {
	if BehaviorAuditSchemaVersion != "behavior-audit-v0.1" {
		t.Fatal(BehaviorAuditSchemaVersion)
	}
	if BehaviorReviewerVersion != "behavior-reviewer-v0.1" {
		t.Fatal(BehaviorReviewerVersion)
	}
}