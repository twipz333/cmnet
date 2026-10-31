package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"cmnet/internal/declog"
	"cmnet/internal/session"
)

func TestDeclogHistoryEmptyAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cmnet.db")
	srv := New(session.ShellConfig{CachePath: dbPath}, "127.0.0.1:0")

	w := do(t, srv, http.MethodGet, "/api/declog/history", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("empty list code=%d body=%s", w.Code, w.Body.String())
	}
	var empty historyListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Evidence != "empty" || empty.Count != 0 || empty.Path == "" {
		t.Fatalf("unexpected empty response: %+v", empty)
	}
	if empty.Note == "" {
		t.Fatal("expected honest empty note")
	}

	path := declog.PathFor(dbPath)
	l, err := declog.Open(path, declog.Retention{})
	if err != nil {
		t.Fatal(err)
	}
	id, err := l.StartDraft(declog.Header{
		OurTeam:       "radiant",
		FirstPick:     "radiant",
		Source:        "manual",
		BundleVersion: "test-bundle",
		PatchTag:      "7.39",
		SnapshotHash:  "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	str := 0.61
	if err := l.Decision(declog.Decision{
		Seq: 0, Action: "ban",
		Recs:       []declog.Rec{{HeroID: 11, Score: 0.55, Why: "meta mid"}, {HeroID: 74, Score: 0.50}},
		Chosen:     74,
		ChosenRank: 2,
		Strength:   &str,
	}); err != nil {
		t.Fatal(err)
	}
	if err := l.Reason(0, "тиммейт захотел"); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()

	w = do(t, srv, http.MethodGet, "/api/declog/history", "", nil)
	var list historyListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || len(list.Drafts) != 1 {
		t.Fatalf("want 1 draft, got %+v", list)
	}
	sum := list.Drafts[0]
	if sum.ID != id || sum.DecisionCount != 1 || sum.WithReason != 1 || sum.TookTop != 0 {
		t.Fatalf("summary mismatch: %+v", sum)
	}
	// Outcome missing → partial evidence, never invent win/loss.
	if list.Evidence != "partial" {
		t.Fatalf("evidence=%q want partial (missing outcome)", list.Evidence)
	}
	if sum.Outcome != nil {
		t.Fatal("must not invent outcome")
	}

	w = do(t, srv, http.MethodGet, "/api/declog/history?id="+id, "", nil)
	var detail historyDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if !detail.Found || detail.Draft == nil || len(detail.Draft.Decisions) != 1 {
		t.Fatalf("detail missing: %+v", detail)
	}
	dec := detail.Draft.Decisions[0]
	if dec.Chosen != 74 || dec.ChosenRank != 2 || dec.Reason != "тиммейт захотел" {
		t.Fatalf("decision mismatch: %+v", dec)
	}
	if dec.TopHero != 11 || dec.TopWhy != "meta mid" || dec.TopScore == nil || *dec.TopScore != 0.55 {
		t.Fatalf("top rec lost: %+v", dec)
	}
	if dec.Accepted {
		t.Fatal("rank 2 must not look accepted")
	}

	w = do(t, srv, http.MethodGet, "/api/declog/history?id=missing", "", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Found || detail.Evidence != "empty" {
		t.Fatalf("missing id should be empty evidence: %+v", detail)
	}
}

func TestDeclogHistoryUnavailableWithoutCache(t *testing.T) {
	srv := New(session.ShellConfig{}, "127.0.0.1:0")
	w := do(t, srv, http.MethodGet, "/api/declog/history", "", nil)
	var resp historyListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Evidence != "unavailable" || resp.Note == "" {
		t.Fatalf("want unavailable: %+v", resp)
	}
}
