package web

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"cmnet/internal/outcome"
	"cmnet/internal/sources/opendota"
)

// outcomeAgent polls OpenDota for the finished match after the captain skips
// win/loss in the post-draft survey.
type outcomeAgent struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	status string // human-readable, for the status bar
}

func (a *outcomeAgent) Status() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *outcomeAgent) setStatus(s string) {
	a.mu.Lock()
	a.status = s
	a.mu.Unlock()
}

// Stop cancels any in-flight search (manual outcome, new resolve, shutdown).
func (a *outcomeAgent) Stop() {
	a.mu.Lock()
	c := a.cancel
	a.cancel = nil
	a.status = ""
	a.mu.Unlock()
	if c != nil {
		c()
	}
}

// Start kicks off a background poll. Replaces any previous run.
func (s *Server) startOutcomeAgent(parent context.Context, q outcome.Query) {
	s.outcome.Stop()
	ctx, cancel := context.WithCancel(parent)
	s.outcome.mu.Lock()
	s.outcome.cancel = cancel
	s.outcome.status = "агент ищет результат матча…"
	s.outcome.mu.Unlock()
	s.setStatus(s.outcome.Status())
	s.broadcast(false)

	go s.runOutcomeAgent(ctx, q)
}

func (s *Server) runOutcomeAgent(ctx context.Context, q outcome.Query) {
	src := opendota.New()
	defer func() {
		s.outcome.mu.Lock()
		s.outcome.cancel = nil
		s.outcome.mu.Unlock()
	}()

	// Battle Cup games finish in ~30–50 min; OpenDota often lags a few minutes
	// after that. Cap well above a long game so we do not spam forever.
	deadline := time.Now().Add(3 * time.Hour)
	delay := 20 * time.Second
	attempt := 0
	for {
		attempt++
		if ctx.Err() != nil {
			s.outcome.setStatus("")
			return
		}
		res, err := outcome.Resolve(ctx, src, q)
		if err == nil {
			matchStr := strconv.FormatInt(res.MatchID, 10)
			if err := s.writeResolvedOutcome(q.DraftID, res.Win, matchStr, res.Note); err != nil {
				s.outcome.setStatus("агент: не удалось записать итог: " + err.Error())
				s.setStatus(s.outcome.Status())
				s.broadcast(false)
				return
			}
			msg := "агент: записан итог"
			if res.Win {
				msg += " — победа"
			} else {
				msg += " — поражение"
			}
			if res.MatchID > 0 {
				msg += " (match " + matchStr + ")"
			}
			s.outcome.setStatus("")
			s.setStatus(msg)
			s.broadcast(false)
			return
		}
		if errors.Is(err, outcome.ErrNotFound) && q.MatchID == 0 {
			// No id and no board hit yet — keep looking until deadline; the match
			// may simply not be in recentMatches yet.
			s.outcome.setStatus(fmt.Sprintf("агент ищет матч по составу… (попытка %d)", attempt))
		} else if errors.Is(err, outcome.ErrPending) || errors.Is(err, outcome.ErrNotFound) {
			s.outcome.setStatus(fmt.Sprintf("агент ждёт результат в OpenDota… (попытка %d)", attempt))
		} else if ctx.Err() != nil {
			s.outcome.setStatus("")
			return
		} else {
			s.outcome.setStatus("агент: " + err.Error())
			s.setStatus(s.outcome.Status())
			s.broadcast(false)
			// Transient API errors: keep polling.
		}
		s.setStatus(s.outcome.Status())
		s.broadcast(false)

		if time.Now().After(deadline) {
			s.outcome.setStatus("агент: не нашёл результат за 3 ч — отметь вручную в следующий раз")
			s.setStatus(s.outcome.Status())
			s.broadcast(false)
			return
		}
		select {
		case <-ctx.Done():
			s.outcome.setStatus("")
			return
		case <-time.After(delay):
		}
		if delay < 2*time.Minute {
			delay += 10 * time.Second
		}
	}
}

func (s *Server) writeResolvedOutcome(draftID string, win bool, matchID, note string) error {
	sess, err := s.session()
	if err != nil {
		return err
	}
	return sess.LogOutcomeFor(draftID, win, matchID, note)
}
