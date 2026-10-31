package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmnet/internal/session"
	"cmnet/internal/share"
)

func TestGuestStateAndRefusals(t *testing.T) {
	t.Chdir(t.TempDir()) // guest.json is written to the working directory
	srv := New(session.ShellConfig{
		Guest:      true,
		ApplyShare: func(share.Payload) (string, error) { return "ok", nil },
	}, "127.0.0.1:0")

	w := do(t, srv, http.MethodGet, "/api/state", "", nil)
	var st stateDTO
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Share.Guest || st.Share.CanHost {
		t.Fatalf("guest share view = %+v", st.Share)
	}

	w = do(t, srv, http.MethodPost, "/api/share/start", "{}", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Share.Host != nil || !strings.Contains(st.Status, "гостев") {
		t.Fatalf("guest could open a share: status=%q host=%+v", st.Status, st.Share.Host)
	}

	w = do(t, srv, http.MethodPost, "/api/share/receive", `{"code":"abc"}`, nil)
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Share.Receiving || !strings.Contains(st.Status, "символов") {
		t.Fatalf("bad code started a receive: status=%q", st.Status)
	}
}

func TestHostWithoutModelCannotShare(t *testing.T) {
	srv := New(session.ShellConfig{
		SharePayload: func() (share.Payload, error) { return share.Payload{}, errNoModel },
	}, "127.0.0.1:0")
	w := do(t, srv, http.MethodPost, "/api/share/start", "{}", nil)
	var st stateDTO
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Share.CanHost || st.Share.Host != nil && st.Share.Host.Active {
		t.Fatalf("share opened without a model: %+v", st.Share)
	}
}

var errNoModel = &shareErr{"модель ещё не обучена"}

type shareErr struct{ s string }

func (e *shareErr) Error() string { return e.s }

func TestLocalFileExportImport(t *testing.T) {
	t.Setenv("CMNET_LOCAL_ONLY", "1")
	dir := t.TempDir()
	packPath := filepath.Join(dir, "friends.cmnetpack")
	applied := false
	srv := New(session.ShellConfig{
		SharePayload: func() (share.Payload, error) {
			return share.Payload{
				Meta: share.Meta{BundleVersion: "bv1", PatchTag: "7.39", HasSnapshot: true, Scouted: 2},
				Files: []share.File{
					{Name: "bundle.json", Data: []byte(`{"bundle_version":"bv1"}`)},
					{Name: "snapshot.json", Data: []byte(`{"ok":true}`)},
				},
			}, nil
		},
		ApplyShare: func(p share.Payload) (string, error) {
			applied = true
			if string(p.File("bundle.json")) != `{"bundle_version":"bv1"}` {
				t.Fatalf("unexpected bundle: %q", p.File("bundle.json"))
			}
			return "импортировано: модель. резервная копия: " + filepath.Join(dir, "import-backup", "stamp"), nil
		},
	}, "127.0.0.1:0")

	w := do(t, srv, http.MethodGet, "/api/state", "", nil)
	st := decodeState(t, w)
	if !st.Share.LocalOnly || !st.Share.FileMode || !st.Share.CanExport || !st.Share.CanImport || st.Share.CanHost {
		t.Fatalf("local file share view = %+v", st.Share)
	}

	w = do(t, srv, http.MethodPost, "/api/share/start", "{}", nil)
	st = decodeState(t, w)
	if st.Share.Host != nil || !strings.Contains(st.Status, "local-only") {
		t.Fatalf("relay start should stay blocked: status=%q host=%+v", st.Status, st.Share.Host)
	}

	body, _ := json.Marshal(map[string]string{"path": packPath})
	w = do(t, srv, http.MethodPost, "/api/share/export", string(body), nil)
	st = decodeState(t, w)
	if !strings.Contains(st.Status, "пакет сохранён") || st.Share.LastExport == nil {
		t.Fatalf("export failed: status=%q last=%+v", st.Status, st.Share.LastExport)
	}
	if _, err := os.Stat(packPath); err != nil {
		t.Fatalf("pack missing: %v", err)
	}

	previewBody, _ := json.Marshal(map[string]any{"path": packPath, "confirm": false})
	w = do(t, srv, http.MethodPost, "/api/share/import", string(previewBody), nil)
	st = decodeState(t, w)
	if applied || st.Share.ImportPreview == nil || !strings.Contains(st.Status, "подтверди") {
		t.Fatalf("preview should not apply: applied=%v status=%q preview=%+v", applied, st.Status, st.Share.ImportPreview)
	}

	confirmBody, _ := json.Marshal(map[string]any{"path": packPath, "confirm": true})
	w = do(t, srv, http.MethodPost, "/api/share/import", string(confirmBody), nil)
	st = decodeState(t, w)
	if !applied || st.Share.LastImport == nil || !strings.Contains(st.Status, "импортировано") {
		t.Fatalf("confirm import failed: applied=%v status=%q last=%+v", applied, st.Status, st.Share.LastImport)
	}
	if st.Share.LastImport.BackupNote == "" {
		t.Fatalf("expected backup note on last import: %+v", st.Share.LastImport)
	}
}
