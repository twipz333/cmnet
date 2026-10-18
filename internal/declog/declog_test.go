package declog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTemp(t *testing.T) (*Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	l, err := Open(path, Retention{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, path
}

// The acceptance criterion of §6.1: one played draft must produce a record that
// can be read back offline, whole — the context, what was shown, what was chosen,
// and why not.
func TestOneDraftRoundTrips(t *testing.T) {
	l, path := openTemp(t)

	if _, err := l.StartDraft(Header{
		OurTeam:   "radiant",
		FirstPick: "dire",
		Source:    "gsi",
		Enemies: []Player{{
			AccountID: 42,
			Persona:   "opponent",
			Heroes:    []HeroStat{{HeroID: 8, Games: 30, WinRate: 0.6}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := l.Decision(Decision{
		Seq: 6, Action: "pick",
		Board:      Board{RadiantBans: []int{1, 2}, DireBans: []int{3}},
		Recs:       []Rec{{HeroID: 11, Score: 0.55, Why: "win 55.0%"}, {HeroID: 12, Score: 0.54}},
		EnemyPred:  []Pred{{HeroID: 8, Prob: 0.2, Source: "player"}},
		Chosen:     12,
		ChosenRank: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := l.Reason(6, "он не играет 11 на своей позиции"); err != nil {
		t.Fatal(err)
	}
	if err := l.Outcome(Outcome{Win: true, MatchID: "123"}); err != nil {
		t.Fatal(err)
	}

	drafts, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 {
		t.Fatalf("loaded %d drafts, want 1", len(drafts))
	}
	d := drafts[0]
	if d.Header.Source != "gsi" || len(d.Header.Enemies) != 1 {
		t.Errorf("header lost: %+v", d.Header)
	}
	if len(d.Header.Enemies[0].Heroes) != 1 {
		t.Error("the opponent profile as of the draft is missing; the record cannot be re-scored later")
	}
	if len(d.Decisions) != 1 {
		t.Fatalf("%d decisions, want 1", len(d.Decisions))
	}
	dec := d.Decisions[0]
	if dec.Chosen != 12 || dec.ChosenRank != 2 || dec.Accepted() {
		t.Errorf("decision = %+v, want the second recommendation taken", dec.Decision)
	}
	if len(dec.Recs) != 2 || dec.Recs[0].Why == "" {
		t.Error("what was shown did not survive the round trip")
	}
	if dec.Reason == "" {
		t.Error("the refusal reason is missing — it is the field this log exists for")
	}
	if d.Outcome == nil || !d.Outcome.Win {
		t.Error("outcome lost")
	}
}

// A step taken back must not survive as if it had been drafted: a mistyped hero
// would otherwise become data.
func TestUndoRemovesTheDecision(t *testing.T) {
	l, path := openTemp(t)
	if _, err := l.StartDraft(Header{}); err != nil {
		t.Fatal(err)
	}
	_ = l.Decision(Decision{Seq: 4, Chosen: 99})
	_ = l.Undo(4)

	drafts, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(drafts[0].Decisions); n != 0 {
		t.Fatalf("%d decisions after undo, want 0", n)
	}

	// Re-entering the step supersedes the undo, and the corrected hero is what a
	// reader sees.
	_ = l.Decision(Decision{Seq: 4, Chosen: 7})
	drafts, _ = Load(path)
	if n := len(drafts[0].Decisions); n != 1 || drafts[0].Decisions[0].Chosen != 7 {
		t.Fatalf("after redo: %d decisions, first chosen %v", n, drafts[0].Decisions)
	}
}

// A step re-entered (manual correction) must replace the earlier line rather than
// appear twice.
func TestRepeatedStepSupersedes(t *testing.T) {
	l, path := openTemp(t)
	_, _ = l.StartDraft(Header{})
	_ = l.Decision(Decision{Seq: 2, Chosen: 5})
	_ = l.Decision(Decision{Seq: 2, Chosen: 6})

	drafts, _ := Load(path)
	if n := len(drafts[0].Decisions); n != 1 {
		t.Fatalf("%d decisions for one step, want 1", n)
	}
	if got := drafts[0].Decisions[0].Chosen; got != 6 {
		t.Errorf("chosen = %d, want the corrected 6", got)
	}
}

// Erasing a player must reach the log, and must not take unrelated records with it.
func TestForgetRedactsOnlyThatPlayer(t *testing.T) {
	l, path := openTemp(t)
	_, _ = l.StartDraft(Header{
		Allies:  []Player{{AccountID: 1, Persona: "us"}},
		Enemies: []Player{{AccountID: 42, Persona: "them", Heroes: []HeroStat{{HeroID: 8, Games: 3}}}, {AccountID: 43}},
	})
	_ = l.Decision(Decision{Seq: 0, Chosen: 7})

	n, err := l.Forget(42, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("touched %d drafts, want 1", n)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\"accountId\":42") || strings.Contains(string(raw), "them") {
		t.Error("the forgotten account is still in the file")
	}
	drafts, _ := Load(path)
	if len(drafts) != 1 || len(drafts[0].Decisions) != 1 {
		t.Error("forgetting one player destroyed unrelated records")
	}
	if len(drafts[0].Header.Enemies) != 1 || drafts[0].Header.Enemies[0].AccountID != 43 {
		t.Errorf("the other opponent was removed too: %+v", drafts[0].Header.Enemies)
	}
	if len(drafts[0].Header.Allies) != 1 {
		t.Error("our own side was removed by forgetting an opponent")
	}

	// The log must keep working afterwards: the handle is closed and reopened.
	if err := l.Decision(Decision{Seq: 1, Chosen: 9}); err != nil {
		t.Fatalf("log broken after Forget: %v", err)
	}
}

// The match id resolves through any Dota API back to the ten people who played the
// game, so leaving it behind would undo removing the account id.
func TestForgetClearsTheMatchID(t *testing.T) {
	l, path := openTemp(t)
	_, _ = l.StartDraft(Header{Enemies: []Player{{AccountID: 42}}})
	_ = l.Decision(Decision{Seq: 0, Chosen: 7})
	_ = l.Outcome(Outcome{Win: true, MatchID: "7654321"})

	if _, err := l.Forget(42, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "7654321") {
		t.Error("the match id survived; it identifies the player indirectly")
	}
	drafts, _ := Load(path)
	if drafts[0].Outcome == nil || !drafts[0].Outcome.Win {
		t.Error("clearing the match id destroyed the outcome itself")
	}
}

// Purge is the blunt option: whole drafts go, because free text can name someone
// and no program can reliably tell.
func TestPurgeDeletesWholeDrafts(t *testing.T) {
	l, path := openTemp(t)
	_, _ = l.StartDraft(Header{Enemies: []Player{{AccountID: 42}}})
	_ = l.Decision(Decision{Seq: 0, Chosen: 7})
	_ = l.Reason(0, "Вася на этом герое не играет")
	_, _ = l.StartDraft(Header{Enemies: []Player{{AccountID: 43}}})
	_ = l.Decision(Decision{Seq: 0, Chosen: 8})

	// Through the logger, not the package function: with the log open, the rewrite
	// cannot replace a file this process still holds (Windows refuses the rename).
	n, err := l.Forget(42, true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged %d drafts, want 1", n)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "Вася") {
		t.Error("free text from the purged draft is still in the file")
	}
	drafts, _ := Load(path)
	if len(drafts) != 1 || len(drafts[0].Decisions) != 1 {
		t.Errorf("purge took the unrelated draft with it: %+v", drafts)
	}
}

// Decisions hold the board BEFORE each action and only our steps are logged, so
// the final composition exists only if it is written explicitly.
func TestFinalBoardIsRecoverable(t *testing.T) {
	l, path := openTemp(t)
	_, _ = l.StartDraft(Header{})
	_ = l.Decision(Decision{Seq: 0, Action: "pick", Board: Board{RadiantPicks: []int{1}}, Chosen: 2})
	_ = l.FinalBoard(Board{RadiantPicks: []int{1, 2, 3}, DirePicks: []int{4, 5, 6}})

	d := loadSingle(t, path)
	if d.Final == nil {
		t.Fatal("no final board; the composition the outcome belongs to is unrecoverable")
	}
	if len(d.Final.DirePicks) != 3 {
		t.Errorf("final board = %+v, want the enemy's last picks too", d.Final)
	}
}

func loadSingle(t *testing.T, path string) Draft {
	t.Helper()
	drafts, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 {
		t.Fatalf("%d drafts, want 1", len(drafts))
	}
	return drafts[0]
}

func TestPruneEnforcesBothBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	now := time.Now()

	// Three drafts, the first of them long past retention.
	write := func(id string, ts int64) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, r := range []Record{
			{Kind: KindDraft, TS: ts, DraftID: id, Header: &Header{}},
			{Kind: KindDecision, TS: ts, DraftID: id, Decision: &Decision{Seq: 0, Chosen: 1}},
		} {
			b, _ := json.Marshal(r)
			f.Write(append(b, '\n'))
		}
	}
	write("old", now.AddDate(0, 0, -400).Unix())
	write("mid", now.AddDate(0, 0, -2).Unix())
	write("new", now.Unix())

	dropped, err := Prune(path, Retention{MaxAgeDays: 180}, now)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("age prune dropped %d, want 1", dropped)
	}
	drafts, _ := Load(path)
	if len(drafts) != 2 {
		t.Fatalf("%d drafts after age prune, want 2", len(drafts))
	}

	if _, err := Prune(path, Retention{MaxDrafts: 1}, now); err != nil {
		t.Fatal(err)
	}
	drafts, _ = Load(path)
	if len(drafts) != 1 || drafts[0].ID != "new" {
		t.Fatalf("count prune kept %v, want just the newest", draftIDs(drafts))
	}
}

func draftIDs(ds []Draft) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out
}

// Export must not hand out account ids, and must not hand out a mapping anyone can
// invert by hashing the (small) id space.
func TestExportPseudonymisesAndSalts(t *testing.T) {
	l, path := openTemp(t)
	_, _ = l.StartDraft(Header{Enemies: []Player{{AccountID: 42, Persona: "them"}}})
	_ = l.Decision(Decision{Seq: 0, Chosen: 7})
	_ = l.Outcome(Outcome{Win: false, MatchID: "7654321"})

	a, err := Export(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(a)
	if strings.Contains(s, "\"accountId\":42") {
		t.Error("the real account id survived the export")
	}
	if strings.Contains(s, "them") {
		t.Error("the persona name survived the export")
	}
	if strings.Contains(s, "7654321") {
		t.Error("the match id survived the export; it identifies the players through the API")
	}
	if !strings.Contains(s, "\"chosen\":7") {
		t.Error("the export dropped the substance along with the identifiers")
	}

	b, err := Export(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == s {
		t.Error("two exports produced identical pseudonyms — the salt is not random, " +
			"so the mapping can be inverted by hashing the id space")
	}
}

// Two drafts opened inside the same millisecond — resetting the board twice, or
// a GSI reconnect — must stay two records. They shared an id when the id was the
// timestamp alone, and a reader merged the games into one.
func TestDraftsInTheSameInstantStaySeparate(t *testing.T) {
	l, path := openTemp(t)
	frozen := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	l.nowFn = func() time.Time { return frozen }

	for i := 0; i < 2; i++ {
		if _, err := l.StartDraft(Header{}); err != nil {
			t.Fatal(err)
		}
		if err := l.Decision(Decision{Seq: 0, Chosen: i + 1}); err != nil {
			t.Fatal(err)
		}
	}
	drafts, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("%d drafts, want 2 — ids collided: %v", len(drafts), draftIDs(drafts))
	}
}

// A draft undone after it finished, and then abandoned, must not read offline as a
// draft that ended on a composition nobody played.
func TestClearedFinalBoardDoesNotSurvive(t *testing.T) {
	l, path := openTemp(t)
	_, _ = l.StartDraft(Header{})
	_ = l.FinalBoard(Board{RadiantPicks: []int{1, 2, 3}})
	_ = l.ClearFinalBoard()

	if d := loadSingle(t, path); d.Final != nil {
		t.Errorf("final board = %+v, want nil after it was withdrawn", d.Final)
	}
}

// The reason belonged to the choice that was taken back. Reattaching it to whatever
// is entered at that step next would put an explanation under a decision it was
// never about — and that field is the point of the log.
func TestUndoDropsTheReasonToo(t *testing.T) {
	l, path := openTemp(t)
	_, _ = l.StartDraft(Header{})
	_ = l.Decision(Decision{Seq: 3, Chosen: 10})
	_ = l.Reason(3, "он это не играет")
	_ = l.Undo(3)
	_ = l.Decision(Decision{Seq: 3, Chosen: 11})

	d := loadSingle(t, path)
	if len(d.Decisions) != 1 {
		t.Fatalf("%d decisions, want 1", len(d.Decisions))
	}
	if d.Decisions[0].Reason != "" {
		t.Errorf("reason %q carried over to the corrected pick %d",
			d.Decisions[0].Reason, d.Decisions[0].Chosen)
	}
}

// Scouting an enemy mid-draft rebuilds the recommender on a new snapshot. Decisions
// after that point must not read as having been made under the opening one.
func TestContextChangeIsAttributed(t *testing.T) {
	l, path := openTemp(t)
	_, _ = l.StartDraft(Header{SnapshotHash: "old"})
	_ = l.Decision(Decision{Seq: 0, Chosen: 1})
	_ = l.UpdateContext(Header{SnapshotHash: "new"})
	_ = l.Decision(Decision{Seq: 2, Chosen: 2})

	d := loadSingle(t, path)
	if len(d.Contexts) != 2 {
		t.Fatalf("%d contexts, want 2", len(d.Contexts))
	}
	if got := d.ContextOf(d.Decisions[0]).SnapshotHash; got != "old" {
		t.Errorf("first decision attributed to snapshot %q, want old", got)
	}
	if got := d.ContextOf(d.Decisions[1]).SnapshotHash; got != "new" {
		t.Errorf("second decision attributed to snapshot %q, want new", got)
	}
}

// A line lost to a killed process must cost that line, not the file.
func TestTruncatedLineIsSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := `{"kind":"draft","ts":1,"draft":"a","header":{}}
{"kind":"decision","ts":2,"draft":"a","decision":{"seq":0,"chosen":5}}
{"kind":"decision","ts":3,"draf`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	drafts, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || len(drafts[0].Decisions) != 1 {
		t.Fatalf("got %d drafts / %v decisions, want the two complete lines",
			len(drafts), len(drafts[0].Decisions))
	}
}

// A nil logger is the "logging off" case, and every front end calls straight into
// it without a branch.
func TestNilLoggerIsSilent(t *testing.T) {
	var l *Logger
	if _, err := l.StartDraft(Header{}); err != nil {
		t.Error(err)
	}
	if err := l.Decision(Decision{}); err != nil {
		t.Error(err)
	}
	if err := l.Reason(0, "x"); err != nil {
		t.Error(err)
	}
	if err := l.Close(); err != nil {
		t.Error(err)
	}
}

// Decisions written before any draft was opened have no context and are dropped
// rather than filed under an empty id.
func TestDecisionWithoutDraftIsDropped(t *testing.T) {
	l, path := openTemp(t)
	if err := l.Decision(Decision{Seq: 0, Chosen: 3}); err != nil {
		t.Fatal(err)
	}
	drafts, _ := Load(path)
	if len(drafts) != 0 {
		t.Fatalf("%d drafts, want none", len(drafts))
	}
}
