package main

// Host/guest sharing: the host hands its model and scouting to friends with a
// code, and a guest build runs on what it was given instead of collecting and
// training its own (see internal/share).

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"cmnet/internal/cache"
	"cmnet/internal/scout"
	"cmnet/internal/session"
	"cmnet/internal/share"
	"cmnet/internal/tui"
)

// edition is set at link time: -ldflags "-X main.edition=guest" builds the guest
// app. The --guest flag does the same for a development run.
var edition = ""

const heroesFile = "heroes.json"

// maxSharedAssets caps the portraits sent along: they are nice to have, and a
// guest on a slow line should not wait for them.
const maxSharedAssets = 60 << 20

// applyGuest strips everything that collects data or builds a model. What is
// left is the draft screen, timers and settings, running on what the host sent.
func applyGuest(cfg *tui.ShellConfig, h *homeFlags) {
	cfg.Guest = true
	cfg.Sync = nil
	cfg.Train = nil
	cfg.Scout = nil
	cfg.ScoutAllies = nil
	cfg.AddPlayer = nil
	cfg.RefreshProfiles = nil
	cfg.Corpus = nil
	cfg.CorpusMeta = nil
	cfg.SetHeroPool = nil
	cfg.SetItemRules = nil
	cfg.RemovePlayer = nil
	cfg.ClearLayer3 = nil
	cfg.ClearScout = nil
	h.auto.enabled = false
}

// sharePayload collects what a guest needs from the host's data folder.
func sharePayload(h homeFlags) func() (share.Payload, error) {
	return func() (share.Payload, error) {
		bundleData, err := os.ReadFile(h.bundlePath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return share.Payload{}, errors.New("модель ещё не обучена: делиться нечем")
			}
			return share.Payload{}, err
		}
		var head struct {
			BundleVersion string `json:"bundle_version"`
			PatchTag      string `json:"patch_tag"`
		}
		_ = json.Unmarshal(bundleData, &head)
		p := share.Payload{Meta: share.Meta{BundleVersion: head.BundleVersion, PatchTag: head.PatchTag, HostVersion: version}}
		p.Files = append(p.Files, share.File{Name: "bundle.json", Data: bundleData})

		if snapData, err := os.ReadFile(h.snapshotPath); err == nil {
			p.Files = append(p.Files, share.File{Name: "snapshot.json", Data: snapData})
			p.Meta.HasSnapshot = true
			if snap, err := scout.LoadSnapshot(h.snapshotPath); err == nil && snap.Layer2 != nil {
				p.Meta.Scouted = len(snap.Layer2.Players)
			}
		}

		if team, err := os.ReadFile(teamPath(h)); err == nil {
			p.Files = append(p.Files, share.File{Name: teamFileName, Data: team})
		}

		// Hero names live in the host's database; the guest has none.
		if store, err := cache.Open(h.dbPath); err == nil {
			hs, herr := store.Heroes()
			store.Close()
			if herr == nil && len(hs) > 0 {
				if data, err := json.Marshal(hs); err == nil {
					p.Files = append(p.Files, share.File{Name: heroesFile, Data: data})
				}
			}
		}

		// Portraits, so the guest's draft screen looks like the host's.
		dir := assetsDirFor(h.dbPath)
		total := 0
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || total > maxSharedAssets {
				return nil
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			total += len(data)
			p.Files = append(p.Files, share.File{Name: "assets/" + filepath.ToSlash(rel), Data: data})
			return nil
		})
		return p, nil
	}
}

// applyShared writes a received payload into the guest's data folder.
func applyShared(h homeFlags) func(share.Payload) (string, error) {
	return func(p share.Payload) (string, error) {
		bundleData := p.File("bundle.json")
		if len(bundleData) == 0 {
			return "", errors.New("хост не прислал модель")
		}
		if err := writeAtomic(h.bundlePath, bundleData); err != nil {
			return "", err
		}
		if snap := p.File("snapshot.json"); len(snap) > 0 {
			if err := writeAtomic(h.snapshotPath, snap); err != nil {
				return "", err
			}
		} else if err := os.Remove(h.snapshotPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err // the host has no scouting; old scouting would mislead
		}
		if team := p.File(teamFileName); len(team) > 0 {
			if err := writeAtomic(teamPath(h), team); err != nil {
				return "", err
			}
		}
		if data := p.File(heroesFile); len(data) > 0 {
			var hs []cache.Hero
			if err := json.Unmarshal(data, &hs); err == nil && len(hs) > 0 {
				store, err := cache.Open(h.dbPath)
				if err != nil {
					return "", fmt.Errorf("база героев: %w", err)
				}
				err = store.UpsertHeroes(hs)
				store.Close()
				if err != nil {
					return "", fmt.Errorf("база героев: %w", err)
				}
			}
		}
		dir := assetsDirFor(h.dbPath)
		for _, f := range p.Files {
			rel, ok := strings.CutPrefix(f.Name, "assets/")
			if !ok {
				continue
			}
			if name, ok := share.SafeName(rel); ok {
				_ = writeAtomic(filepath.Join(dir, filepath.FromSlash(name)), f.Data)
			}
		}
		msg := "получено от хоста: модель"
		if p.Meta.PatchTag != "" {
			msg += " патча " + p.Meta.PatchTag
		}
		if p.Meta.Scouted > 0 {
			msg += fmt.Sprintf(", разведка %d соперников", p.Meta.Scouted)
		} else if p.Meta.HasSnapshot {
			msg += " и разведка"
		}
		return msg, nil
	}
}

func writeAtomic(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// shareShell wires sharing into the shell: hosts can share, guests receive.
func shareShell(cfg *session.ShellConfig, h homeFlags) {
	teamOps(cfg, h)
	if cfg.Guest {
		cfg.ApplyShare = applyShared(h)
		return
	}
	cfg.SharePayload = sharePayload(h)
}
