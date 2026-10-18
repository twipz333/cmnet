package behavioraudit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

func TestCaseAPIPayloadAmbiguousNames(t *testing.T) {
	space := action.DefaultSpaceV0()
	smoke, err := space.New(action.KindSmoke, action.TargetBot)
	if err != nil {
		t.Fatal(err)
	}
	group, err := space.New(action.KindGroup, action.TargetNone)
	if err != nil {
		t.Fatal(err)
	}
	c := AuditCase{
		ID:                       "amb-1",
		AutoStatus:               macrolabel.LabelStatusAmbiguous,
		MacroActionSchemaVersion: action.SchemaVersion,
		Competing: []macrolabel.CompetingCandidate{
			{ActionID: smoke.ID, Confidence: 0.55},
			{ActionID: group.ID, Confidence: 0.52},
		},
	}
	payload := caseAPIPayload(c)
	raw, err := json.Marshal(payload["competing"])
	if err != nil {
		t.Fatal(err)
	}
	var named []NamedCompeting
	if err := json.Unmarshal(raw, &named); err != nil {
		t.Fatal(err)
	}
	if named[0].ActionName != "SMOKE BOT" || named[1].ActionName != "GROUP" {
		t.Fatalf("names=%q / %q", named[0].ActionName, named[1].ActionName)
	}
	if strings.Contains(strings.ToLower(named[0].ActionName), "action ") {
		t.Fatal("leakage")
	}
	if named[0].Description == "" {
		t.Fatal("missing description")
	}
}

func TestHandleActionsHumanLabels(t *testing.T) {
	s := NewReviewServer(nil, "nul", "127.0.0.1:0")
	req := httptest.NewRequest(http.MethodGet, "/api/actions", nil)
	rr := httptest.NewRecorder()
	s.handleActions(rr, req)
	if rr.Code != 200 {
		t.Fatalf("code=%d", rr.Code)
	}
	var rows []struct {
		ID         int    `json:"id"`
		Label      string `json:"label"`
		ActionName string `json:"action_name"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("empty")
	}
	for _, r := range rows {
		if strings.HasPrefix(strings.ToLower(r.Label), "action ") {
			t.Fatalf("label leakage %q", r.Label)
		}
		if r.ActionName == "" || r.ActionName != r.Label {
			t.Fatalf("name=%q label=%q", r.ActionName, r.Label)
		}
		if strings.Contains(r.Label, " / ") {
			t.Fatalf("old slash form %q", r.Label)
		}
	}
	var foundSmoke, foundGroup bool
	for _, r := range rows {
		if r.ActionName == "SMOKE BOT" {
			foundSmoke = true
		}
		if r.ActionName == "GROUP" {
			foundGroup = true
		}
	}
	if !foundSmoke || !foundGroup {
		t.Fatal("missing canonical names in correction list")
	}
}

func TestListenAndServeSchemaMismatch(t *testing.T) {
	s := NewReviewServer([]AuditCase{{
		ID: "x", MacroActionSchemaVersion: "macro-action-v9.9",
	}}, "nul", "127.0.0.1:32199")
	err := s.ListenAndServe()
	if err == nil {
		t.Fatal("expected schema error")
	}
	if !strings.Contains(err.Error(), "schema mismatch") {
		t.Fatalf("got %v", err)
	}
}
