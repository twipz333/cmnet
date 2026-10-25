package gsi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmnet/internal/draft"
)

// The existing parser tests build their own minimal JSON, which only proves the
// parser agrees with our idea of the schema. These run against payloads shaped
// like what the client actually sends — including the fields we deliberately
// ignore, which is where a strict decoder would break.

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParseFullDraftFixture(t *testing.T) {
	snap, err := ParseDraft(fixture(t, "draft_cm_team_keys.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap == nil || !snap.HasDraft {
		t.Fatal("draft block was not recognised")
	}
	if got := snap.RadiantPicks; len(got) != 1 || got[0] != 8 {
		t.Errorf("radiant picks = %v, want [8]", got)
	}
	if got := snap.RadiantBans; len(got) != 2 || got[0] != 74 || got[1] != 41 {
		t.Errorf("radiant bans = %v, want [74 41] in slot order", got)
	}
	if snap.CurrentTeam != draft.Dire {
		t.Errorf("current team = %v, want Dire (activeteam 3)", snap.CurrentTeam)
	}
	if snap.CurrentIsPick {
		t.Error("current action is a ban in this fixture")
	}
	// Radiant has three actions to Dire's two, so Radiant moved first.
	if snap.FirstPick != draft.Radiant {
		t.Errorf("first pick = %v, want Radiant inferred from the board", snap.FirstPick)
	}
	if snap.Live == nil || snap.Live.MatchID != "8123456789" {
		t.Errorf("map block did not survive: %+v", snap.Live)
	}
}

// The whole player/hero half of the payload used to be decoded and thrown away.
func TestParseInGameFixture(t *testing.T) {
	snap, err := ParseDraft(fixture(t, "ingame_player_hero.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap.Live == nil {
		t.Fatal("no live block")
	}
	if !snap.Live.Paused {
		t.Error("paused was not read; a stopped clock would look like a stalled feed")
	}
	if snap.Live.RadiantScore != 11 || snap.Live.DireScore != 7 {
		t.Errorf("score = %d/%d, want 11/7", snap.Live.RadiantScore, snap.Live.DireScore)
	}
	self := snap.Live.Self
	if self == nil {
		t.Fatal("the local player's own line was dropped")
	}
	if self.Kills != 4 || self.Deaths != 1 || self.Assists != 6 {
		t.Errorf("KDA = %d/%d/%d, want 4/1/6", self.Kills, self.Deaths, self.Assists)
	}
	if self.GPM != 542 || self.XPM != 611 || self.NetWorth != 9340 {
		t.Errorf("gpm/xpm/net worth = %d/%d/%d", self.GPM, self.XPM, self.NetWorth)
	}
	if self.HeroID != 8 || self.Level != 12 {
		t.Errorf("hero = %d level %d, want 8 level 12", self.HeroID, self.Level)
	}
	if self.Alive || self.RespawnSeconds != 27 {
		t.Errorf("alive=%v respawn=%d, want dead with 27s", self.Alive, self.RespawnSeconds)
	}
	if self.BuybackCost != 1420 {
		t.Errorf("buyback cost = %d, want 1420", self.BuybackCost)
	}
}

// Older clients name the containers radiant/dire instead of team2/team3. The
// parser accepts both, and that tolerance is a requirement rather than a
// safety net — the schema is undocumented and shifts between versions.
func TestParseAcceptsBothTeamContainerNames(t *testing.T) {
	snap, err := ParseDraft(fixture(t, "map_only_radiant_dire_keys.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := snap.RadiantPicks; len(got) != 1 || got[0] != 1 {
		t.Errorf("radiant picks = %v, want [1] (empty slots skipped)", got)
	}
	if got := snap.RadiantBans; len(got) != 1 || got[0] != 5 {
		t.Errorf("radiant bans = %v, want [5]", got)
	}
}

// Roshan is spectator-only, so the parser must not carry a field for it: a value
// that is always absent reads as "Roshan is up" to anything downstream.
func TestRoshanIsNotParsed(t *testing.T) {
	body := []byte(`{"map":{"clock_time":600,"game_state":"X","roshan_state":"alive","roshan_state_end_seconds":180}}`)
	snap, err := ParseDraft(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap == nil || snap.Live == nil {
		t.Fatal("map block was not parsed at all")
	}
	// Nothing to assert beyond the absence of a field — this test exists so that
	// adding one without reading SCHEMA.md fails here first.
	if snap.Live.ClockTime != 600 {
		t.Errorf("clock = %d, want the rest of the map block still read", snap.Live.ClockTime)
	}
}

// An unknown or missing token must be rejected: the endpoint used to accept a
// POST from anything on the machine that could reach the port.
func TestServerRejectsWrongToken(t *testing.T) {
	s := NewServerWithToken("127.0.0.1:0", "cmnet-local")

	post := func(body []byte) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
		w := httptest.NewRecorder()
		s.handle(w, req)
	}

	post([]byte(`{"auth":{"token":"wrong"},"draft":{"activeteam":2,"pick":true,"radiant":{"pick0_id":1}}}`))
	if n, msg, _ := s.Health(); n != 1 || !strings.Contains(msg, "auth") {
		t.Fatalf("a mismatched token was not rejected (errors=%d, last=%q)", n, msg)
	}
	select {
	case ev := <-s.Events():
		t.Fatalf("a rejected event still reached the consumer: %+v", ev)
	default:
	}

	post(fixture(t, "draft_cm_team_keys.json"))
	select {
	case ev := <-s.Events():
		if !ev.HasDraft {
			t.Error("the accepted event carried no draft")
		}
	default:
		t.Error("an event with the right token never arrived")
	}
}

// A payload that fails to decode must be counted, not silently dropped: that is
// how a schema change announces itself.
func TestServerCountsParseFailures(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{not json"))
	s.handle(httptest.NewRecorder(), req)

	n, msg, _ := s.Health()
	if n != 1 || msg == "" {
		t.Fatalf("parse failure was not recorded (errors=%d, last=%q)", n, msg)
	}
}
