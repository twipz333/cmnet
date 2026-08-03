package gsi

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"cmnet/internal/draft"
)

// Server is a local HTTP endpoint that Dota POSTs game-state events to. Parsed
// draft snapshots are delivered on Events() with latest-wins semantics so a slow
// consumer never blocks the game's POSTs.
type Server struct {
	addr   string
	events chan *draft.Snapshot
	http   *http.Server

	mu   sync.Mutex
	last *draft.Snapshot // merged running state (last picks + latest live clock)
}

// NewServer creates a Server listening on addr (e.g. "127.0.0.1:53000").
func NewServer(addr string) *Server {
	s := &Server{
		addr:   addr,
		events: make(chan *draft.Snapshot, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	s.http = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Events returns the channel of parsed draft snapshots.
func (s *Server) Events() <-chan *draft.Snapshot { return s.events }

// Addr returns the configured listen address.
func (s *Server) Addr() string { return s.addr }

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Acknowledge immediately; parsing failures must not stall the game.
	w.WriteHeader(http.StatusOK)

	snap, err := ParseDraft(body)
	if err != nil || snap == nil {
		return // uninteresting event or malformed payload: ignore
	}
	s.push(s.merge(snap))
}

// merge folds a freshly parsed event into the running state. A map-only event
// (post-draft / All Pick: HasDraft=false) retains the last known picks instead of
// wiping them, and a draft-only event (no map block) carries the last live clock
// forward. The result is a self-contained snapshot the consumer can render directly.
func (s *Server) merge(in *draft.Snapshot) *draft.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := *in // copy; never mutate the caller's snapshot
	if !in.HasDraft && s.last != nil {
		out.RadiantPicks = s.last.RadiantPicks
		out.RadiantBans = s.last.RadiantBans
		out.DirePicks = s.last.DirePicks
		out.DireBans = s.last.DireBans
	}
	if out.Live == nil && s.last != nil {
		out.Live = s.last.Live
	}
	s.last = &out
	return &out
}

// push delivers a snapshot with latest-wins semantics: if the buffer already
// holds an unread snapshot, drop it and replace with the newer one.
func (s *Server) push(snap *draft.Snapshot) {
	for {
		select {
		case s.events <- snap:
			return
		default:
			select {
			case <-s.events: // drop the stale snapshot, retry
			default:
			}
		}
	}
}

// Start runs the server until ctx is canceled, then shuts down gracefully.
func (s *Server) Start(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() {
		err := s.http.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errc <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return s.http.Shutdown(shutCtx)
	case err := <-errc:
		return err
	}
}
