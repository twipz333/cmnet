package behavioraudit

import (
	"strconv"
	"strings"
	"testing"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

func TestHumanActionNameKnown(t *testing.T) {
	space := action.DefaultSpaceV0()
	a, err := space.New(action.KindSmoke, action.TargetBot)
	if err != nil {
		t.Fatal(err)
	}
	name := HumanActionName(space, a.ID)
	if name != "SMOKE BOT" {
		t.Fatalf("got %q want SMOKE BOT", name)
	}
	if strings.Contains(strings.ToLower(name), "action ") {
		t.Fatalf("numeric leakage: %q", name)
	}
}

func TestHumanActionNameGroup(t *testing.T) {
	space := action.DefaultSpaceV0()
	a, err := space.New(action.KindGroup, action.TargetNone)
	if err != nil {
		t.Fatal(err)
	}
	name := HumanActionName(space, a.ID)
	if name != "GROUP" {
		t.Fatalf("got %q want GROUP", name)
	}
}

func TestResolveActionViewUnknown(t *testing.T) {
	space := action.DefaultSpaceV0()
	v := ResolveActionView(space, 99999)
	if v.Known {
		t.Fatal("expected unknown")
	}
	if !strings.Contains(v.ActionName, "UNKNOWN ACTION") || !strings.Contains(v.ActionName, "99999") {
		t.Fatalf("unexpected name %q", v.ActionName)
	}
}

func TestCheckMacroSchema(t *testing.T) {
	ok, want := CheckMacroSchema(action.SchemaVersion)
	if !ok || want != action.SchemaVersion {
		t.Fatalf("ok=%v want=%s", ok, want)
	}
	ok, _ = CheckMacroSchema("macro-action-v9.9")
	if ok {
		t.Fatal("expected mismatch")
	}
	ok, _ = CheckMacroSchema("")
	if ok {
		t.Fatal("empty should mismatch")
	}
}

func TestValidateCasesMacroSchema(t *testing.T) {
	okCases := []AuditCase{{ID: "a", MacroActionSchemaVersion: action.SchemaVersion}}
	if err := ValidateCasesMacroSchema(okCases); err != nil {
		t.Fatal(err)
	}
	bad := []AuditCase{{ID: "b", MacroActionSchemaVersion: "macro-action-v9.9"}}
	if err := ValidateCasesMacroSchema(bad); err == nil {
		t.Fatal("expected schema mismatch error")
	}
	legacy := []AuditCase{{ID: "c"}} // empty version — warn only
	if err := ValidateCasesMacroSchema(legacy); err != nil {
		t.Fatal(err)
	}
}

func TestAmbiguousNamedCandidates(t *testing.T) {
	space := action.DefaultSpaceV0()
	smoke, err := space.New(action.KindSmoke, action.TargetBot)
	if err != nil {
		t.Fatal(err)
	}
	group, err := space.New(action.KindGroup, action.TargetNone)
	if err != nil {
		t.Fatal(err)
	}
	named := EnrichCompeting(space, []macrolabel.CompetingCandidate{
		{ActionID: smoke.ID, Confidence: 0.55},
		{ActionID: group.ID, Confidence: 0.52},
	})
	if len(named) != 2 {
		t.Fatalf("len=%d", len(named))
	}
	leakA := "action " + strconv.Itoa(smoke.ID)
	leakB := "action " + strconv.Itoa(group.ID)
	if named[0].ActionName == leakA || named[1].ActionName == leakB {
		t.Fatalf("numeric leakage: %q / %q", named[0].ActionName, named[1].ActionName)
	}
	if named[0].ActionName != "SMOKE BOT" || named[1].ActionName != "GROUP" {
		t.Fatalf("a=%q b=%q", named[0].ActionName, named[1].ActionName)
	}
	if named[0].Description == "" || named[1].Description == "" {
		t.Fatal("expected descriptions")
	}
}

func TestEnrichRejectedHumanNames(t *testing.T) {
	space := action.DefaultSpaceV0()
	farm, err := space.New(action.KindFarm, action.TargetOurJungle)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := space.New(action.KindReset, action.TargetNone)
	if err != nil {
		t.Fatal(err)
	}
	named := EnrichRejected(space, []macrolabel.RejectedCandidate{
		{ActionID: farm.ID, Confidence: 0.71, Reason: "below_margin"},
		{ActionID: reset.ID, Confidence: 0.63, Reason: "below_margin"},
	})
	if named[0].ActionName != "FARM OUR_JUNGLE" {
		t.Fatalf("got %q", named[0].ActionName)
	}
	if named[1].ActionName != "RESET" {
		t.Fatalf("got %q", named[1].ActionName)
	}
	for _, n := range named {
		if strings.HasPrefix(strings.ToLower(n.ActionName), "action ") {
			t.Fatalf("leakage %q", n.ActionName)
		}
	}
}

func TestEnrichCompetingUnknown(t *testing.T) {
	space := action.DefaultSpaceV0()
	named := EnrichCompeting(space, []macrolabel.CompetingCandidate{{ActionID: 42, Confidence: 0.5}})
	if !named[0].Unknown {
		t.Fatal("expected unknown")
	}
	if named[0].ActionName != "UNKNOWN ACTION (id=42)" {
		t.Fatalf("got %q", named[0].ActionName)
	}
}
