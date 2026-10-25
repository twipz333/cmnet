package minimap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cmnet/internal/dota/worldmap"
)

const envSource2Viewer = "DOTA_SOURCE2VIEWER"

// InstallFromDota extracts overview + minimap texture from a local Dota install into cacheDir.
func InstallFromDota(dotaPath, cacheDir, source2ViewerCLI string) error {
	if dotaPath == "" {
		var err error
		dotaPath, err = DiscoverDotaInstall("")
		if err != nil {
			return err
		}
	}
	ok, err := hasPak(dotaPath)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("minimap: no pak01_dir.vpk under %s", dotaPath)
	}
	cli, err := findSource2Viewer(source2ViewerCLI)
	if err != nil {
		return err
	}
	pak := filepath.Join(dotaPath, "game", "dota", "pak01_dir.vpk")

	tmp, err := os.MkdirTemp("", "cmnet-dota-map-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := runViewer(cli, pak, tmp, false, "resource/overviews/dota.txt"); err != nil {
		return fmt.Errorf("minimap: extract overview: %w", err)
	}
	if err := runViewer(cli, pak, tmp, true, "materials/overviews/dota.vmat_c"); err != nil {
		// Fallback: path filter without _c (CLI may match stem).
		if err2 := runViewer(cli, pak, tmp, true, "materials/overviews/dota.vmat"); err2 != nil {
			return fmt.Errorf("minimap: extract overview material: %v / %w", err, err2)
		}
	}

	overviewPath := filepath.Join(tmp, "resource", "overviews", "dota.txt")
	overviewBytes, err := os.ReadFile(overviewPath)
	if err != nil {
		return fmt.Errorf("minimap: read extracted overview: %w", err)
	}
	pngPath, assetRel, err := pickOverviewPNG(tmp)
	if err != nil {
		return err
	}
	pngBytes, err := os.ReadFile(pngPath)
	if err != nil {
		return err
	}

	meta := SourceMeta{
		Source:             "local-dota-install",
		DotaPath:           dotaPath,
		ClientBuild:        readClientBuild(dotaPath),
		ExtractedAt:        time.Now().UTC().Format(time.RFC3339),
		OverviewPath:       "resource/overviews/dota.txt",
		AssetPath:          assetRel,
		MapName:            "dota",
		MapGeometryVersion: worldmap.CurrentVersion,
	}
	return SaveCache(cacheDir, meta, pngBytes, string(overviewBytes))
}

// InstallFromFiles copies a manual image + overview into cacheDir.
func InstallFromFiles(imagePath, overviewPath, cacheDir string) error {
	if imagePath == "" || overviewPath == "" {
		return fmt.Errorf("minimap: --image and --overview are required for manual import")
	}
	pngBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return err
	}
	overviewBytes, err := os.ReadFile(overviewPath)
	if err != nil {
		return err
	}
	meta := SourceMeta{
		Source:             "manual-import",
		ExtractedAt:        time.Now().UTC().Format(time.RFC3339),
		OverviewPath:       overviewPath,
		AssetPath:          imagePath,
		MapName:            "dota",
		MapGeometryVersion: worldmap.CurrentVersion,
	}
	return SaveCache(cacheDir, meta, pngBytes, string(overviewBytes))
}

func findSource2Viewer(explicit string) (string, error) {
	candidates := []string{}
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if v := os.Getenv(envSource2Viewer); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates,
		filepath.Join("tools", "Source2Viewer", "Source2Viewer-CLI.exe"),
		"Source2Viewer-CLI.exe",
		"Source2Viewer-CLI",
	)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if path, err := exec.LookPath(c); err == nil {
			return path, nil
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs, nil
		}
	}
	return "", fmt.Errorf("minimap: Source2Viewer-CLI not found (set %s or --source2viewer)", envSource2Viewer)
}

func runViewer(cli, pak, outDir string, decompile bool, filter string) error {
	args := []string{"-i", pak, "-o", outDir, "-f", filter}
	if decompile {
		args = append(args, "-d")
	}
	cmd := exec.Command(cli, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", cli, args, err)
	}
	return nil
}

func pickOverviewPNG(extractRoot string) (absPath, relAsset string, err error) {
	overviews := filepath.Join(extractRoot, "materials", "overviews")
	preferred := []string{
		filepath.Join(overviews, "dota.png"),
		filepath.Join(overviews, "dota_psd.png"),
	}
	for _, p := range preferred {
		if st, e := os.Stat(p); e == nil && !st.IsDir() && st.Size() > 0 {
			rel, _ := filepath.Rel(extractRoot, p)
			return p, filepath.ToSlash(rel), nil
		}
	}
	entries, err := os.ReadDir(overviews)
	if err != nil {
		return "", "", fmt.Errorf("minimap: no overview png under %s: %w", overviews, err)
	}
	var best string
	var bestSize int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
			continue
		}
		// Prefer current-map names; skip seasonal/minimal when possible.
		name := strings.ToLower(e.Name())
		if strings.Contains(name, "minimal") || strings.Contains(name, "autumn") ||
			strings.Contains(name, "desert") || strings.Contains(name, "diretide") ||
			strings.Contains(name, "glade") {
			continue
		}
		info, _ := e.Info()
		if info == nil {
			continue
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			best = filepath.Join(overviews, e.Name())
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("minimap: no suitable overview png in %s", overviews)
	}
	rel, _ := filepath.Rel(extractRoot, best)
	return best, filepath.ToSlash(rel), nil
}

func readClientBuild(dotaPath string) string {
	// Prefer Steam library buildid (matches prior source.json extracts).
	steamapps := filepath.Clean(filepath.Join(dotaPath, "..", ".."))
	acf := filepath.Join(steamapps, "appmanifest_570.acf")
	if b, err := os.ReadFile(acf); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if !strings.Contains(line, `"buildid"`) {
				continue
			}
			parts := strings.Split(line, `"`)
			vals := make([]string, 0, 2)
			for i, p := range parts {
				if i%2 == 1 {
					vals = append(vals, p)
				}
			}
			if len(vals) >= 2 && vals[0] == "buildid" {
				return vals[1]
			}
		}
	}
	inf := filepath.Join(dotaPath, "game", "dota", "steam.inf")
	b, err := os.ReadFile(inf)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ClientVersion=") {
			return strings.TrimPrefix(line, "ClientVersion=")
		}
	}
	return ""
}
