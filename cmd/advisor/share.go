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
	"time"

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

// sharePayload collects what a guest/friend needs from the host's data folder.
// Bundle and/or scout snapshot are enough for a portable local package.
func sharePayload(h homeFlags) func() (share.Payload, error) {
	return func() (share.Payload, error) {
		bundleData, bundleErr := os.ReadFile(h.bundlePath)
		if bundleErr != nil && !errors.Is(bundleErr, fs.ErrNotExist) {
			return share.Payload{}, bundleErr
		}
		snapData, snapErr := os.ReadFile(h.snapshotPath)
		if snapErr != nil && !errors.Is(snapErr, fs.ErrNotExist) {
			return share.Payload{}, snapErr
		}
		if len(bundleData) == 0 && len(snapData) == 0 {
			return share.Payload{}, errors.New("нечем делиться: нет модели (bundle.json) и нет разведки (snapshot.json)")
		}

		p := share.Payload{Meta: share.Meta{HostVersion: version}}
		if len(bundleData) > 0 {
			var head struct {
				BundleVersion string `json:"bundle_version"`
				PatchTag      string `json:"patch_tag"`
			}
			_ = json.Unmarshal(bundleData, &head)
			p.Meta.BundleVersion = head.BundleVersion
			p.Meta.PatchTag = head.PatchTag
			p.Files = append(p.Files, share.File{Name: "bundle.json", Data: bundleData})
		}

		if len(snapData) > 0 {
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

// applyShared writes a received/imported payload into the local data folder.
// Bundle-only, scout-only, or both are accepted; never silently wipes the other
// side when the package intentionally omits it (scout-only keeps local model).
// Existing targets are copied to import-backup/<stamp>/ before overwrite.
func applyShared(h homeFlags) func(share.Payload) (string, error) {
	return func(p share.Payload) (string, error) {
		backupNote, err := backupBeforeImport(h)
		if err != nil {
			return "", err
		}
		bundleData := p.File("bundle.json")
		snap := p.File("snapshot.json")
		if len(bundleData) == 0 && len(snap) == 0 {
			return "", errors.New("в пакете нет ни модели, ни разведки")
		}
		if len(bundleData) > 0 {
			if err := writeAtomic(h.bundlePath, bundleData); err != nil {
				return "", err
			}
		}
		if len(snap) > 0 {
			if err := writeAtomic(h.snapshotPath, snap); err != nil {
				return "", err
			}
		} else if len(bundleData) > 0 {
			// Full model package without scouting: clear stale scout so it cannot mislead.
			if err := os.Remove(h.snapshotPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}
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
		parts := make([]string, 0, 3)
		if len(bundleData) > 0 {
			part := "модель"
			if p.Meta.PatchTag != "" {
				part += " патча " + p.Meta.PatchTag
			}
			parts = append(parts, part)
		}
		if p.Meta.Scouted > 0 {
			parts = append(parts, fmt.Sprintf("разведка %d соперников", p.Meta.Scouted))
		} else if len(snap) > 0 || p.Meta.HasSnapshot {
			parts = append(parts, "разведка")
		}
		msg := "импортировано: " + strings.Join(parts, ", ")
		if backupNote != "" {
			msg += ". " + backupNote
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

// backupBeforeImport copies existing share targets into import-backup/<stamp>/
// so an intentional import never silently destroys prior local evidence.
func backupBeforeImport(h homeFlags) (string, error) {
	candidates := []string{h.bundlePath, h.snapshotPath, teamPath(h)}
	var existing []string
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			existing = append(existing, p)
		}
	}
	if len(existing) == 0 {
		return "", nil
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	destRoot := filepath.Join(filepath.Dir(h.bundlePath), "import-backup", stamp)
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return "", fmt.Errorf("резервная копия перед импортом: %w", err)
	}
	for _, src := range existing {
		dst := filepath.Join(destRoot, filepath.Base(src))
		data, err := os.ReadFile(src)
		if err != nil {
			return "", fmt.Errorf("резервная копия %s: %w", filepath.Base(src), err)
		}
		if err := writeAtomic(dst, data); err != nil {
			return "", fmt.Errorf("резервная копия %s: %w", filepath.Base(src), err)
		}
	}
	return "резервная копия: " + destRoot, nil
}

// shareShell wires sharing into the shell. Hosts export payloads; everyone can
// import a local package (ApplyShare). Guests remain receive-capable for legacy
// relay builds outside CMNET_LOCAL_ONLY.
func shareShell(cfg *session.ShellConfig, h homeFlags) {
	teamOps(cfg, h)
	cfg.ApplyShare = applyShared(h)
	if cfg.Guest {
		return
	}
	cfg.SharePayload = sharePayload(h)
}
