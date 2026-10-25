package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/probe"
	"cmnet/internal/gsi/track"
)

func TestReplayUsesProductionExtractor(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "payloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	bodies := [][]byte{
		[]byte(`{"map":{"game_time":10,"clock_time":10,"matchid":"x","game_state":"DOTA_GAMERULES_STATE_GAME_IN_PROGRESS"},"minimap":{}}`),
		[]byte(`{"map":{"game_time":11,"clock_time":11,"matchid":"x"},"minimap":{"o1":{"xpos":100,"ypos":200,"image":"minimap_enemyicon","team":2,"unitname":"npc_dota_hero_luna","name":"npc_dota_hero_luna"}}}`),
		[]byte(`{"map":{"game_time":12,"clock_time":12,"matchid":"x"},"minimap":{}}`),
	}
	var idx []byte
	for i, body := range bodies {
		seq := uint64(i + 1)
		rel := fmt.Sprintf("payloads/%08d.json", seq)
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), body, 0o644); err != nil {
			t.Fatal(err)
		}
		rec := probe.IndexRecord{Seq: seq, File: rel, Bytes: len(body), ValidJSON: true}
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		idx = append(idx, line...)
		idx = append(idx, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "index.ndjson"), idx, 0o644); err != nil {
		t.Fatal(err)
	}

	sess, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	roster := track.Roster{
		AllySide: observe.TeamDire,
		Allies:   []track.HeroRef{{UnitName: "npc_dota_hero_mirana"}},
		Enemies:  []track.HeroRef{{UnitName: "npc_dota_hero_luna"}},
	}
	res, err := Run(sess, Options{Roster: roster})
	if err != nil {
		t.Fatal(err)
	}
	if res.Processed != 3 {
		t.Fatalf("processed %d", res.Processed)
	}
	var luna track.EnemyHeroTrack
	for _, e := range res.State.Enemies {
		if e.Hero == "npc_dota_hero_luna" {
			luna = e
		}
	}
	if luna.VisibleNow {
		t.Fatal("final tick must be missing")
	}
	if len(luna.LastSeenPositions) != 1 || luna.LastSeenPositions[0].X != 100 {
		t.Fatalf("last seen %+v", luna.LastSeenPositions)
	}
}

func TestLoadRosterFile(t *testing.T) {
	r, err := track.LoadRosterFile(filepath.Join("testdata", "roster.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Valid() || len(r.Enemies) != 2 {
		t.Fatalf("%+v", r)
	}
}
