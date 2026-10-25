package main

import (
	"context"
	"fmt"
	"sort"

	"cmnet/internal/declog"
	"cmnet/internal/roles"
	"cmnet/internal/scout"
)

// Wiring for the prospective decision log (§6.1): where it lives, what per-draft
// context it gets, and how erasing a player reaches it.
//
// The log is opened once per run and shared by every rebuild of the draft config
// (a mid-draft scout rebuilds the recommender), so one process keeps one file
// handle and one notion of "the draft currently in progress".

// openDecisionLog opens the log next to the cache. Failure is not fatal: the
// advisor's job is to advise, and a machine that cannot write the observational
// log can still do that. The caller reports the reason and carries on with nil,
// which the session treats as "logging off".
func openDecisionLog(dbPath string, enabled bool, retention declog.Retention) (*declog.Logger, error) {
	if !enabled {
		return nil, nil
	}
	l, err := declog.Open(declog.PathFor(dbPath), retention)
	if err != nil {
		return nil, fmt.Errorf("decision log: %w", err)
	}
	return l, nil
}

// logHeaderFrom builds the per-draft context: who is on the two sides, with the
// profiles as they stand right now.
//
// Profiles are copied in rather than referenced, because the question the log has
// to answer later is "given what we knew then, was the advice good" — and by the
// time anyone asks, the cached profiles will have moved on (§6.1).
func logHeaderFrom(ctx context.Context, dbPath string, snap *scout.OpponentSnapshot, prior *roles.Prior) declog.Header {
	h := declog.Header{}
	if snap == nil {
		return h
	}
	h.SnapshotHash = snap.SnapshotVersion

	var ids []int64
	collect := func(l2 *scout.Layer2) []declog.Player {
		if l2 == nil {
			return nil
		}
		out := make([]declog.Player, 0, len(l2.Players))
		for _, p := range l2.Players {
			if p.AccountID <= 0 {
				continue
			}
			ids = append(ids, p.AccountID)
			pl := declog.Player{AccountID: p.AccountID, Heroes: logHeroes(p.Heroes)}
			if prior != nil && len(pl.Heroes) > 0 {
				pl.Position = prior.Mode(pl.Heroes[0].HeroID)
			}
			out = append(out, pl)
		}
		return out
	}
	h.Allies = collect(snap.Allies)
	h.Enemies = collect(snap.Layer2)

	// Layer 3 too: it feeds the enemy prediction with a weight that scales with how
	// many drafts were seen, so a record without it cannot explain the advice it is
	// a record of.
	if l3 := snap.Layer3; l3 != nil {
		for _, td := range l3.TeamDrafts {
			h.EventDrafts = append(h.EventDrafts, declog.EventDraft{
				MatchID: td.MatchID,
				Radiant: td.Radiant,
				Picks:   append([]int(nil), td.Picks...),
				Bans:    append([]int(nil), td.Bans...),
			})
		}
	}

	// Names come from the persona cache, never from the snapshot: its "nickname" is
	// whatever the user typed to identify the player, usually a profile URL.
	if len(ids) > 0 {
		names := personaNames(ctx, dbPath, ids)
		for i := range h.Allies {
			h.Allies[i].Persona = names[h.Allies[i].AccountID]
		}
		for i := range h.Enemies {
			h.Enemies[i].Persona = names[h.Enemies[i].AccountID]
		}
	}
	return h
}

// logHeroes copies a player's whole profile, most-played first.
//
// Whole, not a top-N: the snapshot the recommender actually read is a file the
// user can overwrite on the next scout, so its content hash in the header proves
// equality only while that exact file survives. A truncated copy would leave the
// record unable to answer "what did we know then" the moment the snapshot moves —
// which is the one question this log exists to answer later.
func logHeroes(hs []scout.HeroAffinity) []declog.HeroStat {
	if len(hs) == 0 {
		return nil
	}
	sorted := append([]scout.HeroAffinity(nil), hs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Games != sorted[j].Games {
			return sorted[i].Games > sorted[j].Games
		}
		return sorted[i].HeroID < sorted[j].HeroID
	})
	out := make([]declog.HeroStat, 0, len(sorted))
	for _, h := range sorted {
		out = append(out, declog.HeroStat{HeroID: h.HeroID, Games: h.Games, WinRate: h.WinRate})
	}
	return out
}

// forgetInLog erases a player from the decision log, through the open logger when
// there is one (the file cannot be replaced while this process holds it open on
// Windows) and directly otherwise.
func forgetInLog(log *declog.Logger, dbPath string, accountID int64) (int, error) {
	if log != nil {
		return log.Forget(accountID, false)
	}
	return declog.ForgetIn(declog.PathFor(dbPath), accountID)
}
