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

	"cmnet/internal/draft"
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
	// AddEnemy, when non-nil, scouts one enemy profile mid-draft (#3) and returns a
	// rebuilt recommender (with the updated snapshot blended in) plus a summary.
	AddEnemy func(url string) (*recommender.Recommender, string, error)
}

// DefaultAlertLead is how many seconds before a spawn an objective is flagged Due.
const DefaultAlertLead = 15

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

	gsiMode   bool
	gsiSeen   bool
	live      *draft.Live
	alerts    []objectives.Alert
	alertLead int
	belledAt  map[objectives.Kind]int
	// lastDraftSig is the draft content of the most recent GSI snapshot that was
	// actually recomputed, so repeat events carrying only a new clock are skipped.
	lastDraftSig string
}

// New builds a session for cfg and computes the first result.
func New(cfg Config) *Session {
	s := &Session{
		cfg:       cfg,
		gsiMode:   cfg.GSIEvents != nil,
		alertLead: DefaultAlertLead,
		belledAt:  map[objectives.Kind]int{},
	}
	if s.gsiMode {
		// Start from an empty, not-yet-known draft; events fill it in.
		s.view = &draft.Snapshot{CurrentTeam: cfg.FirstPick, CurrentIsPick: false}
	} else {
		s.drafts = draft.New(cfg.FirstPick)
		s.view = s.drafts
	}
	s.recompute()
	return s
}

// recompute refreshes the cached result. Callers must hold mu.
func (s *Session) recompute() {
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
	if err := s.drafts.Apply(heroID); err != nil {
		return err
	}
	s.recompute()
	return nil
}

// Undo reverts the most recent Apply and recomputes.
func (s *Session) Undo() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode {
		return errManualOnly
	}
	if err := s.drafts.Undo(); err != nil {
		return err
	}
	s.recompute()
	return nil
}

// Redo re-applies the most recently undone step and recomputes.
func (s *Session) Redo() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode {
		return errManualOnly
	}
	if err := s.drafts.Redo(); err != nil {
		return err
	}
	s.recompute()
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
func (s *Session) ToggleSide() draft.Team {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.OurTeam = s.cfg.OurTeam.Opponent()
	s.recompute()
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
	rec, summary, err := fn(url)
	if err != nil {
		return summary, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if rec != nil {
		s.cfg.Rec = rec
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
	s.view = snap
	s.live = snap.Live
	s.gsiSeen = true
	// Dota posts up to ten events a second (throttle 0.1 in the GSI config), and
	// once the draft is over the picks stop changing while the clock keeps ticking.
	// Recommending is a tree search, so recomputing on every event burns the CPU —
	// and holds this lock — for a result that cannot have changed. Recompute only
	// when the draft itself moved; alerts follow the clock and are cheap.
	if sig := draftSignature(snap); sig != s.lastDraftSig {
		s.lastDraftSig = sig
		s.recompute()
	}
	return s.refreshAlerts()
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
func (s *Session) refreshAlerts() bool {
	s.alerts = nil
	if s.live == nil || len(s.cfg.Objectives) == 0 {
		return false
	}
	s.alerts = objectives.Upcoming(s.cfg.Objectives, s.live.ClockTime, s.alertLead)

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
