package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"cmnet/internal/scout"
	"cmnet/internal/session"
)

func (s *Server) routeTeam(mux *http.ServeMux) {
	mux.HandleFunc("/api/team/seat", s.mutate(s.setSeat))
}

// teamView is the cached team sheet for the state frame (nil when this build
// keeps none). It is read from disk when a session is built and replaced on
// every seat change, so frames never touch the file.
func (s *Server) teamView() *session.TeamSheet {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.team == nil {
		return nil
	}
	t := *s.team
	return &t
}

// applyTeam loads the sheet and pins its seats into sess. Called for every new
// session: a rebuilt one (new bundle, new scouting) starts with empty seats.
func (s *Server) applyTeam(sess *session.Session) {
	if s.shell.GetTeam == nil || sess == nil {
		return
	}
	t, err := s.shell.GetTeam()
	if err != nil {
		return
	}
	s.mu.Lock()
	s.team = &t
	s.mu.Unlock()
	pinSide(sess, "our", t.Our)
	pinSide(sess, "enemy", t.Enemy)
}

func pinSide(sess *session.Session, side string, seats [5]int64) {
	for i, acct := range seats {
		if acct > 0 {
			_ = sess.SetLineupSlot(side, i+1, acct, 0, false)
		} else {
			_ = sess.SetLineupSlot(side, i+1, 0, 0, true)
		}
	}
}

func (s *Server) setSeat(r *http.Request) (string, error) {
	if s.shell.SetSeat == nil {
		return "", errors.New("состав команды недоступен в этом режиме")
	}
	var req struct {
		Side      string `json:"side"`
		Position  int    `json:"position"`
		AccountID int64  `json:"accountId"`
		// Ref accepts a raw Steam/OpenDota/STRATZ URL or vanity
		// (steamcommunity.com/id/…). When set, it overrides accountId after
		// ResolveAccountID — same path scout already uses for paste fields.
		Ref string `json:"ref"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	accountID := req.AccountID
	if ref := strings.TrimSpace(req.Ref); ref != "" {
		id, err := scout.ResolveAccountID(r.Context(), nil, ref)
		if err != nil {
			return "", fmt.Errorf("не удалось распознать игрока: %w", err)
		}
		accountID = id
	}
	t, err := s.shell.SetSeat(req.Side, req.Position, accountID)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.team = &t
	sess := s.sess
	s.mu.Unlock()
	if sess != nil {
		seats, _ := t.Side(req.Side)
		pinSide(sess, req.Side, *seats)
	}
	s.shareRefresh()
	if accountID == 0 {
		return "место освобождено", nil
	}
	return "состав обновлён", nil
}
