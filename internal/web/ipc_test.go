package web

import (
	"cmnet/internal/session"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecureLocalAPI(t *testing.T) {
	s := testServer(t)
	s.ipcToken = "unit-secret"
	tests := []struct {
		name string
		host string
		origin string
		fetchSite string
		auth string
		want int
	}{
		{"unauthenticated", "127.0.0.1:54000", "", "", "", http.StatusUnauthorized},
		{"bad token", "127.0.0.1:54000", "", "", "Bearer incorrect", http.StatusUnauthorized},
		{"authenticated", "127.0.0.1:54000", "", "", "Bearer unit-secret", http.StatusOK},
		{"dns rebinding", "evil.example:54000", "", "", "Bearer unit-secret", http.StatusForbidden},
		{"cross origin", "127.0.0.1:54000", "https://evil.example", "", "Bearer unit-secret", http.StatusForbidden},
		{"foreign local origin", "127.0.0.1:54000", "http://localhost:54001", "", "Bearer unit-secret", http.StatusForbidden},
		{"cross site GET", "127.0.0.1:54000", "", "cross-site", "Bearer unit-secret", http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name,func(t *testing.T){
			r := httptest.NewRequest(http.MethodGet,"/api/v1/state",nil)
			r.Host=tc.host
			if tc.origin!="" {r.Header.Set("Origin",tc.origin)}
			if tc.fetchSite!="" {r.Header.Set("Sec-Fetch-Site",tc.fetchSite)}
			if tc.auth!="" {r.Header.Set("Authorization",tc.auth)}
			w:=httptest.NewRecorder()
			s.secureRoutes().ServeHTTP(w,r)
			if w.Code!=tc.want {t.Fatalf("got %d want %d: %s",w.Code,tc.want,w.Body.String())}
			if w.Code==http.StatusOK && (!strings.Contains(w.Body.String(),"\"schemaVersion\":1") || !strings.Contains(w.Body.String(),"\"sessionId\"")) {
				t.Errorf("missing versioned contract: %s",w.Body.String())
			}
		})
	}
}

func TestWebListenerNeverBindsToPublicInterface(t *testing.T) {
	s:=testServer(t)
	s.addr="0.0.0.0:0"
	if err:=s.Run(context.Background()); err==nil || !strings.Contains(err.Error(),"non-loopback") {
		t.Fatalf("expected hard failure for public listener, got %v",err)
	}
}

func TestReloadPinsActiveBoardUntilNextDraft(t *testing.T) {
	s:=testServer(t)
	original:=s.sess.Config()
	s.shell.BuildScan=func()(session.Config,error){
		c:=original
		c.BundleVersion="next-bundle"
		return c,nil
	}
	before:=decodeState(t,do(t,s,http.MethodPost,"/api/apply",`{"heroId":1}`,nil))
	if len(before.Board.RadiantBans)!=1 {t.Fatal("first pick not recorded")}
	if err:=s.reloadSession();err!=nil {t.Fatal(err)}
	after:=decodeState(t,do(t,s,http.MethodGet,"/api/state","",nil))
	if !after.ModelUpdatePending || after.Meta.BundleVersion!=original.BundleVersion || len(after.Board.RadiantBans)!=1 {
		t.Fatalf("model changed mid-draft: %+v",after)
	}
	next:=decodeState(t,do(t,s,http.MethodPost,"/api/new-draft","",nil))
	if next.ModelUpdatePending || next.Meta.BundleVersion!="next-bundle" || len(next.Board.RadiantBans)!=0 {
		t.Fatalf("new model did not activate on new draft: %+v",next)
	}
}
