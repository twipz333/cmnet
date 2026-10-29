package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cmnet/internal/assets"
	"cmnet/internal/cache"
	"cmnet/internal/declog"
	"cmnet/internal/scout"
	"cmnet/internal/session"
)

// scoutWindowDays is the window the scout collects over, and therefore the window
// the cached per-player rows are keyed by. A row collected over a different window
// answers a different question, so the two must not be mixed (see
// cache.PlayerHeroStats).
const scoutWindowDays = 30

// opKnownPlayers lists the accounts this machine has seen.
//
// The list is the point of the feature: picking a teammate from it beats pasting
// five profile URLs before every Battle Cup, and it doubles as the record of which
// opponents are still cheap to re-scout.
func opKnownPlayers(dbPath, snapshotPath, role string) ([]session.KnownPlayer, error) {
	store, err := cache.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// Anyone already in the snapshot belongs in the book, even if they were
	// scouted before the book existed. Backfilled on read rather than by a
	// migration: the snapshot is a file the user can swap or hand over, so the
	// two can always drift, and reconciling them here is the only place that
	// stays correct afterwards.
	inSnap := snapshotMembers(snapshotPath)
	if len(inSnap) > 0 {
		// Names come from the persona cache, never from the snapshot. The snapshot's
		// "nickname" is whatever the user typed to identify the player — usually a
		// profile URL — so copying it into the book replaces real names with links.
		ids := make([]int64, 0, len(inSnap))
		for id := range inSnap {
			ids = append(ids, id)
		}
		known, _ := store.Players(ids)
		for id, member := range inSnap {
			_ = store.RememberPlayer(cache.KnownPlayer{
				AccountID: id,
				Persona:   known[id].Persona,
				Role:      member.role,
				LastSeen:  member.lastSeen,
			})
		}
	}

	rows, err := store.KnownPlayers(cache.KnownRole(role))
	if err != nil {
		return nil, err
	}

	// Use the profile cache as the source of truth for current Steam names;
	// known_players.persona is only a fallback for older/offline databases.
	ids := make([]int64, 0, len(rows))
	for _, p := range rows {
		ids = append(ids, p.AccountID)
	}
	profiles, _ := store.Players(ids)
	out := make([]session.KnownPlayer, 0, len(rows))
	for _, p := range rows {
		avatarVersion := int64(0)
		if profile, ok := profiles[p.AccountID]; ok {
			if profile.Persona != "" { p.Persona = profile.Persona }
			if profile.AvatarURL != "" { avatarVersion = profile.FetchedAt }
		}
		v := session.KnownPlayer{
			AccountID: p.AccountID,
			Persona:   p.Persona,
			AvatarVersion: avatarVersion,
			Role:      string(p.Role),
			Note:      p.Note,
			LastSeen:  p.LastSeen,
			Pinned:    p.Pinned,
		}
		// "Cached" is shown as a count rather than a claim: the user can see that a
		// repeat scout of this opponent has something to start from.
		if heroes, err := store.PlayerHeroStats(p.AccountID, scoutWindowDays); err == nil {
			v.Heroes = len(heroes)
		}
		if m, ok := inSnap[p.AccountID]; ok {
			v.InSnapshot = string(m.role)
		}
		out = append(out, v)
	}
	return out, nil
}

// opForgetPlayer drops an account and everything cached about it.
//
// Deliberately separate from removing them from the snapshot: that one leaves the
// cache warm so the next scout of the same opponent is fast, which is the whole
// point of keeping the rows. This is the path for actually forgetting someone.
func opForgetPlayer(dbPath string, accountID int64, log *declog.Logger) (string, error) {
	store, err := cache.Open(dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	if err := store.ForgetPlayer(accountID); err != nil {
		return "", err
	}
	// The avatar is a local file independent of the database: explicitly remove
	// it as part of "Forget" rather than leaving identifiable images on disk.
	_ = os.Remove(filepathJoinAvatar(dbPath, accountID))
	// The decision log is the other place their identity ended up (§6.1): headers
	// carry the ten account ids and the profiles as of each draft. Forgetting has
	// to mean everywhere, or it means nothing.
	drafts, lerr := forgetInLog(log, dbPath, accountID)
	msg := fmt.Sprintf("account %d forgotten, cached stats dropped", accountID)
	if lerr != nil {
		return msg, fmt.Errorf("decision log still holds this player: %w", lerr)
	}
	if drafts > 0 {
		msg += fmt.Sprintf("; redacted from %d logged drafts", drafts)
	}
	return msg, nil
}

// opPinPlayer keeps an account out of the automatic pruning.
func opPinPlayer(dbPath string, accountID int64, pinned bool) (string, error) {
	store, err := cache.Open(dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	if err := store.SetPlayerPinned(accountID, pinned); err != nil {
		return "", err
	}
	if pinned {
		return fmt.Sprintf("account %d pinned", accountID), nil
	}
	return fmt.Sprintf("account %d unpinned", accountID), nil
}

// snapMember is one player found in the snapshot.
//
// No name: the snapshot stores whatever the user typed to identify the player,
// which is usually a profile URL. Names belong to the persona cache.
type snapMember struct {
	role     cache.KnownRole
	lastSeen int64
}

// snapshotMembers reads who is currently in the lineup.
//
// Best-effort: a missing or unreadable snapshot simply means "nobody is in the
// lineup", which is true, and is not worth failing the screen over.
func snapshotMembers(snapshotPath string) map[int64]snapMember {
	out := map[int64]snapMember{}
	snap, err := scout.LoadSnapshot(snapshotPath)
	if err != nil || snap == nil {
		return out
	}
	now := time.Now().Unix()
	add := func(players []scout.PlayerAffinity, role cache.KnownRole) {
		for _, p := range players {
			if p.AccountID <= 0 {
				continue
			}
			out[p.AccountID] = snapMember{role: role, lastSeen: now}
		}
	}
	if snap.Allies != nil {
		add(snap.Allies.Players, cache.RoleAlly)
	}
	if snap.Layer2 != nil {
		add(snap.Layer2.Players, cache.RoleEnemy)
	}
	return out
}

// opAddPlayer files an account in the book without scouting it.
//
// Scouting costs API calls and takes time; assembling tonight's stack should not.
// The nickname is looked up when it is cheap and left blank otherwise — a row
// with an id is still a row the user can select.
func opAddPlayer(ctx context.Context, dbPath, ref string, ally bool) (string, error) {
	id, err := scout.ParseAccountID(ref)
	if err != nil {
		return "", fmt.Errorf("%q: %w", ref, err)
	}
	store, err := cache.Open(dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()

	role := cache.RoleEnemy
	if ally {
		role = cache.RoleAlly
	}
	persona := personaNames(ctx, dbPath, []int64{id})[id]
	if err := store.RememberPlayer(cache.KnownPlayer{
		AccountID: id, Persona: persona, Role: role,
	}); err != nil {
		return "", err
	}
	if persona != "" {
		return fmt.Sprintf("%s (%d) добавлен в список", persona, id), nil
	}
	return fmt.Sprintf("%d добавлен в список", id), nil
}

// cachePlayerAffinity stores what a scout just learned about each player.
//
// This is the half of the enemy cache that was missing: the snapshot got the
// numbers and the cache got nothing, so "how much do we already know about this
// opponent" had no answer and a re-scout of the same team started from zero.
//
// Rows are keyed by the window they were collected over, so a 30-day row is never
// served as an answer about a different one. Best-effort: failing to cache must
// not fail the scout the user is waiting on.
func cachePlayerAffinity(dbPath string, players []scout.PlayerAffinity, windowDays int) {
	if len(players) == 0 || windowDays <= 0 {
		return
	}
	store, err := cache.Open(dbPath)
	if err != nil {
		return
	}
	defer store.Close()

	for _, p := range players {
		if p.AccountID <= 0 || len(p.Heroes) == 0 {
			continue
		}
		rows := make([]cache.PlayerHeroRow, 0, len(p.Heroes))
		for _, h := range p.Heroes {
			rows = append(rows, cache.PlayerHeroRow{
				AccountID: p.AccountID,
				HeroID:    h.HeroID,
				Matches:   h.Games,
				// The source reports a rate, not a count; storing the count it implies
				// keeps the row in the same shape as one built from our own matches.
				Wins: int(float64(h.Games)*h.WinRate + 0.5),
			})
		}
		_ = store.ReplacePlayerHeroStats(p.AccountID, windowDays, rows)
	}
}

// opRefreshProfiles explicitly rechecks profile presentation data. No scouting
// statistics are changed, and failures do not erase working cached metadata.
func opRefreshProfiles(ctx context.Context, dbPath string, ids []int64) (string, error) {
	if len(ids) == 0 || len(ids) > 10 {
		return "", fmt.Errorf("choose 1 to 10 player accounts to refresh")
	}
	for _, id := range ids {
		if id <= 0 || id >= 1<<32 {
			return "", fmt.Errorf("invalid account id: %d", id)
		}
	}
	_, updated := refreshProfiles(ctx, dbPath, ids, true)
	if updated == 0 {
		return "", fmt.Errorf("could not refresh profiles: OpenDota unavailable or timed out")
	}
	return fmt.Sprintf("Steam profiles checked: %d of %d; names and avatars updated when available", updated, len(ids)), nil
}

// filepathJoinAvatar keeps per-account images in the same local data directory.
func filepathJoinAvatar(dbPath string, id int64) string {
	return filepath.Join(assetsDirFor(dbPath), string(assets.KindAvatar), assets.AvatarFile(id))
}
