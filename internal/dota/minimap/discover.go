package minimap

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DiscoverDotaInstall locates a Dota 2 install that contains game/dota/pak01_dir.vpk.
// Preference: explicitPath, then common Steam roots + libraryfolders.vdf.
func DiscoverDotaInstall(explicitPath string) (string, error) {
	if explicitPath != "" {
		if ok, err := hasPak(explicitPath); ok {
			return filepath.Clean(explicitPath), nil
		} else if err != nil {
			return "", err
		}
		return "", fmt.Errorf("minimap: --dota-path %q has no game/dota/pak01_dir.vpk", explicitPath)
	}

	var tried []string
	for _, root := range steamRoots() {
		cand := filepath.Join(root, "steamapps", "common", "dota 2 beta")
		tried = append(tried, cand)
		if ok, _ := hasPak(cand); ok {
			return cand, nil
		}
		libs := filepath.Join(root, "steamapps", "libraryfolders.vdf")
		for _, lib := range parseLibraryFolders(libs) {
			cand = filepath.Join(lib, "steamapps", "common", "dota 2 beta")
			tried = append(tried, cand)
			if ok, _ := hasPak(cand); ok {
				return cand, nil
			}
		}
	}
	return "", fmt.Errorf("minimap: could not find Dota install (pak01_dir.vpk); tried %d candidates; pass --dota-path", len(tried))
}

func hasPak(dotaPath string) (bool, error) {
	pak := filepath.Join(dotaPath, "game", "dota", "pak01_dir.vpk")
	st, err := os.Stat(pak)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !st.IsDir(), nil
}

func steamRoots() []string {
	roots := []string{
		`C:\Program Files (x86)\Steam`,
		`C:\Program Files\Steam`,
		`D:\Steam`,
		`D:\SteamLibrary`,
		`E:\SteamLibrary`,
		`F:\SteamLibrary`,
		`F:\Steam`,
	}
	if runtime.GOOS == "windows" {
		if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
			roots = append(roots, filepath.Join(pf, "Steam"))
		}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			roots = append(roots, filepath.Join(pf, "Steam"))
		}
	}
	// Dedup while preserving order.
	seen := map[string]struct{}{}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		r = filepath.Clean(r)
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

// parseLibraryFolders extracts "path" values from Steam libraryfolders.vdf.
func parseLibraryFolders(vdfPath string) []string {
	f, err := os.Open(vdfPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		// "path"		"F:\\SteamLibrary"
		if !strings.Contains(strings.ToLower(line), `"path"`) {
			continue
		}
		parts := strings.Split(line, `"`)
		// ["", "path", "\t", "F:\\SteamLibrary", ...]
		vals := make([]string, 0, 2)
		for i, p := range parts {
			if i%2 == 1 {
				vals = append(vals, p)
			}
		}
		if len(vals) >= 2 && strings.EqualFold(vals[0], "path") {
			p := filepath.Clean(strings.ReplaceAll(vals[1], `\\`, `\`))
			out = append(out, p)
		}
	}
	return out
}
