package session

import (
	"path/filepath"
	"testing"

	"cmnet/internal/declog"
	"cmnet/internal/draft"
)

func loggedSession(t *testing.T, cfg Config) (*Session, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), declog.FileName)
	l, err := declog.Open(path, declog.Retention{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	cfg.Log = l
	cfg.Rec = testRecommender(t)
	return New(cfg), path
}

func loadOne(t *testing.T, path string) declog.Draft {
	t.Helper()
	drafts, err := declog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 {
		t.Fatalf("%d drafts logged, want 1", len(drafts))
	}
	return drafts[0]
}

// Only our own steps are decisions. The enemy's are not ours to explain, and
// logging them would make "how often was the advice taken" meaningless.
func TestManualApplyLogsOnlyOurSteps(t *testing.T) {
	sess, path := loggedSession(t, Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant})

	// 7.34+ CM opens F F S: with Radiant first-picking, steps 0-1 are ours and
	// step 2 is the opponent's.
	for i, wantOurs := range []bool{true, true, false} {
		step, _ := sess.CurrentStep()
		if (step.Team == draft.Radiant) != wantOurs {
			t.Fatalf("step %d team = %v, want ours=%v", i, step.Team, wantOurs)
		}
		if err := sess.Apply(i + 1); err != nil {
			t.Fatal(err)
		}
	}

	d := loadOne(t, path)
	if len(d.Decisions) != 2 {
		t.Fatalf("%d decisions logged, want only our two", len(d.Decisions))
	}
	dec := d.Decisions[0]
	if dec.Seq != 0 || dec.Chosen != 1 {
		t.Errorf("decision = %+v, want step 0 with hero 1", dec.Decision)
	}
	if dec.Action != "ban" {
		t.Errorf("action = %q, want ban (the CM sequence opens with bans)", dec.Action)
	}
	if len(dec.Recs) == 0 {
		t.Error("no recommendations recorded; the log cannot say what the captain refused")
	}
	if d.Header.OurTeam != "radiant" || d.Header.Source != "manual" {
		t.Errorf("header = %+v", d.Header)
	}
}

// The recommendations stored must be the ones that were on screen for that step,
// not the ones computed after the board moved.
func TestLoggedRecsAreTheOnesThatWereShown(t *testing.T) {
	sess, path := loggedSession(t, Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant})

	shown := sess.Result().Recs
	if len(shown) == 0 {
		t.Skip("recommender produced nothing on an empty board")
	}
	if err := sess.Apply(shown[0].HeroID); err != nil {
		t.Fatal(err)
	}

	dec := loadOne(t, path).Decisions[0]
	if dec.ChosenRank != 1 {
		t.Errorf("chosenRank = %d, want 1 — the top recommendation was taken", dec.ChosenRank)
	}
	if len(dec.Recs) != len(shown) || dec.Recs[0].HeroID != shown[0].HeroID {
		t.Errorf("logged recs %v do not match what was displayed %v", dec.Recs, shown)
	}
	if seq, ok := sess.LastLoggedDecision(); !ok || seq != 0 {
		t.Errorf("LastLoggedDecision = (%d,%v), want (0,true)", seq, ok)
	}
}

// Undo has to reach the log: a hero entered by mistake must not survive as data.
func TestUndoRetractsTheLoggedDecision(t *testing.T) {
	sess, path := loggedSession(t, Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant})
	if err := sess.Apply(1); err != nil {
		t.Fatal(err)
	}
	if err := sess.Undo(); err != nil {
		t.Fatal(err)
	}
	if n := len(loadOne(t, path).Decisions); n != 0 {
		t.Fatalf("%d decisions after undo, want 0", n)
	}
	if _, ok := sess.LastLoggedDecision(); ok {
		t.Error("a reason would still attach to the retracted decision")
	}

	// Redo puts it back, on the same step.
	if err := sess.Redo(); err != nil {
		t.Fatal(err)
	}
	d := loadOne(t, path)
	if len(d.Decisions) != 1 || d.Decisions[0].Seq != 0 || d.Decisions[0].Chosen != 1 {
		t.Errorf("after redo: %+v", d.Decisions)
	}
}

// Under GSI there is no "apply": a decision is a hero that appeared on the board.
func TestGSILogsTheHeroThatAppeared(t *testing.T) {
	sess, path := loggedSession(t, Config{
		OurTeam:   draft.Radiant,
		FirstPick: draft.Radiant,
		GSIEvents: make(chan *draft.Snapshot),
	})

	sess.ApplyGSI(&draft.Snapshot{HasDraft: true, CurrentTeam: draft.Radiant, FirstPick: draft.Radiant})
	sess.ApplyGSI(&draft.Snapshot{
		HasDraft: true, RadiantBans: []int{4},
		CurrentTeam: draft.Dire, FirstPick: draft.Radiant,
	})

	d := loadOne(t, path)
	if len(d.Decisions) != 1 {
		t.Fatalf("%d decisions, want 1", len(d.Decisions))
	}
	if got := d.Decisions[0].Chosen; got != 4 {
		t.Errorf("chosen = %d, want the hero that appeared (4)", got)
	}
	if d.Header.Source != "gsi" {
		t.Errorf("source = %q, want gsi", d.Header.Source)
	}
}

// Attaching to a draft already under way (or missing events) shows several heroes
// at once. There is no honest way to say which recommendation each answered, so
// nothing is logged rather than something wrong.
func TestGSIDoesNotLogAMultiHeroJump(t *testing.T) {
	sess, path := loggedSession(t, Config{
		OurTeam:   draft.Radiant,
		FirstPick: draft.Radiant,
		GSIEvents: make(chan *draft.Snapshot),
	})

	sess.ApplyGSI(&draft.Snapshot{
		HasDraft: true, RadiantBans: []int{4}, DireBans: []int{5}, RadiantPicks: []int{1},
		CurrentTeam: draft.Radiant, FirstPick: draft.Radiant,
	})

	drafts, err := declog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range drafts {
		if len(d.Decisions) != 0 {
			t.Fatalf("logged %d decisions from a board that jumped by three heroes", len(d.Decisions))
		}
	}
}

// A new game must not be filed as a continuation of the previous draft.
func TestGSIBoardResetStartsANewDraft(t *testing.T) {
	sess, path := loggedSession(t, Config{
		OurTeam:   draft.Radiant,
		FirstPick: draft.Radiant,
		GSIEvents: make(chan *draft.Snapshot),
	})

	sess.ApplyGSI(&draft.Snapshot{HasDraft: true, CurrentTeam: draft.Radiant, FirstPick: draft.Radiant})
	sess.ApplyGSI(&draft.Snapshot{HasDraft: true, RadiantBans: []int{4}, CurrentTeam: draft.Radiant, FirstPick: draft.Radiant})
	// Board empties: a new lobby.
	sess.ApplyGSI(&draft.Snapshot{HasDraft: true, CurrentTeam: draft.Radiant, FirstPick: draft.Radiant})
	sess.ApplyGSI(&draft.Snapshot{HasDraft: true, RadiantBans: []int{6}, CurrentTeam: draft.Radiant, FirstPick: draft.Radiant})

	drafts, err := declog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("%d drafts logged, want 2 (the reset starts a new one)", len(drafts))
	}
	if drafts[1].Decisions[0].Chosen != 6 {
		t.Errorf("second draft holds %+v", drafts[1].Decisions)
	}
}

// With first pick, the last steps of a Captains Mode draft are the opponent's, and
// decisions store the board BEFORE each action — so without an explicit final board
// the composition the outcome belongs to cannot be reconstructed.
func TestFinishedDraftRecordsTheFinalBoard(t *testing.T) {
	sess, path := loggedSession(t, Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant})

	for hero := 1; ; hero++ {
		if _, ok := sess.CurrentStep(); !ok {
			break
		}
		if err := sess.Apply(hero); err != nil {
			t.Fatal(err)
		}
	}

	d := loadOne(t, path)
	if d.Final == nil {
		t.Fatal("no final board recorded for a completed draft")
	}
	if len(d.Final.RadiantPicks) != draft.PicksPerTeam || len(d.Final.DirePicks) != draft.PicksPerTeam {
		t.Errorf("final board = %+v, want five picks a side", d.Final)
	}

	// Taking the last step back un-finishes the draft; the corrected board is
	// written when it is filled again.
	if err := sess.Undo(); err != nil {
		t.Fatal(err)
	}
	if err := sess.Apply(99); err != nil {
		t.Fatal(err)
	}
	d = loadOne(t, path)
	if d.Final == nil || !containsHero(*d.Final, 99) {
		t.Errorf("the corrected final board was not written: %+v", d.Final)
	}
}

func containsHero(b declog.Board, id int) bool {
	for _, g := range [][]int{b.RadiantPicks, b.DirePicks, b.RadiantBans, b.DireBans} {
		for _, h := range g {
			if h == id {
				return true
			}
		}
	}
	return false
}

// After an undo, a reason must attach to the decision that is now the latest —
// not be dropped because the pointer was reset to "nothing".
func TestReasonAfterUndoAttachesToThePreviousDecision(t *testing.T) {
	sess, path := loggedSession(t, Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant})
	// Fill steps until we have two of our own, then take the second back.
	ours := 0
	for hero := 1; ours < 2; hero++ {
		step, ok := sess.CurrentStep()
		if !ok {
			t.Fatal("draft ended before we made two decisions")
		}
		if step.Team == draft.Radiant {
			ours++
		}
		if err := sess.Apply(hero); err != nil {
			t.Fatal(err)
		}
	}
	if err := sess.Undo(); err != nil {
		t.Fatal(err)
	}
	seq, ok := sess.LastLoggedDecision()
	if !ok {
		t.Fatal("no decision to annotate after undoing the latest one")
	}
	if err := sess.LogReason(-1, "первая всё же была не та"); err != nil {
		t.Fatal(err)
	}

	d := loadOne(t, path)
	if len(d.Decisions) != 1 || d.Decisions[0].Seq != seq {
		t.Fatalf("decisions = %+v, want just step %d", d.Decisions, seq)
	}
	if d.Decisions[0].Reason == "" {
		t.Error("the reason was discarded instead of annotating the remaining decision")
	}
}

// Undoing after the draft finished must withdraw the recorded final board in the
// file, not just in memory: if the session then closes, the log would otherwise
// claim the draft ended on a composition that was taken back.
func TestUndoAfterCompletionWithdrawsTheFinalBoard(t *testing.T) {
	sess, path := loggedSession(t, Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant})
	for hero := 1; ; hero++ {
		if _, ok := sess.CurrentStep(); !ok {
			break
		}
		if err := sess.Apply(hero); err != nil {
			t.Fatal(err)
		}
	}
	if loadOne(t, path).Final == nil {
		t.Fatal("no final board after completing the draft")
	}
	if err := sess.Undo(); err != nil {
		t.Fatal(err)
	}
	if d := loadOne(t, path); d.Final != nil {
		t.Errorf("final board = %+v, want it withdrawn once a step was taken back", d.Final)
	}
}

// Swapping sides mid-draft would otherwise file two teams' decisions under one
// header that names only one of them.
func TestSideSwapStartsANewLoggedDraft(t *testing.T) {
	sess, path := loggedSession(t, Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant})
	if err := sess.Apply(1); err != nil {
		t.Fatal(err)
	}
	sess.ToggleSide() // now Dire; step 1 is still Radiant's, step 2 is ours (F F S)
	for _, hero := range []int{2, 3} {
		if err := sess.Apply(hero); err != nil {
			t.Fatal(err)
		}
	}

	drafts, err := declog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("%d logged drafts, want 2", len(drafts))
	}
	if drafts[0].Header.OurTeam != "radiant" || drafts[1].Header.OurTeam != "dire" {
		t.Errorf("headers = %q / %q", drafts[0].Header.OurTeam, drafts[1].Header.OurTeam)
	}
}

// A reason with no seq attaches to the most recent decision — what a single input
// box in either front end means by it.
func TestLogReasonAttachesToTheLastDecision(t *testing.T) {
	sess, path := loggedSession(t, Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant})
	if err := sess.Apply(1); err != nil {
		t.Fatal(err)
	}
	if err := sess.LogReason(-1, "он это не играет"); err != nil {
		t.Fatal(err)
	}
	if err := sess.LogOutcome(true, "", "battle cup r2"); err != nil {
		t.Fatal(err)
	}

	d := loadOne(t, path)
	if d.Decisions[0].Reason != "он это не играет" {
		t.Errorf("reason = %q", d.Decisions[0].Reason)
	}
	if d.Outcome == nil || !d.Outcome.Win || d.Outcome.Note != "battle cup r2" {
		t.Errorf("outcome = %+v", d.Outcome)
	}
}

// Without a logger every call must be a silent no-op: the front ends call these
// unconditionally.
func TestNoLoggerIsInert(t *testing.T) {
	sess := New(Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant, Rec: testRecommender(t)})
	if sess.Logging() {
		t.Fatal("Logging() true without a logger")
	}
	if err := sess.Apply(1); err != nil {
		t.Fatal(err)
	}
	if err := sess.LogReason(-1, "x"); err != nil {
		t.Error(err)
	}
	if err := sess.LogOutcome(true, "", ""); err != nil {
		t.Error(err)
	}
	if sess.LogFailure() != "" {
		t.Errorf("LogFailure = %q, want empty", sess.LogFailure())
	}
}

// Changing the first-pick side resets the board, so what follows is a different
// draft — filing it under the old one would merge two games into one record.
func TestFirstPickSwapStartsANewLoggedDraft(t *testing.T) {
	sess, path := loggedSession(t, Config{OurTeam: draft.Radiant, FirstPick: draft.Radiant})
	if err := sess.Apply(1); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.ToggleFirstPick(); err != nil {
		t.Fatal(err)
	}
	// The opponent now opens the draft (7.34+: F F S), so our first step of the
	// new draft is the third one.
	for _, hero := range []int{2, 4} {
		if err := sess.Apply(hero); err != nil {
			t.Fatal(err)
		}
	}
	if step, _ := sess.CurrentStep(); step.Team != draft.Radiant {
		t.Fatalf("expected our step after the swap, got %v", step.Team)
	}
	if err := sess.Apply(3); err != nil {
		t.Fatal(err)
	}

	drafts, err := declog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("%d drafts, want 2", len(drafts))
	}
}
