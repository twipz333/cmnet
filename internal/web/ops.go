package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"cmnet/internal/session"
)

// maxProgressLines is how much of a long operation's log the UI keeps. The TUI
// shows the last 8; the browser has room for more, but this still bounds memory
// for a sync that emits thousands of lines.
const maxProgressLines = 400

// ops tracks the single long-running operation the shell allows at a time
// (sync/train/scout), mirroring the terminal shell's "running" screen: starting a
// second one while the first is in flight is rejected rather than queued.
type ops struct {
	mu      sync.Mutex
	name    string // "" when idle
	log     []string
	result  string
	failed  bool
	cancelF context.CancelFunc
	// rev increments on every appended line. The progress pump compares it against
	// the last revision it sent, so a quiet operation costs nothing and a chatty one
	// still only produces one frame per tick. It is also what the browser watches for
	// liveness: the log itself is capped, so its LENGTH stops changing on a long run
	// while work is still being done.
	rev uint64
	// startedAt/lastLineAt let the browser report how long the operation has been
	// running and how long it has been quiet. Both are measured here rather than in
	// the page: a browser that reconnects mid-operation has no idea when it began.
	startedAt  time.Time
	lastLineAt time.Time
}

func newOps() *ops { return &ops{} }

// opsDTO is the operation state as the browser sees it.
type opsDTO struct {
	Running string   `json:"running"` // "" when idle
	Log     []string `json:"log"`
	Result  string   `json:"result"`
	Failed  bool     `json:"failed"`
	// Rev is the monotonic count of log lines ever appended — unlike len(Log), which
	// saturates at maxProgressLines.
	Rev uint64 `json:"rev"`
	// Milliseconds since the operation started and since its last log line, as the
	// SERVER measured them at the moment this frame was built. The page ticks them
	// forward locally between frames.
	RunningMs uint64 `json:"runningMs"`
	QuietMs   uint64 `json:"quietMs"`
}

func (o *ops) dto() opsDTO {
	o.mu.Lock()
	defer o.mu.Unlock()
	logCopy := make([]string, len(o.log))
	copy(logCopy, o.log)
	d := opsDTO{Running: o.name, Log: logCopy, Result: o.result, Failed: o.failed, Rev: o.rev}
	if o.name != "" {
		d.RunningMs = uint64(time.Since(o.startedAt).Milliseconds())
		d.QuietMs = uint64(time.Since(o.lastLineAt).Milliseconds())
	}
	return d
}

// begin claims the operation slot. ok is false when another op is already running.
func (o *ops) begin(name string, cancel context.CancelFunc) (ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.name != "" {
		return false
	}
	now := time.Now()
	o.name = name
	o.log = nil
	o.result = ""
	o.failed = false
	o.cancelF = cancel
	o.startedAt = now
	o.lastLineAt = now
	return true
}

func (o *ops) append(line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.log = append(o.log, line)
	if len(o.log) > maxProgressLines {
		o.log = o.log[len(o.log)-maxProgressLines:]
	}
	o.rev++
	o.lastLineAt = time.Now()
}

// revision reports how many lines have been appended so far.
func (o *ops) revision() uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.rev
}

func (o *ops) finish(summary string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.name = ""
	o.cancelF = nil
	if err != nil {
		o.result = "error: " + err.Error()
		o.failed = true
		return
	}
	o.result = summary
	o.failed = false
}

// cancel stops the running operation, if any.
func (o *ops) cancel() {
	o.mu.Lock()
	c := o.cancelF
	o.mu.Unlock()
	if c != nil {
		c()
	}
}

func (o *ops) running() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.name
}

// --- routes -----------------------------------------------------------------

func (s *Server) routeOps(mux *http.ServeMux) {
	mux.HandleFunc("/api/ops/sync", s.mutate(s.startSync))
	mux.HandleFunc("/api/ops/train", s.mutate(s.startTrain))
	mux.HandleFunc("/api/ops/scout", s.mutate(s.startScout))
	mux.HandleFunc("/api/ops/scout-allies", s.mutate(s.startScoutAllies))
	mux.HandleFunc("/api/ops/cancel", s.mutate(s.cancelOp))

	mux.HandleFunc("/api/corpus", s.handleCorpus)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/stats/hero-pool", s.mutate(s.setHeroPool))
	mux.HandleFunc("/api/stats/item-rules", s.mutate(s.setItemRules))
	mux.HandleFunc("/api/stats/remove-player", s.mutate(s.removePlayer))
	mux.HandleFunc("/api/stats/clear-layer3", s.mutate(s.clearLayer3))
}

// --- long operations --------------------------------------------------------

// longOp is the shape of sync/train (no arguments) and, after binding players, of
// scout/scout-allies.
type longOp func(ctx context.Context, progress func(string)) (string, error)

// run starts fn in the background, streaming its progress lines to every browser.
// Returns immediately: the UI follows along over SSE, exactly like the terminal
// shell's spinner screen. reloadAfter rebuilds the draft session on success, so a
// fresh bundle or snapshot takes effect without a restart.
func (s *Server) run(name string, fn longOp, reloadAfter bool) (string, error) {
	if fn == nil {
		return "", fmt.Errorf("%s is not available in this build", name)
	}

	s.mu.Lock()
	base := s.baseCtx
	s.mu.Unlock()
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)

	if !s.ops.begin(name, cancel) {
		cancel()
		return "", fmt.Errorf("%s is already running", s.ops.running())
	}

	// The progress pump decouples the operation from the browsers watching it.
	// Broadcasting inside the progress callback instead made every log line
	// serialise the entire UI state and take the session lock — so a chatty sync
	// spent its time rendering frames, and any competing session work (a GSI event
	// recomputing the draft, the Statistics screen loading) stalled it further. Now
	// the operation only appends, and one frame per tick carries the latest state.
	pumpDone := make(chan struct{})
	go s.pumpProgress(pumpDone)

	go func() {
		defer cancel()
		summary, err := fn(ctx, s.ops.append)
		// The reload happens BEFORE finish: it is part of the operation, so the slot
		// must stay claimed until it is done (otherwise a second operation can start
		// mid-reload), and a reload failure has to reach the user as a failure rather
		// than a success with a stray log line.
		if err == nil && reloadAfter {
			if rerr := s.reloadSession(); rerr != nil {
				s.ops.append("reload failed: " + rerr.Error())
				err = fmt.Errorf("%s finished but reloading it failed: %w", name, rerr)
			}
		}
		s.ops.finish(summary, err)
		close(pumpDone) // stop the pump before the final, authoritative frame
		s.broadcast(false)
	}()

	return name + " started", nil
}

// progressInterval is how often a running operation's log is pushed to browsers.
// Fast enough to read as live, slow enough that a sync emitting thousands of lines
// costs a bounded number of frames.
const progressInterval = 200 * time.Millisecond

// keepaliveInterval is how often a frame is sent even with nothing new to say.
// Without it a genuinely quiet phase and a dead connection look identical in the
// browser, which is the confusion this whole path exists to remove. One small frame
// every few seconds also keeps the SSE stream from being reaped as idle.
const keepaliveInterval = 3 * time.Second

// pumpProgress broadcasts the current state while an operation runs: promptly when
// something was logged, and on a slow keepalive otherwise.
func (s *Server) pumpProgress(done <-chan struct{}) {
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	var (
		sent     uint64
		lastSend = time.Now()
	)
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			rev := s.ops.revision()
			if rev == sent && time.Since(lastSend) < keepaliveInterval {
				continue
			}
			sent = rev
			lastSend = time.Now()
			s.broadcast(false)
		}
	}
}

func (s *Server) startSync(*http.Request) (string, error) {
	// Sync only fills the cache; the bundle is unchanged until Train runs.
	return s.run("sync", s.shell.Sync, false)
}

func (s *Server) startTrain(*http.Request) (string, error) {
	return s.run("train", s.shell.Train, true)
}

// playersReq carries the space- or comma-separated player list from the scout
// screens.
type playersReq struct {
	Players string `json:"players"`
}

// splitPlayers accepts whitespace- or comma-separated ids/URLs, like the TUI input.
func splitPlayers(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}

func (s *Server) startScout(r *http.Request) (string, error) {
	return s.startScoutLike(r, "scout", s.shell.Scout)
}

func (s *Server) startScoutAllies(r *http.Request) (string, error) {
	return s.startScoutLike(r, "scout allies", s.shell.ScoutAllies)
}

func (s *Server) startScoutLike(r *http.Request, name string,
	fn func(context.Context, []string, func(string)) (string, error)) (string, error) {
	var req playersReq
	if err := decode(r, &req); err != nil {
		return "", err
	}
	players := splitPlayers(req.Players)
	if len(players) == 0 {
		return "", fmt.Errorf("give at least one player id or profile URL")
	}
	if fn == nil {
		return "", fmt.Errorf("%s is not available in this build", name)
	}
	return s.run(name, func(ctx context.Context, progress func(string)) (string, error) {
		return fn(ctx, players, progress)
	}, true)
}

func (s *Server) cancelOp(*http.Request) (string, error) {
	name := s.ops.running()
	if name == "" {
		return "", fmt.Errorf("nothing is running")
	}
	s.ops.cancel()
	return "cancelling " + name + "…", nil
}

// --- snapshot editing -------------------------------------------------------

// handleCorpus serves the Data screen's picture of the cache.
//
// Its own endpoint rather than a field on /api/state: this runs half a dozen
// aggregate scans, and /api/state is built on every poll and every GSI event.
// The screen asks for it when it opens and when the user refreshes.
func (s *Server) handleCorpus(w http.ResponseWriter, r *http.Request) {
	if s.shell.Corpus == nil {
		http.Error(w, "corpus statistics are not available in this build", http.StatusNotFound)
		return
	}
	stats, err := s.shell.Corpus()
	if err != nil {
		// Reported in the page rather than as an HTTP error: an unreadable cache is
		// something to explain on the screen the user is looking at, not a dead fetch.
		writeJSON(w, http.StatusOK, corpusDTO{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, corpusDTO{Corpus: &stats})
}

type corpusDTO struct {
	Corpus *session.CorpusStats `json:"corpus,omitempty"`
	Error  string               `json:"error,omitempty"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.shell.Stats == nil {
		http.Error(w, "statistics are not available in this build", http.StatusNotFound)
		return
	}
	view, err := s.shell.Stats()
	if err != nil {
		writeJSON(w, http.StatusOK, struct {
			Error string `json:"error"`
		}{err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, statsDTO{Stats: view})
}

type statsDTO struct {
	Stats session.StatsView `json:"stats"`
	Error string            `json:"error,omitempty"`
}

func (s *Server) setHeroPool(r *http.Request) (string, error) {
	var req struct {
		Tokens string `json:"tokens"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	if s.shell.SetHeroPool == nil {
		return "", fmt.Errorf("hero pool editing is not available in this build")
	}
	// Comma-separated so multi-word names ("Crystal Maiden") survive, matching the
	// terminal shell's parsing.
	var tokens []string
	for _, t := range strings.Split(req.Tokens, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tokens = append(tokens, t)
		}
	}
	summary, err := s.shell.SetHeroPool(tokens)
	if err != nil {
		return "", err
	}
	if err := s.reloadSession(); err != nil {
		return summary, nil // the pool was saved; the draft reload can retry later
	}
	return summary, nil
}

func (s *Server) setItemRules(r *http.Request) (string, error) {
	var req struct {
		Text string `json:"text"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	if s.shell.SetItemRules == nil {
		return "", fmt.Errorf("advice rules are not available in this build")
	}
	summary, err := s.shell.SetItemRules(req.Text)
	if err != nil {
		return "", err
	}
	_ = s.reloadSession()
	return summary, nil
}

func (s *Server) removePlayer(r *http.Request) (string, error) {
	var req struct {
		AccountID int64 `json:"accountId"`
		Ally      bool  `json:"ally"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	if s.shell.RemovePlayer == nil {
		return "", fmt.Errorf("player removal is not available in this build")
	}
	summary, err := s.shell.RemovePlayer(req.AccountID, req.Ally)
	if err != nil {
		return "", err
	}
	_ = s.reloadSession()
	return summary, nil
}

func (s *Server) clearLayer3(*http.Request) (string, error) {
	if s.shell.ClearLayer3 == nil {
		return "", fmt.Errorf("clearing event drafts is not available in this build")
	}
	summary, err := s.shell.ClearLayer3()
	if err != nil {
		return "", err
	}
	_ = s.reloadSession()
	return summary, nil
}
