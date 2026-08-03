package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"cmnet/internal/bundle"
	"cmnet/internal/herogrid"
	"cmnet/internal/objectives"
)

func newGridBakeCmd() *cobra.Command {
	var (
		bundlePath string
		outPath    string
		gridPath   string
	)
	cmd := &cobra.Command{
		Use:   "grid-bake",
		Short: "Bake a Dota2ProTracker hero_grid_config.json into an existing bundle's matrices",
		Long: "Reads the per-match hero grid you download into Dota (Best with / Worst with / " +
			"Best against / Worst against) and OVERWRITES the synergy/counter cells it covers in " +
			"an already-trained bundle, leaving non-grid matchups at their computed values. " +
			"Counter rules are mirrored (A strong vs B => B weak vs A). Re-hashes the bundle so " +
			"friends load the updated artifact. No retrain/sync needed.\n\n" +
			"The grid path is auto-detected under Steam userdata if --grid is omitted.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if gridPath == "" {
				found, err := detectHeroGrids()
				if err != nil {
					return err
				}
				switch len(found) {
				case 0:
					return fmt.Errorf("could not auto-detect hero_grid_config.json; pass --grid <path> " +
						"(usually <Steam>/userdata/<id>/570/remote/cfg/hero_grid_config.json)")
				case 1:
					gridPath = found[0]
					cmd.Println("using auto-detected grid:", gridPath)
				default:
					return fmt.Errorf("multiple hero grids found, pass --grid to choose one:\n  %s",
						strings.Join(found, "\n  "))
				}
			}
			if outPath == "" {
				outPath = bundlePath
			}
			summary, err := opGridBake(bundlePath, gridPath, outPath, func(s string) { cmd.Println(s) })
			if err != nil {
				return err
			}
			cmd.Println(summary)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&bundlePath, "bundle", "bundle.json", "input bundle to patch (must already contain matrices: train first)")
	f.StringVar(&outPath, "out", "", "output bundle path (default: overwrite --bundle)")
	f.StringVar(&gridPath, "grid", "", "path to hero_grid_config.json (auto-detected if empty)")
	return cmd
}

// opGridBake loads a bundle, overlays the hero grid onto its matrices, and re-saves.
func opGridBake(bundlePath, gridPath, outPath string, log func(string)) (string, error) {
	b, err := bundle.Load(bundlePath)
	if err != nil {
		return "", fmt.Errorf("load bundle: %w", err)
	}
	if len(b.SynergyMatrix) == 0 || len(b.CounterMatrix) == 0 {
		return "", fmt.Errorf("bundle %q has no synergy/counter matrices to overlay; run train first (M3)", bundlePath)
	}

	data, err := os.ReadFile(gridPath)
	if err != nil {
		return "", fmt.Errorf("read grid: %w", err)
	}
	gf, err := herogrid.Parse(data)
	if err != nil {
		return "", err
	}

	hm := b.HeroMap()
	overlay := gf.BuildOverlay(hm, herogrid.DefaultParams())
	log(fmt.Sprintf("grid: %d key heroes mapped, %d grid hero ids not in bundle (skipped); %d synergy + %d counter cells overlaid",
		overlay.Heroes, overlay.Skipped, len(overlay.Synergy), len(overlay.Counter)))

	overlay.ApplyTo(b.SynergyMatrix, b.CounterMatrix)

	if b.PatchTag == "" && overlay.Patch != "" {
		b.PatchTag = overlay.Patch
		log("patch tag set from grid: " + overlay.Patch)
	}

	// Backfill objective spawn timers (M6) so a bundle trained before M6 still gets
	// the live-game timer panel without a full retrain.
	if len(b.Objectives) == 0 {
		b.Objectives = objectives.DefaultObjectives()
		log(fmt.Sprintf("objective timers added: %d spawns", len(b.Objectives)))
	}

	if err := b.Save(outPath); err != nil {
		return "", fmt.Errorf("save bundle: %w", err)
	}
	return fmt.Sprintf("grid-bake done → %s (new bundle_version %s). Re-distribute this bundle to teammates.",
		outPath, b.BundleVersion), nil
}

// detectHeroGrids scans common Windows Steam locations for hero_grid_config.json.
// Steam keeps per-user config under <Steam>/userdata/<id>/570/remote/cfg, so the
// userdata id (which varies per account) is globbed.
func detectHeroGrids() ([]string, error) {
	roots := steamRoots()
	seen := map[string]struct{}{}
	var out []string
	for _, root := range roots {
		pattern := filepath.Join(root, "userdata", "*", "570", "remote", "cfg", "hero_grid_config.json")
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	return out, nil
}

// steamRoots returns candidate Steam install directories to probe.
func steamRoots() []string {
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			roots = append(roots, p)
		}
	}
	if v := os.Getenv("ProgramFiles(x86)"); v != "" {
		add(filepath.Join(v, "Steam"))
	}
	if v := os.Getenv("ProgramFiles"); v != "" {
		add(filepath.Join(v, "Steam"))
	}
	// Steam is commonly installed on a secondary drive; probe each drive root.
	for c := 'C'; c <= 'Z'; c++ {
		add(string(c) + `:\Steam`)
		add(string(c) + `:\SteamLibrary`)
	}
	return roots
}
