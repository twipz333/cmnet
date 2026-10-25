package rules

import (
	"reflect"
	"testing"
)

func resolver(m map[string]int) func(string) (int, bool) {
	return func(t string) (int, bool) { id, ok := m[t]; return id, ok }
}

func TestParseAndEval(t *testing.T) {
	names := map[string]int{"axe": 2, "lifestealer": 54, "ursa": 70, "troll": 95}
	lines := []string{
		"# counter rules",
		"axe, lifestealer | ursa => buy Eul",
		"troll => buy Solar Crest",
		"else => standard build",
	}
	rs := Parse(lines, resolver(names))

	// (axe AND lifestealer) present -> first rule fires, else suppressed.
	got := Eval(rs, []int{2, 54, 7})
	if !reflect.DeepEqual(got, []string{"buy Eul"}) {
		t.Fatalf("axe+ls: got %v", got)
	}
	// ursa alone -> first rule fires via OR group.
	if got := Eval(rs, []int{70}); !reflect.DeepEqual(got, []string{"buy Eul"}) {
		t.Fatalf("ursa: got %v", got)
	}
	// axe alone (no lifestealer, no ursa) -> no match -> else.
	if got := Eval(rs, []int{2}); !reflect.DeepEqual(got, []string{"standard build"}) {
		t.Fatalf("axe only: got %v", got)
	}
	// troll + ursa -> two rules fire, else suppressed.
	if got := Eval(rs, []int{95, 70}); !reflect.DeepEqual(got, []string{"buy Eul", "buy Solar Crest"}) {
		t.Fatalf("troll+ursa: got %v", got)
	}
}

func TestParseUnknown(t *testing.T) {
	rs := Parse([]string{"axe, bogus => x"}, resolver(map[string]int{"axe": 2}))
	if u := UnknownTokens(rs); len(u) != 1 || u[0] != "bogus" {
		t.Fatalf("unknown: %v", u)
	}
}
