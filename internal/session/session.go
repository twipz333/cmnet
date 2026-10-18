// Package session holds the front-end-agnostic advisor state: the configuration a
// front end needs, the manual draft state machine, and the latest core Result.
//
// It exists so that every front end (the bubbletea TUI, the local web UI) drives
// the exact same state transitions. Nothing here computes scores — that stays in
// the core (§8) — but everything that decides WHAT the core is asked lives here,
// so two front ends can never diverge in what they show (§6).
package session

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"cmnet/internal/alerts"
	"cmnet/internal/declog"
	"cmnet/internal/draft"
	"cmnet/internal/lineup"
	"cmnet/internal/objectives"
	"cmnet/internal/recommender"
)

// Hero is the display record for a hero (id + names). Names are presentation only
// and never feed the model, so they cannot affect determinism (§6/§8).
type Hero struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`      // internal name
	Localized string `json:"localized"` // display name
}

// Label returns the best available display name.
func (h Hero) Label() string {
	if h.Localized != "" {
		return h.Localized
	}
	if h.Name != "" {
		return h.Name
	}
	return fmt.Sprintf("Hero %d", h.ID)
}

// Config is everything serve must supply to launch a front end.
type Config struct {
	Heroes        []Hero
	OurTeam       draft.Team
	FirstPick     draft.Team
	BundleVersion string
	PatchTag      string
	Rec           *recommender.Recommender
	// GSIEvents, when non-nil, switches the session into read-only GSI mode: the
	// board is driven by snapshots from this channel instead of manual input.
	GSIEvents <-chan *draft.Snapshot
	// Objectives is the patch's spawn-timer table (from the bundle, M6). When set and
	// a live game clock arrives over GSI, front ends show an objective timer panel
	// and alert ahead of each spawn. Empty disables timers.
	Objectives []objectives.Spawn
	// AlertLeads is how many seconds before each spawn kind the UI flags Due /
	// rings. Personal preference from config/flags — not part of the shared bundle.
	AlertLeads objectives.Leads
	// AlertWindows limits which game-clock ranges arm each kind (e.g. wisdom only
	// until 35:00). Empty / unset kinds are unlimited.
	AlertWindows objectives.Windows
	// AlertFile, when set, drives leads/windows/custom alerts and TTS templates
	// (hot-reloaded from alerts.yaml). When nil, AlertLeads/AlertWindows are used alone.
	AlertFile *alerts.File
	// AddEnemy, when non-nil, scouts one enemy profile mid-draft (#3) and returns a
	// rebuilt recommender (with the updated snapshot blended in), the log context
	// that goes with it, and a summary. The header comes back because the advice
	// from here on rests on a different snapshot, and the log must say so rather
	// than keep attributing it to the one the draft opened with (§6.1).
	AddEnemy func(url string) (*recommender.Recommender, declog.Header, string, error)

	// Log, when non-nil, records what was recommended and what the captain actually
	// did (§6.1). Local file, never uploaded, never part of the snapshot.
	Log *declog.Logger
	// LogHeader is the per-draft context the session cannot know by itself: who is
	// on the two sides and with what profiles, plus the snapshot in force. The
	// session fills in the rest (sides, bundle version, input source).
	LogHeader declog.Header
}

// DefaultAlertLead is how many seconds before a spawn an objective is flagged Due
// when Config.AlertLeads leaves Default unset.
const DefaultAlertLead = objectives.DefaultLead

// Session is the shared mutable state behind a front end. All methods are safe for
// concurrent use: the web front end serves requests from many goroutines while GSI
// events arrive on another, and serialising them also keeps inference
// single-threaded as §6 requires.
type Session struct {
	mu  sync.Mutex
	cfg Config

	drafts *draft.DraftState // manual mode only; nil under GSI
	view   draft.View        // what the recommender reads
	result recommender.Result

	gsiMode      bool
	gsiSeen      bool
	live         *draft.Live
	alerts       []objectives.Alert
	alertLeads   objectives.Leads
	alertWindows objectives.Windows
	alertFile    *alerts.File
	belledAt     map[objectives.Kind]int
	// lastDraftSig is the draft content of the most recent GSI snapshot that was
	// actually recomputed, so repeat events carrying only a new clock are skipped.
	lastDraftSig string

	// Decision logging (§6.1). logOpen tracks whether a draft has been opened in
	// the log; lastLoggedSeq is the step of the most recent logged decision (-1 for
	// none), so a front end can attach a refusal reason to it.
	logOpen       bool
	logFinal      bool // the finished board has been written for this draft
	lastLoggedSeq int
	logFail       error
	// loggedChoices are our decisions this draft (in-memory), for the post-draft
	// survey. Cleared when a new logged draft starts.
	loggedChoices []LoggedChoice

	// Draft lineup seats (session-only): who is on which position / hero.
	ourLineup   lineup.Side
	enemyLineup lineup.Side
	lineupInit  bool // seats have been initialised at least once

	// sideManual is set when the captain toggles our side by hand; GSI must not
	// overwrite it until the next match. lastGSIMatch remembers matchid so a new
	// game clears the pin and auto-detects again.
	sideManual   bool
	lastGSIMatch string

	// gsiOutcomeDone avoids writing win/loss twice for the same match.
	gsiOutcomeDone bool
	// pendingStatus is a one-shot status line for the web UI (e.g. auto win/loss).
	pendingStatus string
}

// LoggedChoice is one of our logged steps, as the post-draft survey needs it.
type LoggedChoice struct {
	Seq        int
	Action     string
	Chosen     int
	ChosenRank int
	TopHero    int
	TopWhy     string
}

// New builds a session for cfg and computes the first result.
func New(cfg Config) *Session {
	s := &Session{
		cfg:           cfg,
		gsiMode:       cfg.GSIEvents != nil,
		alertLeads:    normalizeLeads(cfg.AlertLeads),
		alertWindows:  cfg.AlertWindows,
		alertFile:     cfg.AlertFile,
		belledAt:      map[objectives.Kind]int{},
		lastLoggedSeq: -1,
		ourLineup:     lineup.Empty(),
		enemyLineup:   lineup.Empty(),
	}
	if s.gsiMode {
		// Start from an empty, not-yet-known draft; events fill it in.
		s.view = &draft.Snapshot{CurrentTeam: cfg.FirstPick, CurrentIsPick: false}
	} else {
		s.drafts = draft.New(cfg.FirstPick)
		s.view = s.drafts
	}
	s.recompute()
	if cfg.AlertFile != nil {
		s.alertFile = cfg.AlertFile
		s.alertLeads = normalizeLeads(cfg.AlertFile.ToLeads())
		s.alertWindows = cfg.AlertFile.ToWindows()
	}
	return s
}

// recompute refreshes the cached result. Callers must hold mu.
func (s *Session) recompute() {
	s.syncLineupLocked()
	s.applyLineupFixLocked()
	s.result = s.cfg.Rec.Recommend(s.view, s.cfg.OurTeam, recommender.DefaultTopN)
}

// Config returns the session configuration. The recommender pointer inside may be
// replaced by AddEnemy, so read it through this accessor rather than caching it.
func (s *Session) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Recommender returns the current recommender (AddEnemy swaps it in place).
func (s *Session) Recommender() *recommender.Recommender {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Rec
}

// Result returns the latest core result.
func (s *Session) Result() recommender.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result
}

// GSIMode reports whether the board is driven by GSI (read-only) rather than
// manual input.
func (s *Session) GSIMode() bool { return s.gsiMode }

// GSISeen reports whether at least one GSI event has arrived.
func (s *Session) GSISeen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gsiSeen
}

// Live returns the latest in-game state from GSI, or nil.
func (s *Session) Live() *draft.Live {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live
}

// Alerts returns the current objective alert list (empty without a live clock).
func (s *Session) Alerts() []objectives.Alert {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alerts
}

// View returns the draft view the recommender is reading.
func (s *Session) View() draft.View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

// OurTeam is the side the advice is for.
func (s *Session) OurTeam() draft.Team {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.OurTeam
}

// FirstPick is the side that drafts first.
func (s *Session) FirstPick() draft.Team {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.FirstPick
}

// CurrentStep returns the step waiting to be filled; ok is false when the draft is
// complete (or unknown, before the first GSI event).
func (s *Session) CurrentStep() (draft.Step, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view.Current()
}

// AvailableHeroes returns the heroes not yet picked or banned, in bundle order.
func (s *Session) AvailableHeroes() []Hero {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Hero, 0, len(s.cfg.Heroes))
	for _, h := range s.cfg.Heroes {
		if s.view.IsTaken(h.ID) {
			continue
		}
		out = append(out, h)
	}
	return out
}

// errManualOnly is returned by mutating operations under GSI mode, where the board
// is authoritative and must not be edited by hand.
var errManualOnly = fmt.Errorf("draft is driven by GSI; manual edits are disabled")

// Apply fills the current step with heroID and recomputes.
func (s *Session) Apply(heroID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode {
		return errManualOnly
	}
	// Captured before the move: after it the recommender has recomputed for the
	// next step, and logging that would record advice nobody was shown (§6.1).
	seq, step, board, shown := s.captureForLog()
	if err := s.drafts.Apply(heroID); err != nil {
		return err
	}
	s.recompute()
	s.logApplied(seq, step, board, shown, heroID)
	s.logFinalBoard(s.view)
	return nil
}

// captureForLog snapshots what is on screen for the step about to be filled.
// Callers must hold mu.
func (s *Session) captureForLog() (seq int, step draft.Step, board declog.Board, shown recommender.Result) {
	if s.cfg.Log == nil || s.drafts == nil {
		return -1, draft.Step{}, declog.Board{}, recommender.Result{}
	}
	st, ok := s.drafts.Current()
	if !ok {
		return -1, draft.Step{}, declog.Board{}, recommender.Result{}
	}
	return s.drafts.Cursor, st, boardOf(s.drafts), s.result
}

// logApplied writes the decision captured by captureForLog, if it was ours.
// Callers must hold mu.
func (s *Session) logApplied(seq int, step draft.Step, board declog.Board, shown recommender.Result, heroID int) {
	if seq < 0 || step.Team != s.cfg.OurTeam {
		return
	}
	s.logDecision(seq, step, board, shown, heroID)
	s.lastLoggedSeq = seq
}

// Undo reverts the most recent Apply and recomputes.
func (s *Session) Undo() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode {
		return errManualOnly
	}
	// The step about to be cleared, so the log can take back its record of it: a
	// hero typed by mistake must not survive in the data as one that was drafted.
	undone := -1
	if s.cfg.Log != nil && s.drafts != nil && s.drafts.Cursor > 0 {
		if i := s.drafts.Cursor - 1; s.drafts.Seq[i].Team == s.cfg.OurTeam {
			undone = i
		}
	}
	if err := s.drafts.Undo(); err != nil {
		return err
	}
	s.recompute()
	// The draft is no longer finished, so the board recorded as final is not; the
	// corrected one is written when the last step is filled again. Withdrawn in the
	// file too, not just in memory: an undone draft that is then abandoned must not
	// read offline as one that finished on a composition nobody played.
	if s.logFinal {
		s.logFinal = false
		s.logErr(s.cfg.Log.ClearFinalBoard())
	}
	if undone >= 0 {
		s.logErr(s.cfg.Log.Undo(undone))
		s.dropLoggedChoice(undone)
		if s.lastLoggedSeq == undone {
			// Point at the previous step of ours, not at nothing: a reason typed after
			// an undo should annotate the decision that is now the latest, and dropping
			// the pointer would silently discard the field the log exists for.
			s.lastLoggedSeq = s.previousOurStep(undone)
		}
	}
	return nil
}

// Redo re-applies the most recently undone step and recomputes.
func (s *Session) Redo() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode {
		return errManualOnly
	}
	seq, step, board, shown := s.captureForLog()
	if err := s.drafts.Redo(); err != nil {
		return err
	}
	hero := draft.NoHero
	if seq >= 0 && seq < len(s.drafts.Selections) {
		hero = s.drafts.Selections[seq]
	}
	s.recompute()
	if hero != draft.NoHero {
		s.logApplied(seq, step, board, shown, hero)
	}
	s.logFinalBoard(s.view)
	return nil
}

// CanUndo reports whether at least one step has been applied.
func (s *Session) CanUndo() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drafts != nil && s.drafts.Cursor > 0
}

// CanRedo reports whether an undone step is available to redo.
func (s *Session) CanRedo() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drafts != nil && s.drafts.CanRedo()
}

// ToggleStepAction flips the current step between pick and ban. The Valve CM order
// shifts between patches (§12), so the user marks each action as the game actually
// presents it. Returns the new action.
func (s *Session) ToggleStepAction() (draft.Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode {
		return 0, errManualOnly
	}
	step, ok := s.drafts.Current()
	if !ok {
		return 0, fmt.Errorf("draft already complete")
	}
	na := draft.Pick
	if step.Action == draft.Pick {
		na = draft.Ban
	}
	s.drafts.SetCurrentStep(step.Team, na)
	s.recompute()
	return na, nil
}

// ToggleStepTeam flips which team owns the current step. Returns the new team.
func (s *Session) ToggleStepTeam() (draft.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode {
		return 0, errManualOnly
	}
	step, ok := s.drafts.Current()
	if !ok {
		return 0, fmt.Errorf("draft already complete")
	}
	opp := step.Team.Opponent()
	s.drafts.SetCurrentStep(opp, step.Action)
	s.recompute()
	return opp, nil
}

// ToggleSide flips which side the advice is for. Returns the new side.
// Under GSI this also pins the side so the next events do not flip it back.
func (s *Session) ToggleSide() draft.Team {
	s.mu.Lock()
	opp := s.cfg.OurTeam.Opponent()
	s.mu.Unlock()
	return s.SetSide(opp)
}

// SetSide sets our side absolutely and pins it against GSI auto-detect.
func (s *Session) SetSide(team draft.Team) draft.Team {
	s.mu.Lock()
	defer s.mu.Unlock()
	if team != draft.Radiant && team != draft.Dire {
		team = draft.Radiant
	}
	changed := s.cfg.OurTeam != team
	s.cfg.OurTeam = team
	s.sideManual = true
	if changed {
		// The header says which side the advice was for, and decisions are only ours.
		// Continuing the same logged draft under the other side would file two teams'
		// decisions under one stale header.
		s.resetLogDraft()
		s.lastLoggedSeq = -1
		s.recompute()
	}
	return s.cfg.OurTeam
}

// ToggleFirstPick flips the first-pick side. The CM step sequence depends on it, so
// this resets the manual draft. Returns the new first-pick side.
func (s *Session) ToggleFirstPick() (draft.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode {
		return 0, errManualOnly
	}
	nf := s.cfg.FirstPick.Opponent()
	s.cfg.FirstPick = nf
	s.drafts = draft.New(nf)
	s.view = s.drafts
	// The board is gone, so what follows is a different draft in the log too.
	s.resetLogDraft()
	s.lastLoggedSeq = -1
	s.recompute()
	return nf, nil
}

// HasMatrices reports whether the bundle carries synergy/counter matrices.
func (s *Session) HasMatrices() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Rec.HasMatrices()
}

// Matchups returns the synergy/counter lists for a hero, straight from the core.
func (s *Session) Matchups(heroID, topN int) (synergy, counters, counteredBy []recommender.HeroScore, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Rec.Matchups(heroID, topN)
}

// CanAddEnemy reports whether mid-draft enemy scouting is wired up.
func (s *Session) CanAddEnemy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.AddEnemy != nil
}

// AddEnemy scouts one enemy profile (#3), swaps in the rebuilt recommender and
// recomputes. It blocks on network I/O, so front ends should call it off their
// render loop.
func (s *Session) AddEnemy(url string) (string, error) {
	s.mu.Lock()
	fn := s.cfg.AddEnemy
	s.mu.Unlock()
	if fn == nil {
		return "", fmt.Errorf("enemy scouting is not available")
	}
	rec, header, summary, err := fn(url)
	if err != nil {
		return summary, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if rec != nil {
		s.cfg.Rec = rec
		s.cfg.LogHeader = header
		// Only when a draft is already open: before the first decision the opening
		// header is written fresh anyway, and an empty context record would just be
		// noise in the file.
		if s.logOpen {
			s.logErr(s.cfg.Log.UpdateContext(s.logHeaderFor()))
		}
	}
	s.recompute()
	return summary, nil
}

// ApplyGSI adopts a snapshot from GSI, refreshes the objective alerts from the live
// clock, and recomputes. ring reports whether an objective newly entered its
// pre-alert window, so a front end can signal it (terminal bell, browser sound)
// without duplicating the bookkeeping.
func (s *Session) ApplyGSI(snap *draft.Snapshot) (ring bool) {
	if snap == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Before the view moves: under GSI a decision is only visible as a hero that
	// appeared on the board since the previous event (§6.1).
	s.logGSI(snap)
	s.view = snap
	s.live = snap.Live
	s.gsiSeen = true
	// New match → drop a manual side pin so the next lobby auto-detects again.
	if snap.Live != nil {
		mid := snap.Live.MatchID
		if mid != "" && mid != "0" && mid != s.lastGSIMatch {
			s.lastGSIMatch = mid
			s.sideManual = false
			s.gsiOutcomeDone = false
		}
	}
	sideChanged := s.syncOurTeamFromGSILocked(snap)
	s.tryGSIOutcomeLocked(snap)
	// Dota posts up to ten events a second (throttle 0.1 in the GSI config), and
	// once the draft is over the picks stop changing while the clock keeps ticking.
	// Recommending is a tree search, so recomputing on every event burns the CPU —
	// and holds this lock — for a result that cannot have changed. Recompute only
	// when the draft itself moved; alerts follow the clock and are cheap.
	if sig := draftSignature(snap); sig != s.lastDraftSig || sideChanged {
		s.lastDraftSig = sig
		s.recompute()
		// Once GSI reports the draft finished, record the composition it ended on.
		s.logFinalBoard(snap)
	}
	return s.refreshAlerts()
}

// syncOurTeamFromGSILocked applies player.team_name (or hero-on-board fallback)
// when the captain has not pinned the side manually. Callers hold mu.
func (s *Session) syncOurTeamFromGSILocked(snap *draft.Snapshot) bool {
	if s.sideManual || snap == nil || !snap.LocalTeamOK {
		return false
	}
	if snap.LocalTeam == s.cfg.OurTeam {
		return false
	}
	s.cfg.OurTeam = snap.LocalTeam
	return true
}

// draftSignature captures everything the recommender reads out of a GSI snapshot:
// the heroes on both sides and whose turn it is. Two snapshots with equal
// signatures must produce an identical Result, so one can be skipped.
func draftSignature(s *draft.Snapshot) string {
	var b strings.Builder
	for _, group := range [][]int{s.RadiantPicks, s.DirePicks, s.RadiantBans, s.DireBans} {
		for _, id := range group {
			b.WriteString(strconv.Itoa(id))
			b.WriteByte(',')
		}
		b.WriteByte('|')
	}
	b.WriteString(strconv.Itoa(int(s.CurrentTeam)))
	b.WriteByte('/')
	b.WriteString(strconv.FormatBool(s.CurrentIsPick))
	b.WriteByte('/')
	b.WriteString(strconv.FormatBool(s.Done))
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(int(s.FirstPick)))
	return b.String()
}

// refreshAlerts recomputes the objective alert list from the latest live clock and
// reports whether any objective newly entered the pre-alert (Due) window. Ordering
// comes from objectives.Upcoming and is fully deterministic. Callers must hold mu.
//
// Lotus/rune timers are suppressed during the draft — they only make sense once
// the match is actually in progress.
func (s *Session) refreshAlerts() bool {
	s.alerts = nil
	if s.live == nil || len(s.cfg.Objectives) == 0 {
		return false
	}
	if !objectivesLive(s.live, s.view) {
		return false
	}
	if s.alertFile != nil {
		s.alerts = alerts.Upcoming(s.alertFile, s.cfg.Objectives, s.live.ClockTime)
	} else {
		s.alerts = objectives.Upcoming(s.cfg.Objectives, s.live.ClockTime, s.alertLeads, s.alertWindows)
	}

	ring := false
	for _, a := range s.alerts {
		// Signal once per distinct spawn occurrence (keyed by its absolute spawn
		// time), so a kind re-fires only when its next occurrence rolls over.
		if a.Due && s.belledAt[a.Kind] != a.NextAt {
			s.belledAt[a.Kind] = a.NextAt
			ring = true
		}
	}
	return ring
}

// SetAlertFile hot-swaps alerts.yaml settings (leads, windows, custom, TTS).
// Safe to call while GSI is live; next clock tick picks them up.
func (s *Session) SetAlertFile(f *alerts.File) {
	if f == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertFile = f
	s.alertLeads = normalizeLeads(f.ToLeads())
	s.alertWindows = f.ToWindows()
	if s.live != nil {
		s.refreshAlerts()
	}
}

func normalizeLeads(l objectives.Leads) objectives.Leads {
	if l.Default <= 0 {
		l.Default = DefaultAlertLead
	}
	return l
}

// objectivesLive is true only after the draft, when rune/lotus clocks matter.
func objectivesLive(live *draft.Live, view draft.View) bool {
	if live == nil {
		return false
	}
	gs := live.GameState
	if strings.Contains(gs, "HERO_SELECTION") ||
		strings.Contains(gs, "STRATEGY") ||
		strings.Contains(gs, "TEAM_SHOWCASE") ||
		strings.Contains(gs, "WAIT_FOR") ||
		strings.Contains(gs, "INIT") {
		return false
	}
	// Active CM draft block with a current step → still drafting.
	if snap, ok := view.(*draft.Snapshot); ok && snap != nil && snap.HasDraft && !snap.Done {
		if _, stepOK := snap.Current(); stepOK {
			return false
		}
	}
	return strings.Contains(gs, "GAME_IN_PROGRESS") || strings.Contains(gs, "PRE_GAME")
}

// tryGSIOutcomeLocked writes win/loss from map.win_team once per match.
func (s *Session) tryGSIOutcomeLocked(snap *draft.Snapshot) {
	if s.gsiOutcomeDone || s.cfg.Log == nil || snap == nil || snap.Live == nil {
		return
	}
	wt := strings.ToLower(strings.TrimSpace(snap.Live.WinTeam))
	if wt != "radiant" && wt != "dire" {
		return
	}
	win := (wt == "radiant" && s.cfg.OurTeam == draft.Radiant) ||
		(wt == "dire" && s.cfg.OurTeam == draft.Dire)
	matchID := snap.Live.MatchID
	draftID := s.cfg.Log.CurrentDraft()
	if draftID == "" {
		return
	}
	err := s.cfg.Log.OutcomeFor(draftID, declog.Outcome{
		Win: win, MatchID: matchID, Note: "gsi:win_team",
	})
	s.logErr(err)
	if err != nil {
		return
	}
	s.gsiOutcomeDone = true
	if win {
		s.pendingStatus = "итог из GSI: победа"
	} else {
		s.pendingStatus = "итог из GSI: поражение"
	}
}

// TakeStatus returns and clears a pending status line for the front end.
func (s *Session) TakeStatus() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := s.pendingStatus
	s.pendingStatus = ""
	return msg
}
