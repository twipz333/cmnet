package gsi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmnet/internal/draft"
	"cmnet/internal/gsi/track"
)

func TestServerMergeNewMatchWipesPicks(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	s.merge(&draft.Snapshot{
		HasDraft:     true,
		RadiantPicks: []int{8, 14},
		DirePicks:    []int{11},
		Live:         &draft.Live{MatchID: "m1", ClockTime: 10},
	})
	got := s.merge(&draft.Snapshot{
		Done: true,
		Live: &draft.Live{MatchID: "m2", ClockTime: 0},
	})
	if len(got.RadiantPicks) != 0 || len(got.DirePicks) != 0 {
		t.Fatalf("new match reused old picks: radiant=%v dire=%v", got.RadiantPicks, got.DirePicks)
	}
	snap := s.LastSnapshot()
	if snap == nil || snap.Live == nil || snap.Live.MatchID != "m2" {
		t.Fatalf("last snapshot %+v", snap)
	}
}

func TestLivePathHTTPTracker(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	snap := completeDraftSnap()
	snap.Live.MatchID = "1"
	s.merge(snap)
	tr := track.New(track.Options{
		Roster: SnapshotRoster{Last: s.LastSnapshot, Names: testHeroNames()},
		Log:    func(string) {},
	})
	p := NewPipe(tr, 32)
	s.SetPipe(p)
	go p.Run()
	t.Cleanup(p.Close)

	ts := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(ts.Close)

	body, err := os.ReadFile(filepath.Join("observe", "testdata", "ingame_normal.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"minimap"`) || !strings.Contains(string(body), `"buildings"`) ||
		!strings.Contains(string(body), `"abilities"`) || !strings.Contains(string(body), `"items"`) {
		t.Fatal("fixture missing required GSI sections")
	}
	postJSON(t, ts.URL, body)
	waitPipe(t, p, 1)

	st := tr.Snapshot()
	stats := tr.StatsSnapshot()
	if stats.MinimapEntities == 0 {
		t.Fatal("minimap did not reach extractor/tracker")
	}
	if stats.HeroObservations == 0 {
		t.Fatal("hero observations missing")
	}
	if len(st.OwnBuildings) == 0 && len(st.EnemyStructures) == 0 {
		t.Fatal("buildings/structures missing")
	}
	if !st.Self.Present || st.Self.HeroName == nil || *st.Self.HeroName != "npc_dota_hero_mirana" {
		t.Fatalf("self = %+v", st.Self)
	}
	var tide track.EnemyHeroTrack
	found := false
	for _, e := range st.Enemies {
		if e.Hero == "npc_dota_hero_tidehunter" {
			tide = e
			found = true
		}
	}
	if !found {
		t.Fatalf("tidehunter not on roster snapshot, enemies=%v", st.Enemies)
	}
	if !tide.VisibleNow || len(tide.VisiblePositions) == 0 {
		t.Fatalf("tidehunter should be visible: %+v", tide)
	}
	t.Log("visible:\n" + st.FormatLive())

	// Fog: same payload without the tidehunter minimap object.
	fog := stripMinimapUnit(t, body, "npc_dota_hero_tidehunter")
	fog = bumpGameTime(t, fog, 110)
	postJSON(t, ts.URL, fog)
	waitPipe(t, p, 2)
	st = tr.Snapshot()
	for _, e := range st.Enemies {
		if e.Hero != "npc_dota_hero_tidehunter" {
			continue
		}
		if e.VisibleNow {
			t.Fatal("tidehunter still visible after fog payload")
		}
		if len(e.VisiblePositions) != 0 {
			t.Fatal("current position must be empty in fog")
		}
		if len(e.LastSeenPositions) == 0 {
			t.Fatal("last_seen lost")
		}
		if e.LastSeenAgeSeconds == nil {
			t.Fatal("last_seen_age missing")
		}
	}
	t.Log("missing:\n" + st.FormatLive())

	postJSON(t, ts.URL, bumpGameTime(t, body, 120))
	waitPipe(t, p, 3)
	st = tr.Snapshot()
	for _, e := range st.Enemies {
		if e.Hero == "npc_dota_hero_tidehunter" && !e.VisibleNow {
			t.Fatal("tidehunter did not reappear")
		}
	}
	t.Log("reappeared:\n" + st.FormatLive())

	// New match: picks wiped, tracker reset, old last_seen gone.
	postJSON(t, ts.URL, []byte(`{"map":{"matchid":"m2","game_time":3,"clock_time":3}}`))
	waitPipe(t, p, 4)
	st = tr.Snapshot()
	if st.Clock.MatchID != "m2" {
		t.Fatalf("match %s", st.Clock.MatchID)
	}
	if len(st.Enemies) != 0 {
		t.Fatalf("old roster survived match reset: %+v", st.Enemies)
	}
}

func TestServerACKDoesNotWaitForPipe(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	tr := track.New(track.Options{Log: func(string) {}})
	p := NewPipe(tr, 1)
	s.SetPipe(p)
	// stalled consumer
	ts := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(ts.Close)
	start := time.Now()
	for i := 0; i < 8; i++ {
		postJSON(t, ts.URL, []byte(`{"map":{"matchid":"m","game_time":1}}`))
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("HTTP ACK path blocked on a full pipe")
	}
	if p.Stats().Dropped == 0 {
		t.Fatal("expected dropped payloads")
	}
	p.Close()
}

func postJSON(t *testing.T, url string, body []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func stripMinimapUnit(t *testing.T, body []byte, unit string) []byte {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatal(err)
	}
	var mm map[string]map[string]any
	if err := json.Unmarshal(top["minimap"], &mm); err != nil {
		t.Fatal(err)
	}
	for k, v := range mm {
		if name, _ := v["unitname"].(string); name == unit {
			delete(mm, k)
		}
	}
	raw, err := json.Marshal(mm)
	if err != nil {
		t.Fatal(err)
	}
	top["minimap"] = raw
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func bumpGameTime(t *testing.T, body []byte, gameTime int) []byte {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatal(err)
	}
	var mp map[string]any
	if err := json.Unmarshal(top["map"], &mp); err != nil {
		t.Fatal(err)
	}
	mp["game_time"] = gameTime
	mp["clock_time"] = gameTime
	raw, err := json.Marshal(mp)
	if err != nil {
		t.Fatal(err)
	}
	top["map"] = raw
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
