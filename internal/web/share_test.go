package web

import (
	"encoding/json"
	"net/http"
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
