package main

// The team sheet: who plays which position, for us and for the opponents. It
// lives next to the snapshot as team.json, outlives draft sessions (each new
// one gets it as seat pins) and travels to guests with the snapshot.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"cmnet/internal/session"
)

const teamFileName = "team.json"

func teamPath(h homeFlags) string {
	return filepath.Join(filepath.Dir(h.snapshotPath), teamFileName)
}

var teamMu sync.Mutex

func loadTeam(path string) (session.TeamSheet, error) {
	var t session.TeamSheet
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	err = json.Unmarshal(data, &t)
	return t, err
}

func teamOps(cfg *session.ShellConfig, h homeFlags) {
	path := teamPath(h)
	cfg.GetTeam = func() (session.TeamSheet, error) {
		teamMu.Lock()
		defer teamMu.Unlock()
		return loadTeam(path)
	}
	cfg.SetSeat = func(side string, position int, accountID int64) (session.TeamSheet, error) {
		teamMu.Lock()
		defer teamMu.Unlock()
		t, err := loadTeam(path)
		if err != nil {
			return t, err
		}
		if err := t.Seat(side, position, accountID); err != nil {
			return t, err
		}
		data, err := json.MarshalIndent(t, "", "  ")
		if err != nil {
			return t, err
		}
		return t, writeAtomic(path, data)
	}
}
