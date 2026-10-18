package action

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestActionSpaceStableOrdering(t *testing.T) {
	sp := DefaultSpaceV0()
	got := sp.All()
	if len(got) != 34 {
		t.Fatalf("action count=%d want=34", len(got))
	}

	expected := []string{
		"00 HOLD",
		"01 FARM OUR_JUNGLE",
		"02 FARM OUR_TRIANGLE",
		"03 FARM ENEMY_JUNGLE",
		"04 FARM ENEMY_TRIANGLE",
		"05 PUSH TOP",
		"06 PUSH MID",
		"07 PUSH BOT",
		"08 DEFEND TOP",
		"09 DEFEND MID",
		"10 DEFEND BOT",
		"11 DEFEND OUR_BASE",
		"12 CONTROL_OBJECTIVE ROSHAN",
		"13 CONTROL_OBJECTIVE TORMENTOR",
		"14 TAKE_OBJECTIVE ROSHAN",
		"15 TAKE_OBJECTIVE TORMENTOR",
		"16 SMOKE TOP",
		"17 SMOKE MID",
		"18 SMOKE BOT",
		"19 SMOKE ENEMY_JUNGLE",
		"20 SMOKE ENEMY_TRIANGLE",
		"21 INVADE ENEMY_JUNGLE",
		"22 INVADE ENEMY_TRIANGLE",
		"23 GROUP",
		"24 SPLIT_MAP",
		"25 SET_VISION TOP",
		"26 SET_VISION MID",
		"27 SET_VISION BOT",
		"28 SET_VISION ROSHAN",
		"29 SET_VISION ENEMY_JUNGLE",
		"30 SET_VISION ENEMY_TRIANGLE",
		"31 SIEGE ENEMY_BASE",
		"32 RESET",
		"33 RETREAT",
	}

	if len(expected) != len(got) {
		t.Fatalf("expected len=%d got len=%d", len(expected), len(got))
	}

	for i, a := range got {
		line := fmt.Sprintf("%02d %s", a.ID, a.String())
		if line != expected[i] {
			t.Fatalf("order mismatch at %d: got=%s want=%s", i, line, expected[i])
		}
	}
}

func TestActionSpaceValidateInvalid(t *testing.T) {
	sp := DefaultSpaceV0()
	invalid := []MacroAction{
		{SchemaVersion: SchemaVersion, Kind: KindPush, Target: TargetRoshan},
		{SchemaVersion: SchemaVersion, Kind: KindTakeObjective, Target: TargetMid},
		{SchemaVersion: SchemaVersion, Kind: KindReset, Target: TargetBot},
		{SchemaVersion: SchemaVersion, Kind: KindGroup, Target: TargetTop},
		{SchemaVersion: SchemaVersion, Kind: KindSiege, Target: TargetOurBase},
		{SchemaVersion: SchemaVersion, Kind: KindFarm, Target: TargetRoshan},
	}
	for _, a := range invalid {
		if err := sp.Validate(a.Kind, a.Target); err == nil {
			t.Fatalf("expected invalid for %s/%s", a.Kind, a.Target)
		}
	}
}

func TestActionSpaceValidateValidExamples(t *testing.T) {
	sp := DefaultSpaceV0()
	valid := []MacroAction{
		{SchemaVersion: SchemaVersion, Kind: KindHold, Target: TargetNone},
		{SchemaVersion: SchemaVersion, Kind: KindFarm, Target: TargetOurTriangle},
		{SchemaVersion: SchemaVersion, Kind: KindFarm, Target: TargetEnemyJungle},
		{SchemaVersion: SchemaVersion, Kind: KindPush, Target: TargetTop},
		{SchemaVersion: SchemaVersion, Kind: KindDefend, Target: TargetOurBase},
		{SchemaVersion: SchemaVersion, Kind: KindControlObjective, Target: TargetRoshan},
		{SchemaVersion: SchemaVersion, Kind: KindTakeObjective, Target: TargetRoshan},
		{SchemaVersion: SchemaVersion, Kind: KindSmoke, Target: TargetTop},
		{SchemaVersion: SchemaVersion, Kind: KindInvade, Target: TargetEnemyTriangle},
		{SchemaVersion: SchemaVersion, Kind: KindGroup, Target: TargetNone},
		{SchemaVersion: SchemaVersion, Kind: KindSplitMap, Target: TargetNone},
		{SchemaVersion: SchemaVersion, Kind: KindSetVision, Target: TargetRoshan},
		{SchemaVersion: SchemaVersion, Kind: KindSiege, Target: TargetEnemyBase},
		{SchemaVersion: SchemaVersion, Kind: KindReset, Target: TargetNone},
		{SchemaVersion: SchemaVersion, Kind: KindRetreat, Target: TargetNone},
	}
	for _, a := range valid {
		if err := sp.Validate(a.Kind, a.Target); err != nil {
			t.Fatalf("expected valid for %s/%s: %v", a.Kind, a.Target, err)
		}
	}
}

func TestActionSpaceRoundTripByIDAndIDOf(t *testing.T) {
	sp := DefaultSpaceV0()
	for _, a := range sp.All() {
		b, ok := sp.ByID(a.ID)
		if !ok {
			t.Fatalf("missing action by id=%d", a.ID)
		}
		if !a.Equal(b) {
			t.Fatalf("ByID mismatch: id=%d got=%+v want=%+v", a.ID, b, a)
		}
		id2, ok := sp.IDOf(a.Kind, a.Target)
		if !ok {
			t.Fatalf("missing idOf for %s/%s", a.Kind, a.Target)
		}
		if id2 != a.ID {
			t.Fatalf("idOf mismatch: %s/%s got=%d want=%d", a.Kind, a.Target, id2, a.ID)
		}
	}
}

func TestActionSpaceEncoderKindAndTargetIDs(t *testing.T) {
	if id, ok := KindID(KindPush); !ok || id != 2 {
		t.Fatalf("KindID(PUSH)=%d ok=%v want=2,true", id, ok)
	}
	if id, ok := TargetID(TargetEnemyTriangle); !ok || id != 9 {
		t.Fatalf("TargetID(ENEMY_TRIANGLE)=%d ok=%v want=9,true", id, ok)
	}
}

func TestRegistryV0MatchesSchemaFile(t *testing.T) {
	sp := DefaultSpaceV0()
	want := RegistryV0()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	path := filepath.Join(filepath.Dir(file), "schema_v0.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema file: %v", err)
	}
	var got SchemaV0
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal schema file: %v", err)
	}

	if got.SchemaVersion != want.SchemaVersion {
		t.Fatalf("schema_version mismatch: %s vs %s", got.SchemaVersion, want.SchemaVersion)
	}
	if len(got.Actions) != len(want.Actions) {
		t.Fatalf("actions len mismatch: %d vs %d", len(got.Actions), len(want.Actions))
	}
	if !reflect.DeepEqual(got.Actions, want.Actions) {
		t.Fatalf("schema actions mismatch")
	}

	_ = sp
}

func TestEncodeActionDeterministic(t *testing.T) {
	sp := DefaultSpaceV0()
	for _, a := range sp.All() {
		e1, err := EncodeAction(a)
		if err != nil {
			t.Fatal(err)
		}
		e2, err := EncodeAction(a)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(e1, e2) {
			t.Fatalf("encode non-deterministic for id=%d: %+v vs %+v", a.ID, e1, e2)
		}
	}
}
