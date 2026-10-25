package observe

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

func extractFile(t *testing.T, name string) *Batch {
	t.Helper()
	var ex Extractor
	batch, err := ex.Extract(loadFixture(t, name), ExtractOptions{
		ReceivedAt: time.UnixMilli(1_000_000),
		Seq:        1,
	})
	if err != nil {
		t.Fatalf("extract %s: %v", name, err)
	}
	return batch
}

func TestExtractUnknownFieldsDoNotBreakParsing(t *testing.T) {
	batch := extractFile(t, "ingame_normal.json")
	if !batch.Match.Present {
		t.Fatal("match missing")
	}
	if batch.Match.ClockTime == nil || *batch.Match.ClockTime != 50 {
		t.Fatalf("clock_time = %v", batch.Match.ClockTime)
	}
}

func TestExtractEmptySectionsOK(t *testing.T) {
	batch := extractFile(t, "empty_sections.json")
	if batch == nil {
		t.Fatal("nil batch")
	}
	if len(batch.Events) != 0 || len(batch.Roshan) != 0 {
		t.Fatalf("expected empty observations, events=%d roshan=%d", len(batch.Events), len(batch.Roshan))
	}
}

func TestExtractMissingSectionsOK(t *testing.T) {
	var ex Extractor
	batch, err := ex.Extract([]byte(`{"map":{"clock_time":1}}`), ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Self.Present {
		t.Fatal("self should be absent")
	}
	if batch.Match.GameTime != nil {
		t.Fatal("game_time must be unavailable, not zero")
	}
	if batch.Match.ClockTime == nil || *batch.Match.ClockTime != 1 {
		t.Fatalf("clock_time = %v", batch.Match.ClockTime)
	}
}

func TestExtractInvalidJSON(t *testing.T) {
	var ex Extractor
	if _, err := ex.Extract([]byte(`{not json`), ExtractOptions{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestExtractHeroObservation(t *testing.T) {
	batch := extractFile(t, "ingame_normal.json")
	var found *HeroPosition
	for i := range batch.Heroes {
		if batch.Heroes[i].Hero == "npc_dota_hero_tidehunter" {
			found = &batch.Heroes[i]
		}
	}
	if found == nil {
		t.Fatalf("missing tidehunter, heroes=%v", heroes(batch))
	}
	if found.ImageKind != HeroImageEnemy || found.Position.X != -5965 {
		t.Fatalf("tidehunter = %+v", found)
	}
}

func TestExtractInvalidHeroLikeFiltered(t *testing.T) {
	batch := extractFile(t, "ingame_normal.json")
	for _, h := range batch.Heroes {
		if h.Hero == "npc_dota_hero_nevermore" {
			t.Fatalf("nevermore at 0,0 plaincircle must not be a hero observation: %+v", h)
		}
		if h.Position.Origin() {
			t.Fatalf("origin hero leaked: %+v", h)
		}
		if h.Image == ImagePlainCircle {
			t.Fatalf("plaincircle leaked: %+v", h)
		}
	}
}

func TestExtractMultipleLunaPositions(t *testing.T) {
	batch := extractFile(t, "luna_triple.json")
	var luna []HeroPosition
	for _, h := range batch.Heroes {
		if h.Hero == "npc_dota_hero_luna" {
			luna = append(luna, h)
		}
	}
	if len(luna) != 3 {
		t.Fatalf("luna observations = %d, want 3 (%v)", len(luna), luna)
	}
}

func TestExtractSelfPositionFromHeroBlock(t *testing.T) {
	batch := extractFile(t, "ingame_normal.json")
	if batch.Self.Position == nil {
		t.Fatal("self position missing")
	}
	if batch.Self.Position.X != 5285 || batch.Self.Position.Y != -3553 {
		t.Fatalf("self position = %+v, want hero.xpos/ypos", batch.Self.Position)
	}
}

func TestExtractCreepsWardsBuildings(t *testing.T) {
	batch := extractFile(t, "ingame_normal.json")
	if len(batch.Creeps) != 3 {
		t.Fatalf("creeps = %d", len(batch.Creeps))
	}
	classes := map[CreepClass]int{}
	for _, c := range batch.Creeps {
		classes[c.Class]++
	}
	if classes[CreepMelee] != 1 || classes[CreepRanged] != 1 || classes[CreepFlagbearer] != 1 {
		t.Fatalf("creep classes = %v", classes)
	}
	if len(batch.Wards) != 1 || batch.Wards[0].Class != WardObserver {
		t.Fatalf("wards = %+v", batch.Wards)
	}
	if len(batch.OwnBuildings) != 1 || batch.OwnBuildings[0].Class != StructureTowerT1 {
		t.Fatalf("buildings = %+v", batch.OwnBuildings)
	}
	if batch.OwnBuildings[0].Health == nil || *batch.OwnBuildings[0].Health != 1800 {
		t.Fatalf("building hp = %+v", batch.OwnBuildings[0].Health)
	}
	var t1 *StructureObservation
	for i := range batch.Structures {
		if batch.Structures[i].Class == StructureTowerT1 {
			t1 = &batch.Structures[i]
		}
	}
	if t1 == nil || t1.Team != TeamRadiant {
		t.Fatalf("enemy t1 missing: %+v", batch.Structures)
	}
}

func TestExtractRoshanValidAndOrigin(t *testing.T) {
	ok := extractFile(t, "roshan_visible.json")
	if len(ok.Roshan) != 1 || ok.Roshan[0].Position.X != 2859 {
		t.Fatalf("roshan = %+v", ok.Roshan)
	}
	noise := extractFile(t, "roshan_origin.json")
	if len(noise.Roshan) != 0 {
		t.Fatalf("origin roshan leaked: %+v", noise.Roshan)
	}
}

func TestExtractEventsNormalized(t *testing.T) {
	batch := extractFile(t, "events_sample.json")
	kinds := map[EventKind]int{}
	for _, e := range batch.Events {
		kinds[e.Kind]++
		if e.Kind == EventItemPurchase && (e.ItemID == nil || *e.ItemID != 43) {
			t.Fatalf("item purchase %+v", e)
		}
		if e.Kind == EventSmokeActivated && (e.PlayerID == nil || *e.PlayerID != 9) {
			t.Fatalf("smoke %+v", e)
		}
		if e.Kind == EventTowerKill && e.Team != TeamRadiant {
			t.Fatalf("tower kill team %+v", e)
		}
	}
	if kinds[EventSmokeActivated] != 1 || kinds[EventItemPurchase] != 1 || kinds[EventTowerKill] != 1 {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestExtractZeroScoreIsNotMissing(t *testing.T) {
	batch := extractFile(t, "ingame_normal.json")
	if batch.Match.RadiantScore == nil || *batch.Match.RadiantScore != 0 {
		t.Fatalf("radiant_score should be present zero, got %v", batch.Match.RadiantScore)
	}
}

func heroes(b *Batch) []string {
	out := make([]string, len(b.Heroes))
	for i, h := range b.Heroes {
		out[i] = h.Hero
	}
	return out
}

func TestMinimapPresentVsAbsent(t *testing.T) {
	var ex Extractor
	absent, err := ex.Extract([]byte(`{"map":{"clock_time":1}}`), ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if absent.MinimapPresent {
		t.Fatal("missing minimap key must not be present")
	}
	empty, err := ex.Extract([]byte(`{"minimap":{}}`), ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !empty.MinimapPresent {
		t.Fatal("empty minimap object is a valid observation")
	}
	if len(empty.Creeps) != 0 {
		t.Fatalf("creeps=%d", len(empty.Creeps))
	}
	full := extractFile(t, "ingame_normal.json")
	if !full.MinimapPresent {
		t.Fatal("fixture minimap must be present")
	}
}
