package scout

import (
	"context"
	"fmt"
	"sort"
)

// BuildLayer3 reconstructs the opposing team's drafts in the current event using a
// single resolved account_id as the entry point: that player's recent matches
// since eventSinceUnix are fetched, and from each match record the full draft of
// the team the entry player was on (the opponent team) is extracted. One open
// account_id reveals the whole team's drafts (§9).
func BuildLayer3(ctx context.Context, src Source, entryID int64, eventWindow string, eventSinceUnix int64, maxMatches int, progress func(string)) *Layer3 {
	if progress == nil {
		progress = func(string) {}
	}
	refs, err := src.RecentMatches(ctx, entryID)
	if err != nil {
		return nil // no entry data -> degrade to layers 1+2
	}
	// Newest first, then within the event window.
	sort.Slice(refs, func(i, j int) bool { return refs[i].StartTime > refs[j].StartTime })

	l3 := &Layer3{EventWindow: eventWindow, EntryAccountID: entryID}
	for _, ref := range refs {
		if ctx.Err() != nil {
			break
		}
		if eventSinceUnix > 0 && ref.StartTime < eventSinceUnix {
			continue
		}
		if maxMatches > 0 && len(l3.TeamDrafts) >= maxMatches {
			break
		}
		md, err := src.MatchDraft(ctx, ref.MatchID)
		if err != nil {
			continue
		}
		td, ok := teamDraftFor(md, entryID)
		if ok {
			l3.TeamDrafts = append(l3.TeamDrafts, td)
			progress(fmt.Sprintf("layer 3: draft %d (match %d)", len(l3.TeamDrafts), ref.MatchID))
		}
	}
	l3.DraftsObserved = len(l3.TeamDrafts)
	if l3.DraftsObserved == 0 {
		return nil // no usable event drafts -> degrade
	}
	return l3
}

// teamDraftFor extracts the draft of the team the entry player was on.
func teamDraftFor(md *MatchDraft, entryID int64) (TeamDraft, bool) {
	side, found := entrySide(md, entryID)
	if !found {
		return TeamDraft{}, false
	}
	team := 1 // dire
	if side {
		team = 0 // radiant
	}
	td := TeamDraft{MatchID: md.MatchID, Radiant: side}
	for _, e := range md.Entries {
		if e.Team != team {
			continue
		}
		if e.IsPick {
			td.Picks = append(td.Picks, e.HeroID)
		} else {
			td.Bans = append(td.Bans, e.HeroID)
		}
	}
	if len(td.Picks) == 0 && len(td.Bans) == 0 {
		return TeamDraft{}, false
	}
	return td, true
}

func entrySide(md *MatchDraft, entryID int64) (radiant bool, found bool) {
	for _, p := range md.Players {
		if p.AccountID == entryID {
			return p.Radiant, true
		}
	}
	return false, false
}
