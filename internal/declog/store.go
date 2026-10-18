package declog

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"cmnet/internal/atomicfile"
)

// Draft is one assembled draft: its header, its decisions with any reasons
// attached, and the outcome if one was recorded. This is the offline read shape —
// the file is append-only lines, and nobody analysing it should have to re-derive
// which reason belongs to which decision.
type Draft struct {
	ID     string
	TS     int64
	Header Header
	// Contexts holds every version of the draft context, oldest first: Contexts[0]
	// is Header, and later entries are mid-draft replacements (a scout landing).
	// Decision.Context indexes into it, so each decision can be read against the
	// inputs that actually produced it.
	Contexts []Header
	// Final is the board as the draft ended, when it was seen through to the end.
	// Nil for an abandoned draft — decisions alone cannot reconstruct it, since
	// each one holds the board before its own action and only our steps are logged.
	Final     *Board
	Decisions []DecisionView
	Outcome   *Outcome
}

// ContextOf returns the context a decision was made under, falling back to the
// opening header when the log predates context tracking.
//
// Matched on the recorded revision number rather than by slice position: if one
// context line is lost to a truncated write, the survivors keep their own numbers,
// and a decision must not silently be attributed to a different snapshot than the
// one that produced it.
func (d Draft) ContextOf(dec DecisionView) Header {
	if dec.Context > 0 {
		for _, h := range d.Contexts {
			if h.Context == dec.Context {
				return h
			}
		}
	}
	return d.Header
}

// DecisionView is a Decision with its (later-arriving) reason folded in.
type DecisionView struct {
	Decision
	Reason string
}

// Accepted reports whether the captain took the top recommendation.
func (d DecisionView) Accepted() bool { return d.ChosenRank == 1 }

// Load reads the whole log and assembles it into drafts, oldest first. A missing
// file is not an error: it means nothing has been logged yet.
func Load(path string) ([]Draft, error) {
	recs, err := readRecords(path)
	if err != nil {
		return nil, err
	}
	return assemble(recs), nil
}

func assemble(recs []Record) []Draft {
	byID := map[string]*Draft{}
	var order []string
	get := func(id string, ts int64) *Draft {
		d, ok := byID[id]
		if !ok {
			d = &Draft{ID: id, TS: ts}
			byID[id] = d
			order = append(order, id)
		}
		return d
	}
	// Reasons may arrive before or after their decision (the captain can annotate a
	// step later), so they are collected separately and merged at the end.
	reasons := map[string]map[int]string{}
	// Decisions are keyed by step so a re-entered step supersedes the earlier line
	// and an undo removes it; the file keeps the history, a reader sees the draft
	// as it was actually played.
	steps := map[string]map[int]Decision{}
	at := func(id string) map[int]Decision {
		if steps[id] == nil {
			steps[id] = map[int]Decision{}
		}
		return steps[id]
	}

	for _, r := range recs {
		if r.DraftID == "" {
			continue
		}
		d := get(r.DraftID, r.TS)
		switch r.Kind {
		case KindDraft:
			if r.Header != nil {
				d.Header = *r.Header
				d.Contexts = []Header{*r.Header}
				d.TS = r.TS
			}
		case KindContext:
			if r.Header != nil {
				d.Contexts = append(d.Contexts, *r.Header)
			}
		case KindDecision:
			if r.Decision != nil {
				at(r.DraftID)[r.Decision.Seq] = *r.Decision
			}
		case KindUndo:
			if r.Decision != nil {
				delete(at(r.DraftID), r.Decision.Seq)
				// The explanation belonged to the choice that was taken back. Left in
				// place it would reattach to whatever is entered at that step next,
				// which puts a reason under a decision it was never about — and that
				// field is the whole point of the log.
				delete(reasons[r.DraftID], r.Decision.Seq)
			}
		case KindReason:
			if r.Reason != nil {
				if reasons[r.DraftID] == nil {
					reasons[r.DraftID] = map[int]string{}
				}
				reasons[r.DraftID][r.Reason.Seq] = r.Reason.Text
			}
		case KindBoard:
			// A board record with no board un-finishes the draft: the composition
			// recorded as final stopped being final when a step was taken back, and a
			// draft abandoned in that state must not read as complete.
			if r.Board == nil {
				d.Final = nil
				break
			}
			b := *r.Board
			d.Final = &b
		case KindOutcome:
			if r.Outcome != nil {
				o := *r.Outcome
				d.Outcome = &o
			}
		}
	}

	out := make([]Draft, 0, len(order))
	for _, id := range order {
		d := byID[id]
		for _, dec := range steps[id] {
			d.Decisions = append(d.Decisions, DecisionView{Decision: dec})
		}
		sort.SliceStable(d.Decisions, func(i, j int) bool { return d.Decisions[i].Seq < d.Decisions[j].Seq })
		for i := range d.Decisions {
			if txt, ok := reasons[id][d.Decisions[i].Seq]; ok {
				d.Decisions[i].Reason = txt
			}
		}
		out = append(out, *d)
	}
	return out
}

// Prune enforces retention, returning how many drafts it dropped. Called on every
// Open, so the bound holds without anyone running maintenance by hand.
func Prune(path string, r Retention, now time.Time) (int, error) {
	if r.MaxAgeDays <= 0 && r.MaxDrafts <= 0 {
		return 0, nil
	}
	recs, err := readRecords(path)
	if err != nil || len(recs) == 0 {
		return 0, err
	}

	// Draft ids in file order, with the timestamp of their first line: a draft
	// without a header (interrupted before one was written) still ages out.
	var order []string
	first := map[string]int64{}
	for _, rec := range recs {
		if rec.DraftID == "" {
			continue
		}
		if _, seen := first[rec.DraftID]; !seen {
			first[rec.DraftID] = rec.TS
			order = append(order, rec.DraftID)
		}
	}

	keep := map[string]bool{}
	for _, id := range order {
		keep[id] = true
	}
	if r.MaxAgeDays > 0 {
		cutoff := now.AddDate(0, 0, -r.MaxAgeDays).Unix()
		for _, id := range order {
			if first[id] < cutoff {
				keep[id] = false
			}
		}
	}
	if r.MaxDrafts > 0 {
		var alive []string
		for _, id := range order {
			if keep[id] {
				alive = append(alive, id)
			}
		}
		if len(alive) > r.MaxDrafts {
			for _, id := range alive[:len(alive)-r.MaxDrafts] {
				keep[id] = false
			}
		}
	}

	dropped := 0
	for _, id := range order {
		if !keep[id] {
			dropped++
		}
	}
	if dropped == 0 {
		return 0, nil
	}
	out := recs[:0]
	for _, rec := range recs {
		if rec.DraftID == "" || keep[rec.DraftID] {
			out = append(out, rec)
		}
	}
	return dropped, writeAll(path, out)
}

// ForgetIn erases one account from the log: their id, name and profile disappear
// from every header, and the match ids of the drafts they played in are cleared
// with them. Returns how many drafts were touched.
//
// Redaction rather than deletion of whole drafts: the draft records what WE were
// advised and chose, which stays useful and is our own data; what must go is the
// third party's identity. Dropping the surrounding draft would punish the request
// by destroying unrelated records — Purge is there for whoever wants that anyway.
//
// The match id goes too because it is an indirect identifier: it resolves through
// any Dota API back to the ten people who played the game, which defeats removing
// the account id. Free text (refusal reasons, notes) is left alone — a program
// cannot reliably tell whether a sentence names someone, and silently mangling the
// substance of the log would be worse than leaving it. Purge is the honest answer
// when the text matters.
func ForgetIn(path string, accountID int64) (int, error) {
	return forget(path, accountID, false)
}

// Purge deletes every draft the account appears in, whole. The blunt instrument,
// for when redaction is not enough — free-text reasons naming the player, or a
// request to remove the record of a game entirely.
func Purge(path string, accountID int64) (int, error) {
	return forget(path, accountID, true)
}

func forget(path string, accountID int64, purge bool) (int, error) {
	recs, err := readRecords(path)
	if err != nil || len(recs) == 0 {
		return 0, err
	}

	// Which drafts they appear in: their records are spread over several lines, and
	// the header is the only line naming them.
	touched := map[string]bool{}
	for _, r := range recs {
		h := r.Header
		if h == nil {
			continue
		}
		if hasPlayer(h.Allies, accountID) || hasPlayer(h.Enemies, accountID) {
			touched[r.DraftID] = true
		}
	}
	if len(touched) == 0 {
		return 0, nil
	}

	out := make([]Record, 0, len(recs))
	for _, r := range recs {
		if !touched[r.DraftID] {
			out = append(out, r)
			continue
		}
		if purge {
			continue
		}
		if h := r.Header; h != nil {
			h.Allies, _ = dropPlayer(h.Allies, accountID)
			h.Enemies, _ = dropPlayer(h.Enemies, accountID)
		}
		if r.Outcome != nil {
			r.Outcome.MatchID = ""
		}
		out = append(out, r)
	}
	return len(touched), writeAll(path, out)
}

func hasPlayer(ps []Player, id int64) bool {
	for _, p := range ps {
		if p.AccountID == id {
			return true
		}
	}
	return false
}

func dropPlayer(ps []Player, id int64) ([]Player, int) {
	n := 0
	out := ps[:0]
	for _, p := range ps {
		if p.AccountID == id {
			n++
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, n
	}
	return out, n
}

// Forget erases an account from the log while it is open: the handle is closed,
// the file rewritten, and the handle reopened, all under the write lock so no
// record can slip in against the old file. purge deletes the whole drafts instead
// of redacting them.
func (l *Logger) Forget(accountID int64, purge bool) (int, error) {
	if l == nil {
		return 0, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		if err := l.f.Close(); err != nil {
			return 0, err
		}
		l.f = nil
	}
	n, ferr := forget(l.path, accountID, purge)
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return n, err
	}
	l.f = f
	return n, ferr
}

func writeAll(path string, recs []Record) error {
	var buf []byte
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}
	return atomicfile.Write(path, buf, 0o600)
}

// Export writes the log with every account id replaced by a salted pseudonym and
// every persona name removed, for sharing with teammates.
//
// The salt is random per export and never stored. Without one, a "hash" of an
// account id is not pseudonymisation at all: the id space is small enough that
// anyone can hash all of it and invert the mapping in seconds.
//
// Free-text reasons are exported as written. They are the substance of the log,
// and rewriting them is not something a program can do safely — whoever exports is
// responsible for what they typed there.
func Export(path string) ([]byte, error) {
	recs, err := readRecords(path)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	pseudo := func(id int64) int64 {
		h := sha256.New()
		h.Write(salt)
		fmt.Fprintf(h, "%d", id)
		sum := h.Sum(nil)
		// Positive 48-bit value: stays a JSON-safe integer and cannot be mistaken
		// for a real Steam account id.
		var v int64
		for _, b := range sum[:6] {
			v = v<<8 | int64(b)
		}
		return v + 1
	}

	var buf []byte
	for _, r := range recs {
		if h := r.Header; h != nil {
			for i := range h.Allies {
				h.Allies[i].AccountID = pseudo(h.Allies[i].AccountID)
				h.Allies[i].Persona = ""
			}
			for i := range h.Enemies {
				h.Enemies[i].AccountID = pseudo(h.Enemies[i].AccountID)
				h.Enemies[i].Persona = ""
			}
		}
		if r.Outcome != nil {
			r.Outcome.MatchID = "" // identifies the game, and through it the players
		}
		b, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}
	return buf, nil
}
