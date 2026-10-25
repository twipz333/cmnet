package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"cmnet/internal/dota/minimap"
	"cmnet/internal/dota/worldmap"
)

func newDotaMapInstallCmd() *cobra.Command {
	var (
		dotaPath      string
		cacheDir      string
		imagePath     string
		overviewPath  string
		source2Viewer string
		calibrateOnly bool
	)

	cmd := &cobra.Command{
		Use:   "dota-map-install",
		Short: "Extract current Dota client minimap into local cache for Behavior Audit UI",
		Long: "Writes minimap.png, overview.txt, and source.json under the cache dir " +
			"(default data/local/dota-map). Friends use the same cache for identical map backgrounds. " +
			"Does not redistribute Valve assets via git — keep data/local/ gitignored.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cacheDir == "" {
				cacheDir = minimap.DefaultCacheRel
			}
			if !filepath.IsAbs(cacheDir) {
				abs, err := filepath.Abs(cacheDir)
				if err != nil {
					return err
				}
				cacheDir = abs
			}

			if !calibrateOnly {
				switch {
				case imagePath != "" || overviewPath != "":
					if err := minimap.InstallFromFiles(imagePath, overviewPath, cacheDir); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "imported minimap cache -> %s\n", cacheDir)
				default:
					if err := minimap.InstallFromDota(dotaPath, cacheDir, source2Viewer); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "installed minimap cache -> %s\n", cacheDir)
				}
			}

			meta, _, tf, err := minimap.LoadCache(cacheDir)
			if err != nil {
				return fmt.Errorf("load cache after install: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "transform: pos_x=%.0f pos_y=%.0f scale=%g size=%dx%d geometry=%s build=%s\n",
				tf.PosX, tf.PosY, tf.Scale, tf.ImageWidth, tf.ImageHeight,
				meta.MapGeometryVersion, meta.ClientBuild)

			rep, err := minimap.Calibrate(tf)
			if err != nil {
				return err
			}
			if meta.MapGeometryVersion != "" && meta.MapGeometryVersion != worldmap.CurrentVersion {
				fmt.Fprintf(out, "warning: cache map_geometry_version=%s binary=%s\n",
					meta.MapGeometryVersion, worldmap.CurrentVersion)
			}
			b, err := json.MarshalIndent(rep, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "%s\n", b)
			fmt.Fprintf(out, "geometry verdict: %s - %s\n", rep.Verdict, rep.Notes)
			return nil
		},
	}

	cmd.Flags().StringVar(&dotaPath, "dota-path", "", "Dota 2 install root (auto-detect if empty)")
	cmd.Flags().StringVar(&cacheDir, "cache", minimap.DefaultCacheRel, "output cache directory")
	cmd.Flags().StringVar(&imagePath, "image", "", "manual import: path to minimap PNG")
	cmd.Flags().StringVar(&overviewPath, "overview", "", "manual import: path to overview.txt")
	cmd.Flags().StringVar(&source2Viewer, "source2viewer", "", "path to Source2Viewer-CLI (or env DOTA_SOURCE2VIEWER)")
	cmd.Flags().BoolVar(&calibrateOnly, "calibrate", false, "skip extract; print calibration anchors from existing cache")

	return cmd
}
