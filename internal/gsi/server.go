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
	// token, when set, must match the auth token Dota echoes back from the config
	// file. The token was being written into the config and never checked, so the
	// endpoint accepted a POST from anything on the machine that could reach the
	// port and would happily draft off it.
	token string

	mu   sync.Mutex
	last *draft.Snapshot // merged running state (last picks + latest live clock)
	// Parse failures were dropped on the floor. They are the first symptom of the
	// schema shifting under us, so they are counted and the last one is kept.
	parseErrors int
	lastErr     string
	lastEventAt time.Time

	pipe *Pipe
}

// NewServer creates a Server listening on addr (e.g. "127.0.0.1:54001").
func NewServer(addr string) *Server {
	return NewServerWithToken(addr, "")
}

// NewServerWithToken creates a Server that only accepts events carrying token.
// An empty token accepts everything, which is what an install with no token
// configured produces.
func NewServerWithToken(addr, token string) *Server {
	s := &Server{
		addr:   addr,
		token:  token,
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
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Acknowledge immediately; parsing failures must not stall the game.
	w.WriteHeader(http.StatusOK)

	// Reject anything that does not carry the configured token. Dota echoes it
	// back from the config file we wrote, so a mismatch means the POST came from
	// something else.
	if s.token != "" && AuthToken(body) != s.token {
		s.noteError("event rejected: auth token mismatch")
		return
	}

	s.mu.Lock()
	receivedAt := time.Now()
	s.lastEventAt = receivedAt
	pipe := s.pipe
	s.mu.Unlock()

	// Merge the draft snapshot before queuing the tracker. RosterProvider reads
	// LastSnapshot, so a new match_id must wipe picks before Apply runs.
	snap, err := ParseDraft(body)
	if err != nil {
		// Counted rather than ignored: a decode failure is how a schema change
		// announces itself, and silence here is why one would go unnoticed until
		// the draft screen simply stopped updating.
		s.noteError(err.Error())
		if pipe != nil {
			pipe.Submit(receivedAt, body)
		}
		return
	}
	if snap != nil {
		s.push(s.merge(snap))
	}
	if pipe != nil {
		pipe.Submit(receivedAt, body)
	}
}

// merge folds a freshly parsed event into the running state. A map-only event
// (post-draft / All Pick: HasDraft=false) retains the last known picks instead of
// wiping them, and a draft-only event (no map block) carries the last live clock
// forward. The result is a self-contained snapshot the consumer can render directly.
func (s *Server) merge(in *draft.Snapshot) *draft.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := *in // copy; never mutate the caller's snapshot
	newMatch := false
	if in.Live != nil && s.last != nil && s.last.Live != nil {
		a, b := in.Live.MatchID, s.last.Live.MatchID
		if a != "" && b != "" && a != b {
			newMatch = true
		}
	}
	if !in.HasDraft && s.last != nil && !newMatch {
		out.RadiantPicks = s.last.RadiantPicks
		out.RadiantBans = s.last.RadiantBans
		out.DirePicks = s.last.DirePicks
		out.DireBans = s.last.DireBans
	}
	if out.Live == nil && s.last != nil && !newMatch {
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

// noteError records a rejected or unparseable event.
func (s *Server) noteError(msg string) {
	s.mu.Lock()
	s.parseErrors++
	s.lastErr = msg
	s.mu.Unlock()
}

// Health reports what the endpoint has seen: how many events failed, the last
// failure, and when the last good event arrived.
//
// Exposed because the failure mode this replaces was invisible — a schema shift
// or a stale config produced a draft screen that never updated and no way to tell
// which. --gsi-dump is the other half: it writes the payload that broke.
func (s *Server) Health() (parseErrors int, lastErr string, lastEvent time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.parseErrors, s.lastErr, s.lastEventAt
}

// SetPipe attaches a serial observation pipeline. Payloads are submitted after
// the HTTP handler has acknowledged the POST. nil disables it.
func (s *Server) SetPipe(p *Pipe) {
	s.mu.Lock()
	s.pipe = p
	s.mu.Unlock()
}

// LastSnapshot returns a copy of the merged running draft snapshot, or nil.
func (s *Server) LastSnapshot() *draft.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return nil
	}
	out := *s.last
	return &out
}
