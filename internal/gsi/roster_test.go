package gsi

import (
	"testing"

	"cmnet/internal/draft"
	"cmnet/internal/gsi/track"
)

func testHeroNames() track.MapNames {
	return track.MapNames{
		1:  "npc_dota_hero_night_stalker",
		2:  "npc_dota_hero_ember_spirit",
		3:  "npc_dota_hero_rattletrap",
		4:  "npc_dota_hero_luna",
		5:  "npc_dota_hero_lion",
		6:  "npc_dota_hero_dark_willow",
		7:  "npc_dota_hero_wisp",
		9:  "npc_dota_hero_mirana",
		18: "npc_dota_hero_sven",
		29: "npc_dota_hero_tidehunter",
	}
}

func completeDraftSnap() *draft.Snapshot {
	return &draft.Snapshot{
		HasDraft:     true,
		Done:         true,
		LocalTeam:    draft.Dire,
		LocalTeamOK:  true,
		DirePicks:    []int{9, 18, 1, 2, 3},
		RadiantPicks: []int{29, 4, 5, 6, 7},
		Live:         &draft.Live{MatchID: "m1"},
	}
}

func TestSnapshotRosterAlliesEnemies(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	s.merge(completeDraftSnap())
	p := SnapshotRoster{Last: s.LastSnapshot, Names: testHeroNames()}
	r, ok := p.Roster()
	if !ok {
		t.Fatal("roster not ready")
	}
	if len(r.Allies) != 5 || len(r.Enemies) != 5 {
		t.Fatalf("allies=%d enemies=%d", len(r.Allies), len(r.Enemies))
	}
	if r.Allies[0].UnitName != "npc_dota_hero_mirana" {
		t.Fatalf("ally0 = %s", r.Allies[0].UnitName)
	}
	if r.Enemies[0].UnitName != "npc_dota_hero_tidehunter" {
		t.Fatalf("enemy0 = %s", r.Enemies[0].UnitName)
	}
}

func TestSnapshotRosterNotReadyUntilFivePicks(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	snap := completeDraftSnap()
	snap.DirePicks = snap.DirePicks[:4]
	s.merge(snap)
	p := SnapshotRoster{Last: s.LastSnapshot, Names: testHeroNames()}
	if _, ok := p.Roster(); ok {
		t.Fatal("partial draft must not yield a roster")
	}
}

func TestSnapshotRosterNilLast(t *testing.T) {
	p := SnapshotRoster{Last: func() *draft.Snapshot { return nil }, Names: testHeroNames()}
	if _, ok := p.Roster(); ok {
		t.Fatal("nil snapshot must not yield a roster")
	}
}
