package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"cmnet/internal/session"
)

func TestSetSeatAcceptsProfileURLRef(t *testing.T) {
	var gotSide string
	var gotPos int
	var gotID int64
	srv := New(session.ShellConfig{
		SetSeat: func(side string, position int, accountID int64) (session.TeamSheet, error) {
			gotSide, gotPos, gotID = side, position, accountID
			var sheet session.TeamSheet
			sheet.Enemy[position-1] = accountID
			return sheet, nil
		},
	}, "127.0.0.1:0")

	// Numeric Steam profile URL — ResolveAccountID via ParseAccountID, no HTTP.
	body := `{"side":"enemy","position":5,"accountId":0,"ref":"https://steamcommunity.com/profiles/76561198083722517"}`
	w := do(t, srv, http.MethodPost, "/api/team/seat", body, nil)
	var st stateDTO
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.Status, "обновлён") {
		t.Fatalf("status=%q", st.Status)
	}
	if gotSide != "enemy" || gotPos != 5 || gotID != 123456789 {
		t.Fatalf("SetSeat got side=%s pos=%d id=%d", gotSide, gotPos, gotID)
	}
}

func TestSetSeatRejectsBadRef(t *testing.T) {
	called := false
	srv := New(session.ShellConfig{
		SetSeat: func(string, int, int64) (session.TeamSheet, error) {
			called = true
			return session.TeamSheet{}, nil
		},
	}, "127.0.0.1:0")

	// Garbage that is neither numeric nor a vanity URL — no Steam HTTP round-trip.
	w := do(t, srv, http.MethodPost, "/api/team/seat",
		`{"side":"enemy","position":5,"ref":"not-a-steam-id"}`,
		nil)
	var st stateDTO
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("SetSeat must not run on unresolved ref")
	}
	if !strings.Contains(st.Status, "не удалось распознать") {
		t.Fatalf("expected resolve error in status, got %q", st.Status)
	}
}

func TestSetSeatClearsWithoutRef(t *testing.T) {
	var gotID int64 = -1
	srv := New(session.ShellConfig{
		SetSeat: func(_ string, _ int, accountID int64) (session.TeamSheet, error) {
			gotID = accountID
			return session.TeamSheet{}, nil
		},
	}, "127.0.0.1:0")

	w := do(t, srv, http.MethodPost, "/api/team/seat",
		`{"side":"enemy","position":5,"accountId":0}`, nil)
	var st stateDTO
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if gotID != 0 || !strings.Contains(st.Status, "освобождено") {
		t.Fatalf("clear: id=%d status=%q", gotID, st.Status)
	}
}
