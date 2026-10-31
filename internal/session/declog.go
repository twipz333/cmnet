package session

import (
	"fmt"

	"cmnet/internal/declog"
	"cmnet/internal/draft"
	"cmnet/internal/outcome"
	"cmnet/internal/recommender"
)

// Prospective decision logging (§6.1). The session is the only place that sees
// both halves of a decision — what the core recommended and what the human then
// did — so the capture lives here rather than in either front end. Both front ends
// therefore log identically, and neither can log something the other does not.
//
// Everything here is best-effort by construction: a logging failure is reported
// through Config.LogError (if set) and never propagates into the draft. The log
// must not be able to break the thing it is observing.

// logHeaderFor builds the per-draft context from the session configuration.
// Callers must hold mu.
func (s *Session) logHeaderFor() declog.Header {
	h := s.cfg.LogHeader
	h.OurTeam = teamName(s.cfg.OurTeam)
	h.FirstPick = teamName(s.cfg.FirstPick)
	h.Source = "manual"
	if s.gsiMode {
		h.Source = "gsi"
	}
	h.BundleVersion = s.cfg.BundleVersion
	h.PatchTag = s.cfg.PatchTag
	return h
}

func teamName(t draft.Team) string {
	if t == draft.Dire {
		return "dire"
	}
	return "radiant"
}

// startLogDraft opens a draft in the log if one is not open yet. Callers must hold mu.
func (s *Session) startLogDraft() {
	if s.cfg.Log == nil || s.logOpen {
		return
	}
	s.logOpen = true
	if _, err := s.cfg.Log.StartDraft(s.logHeaderFor()); err != nil {
		s.logErr(err)
	}
}

// resetLogDraft ends the current logged draft, so the next decision starts a new
// one. Callers must hold mu.
func (s *Session) resetLogDraft() {
	s.logOpen = false
	s.logFinal = false
	s.loggedChoices = nil
}

// logErr remembers the last logging failure. Callers must hold mu.
//
// Remembered rather than returned or printed: returning it would let a full disk
// abort a draft step, and printing it would corrupt the terminal UI that owns the
// screen. A front end shows it in the status bar, which is the honest outcome —
// the draft continues, and the user can see that the log stopped recording.
func (s *Session) logErr(err error) {
	if err != nil {
		s.logFail = err
	}
}

// LogFailure returns the last decision-logging error, or "" when logging is
// healthy or off.
func (s *Session) LogFailure() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logFail == nil {
		return ""
	}
	return s.logFail.Error()
}

// logDecision records one of our steps: the board as it was, everything shown, and
// the hero the captain took. Callers must hold mu.
//
// res is the result that was on screen for this step — captured BEFORE the board
// moved, because after the move the recommender has already recomputed for the next
// one, and logging that would record advice nobody saw.
func (s *Session) logDecision(seq int, step draft.Step, board declog.Board, res recommender.Result, heroID int) {
	if s.cfg.Log == nil {
		return
	}
	s.startLogDraft()

	d := declog.Decision{
		Seq:        seq,
		Action:     actionName(step.Action),
		Board:      board,
		Recs:       logRecs(res.Recs),
		EnemyPred:  logPreds(res.EnemyPred),
		Chosen:     heroID,
		ChosenRank: rankOf(res.Recs, heroID),
	}
	if res.Strength != nil {
		p := res.Strength.OurProb
		d.Strength = &p
	}
	s.logErr(s.cfg.Log.Decision(d))
	// Keep an in-memory copy for the post-draft survey (file is append-only).
	ch := LoggedChoice{Seq: d.Seq, Action: d.Action, Chosen: d.Chosen, ChosenRank: d.ChosenRank}
	if len(res.Recs) > 0 {
		ch.TopHero = res.Recs[0].HeroID
		ch.TopWhy = res.Recs[0].Why
	}
	replaced := false
	for i := range s.loggedChoices {
		if s.loggedChoices[i].Seq == seq {
			s.loggedChoices[i] = ch
			replaced = true
			break
		}
	}
	if !replaced {
		s.loggedChoices = append(s.loggedChoices, ch)
	}
}

func actionName(a draft.Action) string {
	if a == draft.Ban {
		return "ban"
	}
	return "pick"
}

// rankOf returns the 1-based position of heroID among the shown recommendations,
// or 0 when it was not shown at all.
func rankOf(recs []recommender.Recommendation, heroID int) int {
	for i, r := range recs {
		if r.HeroID == heroID {
			return i + 1
		}
	}
	return 0
}

func logRecs(recs []recommender.Recommendation) []declog.Rec {
	if len(recs) == 0 {
		return nil
	}
	out := make([]declog.Rec, 0, len(recs))
	for _, r := range recs {
		rec := declog.Rec{HeroID: r.HeroID, Score: r.Score, Why: r.Why, Role: r.Role, Player: r.Player}
		if f := r.Factors; f != nil {
			rec.Factors = &declog.Factors{
				Base:          f.Base,
				BaseIsWinProb: f.BaseIsWinProb,
				Comfort:       f.Comfort,
				Capability:    f.Capability,
				EnemyProb:     f.EnemyProb,
				Threat:        f.Threat,
				EnemySynergy:  f.EnemySynergy,
				AltDeficit:    f.AltDeficit,
			}
		}
		out = append(out, rec)
	}
	return out
}

func logPreds(ps []recommender.EnemyPick) []declog.Pred {
	if len(ps) == 0 {
		return nil
	}
	out := make([]declog.Pred, 0, len(ps))
	for _, p := range ps {
		out = append(out, declog.Pred{
			HeroID: p.HeroID, Prob: p.Prob, Source: p.Source,
			Threat: p.Threat, Fit: p.Fit, Danger: p.Danger,
		})
	}
	return out
}

// boardOf snapshots the heroes on the board before an action. Callers must hold mu.
func boardOf(v draft.View) declog.Board {
	return declog.Board{
		RadiantPicks: copyInts(v.Picks(draft.Radiant)),
		DirePicks:    copyInts(v.Picks(draft.Dire)),
		RadiantBans:  copyInts(v.Bans(draft.Radiant)),
		DireBans:     copyInts(v.Bans(draft.Dire)),
	}
}

func copyInts(s []int) []int {
	if len(s) == 0 {
		return nil
	}
	return append([]int(nil), s...)
}

// logGSI records our step from a GSI event. Callers must hold mu, and must call it
// before adopting the new snapshot: the comparison is against the board we were
// showing advice for.
//
// GSI reports the board, not the move, so a decision has to be inferred from the
// difference. Exactly one new hero is a step; anything else is deliberately not
// logged. Two or more at once means the advisor attached to a draft already in
// progress (or missed events), and there is no honest way to say which
// recommendation each hero answered.
func (s *Session) logGSI(next *draft.Snapshot) {
	if s.cfg.Log == nil || next == nil || !next.HasDraft {
		return
	}
	prev := boardOf(s.view)
	prevN, nextBoard := boardCount(prev), boardOf(next)
	nextN := boardCount(nextBoard)

	// An emptier board than before is a new game, not a correction.
	if nextN < prevN {
		s.resetLogDraft()
		s.lastLoggedSeq = -1
		return
	}
	if nextN != prevN+1 {
		return
	}
	hero, ok := addedHero(prev, nextBoard)
	if !ok {
		return
	}
	step, stepOK := s.view.Current()
	if !stepOK || step.Team != s.cfg.OurTeam {
		return
	}
	s.logDecision(prevN, step, prev, s.result, hero)
	s.lastLoggedSeq = prevN
}

// logFinalBoard writes the finished draft once, so the composition the outcome
// belongs to is in the record. Callers must hold mu.
//
// Needed because a decision stores the board before its own action and only our
// steps are decisions: with first pick, the enemy's last picks appear nowhere.
func (s *Session) logFinalBoard(v draft.View) {
	if s.cfg.Log == nil || !s.logOpen || s.logFinal {
		return
	}
	if _, ok := v.Current(); ok {
		return // draft still running
	}
	s.logFinal = true
	s.logErr(s.cfg.Log.FinalBoard(boardOf(v)))
}

// previousOurStep returns the newest filled step of ours before seq, or -1 when
// there is none. Callers must hold mu.
func (s *Session) previousOurStep(seq int) int {
	if s.drafts == nil {
		return -1
	}
	for i := seq - 1; i >= 0 && i < len(s.drafts.Seq); i-- {
		if i < s.drafts.Cursor && s.drafts.Seq[i].Team == s.cfg.OurTeam {
			return i
		}
	}
	return -1
}

func boardCount(b declog.Board) int {
	return len(b.RadiantPicks) + len(b.DirePicks) + len(b.RadiantBans) + len(b.DireBans)
}

// addedHero returns the single hero present in next but not in prev.
func addedHero(prev, next declog.Board) (int, bool) {
	seen := map[int]bool{}
	for _, g := range [][]int{prev.RadiantPicks, prev.DirePicks, prev.RadiantBans, prev.DireBans} {
		for _, id := range g {
			seen[id] = true
		}
	}
	for _, g := range [][]int{next.RadiantPicks, next.DirePicks, next.RadiantBans, next.DireBans} {
		for _, id := range g {
			if id != draft.NoHero && !seen[id] {
				return id, true
			}
		}
	}
	return 0, false
}

// LogReason records why the captain went against the recommendation at step seq.
// A seq of -1 means "the most recent logged decision", which is what a front end
// with a single reason box wants.
//
// This is the field the whole log exists for: the offline corpus can rank
// historical picks, but only a human saying "he cannot play that hero" names a
// feature the model does not have (§6.1).
func (s *Session) LogReason(seq int, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Log == nil || text == "" {
		return nil
	}
	if seq < 0 {
		if s.lastLoggedSeq < 0 {
			return nil
		}
		seq = s.lastLoggedSeq
	}
	err := s.cfg.Log.Reason(seq, text)
	s.logErr(err)
	return err
}

// LogOutcome closes the current draft out with how the game went.
func (s *Session) LogOutcome(win bool, matchID, note string) error {
	return s.LogOutcomeFor("", win, matchID, note)
}

// LogOutcomeFor closes draftID (or the current draft when draftID is empty).
func (s *Session) LogOutcomeFor(draftID string, win bool, matchID, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Log == nil {
		return nil
	}
	if draftID == "" {
		draftID = s.cfg.Log.CurrentDraft()
	}
	err := s.cfg.Log.OutcomeFor(draftID, declog.Outcome{Win: win, MatchID: matchID, Note: note})
	s.logErr(err)
	return err
}

// OutcomeQuery snapshots what the post-draft outcome agent needs to find the
// finished match. ok is false when logging is off or there is no open draft.
func (s *Session) OutcomeQuery() (outcome.Query, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Log == nil {
		return outcome.Query{}, fmt.Errorf("decision logging is off")
	}
	draftID := s.cfg.Log.CurrentDraft()
	if draftID == "" {
		return outcome.Query{}, fmt.Errorf("no open draft to attach an outcome to")
	}
	q := outcome.Query{
		DraftID: draftID,
		OurTeam: s.cfg.OurTeam,
	}
	if s.view != nil {
		q.RadiantPicks = append([]int(nil), s.view.Picks(draft.Radiant)...)
		q.DirePicks = append([]int(nil), s.view.Picks(draft.Dire)...)
	}
	if live := s.live; live != nil {
		if id, err := outcome.ParseMatchID(live.MatchID); err == nil {
			q.MatchID = id
		}
	}
	for _, p := range s.cfg.LogHeader.Allies {
		if p.AccountID > 0 {
			q.AllyIDs = append(q.AllyIDs, p.AccountID)
		}
	}
	if q.MatchID == 0 && len(q.AllyIDs) == 0 {
		return q, fmt.Errorf("нет match id (GSI) и нет союзников в снапшоте — нечем искать матч")
	}
	return q, nil
}

// LastLoggedDecision reports the step of the most recent logged decision, and
// whether there is one. Front ends use it to label the reason box with the
// decision it will annotate.
func (s *Session) LastLoggedDecision() (seq int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastLoggedSeq, s.lastLoggedSeq >= 0
}

// Logging reports whether decision logging is on.
func (s *Session) Logging() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Log != nil
}

// LogPath returns the on-disk decision log path when logging is wired, else "".
func (s *Session) LogPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Log == nil {
		return ""
	}
	return s.cfg.Log.Path()
}


// ReviewChoices returns our logged decisions for the current draft (copy).
// Used by the post-draft survey; callers must not hold mu (it locks).
func (s *Session) ReviewChoices() []LoggedChoice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LoggedChoice, len(s.loggedChoices))
	copy(out, s.loggedChoices)
	return out
}

// OffRecChoices returns only the steps where we did not take the top recommendation
// (ChosenRank != 1). Rank 0 means the pick was not among shown recs at all.
func (s *Session) OffRecChoices() []LoggedChoice {
	all := s.ReviewChoices()
	out := make([]LoggedChoice, 0, len(all))
	for _, c := range all {
		if c.ChosenRank != 1 {
			out = append(out, c)
		}
	}
	return out
}

// dropLoggedChoice removes a step from the in-memory survey list. Callers hold mu.
func (s *Session) dropLoggedChoice(seq int) {
	dst := s.loggedChoices[:0]
	for _, c := range s.loggedChoices {
		if c.Seq != seq {
			dst = append(dst, c)
		}
	}
	s.loggedChoices = dst
}
