// Package herogrid ingests a Dota 2 "hero_grid_config.json" exported by
// Dota2ProTracker (the per-match grid the user downloads into Dota) and turns its
// ranked Best with / Worst with / Best against / Worst against lists into a
// synergy/counter overlay that is baked into the bundle by `advisor grid-bake`.
//
// Why this exists: the matrices computed from the cache (internal/matrices) are
// statistically honest but read as implausible next to sites like D2PT, which
// filter matchups by position from a pro/high-MMR pool. The grid IS that pool, so
// for the ~35 heroes it covers we replace the computed cells with grid-derived
// lift; everything else keeps the computed values (graceful fallback).
//
// Determinism (§6): the grid is the user's local file and friends do not have it,
// so it is ingested at bake time and frozen into the shared bundle — never read at
// serve. Parsing and overlay construction are pure, deterministic functions: cells
// are accumulated in fixed file-traversal order (no map iteration feeds any float
// sum), so the same grid always yields byte-identical matrices.
package herogrid

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// File mirrors the on-disk hero_grid_config.json structure (only the fields we use).
type File struct {
	Configs []Config `json:"configs"`
}

// Config is one named grid (e.g. "Dota2ProTracker 7.41d - Carry").
type Config struct {
	Categories []Category `json:"categories"`
	ConfigName string     `json:"config_name"`
}

// Category is one labeled block of hero ids inside a config.
type Category struct {
	HeroIDs      []int  `json:"hero_ids"`
	CategoryName string `json:"category_name"`
}

// Indexer maps a Valve hero id to a dense index. *features.HeroMap satisfies it.
type Indexer interface {
	Index(id int) (int, bool)
	Len() int
}

// Params controls how a hero's rank within a 7-long list maps to lift magnitude.
// Values are in win-rate points around the 0.5 neutral center, matching the scale
// of internal/matrices lift, so the recommender's beam math is unaffected.
type Params struct {
	SynMax, SynMin float64 // teammate lift for the top vs bottom of a "with" list
	CntMax, CntMin float64 // matchup lift for the top vs bottom of an "against" list
}

// DefaultParams are realistic lift magnitudes (kept modest so structured grid
// signal does not dwarf the computed matrices on non-grid cells).
func DefaultParams() Params {
	return Params{SynMax: 0.08, SynMin: 0.03, CntMax: 0.10, CntMin: 0.03}
}

// Parse decodes a hero_grid_config.json document.
func Parse(data []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode hero grid: %w", err)
	}
	if len(f.Configs) == 0 {
		return nil, fmt.Errorf("hero grid has no configs")
	}
	return &f, nil
}

// Overlay is the parsed grid expressed as final synergy/counter cell values
// (already centered on 0.5 and clamped), keyed by dense hero-index pairs.
type Overlay struct {
	Synergy map[[2]int]float64
	Counter map[[2]int]float64
	Patch   string // patch token sniffed from a config name (e.g. "7.41d"), "" if none
	Heroes  int    // distinct grid "key" heroes that mapped into the index
	Skipped int    // grid hero ids absent from the bundle's hero map
}

// acc accumulates signed lift contributions for one cell in deterministic order.
type acc struct {
	sum float64
	n   int
}

var patchRe = regexp.MustCompile(`\b(\d+\.\d+[a-z]?)\b`)

// BuildOverlay walks the grid and produces synergy/counter overlays. Only configs
// that contain a "Top Heroes" category are processed (the per-role grids); the
// "All Roles" summary config has no matchup blocks and is skipped naturally.
func (f *File) BuildOverlay(hm Indexer, p Params) *Overlay {
	syn := map[[2]int]*acc{}
	cnt := map[[2]int]*acc{}
	seenKey := map[int]struct{}{}
	skipped := map[int]struct{}{}
	patch := ""

	idx := func(id int) (int, bool) {
		i, ok := hm.Index(id)
		if !ok {
			skipped[id] = struct{}{}
		}
		return i, ok
	}
	addSyn := func(a, b int, lift float64) {
		if a == b {
			return
		}
		for _, k := range [][2]int{{a, b}, {b, a}} { // symmetric
			e := syn[k]
			if e == nil {
				e = &acc{}
				syn[k] = e
			}
			e.sum += lift
			e.n++
		}
	}
	addCnt := func(a, b int, lift float64) {
		if a == b {
			return
		}
		k := [2]int{a, b}
		e := cnt[k]
		if e == nil {
			e = &acc{}
			cnt[k] = e
		}
		e.sum += lift
		e.n++
	}

	for _, cfg := range f.Configs {
		if !hasTopHeroes(cfg) {
			continue
		}
		if patch == "" {
			if m := patchRe.FindString(cfg.ConfigName); m != "" {
				patch = m
			}
		}
		// Find the ordered key-hero list and the matchup blocks (in array order).
		var top []int
		var blocks []Category
		for _, c := range cfg.Categories {
			switch {
			case isTopHeroes(c.CategoryName):
				top = c.HeroIDs
			case isMatchupName(c.CategoryName):
				blocks = append(blocks, c)
			}
		}
		// Blocks come in repeating rows; a "Best with" block marks the start of a new
		// key hero's row (it is always the first column of a row), so we advance the
		// key-hero cursor on it. This is robust to column ordering within a row.
		keyCursor := -1
		var keyIdx int
		var keyOK bool
		for _, c := range blocks {
			if c.CategoryName == "Best with" {
				keyCursor++
				keyOK = false
				if keyCursor < len(top) {
					keyIdx, keyOK = idx(top[keyCursor])
					if keyOK {
						seenKey[top[keyCursor]] = struct{}{}
					}
				}
			}
			if !keyOK {
				continue
			}
			n := len(c.HeroIDs)
			for rank, id := range c.HeroIDs {
				oi, ok := idx(id)
				if !ok {
					continue
				}
				switch c.CategoryName {
				case "Best with":
					addSyn(keyIdx, oi, +rankLift(rank, n, p.SynMax, p.SynMin))
				case "Worst with":
					addSyn(keyIdx, oi, -rankLift(rank, n, p.SynMax, p.SynMin))
				case "Best against": // key hero is strong vs oi
					l := rankLift(rank, n, p.CntMax, p.CntMin)
					addCnt(keyIdx, oi, +l)
					addCnt(oi, keyIdx, -l) // inversion: oi is weak vs key
				case "Worst against": // oi is strong vs key hero
					l := rankLift(rank, n, p.CntMax, p.CntMin)
					addCnt(oi, keyIdx, +l)
					addCnt(keyIdx, oi, -l) // inversion: key is weak vs oi
				}
			}
		}
	}

	return &Overlay{
		Synergy: finalize(syn),
		Counter: finalize(cnt),
		Patch:   patch,
		Heroes:  len(seenKey),
		Skipped: len(skipped),
	}
}

// finalize averages each cell's signed contributions and re-centers on 0.5.
func finalize(m map[[2]int]*acc) map[[2]int]float64 {
	out := make(map[[2]int]float64, len(m))
	for k, e := range m {
		if e.n == 0 {
			continue
		}
		out[k] = clamp01(0.5 + e.sum/float64(e.n))
	}
	return out
}

// ApplyTo overwrites the grid-covered cells in the given (dense-indexed) matrices,
// leaving all other cells — the non-grid matchups — at their computed values.
func (o *Overlay) ApplyTo(synergy, counter [][]float64) {
	for k, v := range o.Synergy {
		if inBounds(synergy, k) {
			synergy[k[0]][k[1]] = v
		}
	}
	for k, v := range o.Counter {
		if inBounds(counter, k) {
			counter[k[0]][k[1]] = v
		}
	}
}

func inBounds(g [][]float64, k [2]int) bool {
	return k[0] >= 0 && k[0] < len(g) && k[1] >= 0 && k[1] < len(g)
}

// rankLift maps a 0-based rank within an n-long list to a lift between max (rank 0)
// and min (last rank), linearly. A single-entry list gets max.
func rankLift(rank, n int, max, min float64) float64 {
	if n <= 1 {
		return max
	}
	t := float64(rank) / float64(n-1)
	return max + (min-max)*t
}

func hasTopHeroes(c Config) bool {
	for _, cat := range c.Categories {
		if isTopHeroes(cat.CategoryName) {
			return true
		}
	}
	return false
}

func isTopHeroes(name string) bool {
	return strings.HasPrefix(name, "Top Heroes")
}

func isMatchupName(name string) bool {
	switch name {
	case "Best with", "Worst with", "Best against", "Worst against":
		return true
	}
	return false
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
