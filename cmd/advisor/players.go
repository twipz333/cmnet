package main

import (
	"fmt"

	"cmnet/internal/cache"
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
func opKnownPlayers(dbPath, role string) ([]session.KnownPlayer, error) {
	store, err := cache.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	rows, err := store.KnownPlayers(cache.KnownRole(role))
	if err != nil {
		return nil, err
	}
	out := make([]session.KnownPlayer, 0, len(rows))
	for _, p := range rows {
		v := session.KnownPlayer{
			AccountID: p.AccountID,
			Persona:   p.Persona,
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
		out = append(out, v)
	}
	return out, nil
}

// opForgetPlayer drops an account and everything cached about it.
//
// Deliberately separate from removing them from the snapshot: that one leaves the
// cache warm so the next scout of the same opponent is fast, which is the whole
// point of keeping the rows. This is the path for actually forgetting someone.
func opForgetPlayer(dbPath string, accountID int64) (string, error) {
	store, err := cache.Open(dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	if err := store.ForgetPlayer(accountID); err != nil {
		return "", err
	}
	return fmt.Sprintf("account %d forgotten, cached stats dropped", accountID), nil
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
