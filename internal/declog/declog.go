// Package declog records what the advisor recommended and what the captain
// actually did, one line per event, in a local file.
//
// Why it exists (§6.1 of docs/TARGET-ARCHITECTURE.md): every offline metric this
// project has is retrospective — it scores the model against drafts that already
// happened, played by people who never saw a recommendation. That can say whether
// the model ranks historical picks well; it can never say whether the advice is
// useful to the person holding the draft clock. The only source of that answer is
// a prospective record: the state, what was shown, what was chosen, and — the most
// valuable field here — WHY the captain refused. Refusal reasons are what surface
// the missing features ("this player cannot play it", "the role read is wrong",
// "taking it now shows our hand"), and no amount of corpus mining produces them.
//
// It is deliberately outside the deterministic path (§6). Nothing in this package
// feeds scoring; a failed write must never disturb a live draft, so every write is
// best-effort and errors are returned for logging, not for aborting.
//
// Privacy: the log holds Steam account ids, per-player hero profiles and free
// text. It is written next to the local cache, it is never uploaded, it is never
// part of the snapshot handed to teammates, its retention is bounded, and a single
// player can be erased from it (Forget) — the same guarantee cache.ForgetPlayer
// gives for the aggregates.
package declog

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Kind tags what a line carries. The log is append-only, so facts that arrive
// after a decision — the captain's reason, the match outcome — are separate lines
// referring back to it rather than edits of an earlier one.
type Kind string

const (
	KindDraft    Kind = "draft"    // header: the context a draft opens with
	KindContext  Kind = "context"  // the context changed mid-draft (a scout landed)
	KindDecision Kind = "decision" // one of our steps: what was shown, what was taken
	KindUndo     Kind = "undo"     // a logged decision was taken back
	KindBoard    Kind = "board"    // the finished draft, both sides
	KindReason   Kind = "reason"   // why the captain went off-recommendation
	KindOutcome  Kind = "outcome"  // how the game ended
)

// FileName is the log's name; it lives beside the cache database.
const FileName = "decisions.jsonl"

// Record is one line of the file. Exactly one payload pointer is set.
type Record struct {
	Kind Kind  `json:"kind"`
	TS   int64 `json:"ts"`
	// DraftID groups every line of one draft. Generated locally and meaningless
	// outside this file.
	DraftID string `json:"draft"`

	Header   *Header   `json:"header,omitempty"`
	Decision *Decision `json:"decision,omitempty"`
	Board    *Board    `json:"board,omitempty"`
	Reason   *Reason   `json:"reason,omitempty"`
	Outcome  *Outcome  `json:"outcome,omitempty"`
}

// Header is the per-draft context: which model produced the advice, and who was
// on the two sides with the profiles as they stood at that moment.
//
// Profiles are stored per draft rather than referenced by id on purpose: a player
// profile is a moving object, and a record that says "we recommended this given
// what we knew" is worthless if "what we knew" is resolved at read time from a
// cache that has since been refreshed. This is the same as-of discipline the
// retrospective validation needs (§6.6).
type Header struct {
	BundleVersion string `json:"bundleVersion,omitempty"`
	PatchTag      string `json:"patchTag,omitempty"`
	// SnapshotHash identifies the frozen opponent snapshot (layers 2-3) in force.
	SnapshotHash string `json:"snapshotHash,omitempty"`
	OurTeam      string `json:"ourTeam"`   // radiant|dire
	FirstPick    string `json:"firstPick"` // radiant|dire
	Source       string `json:"source"`    // manual|gsi
	// Context is this header's revision within the draft: 0 for the opening one,
	// higher for a context that replaced it mid-draft (see Decision.Context).
	Context int `json:"context,omitempty"`

	Allies  []Player `json:"allies,omitempty"`
	Enemies []Player `json:"enemies,omitempty"`

	// EventDrafts is layer 3 (§9): the opposing team's drafts already seen in this
	// event, which weight the enemy prediction by n/(n+k). Copied in for the same
	// reason as the profiles — the snapshot file it came from is overwritten by the
	// next scout, so the hash alone cannot bring it back.
	EventDrafts []EventDraft `json:"eventDrafts,omitempty"`
}

// EventDraft is one observed opposing-team draft from the current event.
type EventDraft struct {
	MatchID int64 `json:"matchId,omitempty"`
	Radiant bool  `json:"radiant,omitempty"`
	Picks   []int `json:"picks,omitempty"`
	Bans    []int `json:"bans,omitempty"`
}

// Player is one participant as known before the draft.
type Player struct {
	AccountID int64  `json:"accountId"`
	Persona   string `json:"persona,omitempty"`
	// Position is the role we assumed they would play, 1-5, or 0 for unknown.
	//
	// Filled from the role prior's mode on the player's top hero when a prior is
	// available (§6.3 path A). Player→role assignment with team constraints is §6.5.
	// The field existed empty so mid-campaign schema splits would not happen.
	Position int `json:"position,omitempty"`
	// Heroes is the player's pool as it was used for this draft, most-played first.
	Heroes []HeroStat `json:"heroes,omitempty"`
}

// HeroStat is one hero in a player's profile.
type HeroStat struct {
	HeroID  int     `json:"heroId"`
	Games   int     `json:"games"`
	WinRate float64 `json:"winRate,omitempty"`
}

// Decision is one step of ours: the board before the action, everything the
// advisor put on screen, and what the captain did with it.
type Decision struct {
	// Seq is the draft step this decision filled, counted over BOTH teams from 0 —
	// its position in the Captains Mode sequence, not a counter of our own moves.
	// That makes it stable under undo/redo and manual corrections: a step re-entered
	// lands on the same seq and supersedes the earlier line, so a mistyped hero does
	// not survive in the data as if it had been drafted.
	Seq int `json:"seq"`
	// Context is which version of the draft context produced this advice: 0 is the
	// opening header, 1 the first mid-draft change, and so on.
	//
	// Needed because scouting an enemy mid-draft (#3) rebuilds the recommender on a
	// new snapshot. Without this every decision would appear to have been made under
	// the opening snapshot, and a record that misattributes its own inputs is worse
	// than one that omits them.
	Context int `json:"context,omitempty"`
	// Action is "pick" or "ban".
	Action string `json:"action"`
	Board  Board  `json:"board"`

	Recs      []Rec  `json:"recs,omitempty"`
	EnemyPred []Pred `json:"enemyPred,omitempty"`
	// Strength is the win probability of the board BEFORE this action, our side.
	Strength *float64 `json:"strength,omitempty"`

	// Chosen is the hero the captain actually took.
	Chosen int `json:"chosen"`
	// ChosenRank is its 1-based position in Recs, or 0 when it was not on the list
	// at all. Kept as a rank rather than a bool because "took the second suggestion"
	// and "took something we never mentioned" are different outcomes, and collapsing
	// them into "not accepted" throws away the distinction the log exists to study.
	ChosenRank int `json:"chosenRank"`
}

// Board is the draft state before the logged action.
type Board struct {
	RadiantPicks []int `json:"radiantPicks,omitempty"`
	DirePicks    []int `json:"direPicks,omitempty"`
	RadiantBans  []int `json:"radiantBans,omitempty"`
	DireBans     []int `json:"direBans,omitempty"`
}

// Rec is one shown recommendation.
type Rec struct {
	HeroID int     `json:"heroId"`
	Score  float64 `json:"score"`
	// Role is the MAP lane (1-5) shown with the recommendation, or 0.
	Role int `json:"role,omitempty"`
	// Player is the §6.5 ally nickname assigned to this hero, if any.
	Player string `json:"player,omitempty"`
	// Why is the one-line explanation the UI displayed.
	Why string `json:"why,omitempty"`
	// Factors is the score decomposition where the core computes one: a ban score
	// is assembled from P(enemy takes it), threat and enemy synergy, and the pick
	// score carries its ally-comfort offset separately. A beam-search pick score is
	// the result of a rollout and has no such breakdown — for those only WinProb is
	// set, and nothing here invents the rest (§6.4).
	Factors *Factors `json:"factors,omitempty"`
}

// Factors mirrors recommender.Factors. Duplicated rather than aliased so the log
// format is owned by the log: a field renamed in the recommender must not silently
// change the meaning of records already on disk.
type Factors struct {
	// Base is the pick score before the comfort offset. BaseIsWinProb says whether
	// it is a calibrated win probability (one-ply scoring) or a beam rollout value,
	// which is neither calibrated nor bounded to [0,1] — the two must not be pooled
	// by whoever analyses this file.
	Base          float64 `json:"base,omitempty"`
	BaseIsWinProb bool    `json:"baseIsWinProb,omitempty"`
	Comfort       float64 `json:"comfort,omitempty"`
	Capability    float64 `json:"capability,omitempty"`
	EnemyProb     float64 `json:"enemyProb,omitempty"`
	Threat        float64 `json:"threat,omitempty"`
	EnemySynergy  float64 `json:"enemySynergy,omitempty"`
	AltDeficit    float64 `json:"altDeficit,omitempty"`
}

// Pred is one predicted enemy action, with the decomposition the core does expose
// for it: how much the hero beats our picks (Threat), how well it fits the enemy's
// (Fit), and the two combined with the ban weights (Danger).
type Pred struct {
	HeroID int     `json:"heroId"`
	Prob   float64 `json:"prob"`
	Source string  `json:"source,omitempty"` // meta|player|event (§9 provenance)
	Threat float64 `json:"threat,omitempty"`
	Fit    float64 `json:"fit,omitempty"`
	Danger float64 `json:"danger,omitempty"`
}

// Reason is the captain's own account of a decision, keyed by its Seq.
type Reason struct {
	Seq  int    `json:"seq"`
	Text string `json:"text"`
}

// Outcome closes a draft out.
type Outcome struct {
	Win     bool   `json:"win"`
	MatchID string `json:"matchId,omitempty"`
	Note    string `json:"note,omitempty"`
}

// Retention bounds how much history the file keeps. Both limits apply; zero
// disables that limit.
type Retention struct {
	MaxAgeDays int // drop drafts whose header is older than this
	MaxDrafts  int // keep at most this many of the newest drafts
}

// DefaultRetention keeps a season's worth of Battle Cups and no more. Bounded by
// default rather than unbounded-with-an-opt-in: this file holds other people's
// identifiers, and "we kept everything because nobody configured it" is not an
// acceptable default for that.
func DefaultRetention() Retention { return Retention{MaxAgeDays: 180, MaxDrafts: 200} }

// Logger appends records to the file. Safe for concurrent use; a nil *Logger is a
// valid no-op logger, so front ends need no branches around it.
type Logger struct {
	mu     sync.Mutex
	path   string
	f      *os.File
	nowFn  func() time.Time
	draft  string // current draft id
	ctx    int    // context revision within the current draft
	drafts int    // drafts opened in this process, for id uniqueness
	nonce  string // per-logger, so two processes cannot mint the same id
	nextID func() string
}

// Open prepares the log at path, pruning it to retention first so the bound is
// enforced on every run rather than only when someone remembers to ask.
func Open(path string, r Retention) (*Logger, error) {
	if path == "" {
		return nil, fmt.Errorf("declog: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if _, err := Prune(path, r, time.Now()); err != nil {
		return nil, err
	}
	// 0600: the file holds account ids and free text about named people, and on a
	// shared machine the default mask is not a decision anyone made deliberately.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{path: path, f: f, nowFn: time.Now, nonce: newNonce()}, nil
}

// newNonce is four hex digits of randomness, enough to keep two processes writing
// the same log from minting the same draft id. Falls back to the process id, which
// serves the same purpose, if the system source is unavailable.
func newNonce() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%04x", os.Getpid()&0xffff)
	}
	return fmt.Sprintf("%02x%02x", b[0], b[1])
}

// PathFor returns the log path that belongs next to a cache database.
func PathFor(dbPath string) string {
	dir := filepath.Dir(dbPath)
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, FileName)
}

// Close releases the file. Safe on nil.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// Path reports where the log is written.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// StartDraft opens a new draft, returning its id. The header is written straight
// away so a draft that is abandoned mid-way still leaves a readable record of who
// was on the board.
func (l *Logger) StartDraft(h Header) (string, error) {
	if l == nil {
		return "", nil
	}
	l.mu.Lock()
	id := l.newDraftID()
	l.draft = id
	l.ctx = 0
	l.mu.Unlock()
	return id, l.write(Record{Kind: KindDraft, DraftID: id, Header: &h})
}

// UpdateContext records that the inputs behind the advice changed mid-draft —
// scouting an enemy on the spot rebuilds the recommender on a new snapshot.
// Decisions written afterwards carry the new context index.
func (l *Logger) UpdateContext(h Header) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	id := l.draft
	if id != "" {
		l.ctx++
	}
	ctx := l.ctx
	l.mu.Unlock()
	if id == "" {
		return nil
	}
	h.Context = ctx
	return l.write(Record{Kind: KindContext, DraftID: id, Header: &h})
}

// newDraftID mints an id unique within this file. Callers must hold mu.
func (l *Logger) newDraftID() string {
	if l.nextID != nil {
		return l.nextID()
	}
	// Time-based, so the id doubles as the reading order of the file; a per-process
	// counter, because two drafts opened inside the same millisecond (resetting the
	// board twice in a row does exactly that) would otherwise share an id and a
	// reader would merge two games into one record; and a per-logger nonce, because
	// the counter restarts at one in every process, so two instances sharing a cache
	// directory would collide the same way.
	l.drafts++
	return fmt.Sprintf("%s-%s%d", l.nowFn().UTC().Format("20060102T150405.000"), l.nonce, l.drafts)
}

// Decision records one of our steps. Silently ignored before StartDraft: a
// decision with no context is not worth the line it would take.
func (l *Logger) Decision(d Decision) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	id, ctx := l.draft, l.ctx
	l.mu.Unlock()
	if id == "" {
		return nil
	}
	d.Context = ctx
	return l.write(Record{Kind: KindDecision, DraftID: id, Decision: &d})
}

// FinalBoard records the draft as it ended.
//
// Decisions carry the board BEFORE each action, and only our own steps are
// decisions, so the last enemy picks appear in no record at all. Without this the
// final composition — the thing the outcome is an outcome of, and the input any
// later re-scoring needs — cannot be reconstructed from the log.
func (l *Logger) FinalBoard(b Board) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	id := l.draft
	l.mu.Unlock()
	if id == "" {
		return nil
	}
	return l.write(Record{Kind: KindBoard, DraftID: id, Board: &b})
}

// ClearFinalBoard withdraws the recorded final composition, because a step was
// taken back and the draft is no longer finished.
//
// Written rather than left to the in-memory flag: the file is the record. A draft
// undone and then abandoned would otherwise be read offline as a completed draft
// ending on a composition that was never played.
func (l *Logger) ClearFinalBoard() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	id := l.draft
	l.mu.Unlock()
	if id == "" {
		return nil
	}
	return l.write(Record{Kind: KindBoard, DraftID: id})
}

// Undo takes back the decision logged at step seq.
//
// Written as its own line rather than by editing the file: an append-only log
// survives a crash mid-write, and a rewrite does not. Load applies undo lines when
// it assembles a draft, so readers never see a step the captain took back.
func (l *Logger) Undo(seq int) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	id := l.draft
	l.mu.Unlock()
	if id == "" {
		return nil
	}
	return l.write(Record{Kind: KindUndo, DraftID: id, Decision: &Decision{Seq: seq}})
}

// Reason attaches the captain's explanation to decision seq.
func (l *Logger) Reason(seq int, text string) error {
	if l == nil || text == "" {
		return nil
	}
	l.mu.Lock()
	id := l.draft
	l.mu.Unlock()
	if id == "" {
		return nil
	}
	return l.write(Record{Kind: KindReason, DraftID: id, Reason: &Reason{Seq: seq, Text: text}})
}

// Outcome closes the current draft out.
func (l *Logger) Outcome(o Outcome) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	id := l.draft
	l.mu.Unlock()
	return l.OutcomeFor(id, o)
}

// OutcomeFor closes a specific draft out. Used by the post-draft outcome agent:
// by the time OpenDota has the result, the captain may already have opened the
// next draft, and writing against the logger's "current" id would attach the
// result to the wrong game.
func (l *Logger) OutcomeFor(draftID string, o Outcome) error {
	if l == nil || draftID == "" {
		return nil
	}
	return l.write(Record{Kind: KindOutcome, DraftID: draftID, Outcome: &o})
}

// CurrentDraft reports the open draft id, or "" when none is open.
func (l *Logger) CurrentDraft() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.draft
}

func (l *Logger) write(r Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return fmt.Errorf("declog: log is closed")
	}
	if r.TS == 0 {
		r.TS = l.nowFn().Unix()
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := l.f.Write(b); err != nil {
		return err
	}
	// Flush per record. These are a handful of writes per draft, and the failure
	// this guards against — the machine going down during the game the log is
	// about — is exactly when the record matters.
	return l.f.Sync()
}

// readRecords parses the whole file. A truncated final line (killed mid-write) is
// dropped rather than failing the read: losing the last decision beats losing the
// file.
func readRecords(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			continue // unreadable line: skip, do not abort the file
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil && err != io.EOF {
		return out, err
	}
	return out, nil
}
