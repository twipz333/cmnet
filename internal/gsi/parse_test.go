package gsi

import (
	"reflect"
	"testing"

	"cmnet/internal/draft"
)

func TestParseDraftRadiantDire(t *testing.T) {
	// Slots intentionally out of order to exercise the index sort; ban1_id=0 is an
	// empty slot and must be skipped.
	body := []byte(`{
	  "draft": {
	    "activeteam": 2,
	    "pick": true,
	    "radiant": {
	      "pick1_id": 14, "pick0_id": 8,
	      "ban0_id": 5, "ban1_id": 0
	    },
	    "dire": {
	      "pick0_id": 11,
	      "ban0_id": 2, "ban1_id": 3
	    }
	  }
	}`)
	snap, err := ParseDraft(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if !reflect.DeepEqual(snap.RadiantPicks, []int{8, 14}) {
		t.Errorf("radiant picks = %v, want [8 14]", snap.RadiantPicks)
	}
	if !reflect.DeepEqual(snap.RadiantBans, []int{5}) {
		t.Errorf("radiant bans = %v, want [5] (empty slot skipped)", snap.RadiantBans)
	}
	if !reflect.DeepEqual(snap.DireBans, []int{2, 3}) {
		t.Errorf("dire bans = %v, want [2 3]", snap.DireBans)
	}
	step, ok := snap.Current()
	if !ok || step.Team != draft.Radiant || step.Action != draft.Pick {
		t.Errorf("current = %+v ok=%v, want Radiant Pick", step, ok)
	}
}

func TestParseDraftTeam2Team3Variant(t *testing.T) {
	body := []byte(`{
	  "draft": {
	    "activeteam": 3,
	    "pick": false,
	    "team2": { "pick0_id": 1 },
	    "team3": { "ban0_id": 9 }
	  }
	}`)
	snap, err := ParseDraft(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(snap.RadiantPicks, []int{1}) {
		t.Errorf("radiant picks = %v, want [1]", snap.RadiantPicks)
	}
	if !reflect.DeepEqual(snap.DireBans, []int{9}) {
		t.Errorf("dire bans = %v, want [9]", snap.DireBans)
	}
	step, ok := snap.Current()
	if !ok || step.Team != draft.Dire || step.Action != draft.Ban {
		t.Errorf("current = %+v ok=%v, want Dire Ban", step, ok)
	}
}

func TestParseDraftNoDraftNoMap(t *testing.T) {
	// Neither a draft nor a map block: a wholly uninteresting event, ignored.
	snap, err := ParseDraft([]byte(`{"provider":{"name":"Dota 2"},"hero":{}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap != nil {
		t.Errorf("expected nil snapshot for event with no draft/map, got %+v", snap)
	}
}

func TestParseMapOnlyEventCarriesLive(t *testing.T) {
	// Post-draft / All Pick in-game event: a map block but no draft block. The
	// snapshot must carry Live and report HasDraft=false so the server retains
	// the last known picks (M6).
	body := []byte(`{"map":{"clock_time":355,"game_state":"DOTA_GAMERULES_STATE_GAME_IN_PROGRESS","daytime":true}}`)
	snap, err := ParseDraft(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap == nil {
		t.Fatal("expected a snapshot for a map-only event")
	}
	if snap.HasDraft {
		t.Error("map-only event must have HasDraft=false")
	}
	if !snap.Done {
		t.Error("map-only event must mark the draft phase Done")
	}
	if snap.Live == nil {
		t.Fatal("Live not populated")
	}
	if snap.Live.ClockTime != 355 || !snap.Live.Daytime ||
		snap.Live.GameState != "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS" {
		t.Errorf("Live = %+v, unexpected", snap.Live)
	}
}

func TestParseDraftEventCarriesLiveWhenMapPresent(t *testing.T) {
	// During the CM draft Dota sends draft and map together; both must parse.
	body := []byte(`{"draft":{"activeteam":2,"pick":true,"radiant":{"pick0_id":8},"dire":{}},
	  "map":{"clock_time":-30,"game_state":"DOTA_GAMERULES_STATE_HERO_SELECTION"}}`)
	snap, err := ParseDraft(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !snap.HasDraft {
		t.Error("expected HasDraft=true")
	}
	if snap.Live == nil || snap.Live.ClockTime != -30 {
		t.Errorf("Live = %+v, want clock -30", snap.Live)
	}
}

func TestParseDraftDoneWhenNoActiveTeam(t *testing.T) {
	body := []byte(`{"draft":{"activeteam":0,"pick":false,"radiant":{"pick0_id":1}}}`)
	snap, err := ParseDraft(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := snap.Current(); ok {
		t.Error("expected Current ok=false when no active team")
	}
}

func TestParseDraftMalformed(t *testing.T) {
	if _, err := ParseDraft([]byte(`{not json`)); err == nil {
		t.Error("expected error on malformed JSON")
	}
}
