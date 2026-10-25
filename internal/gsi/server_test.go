package gsi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cmnet/internal/draft"
)

// Drive the handler through httptest (no real port) and confirm a posted draft
// event surfaces on Events().
func TestServerHandlePublishesSnapshot(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	ts := httptest.NewServer(http.HandlerFunc(s.handle))
	defer ts.Close()

	body := `{"draft":{"activeteam":2,"pick":true,"radiant":{"pick0_id":8},"dire":{}}}`
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	select {
	case snap := <-s.Events():
		step, ok := snap.Current()
		if !ok || step.Team != draft.Radiant {
			t.Errorf("snapshot current = %+v ok=%v", step, ok)
		}
		if len(snap.RadiantPicks) != 1 || snap.RadiantPicks[0] != 8 {
			t.Errorf("radiant picks = %v", snap.RadiantPicks)
		}
	case <-time.After(time.Second):
		t.Fatal("no snapshot received")
	}
}

func TestServerIgnoresEventWithNoDraftNoMap(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	ts := httptest.NewServer(http.HandlerFunc(s.handle))
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(`{"hero":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	select {
	case <-s.Events():
		t.Error("event with no draft/map should not publish a snapshot")
	case <-time.After(100 * time.Millisecond):
		// expected: nothing published
	}
}

// merge must retain the last known picks across a later map-only event so the
// post-draft strength readout keeps both lineups (M6).
func TestServerMergeRetainsPicksAcrossMapOnlyEvent(t *testing.T) {
	s := NewServer("127.0.0.1:0")

	// First, a draft event sets the picks.
	d := s.merge(&draft.Snapshot{HasDraft: true, RadiantPicks: []int{8, 14}, DirePicks: []int{11}})
	if len(d.RadiantPicks) != 2 {
		t.Fatalf("draft merge lost picks: %v", d.RadiantPicks)
	}

	// Then a map-only in-game event (no picks) must keep them and add Live.
	m := s.merge(&draft.Snapshot{Done: true, Live: &draft.Live{ClockTime: 600}})
	if len(m.RadiantPicks) != 2 || m.RadiantPicks[0] != 8 || len(m.DirePicks) != 1 {
		t.Errorf("map-only event wiped picks: radiant=%v dire=%v", m.RadiantPicks, m.DirePicks)
	}
	if m.Live == nil || m.Live.ClockTime != 600 {
		t.Errorf("Live not carried: %+v", m.Live)
	}
}

// push must keep only the latest snapshot (latest-wins, buffer size 1).
func TestServerPushLatestWins(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	s.push(&draft.Snapshot{RadiantPicks: []int{1}})
	s.push(&draft.Snapshot{RadiantPicks: []int{2}})
	snap := <-s.Events()
	if len(snap.RadiantPicks) != 1 || snap.RadiantPicks[0] != 2 {
		t.Errorf("expected latest snapshot (picks [2]), got %v", snap.RadiantPicks)
	}
}
