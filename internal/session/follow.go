package session

import "cmnet/internal/draft"

// Following a host: a guest's board can be driven by the host's live draft
// instead of by hand. It reuses the GSI path: the host's board arrives as a
// draft.Snapshot and goes through ApplyGSI, so the guest's screen is read-only
// exactly as it is when a Dota client drives it.

// SetFollowing switches the board between the host's live draft and manual
// entry. Switching either way starts from an empty board.
func (s *Session) SetFollowing(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.following == on {
		return
	}
	s.following = on
	s.gsiSeen = false
	s.lastDraftSig = ""
	if on || s.cfg.GSIEvents != nil {
		s.gsiMode = true
		s.view = &draft.Snapshot{CurrentTeam: s.cfg.FirstPick}
	} else {
		s.gsiMode = false
		s.drafts = draft.NewWithOrder(s.cfg.FirstPick, s.cmOrder())
		s.view = s.drafts
	}
	s.recompute()
}

// Following reports whether the board follows a host.
func (s *Session) Following() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.following
}

// BoardSnapshot is the board as a host sends it to following guests: the
// picks and bans so far, whose step it is, and which side we are, whether the
// board is filled by hand or by the host's own Dota.
func (s *Session) BoardSnapshot() draft.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.view
	snap := draft.Snapshot{
		RadiantPicks: append([]int{}, v.Picks(draft.Radiant)...),
		DirePicks:    append([]int{}, v.Picks(draft.Dire)...),
		RadiantBans:  append([]int{}, v.Bans(draft.Radiant)...),
		DireBans:     append([]int{}, v.Bans(draft.Dire)...),
		FirstPick:    v.FirstPickTeam(),
		LocalTeam:    s.cfg.OurTeam,
		LocalTeamOK:  true,
		HasDraft:     true,
		Live:         s.live,
	}
	if step, ok := v.Current(); ok {
		snap.CurrentTeam = step.Team
		snap.CurrentIsPick = step.Action == draft.Pick
	} else {
		snap.Done = true
	}
	return snap
}
