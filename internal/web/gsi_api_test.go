package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmnet/internal/gsi"
	"cmnet/internal/session"
)

func TestGSIStatusAndInstall(t *testing.T) {
	dir := t.TempDir()
	srv := New(session.ShellConfig{GSI: false}, "127.0.0.1:0")

	w := do(t, srv, http.MethodGet, "/api/gsi/status?cfgDir="+url.QueryEscape(dir), "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", w.Code, w.Body.String())
	}
	var st gsiStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Installed || st.GSIEnabled || st.CfgFileName != gsi.CfgFileName {
		t.Fatalf("unexpected status: %+v", st)
	}
	if st.Note == "" {
		t.Fatal("expected non-empty note")
	}

	payload, err := json.Marshal(map[string]string{"cfgDir": dir, "addr": "127.0.0.1:54001"})
	if err != nil {
		t.Fatal(err)
	}
	w = do(t, srv, http.MethodPost, "/api/gsi/install", string(payload), map[string]string{
		"Content-Type": "application/json",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("install code = %d body=%s", w.Code, w.Body.String())
	}
	// mutate returns state DTO; status text is in the snapshot.
	s := decodeState(t, w)
	if !strings.Contains(s.Status, "GSI config written") {
		t.Fatalf("status = %q, want install confirmation", s.Status)
	}
	cfgPath := filepath.Join(dir, gsi.CfgFileName)
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("cfg missing after install: %v", err)
	}

	w = do(t, srv, http.MethodGet, "/api/gsi/status?cfgDir="+url.QueryEscape(dir), "", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Installed {
		t.Fatalf("expected installed after write: %+v", st)
	}
}
