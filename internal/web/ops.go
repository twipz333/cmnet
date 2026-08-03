package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cmnet/internal/cache"
	"cmnet/internal/jobs"
	"cmnet/internal/session"
)

// maxProgressLines is how much of a job's log the browser is given per frame. The
// full history stays in the database and is paged through /api/jobs/log; this
// bounds what rides along on the live frame.
const maxProgressLines = 400

// jobDTO is one queued or finished job as the browser sees it.
type jobDTO struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	Priority int    `json:"priority"`
	Result   string `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
	Attempts int    `json:"attempts"`
	// Milliseconds since the job started and since its last log line, measured by
	// the SERVER: a browser that connects mid-run has no idea when it began.
	RunningMs uint64 `json:"runningMs"`
	QuietMs   uint64 `json:"quietMs"`
	// LastLine is the most recent line, so a collapsed row still says something.
	LastLine string `json:"lastLine,omitempty"`
	// Seq is the highest log sequence stored, which is what the page watches for
	// liveness — unlike the line count, it keeps moving on a long quiet run.
	Seq int64 `json:"seq"`
}

// logLineDTO is one structured log line.
//
// Structured rather than a formatted string: the page filters by level and groups
// by phase, and neither is recoverable once the fields are flattened into text.
type logLineDTO struct {
	Seq   int64  `json:"seq"`
	At    int64  `json:"at"`
	Level string `json:"level"`
	Phase string `json:"phase,omitempty"`
	Text  string `json:"text"`
}

// opsDTO is the work state as the browser sees it: what is in flight now, and the
// tail of recent history.
type opsDTO struct {
	Active []jobDTO `json:"active"`
	Recent []jobDTO `json:"recent"`
	// Running is the kind of the first active job, or "" — kept because the status
	// bar and the "is anything happening" checks read exactly this.
	Running string `json:"running"`
}

// jobsView builds the operations frame from the queue.
func (s *Server) jobsView() opsDTO {
	q := s.shell.Jobs
	if q == nil {
		return opsDTO{}
	}
	var d opsDTO
	for _, j := range q.Active() {
		dto := s.jobDTO(j)
		d.Active = append(d.Active, dto)
		if d.Running == "" && j.State == cache.JobRunning {
			d.Running = j.Kind
		}
	}
	for _, j := range q.Recent(recentJobs) {
		switch j.State {
		case cache.JobPending, cache.JobRunning:
			continue // already in Active
		}
		d.Recent = append(d.Recent, s.jobDTO(j))
	}
	return d
}

// recentJobs is how much finished history rides along on the live frame. The rest
// is a click away in the job log.
const recentJobs = 12

// logRevision is the highest log sequence across the jobs in flight, plus their
// count so a job starting or finishing also counts as a change.
func (s *Server) logRevision() uint64 {
	q := s.shell.Jobs
	if q == nil {
		return 0
	}
	var rev uint64
	for _, j := range q.Active() {
		rev++
		if _, seq, _, ok := q.LastLine(j.ID); ok {
			rev += uint64(seq)
		}
	}
	return rev
}

func (s *Server) jobDTO(j cache.Job) jobDTO {
	d := jobDTO{
		ID: j.ID, Kind: j.Kind, State: string(j.State), Priority: j.Priority,
		Result: j.Result, Error: j.Error, Attempts: j.Attempts,
	}
	if q := s.shell.Jobs; q != nil {
		if line, seq, at, ok := q.LastLine(j.ID); ok {
			d.LastLine = line
			d.Seq = seq
			if j.State == cache.JobRunning {
				d.QuietMs = uint64(time.Since(time.UnixMilli(at)).Milliseconds())
			}
		}
	}
	if j.State == cache.JobRunning && j.StartedAt > 0 {
		d.RunningMs = uint64(time.Since(time.Unix(j.StartedAt, 0)).Milliseconds())
	}
	return d
}

// --- routes -----------------------------------------------------------------

func (s *Server) routeOps(mux *http.ServeMux) {
	mux.HandleFunc("/api/ops/sync", s.mutate(s.startSync))
	mux.HandleFunc("/api/ops/train", s.mutate(s.startTrain))
	mux.HandleFunc("/api/ops/scout", s.mutate(s.startScout))
	mux.HandleFunc("/api/ops/scout-allies", s.mutate(s.startScoutAllies))
	mux.HandleFunc("/api/ops/cancel", s.mutate(s.cancelOp))

	// The job log is its own endpoint rather than a field on the live frame: a run
	// emits thousands of lines, and the frame is rebuilt several times a second.
	// The page asks for the pages it is actually showing.
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/jobs/log", s.handleJobLog)

	// The local address book: who we have scouted or played with, so a teammate is
	// a click rather than a profile URL pasted again.
	mux.HandleFunc("/api/players", s.handleKnownPlayers)
	mux.HandleFunc("/api/players/forget", s.mutate(s.forgetPlayer))
	mux.HandleFunc("/api/players/pin", s.mutate(s.pinPlayer))

	mux.HandleFunc("/api/corpus", s.handleCorpus)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/stats/hero-pool", s.mutate(s.setHeroPool))
	mux.HandleFunc("/api/stats/item-rules", s.mutate(s.setItemRules))
	mux.HandleFunc("/api/stats/remove-player", s.mutate(s.removePlayer))
	mux.HandleFunc("/api/stats/clear-layer3", s.mutate(s.clearLayer3))
}

// --- long operations --------------------------------------------------------

// queue returns the work queue, or an error naming what is unavailable without it.
func (s *Server) queue(kind string) (*jobs.Queue, error) {
	if s.shell.Jobs == nil {
		return nil, jobs.KindError(kind)
	}
	return s.shell.Jobs, nil
}

// start queues a job and returns immediately.
//
// Nothing is rejected for being "already running" any more: the queue decides what
// may overlap, by resource, and a manual request preempts the background copy of
// the same kind. The browser follows along over SSE.
//
// reloadAfter rebuilds the draft session once the job succeeds, so a fresh bundle
// or snapshot takes effect without a restart. It watches from a goroutine rather
// than blocking the request, and only on success: reloading after a failed train
// would swap in whatever bundle happened to be on disk.
func (s *Server) start(kind, payload string, dedupe, reloadAfter bool) (string, error) {
	q, err := s.queue(kind)
	if err != nil {
		return "", err
	}
	id, err := q.Enqueue(kind, jobs.PriorityManual, payload, dedupe)
	if err != nil {
		return "", err
	}

	// One frame pump per in-flight job, so a chatty run pushes at a bounded rate
	// instead of once per log line. Broadcasting inside the progress callback is
	// what used to make a sync spend its time rendering frames while every other
	// piece of session work queued behind it.
	pumpDone := make(chan struct{})
	go s.pumpProgress(pumpDone)

	go func() {
		defer close(pumpDone)
		s.mu.Lock()
		base := s.baseCtx
		s.mu.Unlock()
		if base == nil {
			base = context.Background()
		}
		_, ferr := q.Follow(base, id, nil)
		if ferr == nil && reloadAfter {
			if rerr := s.reloadSession(); rerr != nil {
				_ = q.Note(id, cache.LogError, "reload failed: "+rerr.Error())
			}
		}
		s.broadcast(false)
	}()

	return kind + " queued", nil
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
			// The revision is the highest log sequence across everything in flight:
			// unlike a line count it keeps moving on a long run, which is exactly what
			// the browser watches to tell a quiet phase from a dead connection.
			rev := s.logRevision()
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
	return s.start(jobs.KindSync, "", true, false)
}

func (s *Server) startTrain(*http.Request) (string, error) {
	return s.start(jobs.KindTrain, "", true, true)
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
	return s.startScoutLike(r, jobs.KindScout)
}

func (s *Server) startScoutAllies(r *http.Request) (string, error) {
	return s.startScoutLike(r, jobs.KindScoutAllies)
}

// startScoutLike queues a scout. Not deduped: two scouts are two different player
// lists, and collapsing them would silently drop one.
func (s *Server) startScoutLike(r *http.Request, kind string) (string, error) {
	var req playersReq
	if err := decode(r, &req); err != nil {
		return "", err
	}
	players := splitPlayers(req.Players)
	if len(players) == 0 {
		return "", fmt.Errorf("give at least one player id or profile URL")
	}
	payload, err := json.Marshal(struct {
		Players []string `json:"players"`
	}{players})
	if err != nil {
		return "", err
	}
	return s.start(kind, string(payload), false, true)
}

// cancelOp stops a specific job, or everything in flight when no id is given.
func (s *Server) cancelOp(r *http.Request) (string, error) {
	q, err := s.queue("cancel")
	if err != nil {
		return "", err
	}
	var req struct {
		ID int64 `json:"id"`
	}
	_ = decode(r, &req) // an empty body means "whatever is running"

	if req.ID > 0 {
		if err := q.Cancel(req.ID); err != nil {
			return "", err
		}
		return "cancelling…", nil
	}
	active := q.Active()
	if len(active) == 0 {
		return "", fmt.Errorf("nothing is running")
	}
	for _, j := range active {
		if err := q.Cancel(j.ID); err != nil {
			return "", err
		}
	}
	return "cancelling…", nil
}

// --- job log ----------------------------------------------------------------

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	q, err := s.queue("jobs")
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	limit := intParam(r, "limit", 50)
	var out []jobDTO
	for _, j := range q.Recent(limit) {
		out = append(out, s.jobDTO(j))
	}
	writeJSON(w, http.StatusOK, struct {
		Jobs []jobDTO `json:"jobs"`
	}{out})
}

func (s *Server) handleJobLog(w http.ResponseWriter, r *http.Request) {
	q, err := s.queue("jobs")
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	id := int64(intParam(r, "id", 0))
	if id <= 0 {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	after := int64(intParam(r, "after", 0))
	lines, err := q.Log(id, after, intParam(r, "limit", maxProgressLines))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]logLineDTO, 0, len(lines))
	for _, l := range lines {
		out = append(out, logLineDTO{Seq: l.Seq, At: l.At, Level: l.Level, Phase: l.Phase, Text: l.Text})
	}
	var job *jobDTO
	if j, err := q.Job(id); err == nil && j != nil {
		d := s.jobDTO(*j)
		job = &d
	}
	writeJSON(w, http.StatusOK, struct {
		Job   *jobDTO      `json:"job,omitempty"`
		Lines []logLineDTO `json:"lines"`
	}{job, out})
}

// --- known players ------------------------------------------------------------

func (s *Server) handleKnownPlayers(w http.ResponseWriter, r *http.Request) {
	if s.shell.KnownPlayers == nil {
		http.Error(w, "the player list is not available in this build", http.StatusNotFound)
		return
	}
	players, err := s.shell.KnownPlayers(r.URL.Query().Get("role"))
	if err != nil {
		writeJSON(w, http.StatusOK, struct {
			Error string `json:"error"`
		}{err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Players []session.KnownPlayer `json:"players"`
	}{players})
}

func (s *Server) forgetPlayer(r *http.Request) (string, error) {
	var req struct {
		AccountID int64 `json:"accountId"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	if s.shell.ForgetPlayer == nil {
		return "", fmt.Errorf("forgetting a player is not available in this build")
	}
	return s.shell.ForgetPlayer(req.AccountID)
}

func (s *Server) pinPlayer(r *http.Request) (string, error) {
	var req struct {
		AccountID int64 `json:"accountId"`
		Pinned    bool  `json:"pinned"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	if s.shell.PinPlayer == nil {
		return "", fmt.Errorf("pinning a player is not available in this build")
	}
	return s.shell.PinPlayer(req.AccountID, req.Pinned)
}

func intParam(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
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
