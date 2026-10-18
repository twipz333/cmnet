package minimap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultCacheRel is the default minimap cache path relative to the repo/cwd.
const DefaultCacheRel = "data/local/dota-map"

const (
	fileMinimapPNG  = "minimap.png"
	fileOverviewTXT = "overview.txt"
	fileSourceJSON  = "source.json"
)

// SourceMeta describes how the local minimap cache was produced.
type SourceMeta struct {
	Source             string  `json:"source"`
	DotaPath           string  `json:"dota_path,omitempty"`
	ClientBuild        string  `json:"client_build,omitempty"`
	ExtractedAt        string  `json:"extracted_at,omitempty"`
	OverviewPath       string  `json:"overview_path,omitempty"`
	AssetPath          string  `json:"asset_path,omitempty"`
	MapName            string  `json:"map_name,omitempty"`
	MapGeometryVersion string  `json:"map_geometry_version,omitempty"`
	ImageWidth         int     `json:"image_width"`
	ImageHeight        int     `json:"image_height"`
	PosX               float64 `json:"pos_x"`
	PosY               float64 `json:"pos_y"`
	Scale              float64 `json:"scale"`
}

// CacheDir joins repoRoot with DefaultCacheRel.
func CacheDir(repoRoot string) string {
	if repoRoot == "" {
		return DefaultCacheRel
	}
	return filepath.Join(repoRoot, DefaultCacheRel)
}

// LoadCache reads source.json + overview + image size from cacheDir.
func LoadCache(cacheDir string) (meta SourceMeta, pngPath string, tf MinimapTransform, err error) {
	pngPath = filepath.Join(cacheDir, fileMinimapPNG)
	metaPath := filepath.Join(cacheDir, fileSourceJSON)
	overviewPath := filepath.Join(cacheDir, fileOverviewTXT)

	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return meta, "", tf, fmt.Errorf("minimap: read %s: %w", metaPath, err)
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return meta, "", tf, fmt.Errorf("minimap: parse source.json: %w", err)
	}

	if f, oerr := os.Open(overviewPath); oerr == nil {
		posX, posY, scale, perr := ParseOverviewTXT(f)
		_ = f.Close()
		if perr == nil {
			meta.PosX, meta.PosY, meta.Scale = posX, posY, scale
		}
	}

	w, h := meta.ImageWidth, meta.ImageHeight
	if w <= 0 || h <= 0 {
		if iw, ih, ierr := pngSize(pngPath); ierr == nil {
			w, h = iw, ih
			meta.ImageWidth, meta.ImageHeight = w, h
		}
	}
	if _, err := os.Stat(pngPath); err != nil {
		return meta, "", tf, fmt.Errorf("minimap: missing %s: %w", pngPath, err)
	}
	if meta.Scale == 0 {
		return meta, "", tf, fmt.Errorf("minimap: scale is zero in cache")
	}
	tf = MinimapTransform{
		PosX: meta.PosX, PosY: meta.PosY, Scale: meta.Scale,
		ImageWidth: w, ImageHeight: h,
	}
	return meta, pngPath, tf, nil
}

// SaveCache writes minimap.png, overview.txt, and source.json into cacheDir.
func SaveCache(cacheDir string, meta SourceMeta, pngBytes []byte, overviewText string) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("minimap: mkdir cache: %w", err)
	}
	if meta.ExtractedAt == "" {
		meta.ExtractedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if len(pngBytes) > 0 {
		if err := os.WriteFile(filepath.Join(cacheDir, fileMinimapPNG), pngBytes, 0o644); err != nil {
			return err
		}
		if w, h, err := pngSizeBytes(pngBytes); err == nil {
			meta.ImageWidth, meta.ImageHeight = w, h
		}
	}
	if overviewText != "" {
		if err := os.WriteFile(filepath.Join(cacheDir, fileOverviewTXT), []byte(overviewText), 0o644); err != nil {
			return err
		}
		posX, posY, scale, err := ParseOverviewTXT(strings.NewReader(overviewText))
		if err == nil {
			meta.PosX, meta.PosY, meta.Scale = posX, posY, scale
		}
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cacheDir, fileSourceJSON), append(b, '\n'), 0o644)
}

func pngSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func pngSizeBytes(b []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}
