package main

// Player profiles: the current Steam name and avatar of the people on the team
// sheet. They change (people rename for a tournament, swap avatars), so they
// are re-checked rather than kept from the day the player was first scouted.

import (
	"context"
	"fmt"
	"os"
	"time"

	"cmnet/internal/assets"
	"cmnet/internal/cache"
	"cmnet/internal/session"
	"cmnet/internal/sources/opendota"
)

// profileFresh is how long a checked profile is not checked again unless asked.
const profileFresh = 30 * time.Minute

type profilesPayload struct {
	IDs   []int64 `json:"ids"`
	Force bool    `json:"force,omitempty"`
}

// opRefreshProfiles looks up name and avatar for ids, skipping the ones checked
// recently (unless force), and stores both: the name in the players table, the
// avatar under assets/avatars.
func opRefreshProfiles(ctx context.Context, dbPath string, ids []int64, force bool, log logFunc) (string, error) {
	if log == nil {
		log = discard
	}
	store, err := assets.New(assetsDirFor(dbPath))
	if err != nil {
		return "", err
	}
	cached := map[int64]cache.Player{}
	if st, err := cache.Open(dbPath); err == nil {
		cached, _ = st.Players(ids)
		st.Close()
	}
	fresh := time.Now().Add(-profileFresh).Unix()
	cl := opendota.New()
	var rows []cache.Player
	checked, renamed := 0, 0
	for _, id := range uniqueIDs(ids) {
		if ctx.Err() != nil {
			break
		}
		prev, had := cached[id]
		if !force && had && prev.FetchedAt >= fresh && store.Has(assets.KindAvatar, assets.AvatarFile(id)) {
			continue
		}
		p, err := cl.Profile(ctx, id)
		if err != nil {
			log(fmt.Sprintf("profile %d: %v", id, err))
			continue
		}
		checked++
		if had && prev.Persona != "" && p.Persona != "" && prev.Persona != p.Persona {
			renamed++
			log(fmt.Sprintf("%d renamed: %s → %s", id, prev.Persona, p.Persona))
		}
		rows = append(rows, cache.Player{AccountID: id, Persona: p.Persona})
		if err := store.RefreshAvatar(ctx, id, p.Avatar); err != nil {
			log(fmt.Sprintf("avatar %d: %v", id, err))
		}
	}
	if len(rows) > 0 {
		if st, err := cache.Open(dbPath); err == nil {
			_ = st.UpsertPlayers(rows)
			st.Close()
		}
	}
	msg := fmt.Sprintf("профили: проверено %d", checked)
	if renamed > 0 {
		msg += fmt.Sprintf(", сменили ник %d", renamed)
	}
	return msg, nil
}

func uniqueIDs(ids []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// opProfiles reads what is known about ids right now, for the page.
func opProfiles(dbPath string, ids []int64) (map[int64]session.Profile, error) {
	out := make(map[int64]session.Profile, len(ids))
	st, err := cache.Open(dbPath)
	if err != nil {
		return out, err
	}
	rows, err := st.Players(ids)
	st.Close()
	if err != nil {
		return out, err
	}
	dir := assetsDirFor(dbPath)
	for _, id := range uniqueIDs(ids) {
		p := session.Profile{}
		if r, ok := rows[id]; ok {
			p.Persona, p.FetchedAt = r.Persona, r.FetchedAt
		}
		if fi, err := os.Stat(dir + "/" + string(assets.KindAvatar) + "/" + assets.AvatarFile(id)); err == nil && fi.Size() > 0 {
			p.Avatar = fi.ModTime().Unix()
		}
		out[id] = p
	}
	return out, nil
}
