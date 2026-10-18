package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/draft"
	"cmnet/internal/model"
	"cmnet/internal/recommender"
	"cmnet/internal/session"
	"cmnet/internal/train"
)

// testServer builds a server over a tiny synthetic bundle, mirroring the
// determinism package's fixture so the numbers are stable.
func testServer(t *testing.T) *Server {
	t.Helper()
	pb := func(ord, hero, team int, pick bool) cache.PickBan {
		return cache.PickBan{Ord: ord, HeroID: hero, Team: team, IsPick: pick}
	}
	matches := []cache.Match{
		{
			ID: 1, StartTime: 100, RadiantWin: true,
			PickBans: []cache.PickBan{
				pb(0, 1, 0, false), pb(1, 2, 1, false),
				pb(2, 3, 0, true), pb(3, 4, 1, true),
				pb(4, 5, 0, true), pb(5, 6, 1, true),
			},
		},
		{
			ID: 2, StartTime: 200, RadiantWin: false,
			PickBans: []cache.PickBan{
				pb(0, 3, 0, false), pb(1, 4, 1, false),
				pb(2, 1, 0, true), pb(3, 6, 1, true),
				pb(4, 2, 0, true), pb(5, 5, 1, true),
			},
		},
	}
	heroIDs := []int{1, 2, 3, 4, 5, 6}
	b := train.Bundle(matches, heroIDs, train.Options{
		WindowDays: 31, PatchTag: "test", MMRTier: "test",
		CreatedAt: "2026-01-01T00:00:00Z",
	})
	hm := b.HeroMap()
	rec := recommender.New(
		&model.WinProb{Model: b.WinProb, HeroMap: hm},
		&model.Predictor{Pick: b.PickPredictor, Ban: b.BanPredictor, HeroMap: hm},
	)
	heroes := make([]session.Hero, 0, len(heroIDs))
	for _, id := range heroIDs {
		heroes = append(heroes, session.Hero{ID: id})
	}
	sess := session.New(session.Config{
		Heroes:        heroes,
		OurTeam:       draft.Radiant,
		FirstPick:     draft.Radiant,
		BundleVersion: b.BundleVersion,
		Rec:           rec,
	})
	return NewForSession(sess, "127.0.0.1:0")
}

func do(t *testing.T, srv *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	return w
}

func decodeState(t *testing.T, w *httptest.ResponseRecorder) stateDTO {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var s stateDTO
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return s
}

func TestStateReportsInitialDraft(t *testing.T) {
	srv := testServer(t)
	s := decodeState(t, do(t, srv, http.MethodGet, "/api/state", "", nil))

	if s.Step == nil {
		t.Fatal("expected an open step on a fresh draft")
	}
	if !s.Step.IsOurs {
		t.Errorf("Radiant first pick with our side Radiant: first step should be ours, got %+v", s.Step)
	}
	if len(s.Heroes) != 6 {
		t.Errorf("available heroes = %d, want 6", len(s.Heroes))
	}
	if len(s.Board.RadiantBans) != 0 {
		t.Errorf("fresh board should be empty, got %+v", s.Board)
	}
}

func TestApplyAdvancesDraftAndRejectsDuplicates(t *testing.T) {
	srv := testServer(t)

	s := decodeState(t, do(t, srv, http.MethodPost, "/api/apply", `{"heroId":1}`, nil))
	if len(s.Board.RadiantBans) != 1 || s.Board.RadiantBans[0] != 1 {
		t.Fatalf("hero 1 should be banned by Radiant, got %+v", s.Board)
	}
	if len(s.Heroes) != 5 {
		t.Errorf("applied hero must leave the picker: got %d heroes, want 5", len(s.Heroes))
	}

	// A rejected transition is reported in the status line, not as an HTTP error —
	// the front ends surface it the same way.
	s = decodeState(t, do(t, srv, http.MethodPost, "/api/apply", `{"heroId":1}`, nil))
	if !strings.Contains(s.Status, "already picked or banned") {
		t.Errorf("duplicate apply should report a status, got %q", s.Status)
	}
	if len(s.Board.RadiantBans) != 1 {
		t.Errorf("duplicate apply must not change the board, got %+v", s.Board)
	}
}

func TestUndoRedoRoundTrip(t *testing.T) {
	srv := testServer(t)
	do(t, srv, http.MethodPost, "/api/apply", `{"heroId":1}`, nil)

	s := decodeState(t, do(t, srv, http.MethodPost, "/api/undo", "", nil))
	if len(s.Board.RadiantBans) != 0 {
		t.Fatalf("undo should clear the ban, got %+v", s.Board)
	}
	if !s.Meta.CanRedo {
		t.Error("undo should enable redo")
	}

	s = decodeState(t, do(t, srv, http.MethodPost, "/api/redo", "", nil))
	if len(s.Board.RadiantBans) != 1 {
		t.Fatalf("redo should restore the ban, got %+v", s.Board)
	}
}

func TestResultMatchesCoreExactly(t *testing.T) {
	srv := testServer(t)
	do(t, srv, http.MethodPost, "/api/apply", `{"heroId":1}`, nil)

	s := decodeState(t, do(t, srv, http.MethodGet, "/api/state", "", nil))
	want := srv.sess.Result()

	if len(s.Result.Recs) != len(want.Recs) {
		t.Fatalf("recs len = %d, core = %d", len(s.Result.Recs), len(want.Recs))
	}
	// The wire format must preserve the core's ranking exactly: same order, same
	// rounded scores. Re-ranking anywhere downstream is what §6 forbids.
	//
	// Compared field by field rather than by struct equality: Recommendation carries
	// a Factors breakdown that is deliberately not serialised (it feeds the decision
	// log in-process, §6.1), so it is nil on this side by design and its absence is
	// not a divergence in what the browser is shown.
	for i, r := range s.Result.Recs {
		w := want.Recs[i]
		if r.HeroID != w.HeroID || r.Score != w.Score || r.Why != w.Why {
			t.Errorf("recs[%d] = %+v, core = %+v", i, r, w)
		}
	}
	for i, e := range s.Result.EnemyPred {
		if e != want.EnemyPred[i] {
			t.Errorf("enemyPred[%d] = %+v, core = %+v", i, e, want.EnemyPred[i])
		}
	}
}

func TestCrossSitePostRejected(t *testing.T) {
	srv := testServer(t)

	w := do(t, srv, http.MethodPost, "/api/apply", `{"heroId":1}`,
		map[string]string{"Sec-Fetch-Site": "cross-site"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST status = %d, want 403", w.Code)
	}

	// The rejected request must not have touched the draft.
	s := decodeState(t, do(t, srv, http.MethodGet, "/api/state", "", nil))
	if len(s.Board.RadiantBans) != 0 {
		t.Errorf("cross-site POST changed state: %+v", s.Board)
	}

	// Same-origin still works.
	w = do(t, srv, http.MethodPost, "/api/apply", `{"heroId":1}`,
		map[string]string{"Sec-Fetch-Site": "same-origin"})
	if w.Code != http.StatusOK {
		t.Errorf("same-origin POST status = %d, want 200", w.Code)
	}
}

func TestGetIsNotAMutation(t *testing.T) {
	srv := testServer(t)
	w := do(t, srv, http.MethodGet, "/api/apply", "", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET on a mutation endpoint = %d, want 405", w.Code)
	}
}

func TestStaticAssetsAreServed(t *testing.T) {
	srv := testServer(t)
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		w := do(t, srv, http.MethodGet, path, "", nil)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
		}
		if w.Body.Len() == 0 {
			t.Errorf("GET %s served an empty body", path)
		}
	}
}
