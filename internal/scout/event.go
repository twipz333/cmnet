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
//
// The same match records also carry the account_ids of the entry player's
// teammates, and those are collected into TeamAccountIDs. That is not a
// convenience: layer 3 needs one id, but per-player affinity needs all five, and
// a squad known two-fifths deep produces a role assignment that is wrong by
// construction. Recovering the other four from a record we already fetched costs
// nothing extra.
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
	seen := map[int64]int{} // teammate account_id -> matches played alongside the entry
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
		// Teammates come from the player list, independent of whether the match
		// carried a usable CM draft. An AP lobby still names the squad; dropping
		// those ids because picks_bans was empty is how a five-stack stayed at one
		// resolved account forever.
		if _, found := entrySide(md, entryID); found {
			for _, id := range teammatesOf(md, entryID) {
				seen[id]++
			}
		}
		if td, ok := teamDraftFor(md, entryID); ok {
			l3.TeamDrafts = append(l3.TeamDrafts, td)
			progress(fmt.Sprintf("layer 3: draft %d (match %d)", len(l3.TeamDrafts), ref.MatchID))
		}
	}
	l3.DraftsObserved = len(l3.TeamDrafts)
	l3.TeamAccountIDs = rankTeammates(seen)
	if n := len(l3.TeamAccountIDs); n > 0 {
		progress(fmt.Sprintf("layer 3: recovered %d teammate account_ids from the same records", n))
	}
	// No drafts and no teammates → nothing to hand the caller. Drafts-only miss still
	// returns the shell so Fetch can discover the squad into layer 2.
	if l3.DraftsObserved == 0 && len(l3.TeamAccountIDs) == 0 {
		return nil
	}
	return l3
}

// teammatesOf returns the account_ids playing alongside the entry player in a
// match. Anonymous participants report id 0 and are skipped: a hidden profile is
// absence of data, and a zero threaded into the address book would look like a
// real account to every JOIN downstream.
func teammatesOf(md *MatchDraft, entryID int64) []int64 {
	side, found := entrySide(md, entryID)
	if !found {
		return nil
	}
	var out []int64
	for _, p := range md.Players {
		if p.AccountID <= 0 || p.AccountID == entryID || p.Radiant != side {
			continue
		}
		out = append(out, p.AccountID)
	}
	return out
}

// rankTeammates orders recovered ids by how often they appeared alongside the
// entry player, id ascending on ties.
//
// Frequency first because a roster can shuffle between rounds: the four who show
// up in every match of the event are the team, and someone who filled in once is
// not. The tie-break makes the order total, which the snapshot hash depends on —
// map iteration order here would give two teammates two different snapshot
// versions for identical data (§6).
func rankTeammates(seen map[int64]int) []int64 {
	if len(seen) == 0 {
		return nil
	}
	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		if seen[out[i]] != seen[out[j]] {
			return seen[out[i]] > seen[out[j]]
		}
		return out[i] < out[j]
	})
	return out
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
