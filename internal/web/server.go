// Package web is the local browser front end: a small HTTP server over the same
// session and operations the TUI uses. It renders nothing itself and computes
// nothing — it serialises the core's Result and streams state changes — so both
// front ends stay byte-identical in what they show (§6/§8).
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	// Aliased: this package already has an embed.FS called assets for the static UI.
	assetstore "cmnet/internal/assets"
	"cmnet/internal/cache"
	"cmnet/internal/draft"
	"cmnet/internal/objectives"
	"cmnet/internal/recommender"
	"cmnet/internal/session"
)

// Server serves the web UI and its JSON API. It offers everything the terminal
// shell does: the draft scan screen plus sync/train/scout and snapshot editing.
type Server struct {
	shell session.ShellConfig
	addr  string
	hub   *hub

	mu       sync.Mutex
	sess     *session.Session // built lazily from shell.BuildScan; nil until then
	sessErr  string           // why the last build failed, for the status bar
	pendingCfg *session.Config // model swaps only between drafts
	revision uint64            // monotonic event revision
	sessionID string           // per-process session identity
	ipcToken string            // per-child native bearer secret
	localOnly bool             // native edition forbids public-relay sharing
	commandMu sync.Mutex      // serializes manual state changes + idempotency
	receipts map[string][32]byte
	receiptOrder []string
	localCache *cache.Store    // local durable draft checkpoints
	draftID string            // changes only when the captain starts a new manual draft
	draftRevision uint64      // persisted board ordering, independent of SSE progress
	status   string           // last human-readable outcome
	gsiOnce  sync.Once
	baseCtx  context.Context // server lifetime, for the GSI pump
	pumpDone bool

	// outcome polls OpenDota when the captain skips win/loss after a draft.
	outcome outcomeAgent

	remoteToken string // shared secret for /api/remote/*; empty = disabled

	shareSt shareState         // host code / guest receive
	team    *session.TeamSheet // cached team sheet; nil until a session loads it
}

// New builds a server over the app's operation callbacks. The draft session is
// built on demand (and rebuilt after train/scout), so the UI still comes up when
// no bundle exists yet — the user can go straight to Train.
func New(shell session.ShellConfig, addr string) *Server {
	s := &Server{shell: shell, addr: addr, hub: newHub(), revision: 1, sessionID: newSessionID(), ipcToken: ipcTokenFromEnv(), draftID: newSessionID(), localOnly: os.Getenv("CMNET_LOCAL_ONLY") == "1"}
	if shell.Auto != nil {
		// A bundle trained in the background is of no use while the server keeps
		// serving the one it loaded at startup. Rebuilding the session picks it up,
		// and pushing a frame lets every open browser see that it happened.
		shell.Auto.SetOnTrained(func() error {
			if err := s.reloadSession(); err != nil {
				return err // reported in the worker's status and retried later
			}
			s.broadcast(false)
			return nil
		})
	}
	return s
}

// autoStatus projects the background worker's state for the browser, or nil when
// there is no worker.
func autoStatus(a *session.Auto) *session.AutoStatus {
	if a == nil {
		return nil
	}
	st := a.Status()
	return &st
}

// NewForSession builds a server over an already-constructed draft session, for
// callers that have no shell operations to offer. Nothing in the binary does —
// the browser UI is always the full shell (`advisor --web`), so that a screen is
// never silently inert — but tests use it to exercise the draft screen alone.
func NewForSession(sess *session.Session, addr string) *Server {
	s := New(session.ShellConfig{}, addr)
	s.sess = sess
	return s
}

// Addr returns the address the server is listening on (resolved after Run binds).
func (s *Server) Addr() string { return s.addr }

// Run starts the HTTP server and blocks until ctx is cancelled or it fails.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	// Never accidentally expose the local desktop API to a LAN.
	if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		_ = ln.Close()
		return fmt.Errorf("non-loopback web listener refused: %s", s.addr)
	}
	s.addr = ln.Addr().String()
	if s.shell.CachePath != "" {
		store, err := cache.Open(s.shell.CachePath)
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("open local checkpoint cache: %w", err)
		}
		s.localCache = store
		defer store.Close()
	}

	s.mu.Lock()
	s.baseCtx = ctx
	s.mu.Unlock()

	// Build the draft session up front when possible so GSI events are not missed
	// while the user is still on another screen. A failure is not fatal: it is
	// reported in the UI and retried on the next request.
	_, _ = s.session()
	s.restoreCheckpoint(ctx)
	s.resumeFollow() // a guest who followed the host's draft picks it up again

	srv := &http.Server{
		Handler:           s.secureRoutes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		// Jobs are not cancelled on shutdown: they are rows, and the queue hands an
		// interrupted one back to be resumed next start. Killing them here would
		// throw away a sync's remaining band for no reason.
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// --- session lifecycle ------------------------------------------------------

// session returns the draft session, building it on first use. The error is also
// cached as a status string so the UI can explain why the draft screen is empty.
func (s *Server) session() (*session.Session, error) {
	s.mu.Lock()
	if s.sess != nil {
		sess := s.sess
		s.mu.Unlock()
		s.startGSIPump(sess)
		return sess, nil
	}
	build := s.shell.BuildScan
	s.mu.Unlock()

	if build == nil {
		return nil, fmt.Errorf("draft screen is not available in this mode")
	}
	cfg, err := build()
	if err != nil {
		s.mu.Lock()
		s.sessErr = err.Error()
		s.mu.Unlock()
		return nil, err
	}
	sess := session.New(cfg)
	s.applyTeam(sess)

	s.mu.Lock()
	s.sess = sess
	s.sessErr = ""
	s.mu.Unlock()

	s.startGSIPump(sess)
	s.followIntoSession()
	return sess, nil
}

// reloadSession freezes an active match's model even if training finishes.
// The candidate model is installed only when the captain starts the next draft.
func (s *Server) reloadSession() error {
	s.mu.Lock()
	build := s.shell.BuildScan
	s.mu.Unlock()
	if build == nil { return nil }
	cfg, err := build()
	if err != nil {
		s.mu.Lock(); s.sessErr = err.Error(); s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	if s.sess != nil && s.sess.DraftInProgress() {
		s.pendingCfg = &cfg
		s.sessErr = ""
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	fresh := session.New(cfg)
	s.applyTeam(fresh)
	s.mu.Lock()
	// Building the new config can race with the first captain pick.
	if s.sess != nil && s.sess.DraftInProgress() {
		s.pendingCfg = &cfg
		s.sessErr = ""
		s.mu.Unlock()
		return nil
	}
	s.sess = fresh
	s.pendingCfg = nil
	s.sessErr = ""
	s.mu.Unlock()
	s.startGSIPump(fresh)
	s.followIntoSession()
	s.shareRefresh()
	return nil
}

// startGSIPump wires GSI events into whatever session is current. It runs once per
// server: the event channel outlives individual sessions (the shell creates one
// GSI server for the whole run), so the pump looks the session up per event
// instead of capturing it.
func (s *Server) startGSIPump(sess *session.Session) {
	evs := sess.Config().GSIEvents
	if evs == nil {
		return
	}
	s.gsiOnce.Do(func() {
		s.mu.Lock()
		ctx := s.baseCtx
		s.pumpDone = true
		s.mu.Unlock()
		if ctx == nil {
			ctx = context.Background()
		}
		go s.pumpGSI(ctx, evs)
	})
}

// gsiFrameInterval bounds how often GSI traffic is pushed to browsers. Dota posts
// up to ten events a second and most of them only move the clock; every push
// serialises the whole state and makes the page re-render, so they are coalesced.
// The state carried is always the latest, so dropping intermediate frames loses
// nothing.
const gsiFrameInterval = 250 * time.Millisecond

// pumpGSI feeds live draft snapshots into the current session and pushes the
// resulting state to every connected browser.
//
// Every event is APPLIED (the session must not miss a pick), but the pushes are
// rate-limited: a coalesced frame is scheduled instead of sent, unless an
// objective just came due, which browsers have to hear about at once.
func (s *Server) pumpGSI(ctx context.Context, evs <-chan *draft.Snapshot) {
	var (
		pending  bool
		lastSent time.Time
		timer    = time.NewTimer(time.Hour)
	)
	timer.Stop()
	defer timer.Stop()

	flush := func(ring bool) {
		pending = false
		lastSent = time.Now()
		s.broadcast(ring)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if pending {
				flush(false)
			}
		case snap, ok := <-evs:
			if !ok {
				return
			}
			s.mu.Lock()
			sess := s.sess
			s.mu.Unlock()
			if sess == nil {
				continue
			}
			if sess.Following() {
				continue // the board follows the host, not this machine's Dota
			}
			ring := sess.ApplyGSI(snap)
			if s.shell.Auto != nil { s.shell.Auto.TouchDraft() } // GSI event, not a GET heartbeat
			if msg := sess.TakeStatus(); msg != "" {
				s.setStatus(msg)
			}

			// A due objective is the one thing the user reacts to in real time.
			if ring {
				timer.Stop()
				flush(true)
				continue
			}
			if wait := gsiFrameInterval - time.Since(lastSent); wait > 0 {
				if !pending {
					pending = true
					timer.Reset(wait)
				}
				continue
			}
			flush(false)
		}
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", staticHandler())
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/matchups", s.handleMatchups)
	mux.HandleFunc("/api/apply", s.mutate(s.doApply))
	mux.HandleFunc("/api/undo", s.mutate(s.doUndo))
	mux.HandleFunc("/api/redo", s.mutate(s.doRedo))
	mux.HandleFunc("/api/step-action", s.mutate(s.doStepAction))
	mux.HandleFunc("/api/step-team", s.mutate(s.doStepTeam))
	mux.HandleFunc("/api/side", s.mutate(s.doSide))
	mux.HandleFunc("/api/first-pick", s.mutate(s.doFirstPick))
	mux.HandleFunc("/api/enemy", s.mutate(s.doAddEnemy))
	mux.HandleFunc("/api/lineup", s.mutate(s.doLineup))
	mux.HandleFunc("/api/reason", s.mutate(s.doReason))
	mux.HandleFunc("/api/outcome", s.mutate(s.doOutcome))
	mux.HandleFunc("/api/outcome/resolve", s.mutate(s.doOutcomeResolve))
	mux.HandleFunc("/api/declog/review", s.handleDeclogReview)
	mux.HandleFunc("/api/reload", s.mutate(s.doReload))
	mux.HandleFunc("/api/new-draft", s.mutate(s.doNewDraft))
	mux.HandleFunc("/api/catalog", s.handleCatalog)
	mux.HandleFunc("/api/intelligence/observation", s.handleIntelligenceObservation)
	// Cached images, served from disk. The page must never reference a CDN: it is
	// opened from a local server and has to work with no network at all, which is
	// also how a laptop behaves on a Battle Cup night more often than one would
	// like. A missing file 404s and the UI falls back to its text chip.
	if s.shell.AssetsDir != "" {
		st, err := assetstore.New(s.shell.AssetsDir)
		if err == nil {
			mux.Handle("/assets/", st.Handler("/assets/"))
		}
	}
	s.routeOps(mux)
	s.routeShare(mux)
	s.routeTeam(mux)
	s.routeRemote(mux)
	// Explicit v1 alias while the Electron wire contract remains supported.
	mux.Handle("/api/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip only the version, never the /api prefix. The same handlers and
		// schemas serve legacy Electron and native clients during migration.
		forward := r.Clone(r.Context())
		u := *r.URL
		u.Path = "/api" + strings.TrimPrefix(r.URL.Path, "/api/v1")
		u.RawPath = ""
		forward.URL = &u
		mux.ServeHTTP(w, forward)
	}))
	return sameSiteOnly(mux)
}

// sameSiteOnly rejects cross-site state-changing requests. The server is bound to
// loopback with no auth (it is a local tool), but any page in the user's browser
// can POST to localhost — this keeps a random tab from driving their draft or
// firing scout requests.
func sameSiteOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			switch r.Header.Get("Sec-Fetch-Site") {
			case "", "same-origin", "none":
				// Same-origin, or a client that sends no fetch metadata (curl).
			default:
				http.Error(w, "cross-site requests are not allowed", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// --- state ------------------------------------------------------------------

// stateDTO is the whole UI state in one object. Result is embedded verbatim, so a
// new core field reaches the browser without touching this file.
type stateDTO struct {
	SchemaVersion int `json:"schemaVersion"`
	SessionID string `json:"sessionId"`
	Revision uint64 `json:"revision"`
	ModelUpdatePending bool `json:"modelUpdatePending"`
	Meta   metaDTO            `json:"meta"`
	Board  boardDTO           `json:"board"`
	Step   *stepDTO           `json:"step"`
	Result recommender.Result `json:"result"`
	Heroes []session.Hero     `json:"heroes"` // still available, in bundle order
	Names  map[string]string  `json:"names"`  // hero id -> display label
	Live   *draft.Live        `json:"live"`
	Alerts []objectives.Alert `json:"alerts"`
	Ops    opsDTO             `json:"ops"`
	Status string             `json:"status"`
	Ring   bool               `json:"ring"` // an objective just entered its alert window
	// Auto is the background collector's state, or nil when it is not running.
	Auto *session.AutoStatus `json:"auto"`
	// Lineup is the pos1-5 roster panel (our seats + enemy guesses).
	Lineup *session.LineupView `json:"lineup,omitempty"`
	// Sequence is the whole draft in order (24 steps) with the heroes placed so
	// far and the current step marked.
	Sequence []seqStepDTO `json:"sequence"`
	// Share is the host/guest panel.
	Share shareDTO `json:"share"`
	// Team is who plays which position (us and them), nil when not kept.
	Team *session.TeamSheet `json:"team"`
}

type metaDTO struct {
	BundleVersion string `json:"bundleVersion"`
	PatchTag      string `json:"patchTag"`
	OurTeam       string `json:"ourTeam"`
	FirstPick     string `json:"firstPick"`
	GSIMode       bool   `json:"gsiMode"`
	// Following: the board is the host's live draft (guest), not this Dota's.
	Following   bool `json:"following"`
	GSISeen     bool `json:"gsiSeen"`
	HasMatrices bool `json:"hasMatrices"`
	CanAddEnemy bool `json:"canAddEnemy"`
	CanUndo     bool `json:"canUndo"`
	CanRedo     bool `json:"canRedo"`
	// Logging reports whether decisions are being recorded (§6.1); LastDecision is
	// the step a reason would be attached to (-1 when none has been logged yet), so
	// the UI can name it instead of asking for a reason about nothing. LogError
	// carries the last logging failure, which the status bar shows: a log that
	// silently stopped recording is worse than one that is off.
	Logging      bool   `json:"logging"`
	LastDecision int    `json:"lastDecision"`
	LogError     string `json:"logError,omitempty"`
	// DraftReady reports whether a bundle is loaded; false means the draft screen
	// cannot work yet (train first). DraftError explains why.
	DraftReady bool   `json:"draftReady"`
	DraftError string `json:"draftError"`
	// Which shell operations this build offers, so the UI can disable the rest.
	CanSync          bool `json:"canSync"`
	CanTrain         bool `json:"canTrain"`
	CanScout         bool `json:"canScout"`
	CanScoutAllies   bool `json:"canScoutAllies"`
	CanStats         bool `json:"canStats"`
	CanConfig        bool `json:"canConfig"`
	CanDiscordConfig bool `json:"canDiscordConfig"`
	CanAlerts        bool `json:"canAlerts"`
	CanEditPool      bool `json:"canEditPool"`
	CanEditRules     bool `json:"canEditRules"`
}

type boardDTO struct {
	RadiantPicks []int `json:"radiantPicks"`
	RadiantBans  []int `json:"radiantBans"`
	DirePicks    []int `json:"direPicks"`
	DireBans     []int `json:"direBans"`
}

// empty reports whether nothing has been drafted yet.
func (b boardDTO) empty() bool {
	return len(b.RadiantPicks)+len(b.RadiantBans)+len(b.DirePicks)+len(b.DireBans) == 0
}

type stepDTO struct {
	Team   string `json:"team"`
	Action string `json:"action"`
	IsOurs bool   `json:"isOurs"`
}

// snapshot builds the current state payload. It never fails: without a session the
// draft fields are empty and meta.draftError explains why.
func (s *Server) snapshot(ring bool) stateDTO {
	s.mu.Lock()
	sess, sessErr, status, sh := s.sess, s.sessErr, s.status, s.shell
	revision, sessionID, pending := s.revision, s.sessionID, s.pendingCfg != nil
	s.mu.Unlock()

	st := stateDTO{
		SchemaVersion: 1, SessionID: sessionID, Revision: revision, ModelUpdatePending: pending,
		Meta: metaDTO{
			DraftReady:       sess != nil,
			DraftError:       sessErr,
			CanSync:          sh.Sync != nil,
			CanTrain:         sh.Train != nil,
			CanScout:         sh.Scout != nil,
			CanScoutAllies:   sh.ScoutAllies != nil,
			CanStats:         sh.Stats != nil,
			CanConfig:        sh.GetConfig != nil && sh.SaveConfig != nil,
			CanDiscordConfig: sh.GetDiscordConfig != nil && sh.SaveDiscordConfig != nil,
			CanAlerts:        sh.GetAlerts != nil && sh.SaveAlerts != nil,
			CanEditPool:      sh.SetHeroPool != nil,
			CanEditRules:     sh.SetItemRules != nil,
			LastDecision:     -1, // no decision logged yet; 0 is a real step number
		},
		Board:  boardDTO{RadiantPicks: []int{}, RadiantBans: []int{}, DirePicks: []int{}, DireBans: []int{}},
		Heroes: []session.Hero{},
		Names:  map[string]string{},
		Ops:    s.jobsView(),
		Status: status,
		Auto:   autoStatus(sh.Auto),
		Ring:   ring,
		Share:  s.shareView(),
		Team:   s.teamView(),
	}
	if sess == nil {
		return st
	}

	cfg := sess.Config()
	view := sess.View()

	names := make(map[string]string, len(cfg.Heroes))
	for _, h := range cfg.Heroes {
		names[strconv.Itoa(h.ID)] = h.Label()
	}
	if step, ok := sess.CurrentStep(); ok {
		st.Step = &stepDTO{
			Team:   step.Team.String(),
			Action: step.Action.String(),
			IsOurs: step.Team == cfg.OurTeam,
		}
	}

	st.Meta.BundleVersion = cfg.BundleVersion
	st.Meta.PatchTag = cfg.PatchTag
	st.Meta.OurTeam = cfg.OurTeam.String()
	st.Meta.FirstPick = cfg.FirstPick.String()
	st.Meta.GSIMode = sess.GSIMode()
	st.Meta.Following = sess.Following()
	st.Meta.GSISeen = sess.GSISeen()
	st.Meta.HasMatrices = sess.HasMatrices()
	st.Meta.CanAddEnemy = sess.CanAddEnemy()
	st.Meta.CanUndo = sess.CanUndo()
	st.Meta.CanRedo = sess.CanRedo()
	st.Meta.Logging = sess.Logging()
	st.Meta.LogError = sess.LogFailure()
	if seq, ok := sess.LastLoggedDecision(); ok {
		st.Meta.LastDecision = seq
	} else {
		st.Meta.LastDecision = -1
	}

	st.Board = boardDTO{
		RadiantPicks: nonNil(view.Picks(draft.Radiant)),
		RadiantBans:  nonNil(view.Bans(draft.Radiant)),
		DirePicks:    nonNil(view.Picks(draft.Dire)),
		DireBans:     nonNil(view.Bans(draft.Dire)),
	}
	st.Result = sess.Result()
	st.Heroes = sess.AvailableHeroes()
	st.Names = names
	st.Live = sess.Live()
	st.Alerts = sess.Alerts()
	lu := sess.Lineup()
	st.Lineup = &lu
	if rec := sess.Recommender(); rec != nil {
		st.Sequence = sequenceOf(view, rec.CMOrder(), cfg.OurTeam)
	}
	if msg := s.outcome.Status(); msg != "" {
		st.Status = msg
	}
	return st
}

// nonNil keeps empty slices as [] in JSON instead of null, so the front end can
// treat every list the same way.
func nonNil(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}

func (s *Server) setStatus(msg string) {
	s.mu.Lock()
	s.status = msg
	s.mu.Unlock()
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	// Retry a failed build on every poll so the draft screen recovers by itself
	// once a bundle appears.
	_, _ = s.session()
	writeJSON(w, http.StatusOK, s.snapshot(false))
}

// --- mutations --------------------------------------------------------------

// mutate wraps a state transition: run it, record its outcome as the status line,
// broadcast the new state to every listener, and answer the caller with it. A
// failed transition is not an HTTP error — it is a message for the status bar, the
// same way the TUI reports it.
func (s *Server) mutate(fn func(*http.Request) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.commandMu.Lock()
		defer s.commandMu.Unlock()
		seenKey, fingerprint, duplicate, valid := s.checkIdempotency(w, r)
		if !valid { return }
		if duplicate {
			writeJSON(w, http.StatusOK, s.snapshot(false))
			return
		}
		msg, err := fn(r)
		if seenKey != "" { s.rememberIdempotency(seenKey, fingerprint) }
		if err != nil {
			s.setStatus(err.Error())
		} else {
			s.setStatus(msg)
		}
		if err == nil && s.shell.Auto != nil { s.shell.Auto.TouchDraft() }
		if err == nil {
			if saveErr := s.saveCheckpoint(); saveErr != nil {
				err = fmt.Errorf("draft changed but could not save checkpoint: %w", saveErr)
				s.setStatus(err.Error())
			}
		}
		s.broadcast(false)
		writeJSON(w, http.StatusOK, s.snapshot(false))
	}
}

type heroReq struct {
	HeroID int `json:"heroId"`
}

func (s *Server) doApply(r *http.Request) (string, error) {
	var req heroReq
	if err := decode(r, &req); err != nil {
		return "", err
	}
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	return "", sess.Apply(req.HeroID)
}

func (s *Server) doUndo(*http.Request) (string, error) {
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	return "", sess.Undo()
}

func (s *Server) doRedo(*http.Request) (string, error) {
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	return "", sess.Redo()
}

func (s *Server) doStepAction(*http.Request) (string, error) {
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	na, err := sess.ToggleStepAction()
	if err != nil {
		return "", err
	}
	return "current step → " + na.String(), nil
}

func (s *Server) doStepTeam(*http.Request) (string, error) {
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	opp, err := sess.ToggleStepTeam()
	if err != nil {
		return "", err
	}
	return "current step team → " + opp.String(), nil
}

func (s *Server) doSide(r *http.Request) (string, error) {
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	if team, set, err := parseSideTeam(r); err != nil {
		return "", err
	} else if set {
		return "our side → " + sess.SetSide(team).String(), nil
	}
	return "our side → " + sess.ToggleSide().String(), nil
}

func (s *Server) doFirstPick(*http.Request) (string, error) {
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	nf, err := sess.ToggleFirstPick()
	if err != nil {
		return "", err
	}
	return "first-pick → " + nf.String() + " (draft reset)", nil
}

// handleDeclogReview returns the post-draft survey payload: whether the draft is
// finished, and which of our steps diverged from the top recommendation.
func (s *Server) handleDeclogReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.session()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ready": false, "logging": false, "choices": []any{}})
		return
	}
	_, hasStep := sess.CurrentStep()
	v := sess.View()
	boardEmpty := len(v.Picks(draft.Radiant))+len(v.Picks(draft.Dire))+len(v.Bans(draft.Radiant))+len(v.Bans(draft.Dire)) == 0
	ready := !hasStep && !boardEmpty
	label := func(id int) string {
		for _, h := range sess.Config().Heroes {
			if h.ID == id {
				return h.Label()
			}
		}
		return strconv.Itoa(id)
	}
	choices := []map[string]any{}
	if ready && sess.Logging() {
		for _, c := range sess.OffRecChoices() {
			choices = append(choices, map[string]any{
				"seq":        c.Seq,
				"action":     c.Action,
				"chosen":     c.Chosen,
				"chosenName": label(c.Chosen),
				"chosenRank": c.ChosenRank,
				"topHero":    c.TopHero,
				"topName":    label(c.TopHero),
				"topWhy":     c.TopWhy,
			})
		}
	}
	key := fmt.Sprintf("%v|%v|%v|%v", v.Picks(draft.Radiant), v.Picks(draft.Dire), v.Bans(draft.Radiant), v.Bans(draft.Dire))
	writeJSON(w, http.StatusOK, map[string]any{
		"ready":    ready,
		"logging":  sess.Logging(),
		"choices":  choices,
		"draftKey": key,
	})
}

// doReason attaches the captain's explanation to the last logged decision (§6.1).
// Seq 0 (absent in the request) means "the most recent one", which is what the
// single reason box in the UI means by it.
func (s *Server) doReason(r *http.Request) (string, error) {
	var req struct {
		Seq  *int   `json:"seq"`
		Text string `json:"text"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return "", fmt.Errorf("empty reason")
	}
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	if !sess.Logging() {
		return "", fmt.Errorf("decision logging is off")
	}
	seq := -1
	if req.Seq != nil {
		seq = *req.Seq
	}
	if err := sess.LogReason(seq, text); err != nil {
		return "", err
	}
	return "reason recorded", nil
}

// doOutcome closes the logged draft out with how the game went.
func (s *Server) doOutcome(r *http.Request) (string, error) {
	var req struct {
		Win     bool   `json:"win"`
		MatchID string `json:"matchId"`
		Note    string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	if !sess.Logging() {
		return "", fmt.Errorf("decision logging is off")
	}
	s.outcome.Stop() // manual answer wins over the auto agent
	if err := sess.LogOutcome(req.Win, strings.TrimSpace(req.MatchID), strings.TrimSpace(req.Note)); err != nil {
		return "", err
	}
	if req.Win {
		return "outcome recorded: win", nil
	}
	return "outcome recorded: loss", nil
}

// doOutcomeResolve starts the background agent that finds the match on OpenDota
// and writes win/loss into the decision log. Used when the post-draft survey is
// skipped without an explicit outcome.
func (s *Server) doOutcomeResolve(*http.Request) (string, error) {
	// OpenDota agent retired: map.win_team from GSI closes the log when the match ends.
	return "итог возьмём из GSI (win_team), когда матч закончится", nil
}

// doLineup sets a captain override on one lineup seat (our or enemy).
func (s *Server) doLineup(r *http.Request) (string, error) {
	var req struct {
		Side      string `json:"side"`
		Position  int    `json:"position"`
		AccountID int64  `json:"accountId"`
		HeroID    int    `json:"heroId"`
		Clear     bool   `json:"clear"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	if err := sess.SetLineupSlot(req.Side, req.Position, req.AccountID, req.HeroID, req.Clear); err != nil {
		return "", err
	}
	return "lineup updated", nil
}

func (s *Server) doAddEnemy(r *http.Request) (string, error) {
	var req struct {
		URL string `json:"url"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		return "", fmt.Errorf("empty enemy id or profile URL")
	}
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	summary, err := sess.AddEnemy(url)
	if err != nil {
		return "", fmt.Errorf("add enemy: %w", err)
	}
	return summary, nil
}

func (s *Server) doReload(*http.Request) (string, error) {
	if err := s.reloadSession(); err != nil {
		return "", err
	}
	return "bundle and snapshot reloaded", nil
}

// --- matchups ---------------------------------------------------------------

func (s *Server) handleMatchups(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("hero"))
	if err != nil {
		http.Error(w, "hero query parameter must be a hero id", http.StatusBadRequest)
		return
	}
	sess, err := s.session()
	if err != nil {
		writeJSON(w, http.StatusOK, matchupsDTO{})
		return
	}
	syn, counters, counteredBy, ok := sess.Matchups(id, matchupsTopN)
	writeJSON(w, http.StatusOK, matchupsDTO{
		Found: ok, Synergy: syn, Counters: counters, CounteredBy: counteredBy,
	})
}

type matchupsDTO struct {
	Found       bool                    `json:"found"`
	Synergy     []recommender.HeroScore `json:"synergy"`
	Counters    []recommender.HeroScore `json:"counters"`
	CounteredBy []recommender.HeroScore `json:"counteredBy"`
}

// matchupsTopN matches the TUI's matrices overlay so both front ends show the same
// number of matchups.
const matchupsTopN = 12

// --- helpers ----------------------------------------------------------------

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("bad request body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
