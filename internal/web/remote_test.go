package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"cmnet/internal/session"
)

func TestRemoteRequiresToken(t *testing.T) {
	srv := New(session.ShellConfig{}, "127.0.0.1:0")
	srv.SetRemoteToken("secret")
	req := httptest.NewRequest(http.MethodGet, "/api/remote/state", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/remote/state", nil)
	req.Header.Set("X-Advisor-Token", "secret")
	w = httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("with token: status %d body %s", w.Code, w.Body.String())
	}
}

func TestRemoteDisabled(t *testing.T) {
	srv := New(session.ShellConfig{}, "127.0.0.1:0")
	req := httptest.NewRequest(http.MethodGet, "/api/remote/state", nil)
	req.Header.Set("X-Advisor-Token", "x")
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
}