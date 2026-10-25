// Package features builds model input vectors from draft data and owns the hero
// index map. The map (hero id -> dense index) is stored in the bundle, never
// hardcoded, because the hero roster changes between patches (§6).
package features

import "sort"

// HeroMap maps non-contiguous Valve hero ids to dense model indices. IDs are
// always sorted ascending so the mapping is deterministic across machines.
type HeroMap struct {
	ids   []int       // dense index -> hero id, sorted ascending
	index map[int]int // hero id -> dense index
}

// NewHeroMap builds a HeroMap from hero ids (deduplicated and sorted).
func NewHeroMap(ids []int) *HeroMap {
	seen := make(map[int]struct{}, len(ids))
	uniq := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	sort.Ints(uniq)

	idx := make(map[int]int, len(uniq))
	for i, id := range uniq {
		idx[id] = i
	}
	return &HeroMap{ids: uniq, index: idx}
}

// Len is the number of heroes (the model's output dimension).
func (h *HeroMap) Len() int { return len(h.ids) }

// Index returns the dense index for a hero id.
func (h *HeroMap) Index(id int) (int, bool) {
	i, ok := h.index[id]
	return i, ok
}

// ID returns the hero id for a dense index.
func (h *HeroMap) ID(index int) int { return h.ids[index] }

// IDs returns the sorted hero id slice (dense index order). The caller must not
// mutate it.
func (h *HeroMap) IDs() []int { return h.ids }
