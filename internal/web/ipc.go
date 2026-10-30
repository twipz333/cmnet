package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Only WPF sets this secret in its child environment, never in argv or a file.
func ipcTokenFromEnv() string { return strings.TrimSpace(os.Getenv("CMNET_IPC_TOKEN")) }

func newSessionID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil { panic("secure random unavailable: " + err.Error()) }
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return hex.EncodeToString(id[:])
}

func isLoopbackHost(hostport string) bool {
	name := hostport
	if host, _, err := net.SplitHostPort(hostport); err == nil { name = host }
	name = strings.Trim(name, "[]")
	if strings.EqualFold(name, "localhost") { return true }
	ip := net.ParseIP(name)
	return ip != nil && ip.IsLoopback()
}

// The listener boundary applies to GET, SSE and POST, not only mutations.
// This prevents DNS rebinding and browser-origin exfiltration of player data.
func (s *Server) secureRoutes() http.Handler {
	next := s.routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "invalid local Host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil { scheme = "https" }
			if err != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" {
				http.Error(w, "invalid Origin", http.StatusForbidden)
				return
			}
		}
		switch r.Header.Get("Sec-Fetch-Site") {
		case "", "same-origin", "none":
		default:
			http.Error(w, "cross-site request", http.StatusForbidden)
			return
		}
		if s.ipcToken != "" && strings.HasPrefix(r.URL.Path, "/api/") {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.ipcToken)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "invalid IPC credential", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
