package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"cmnet/internal/draft"
)

// SetRemoteToken enables /api/remote/* when token is non-empty. Empty disables
// the remote-control surface (Discord bot gets 404 / 503).
func (s *Server) SetRemoteToken(token string) {
	s.mu.Lock()
	s.remoteToken = strings.TrimSpace(token)
	s.mu.Unlock()
}

func (s *Server) remoteEnabled() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.remoteToken == "" {
		return "", false
	}
	return s.remoteToken, true
}

func (s *Server) routeRemote(mux *http.ServeMux) {
	mux.HandleFunc("/api/remote/state", s.remoteAuth(s.handleRemoteState))
	mux.HandleFunc("/api/remote/events", s.remoteAuth(s.handleRemoteEvents))
	mux.HandleFunc("/api/remote/scout", s.remoteAuth(s.mutate(s.startScout)))
	mux.HandleFunc("/api/remote/scout-clear", s.remoteAuth(s.mutate(s.clearScout)))
	mux.HandleFunc("/api/remote/side", s.remoteAuth(s.mutate(s.doSide)))
	mux.HandleFunc("/api/remote/lineup", s.remoteAuth(s.mutate(s.doLineup)))
}

// remoteAuth requires X-Advisor-Token to match the configured remote token.
func (s *Server) remoteAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		want, ok := s.remoteEnabled()
		if !ok {
			http.Error(w, "remote control is disabled (set --remote-token)", http.StatusServiceUnavailable)
			return
		}
		got := strings.TrimSpace(r.Header.Get("X-Advisor-Token"))
		if got == "" {
			// Also accept Authorization: Bearer …
			if a := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(a), "bearer ") {
				got = strings.TrimSpace(a[7:])
			}
		}
		if got != want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleRemoteState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.snapshot(false))
}

func (s *Server) handleRemoteEvents(w http.ResponseWriter, r *http.Request) {
	// Same SSE stream the browser uses; auth already checked.
	s.handleEvents(w, r)
}

func (s *Server) clearScout(r *http.Request) (string, error) {
	var req struct {
		Ally bool `json:"ally"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			return "", err
		}
	}
	if s.shell.ClearScout == nil {
		return "", fmt.Errorf("clear scout is not available in this build")
	}
	summary, err := s.shell.ClearScout(req.Ally)
	if err != nil {
		return "", err
	}
	_ = s.reloadSession()
	return summary, nil
}

// parseSideTeam reads an optional {"team":"radiant"|"dire"} body. ok=false means
// toggle (no team field / empty body).
func parseSideTeam(r *http.Request) (draft.Team, bool, error) {
	if r.Body == nil {
		return 0, false, nil
	}
	var req struct {
		Team string `json:"team"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "EOF" {
			return 0, false, nil
		}
		return 0, false, err
	}
	req.Team = strings.TrimSpace(strings.ToLower(req.Team))
	if req.Team == "" {
		return 0, false, nil
	}
	switch req.Team {
	case "radiant", "r":
		return draft.Radiant, true, nil
	case "dire", "d":
		return draft.Dire, true, nil
	default:
		return 0, false, fmt.Errorf("team must be radiant|dire")
	}
}