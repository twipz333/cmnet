package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"cmnet/internal/atomicfile"
	"cmnet/internal/cache"
)

// HoldoutSchemaVersion is the on-disk manifest contract.
const HoldoutSchemaVersion = 1

// Holdout is a sealed final test set (§6.8 / audit defect 11): match ids that must
// never be used for hyperparameter tuning or iterative eval. Eval may score them
// exactly once as a final report; resealing requires an explicit force.
type Holdout struct {
	SchemaVersion int     `json:"schema_version"`
	SealedAt      string  `json:"sealed_at"` // RFC3339 UTC
	Policy        string  `json:"policy"`    // e.g. newest-cm-frac
	Frac          float64 `json:"frac"`
	Stratify      string  `json:"stratify"` // cm|public-cm
	MatchIDs      []int64 `json:"match_ids"`
	N             int     `json:"n"`
	MinStartTime  int64   `json:"min_start_time"`
	MaxStartTime  int64   `json:"max_start_time"`
	ContentSHA256 string  `json:"content_sha256"`
}

// SealHoldout picks the newest frac of target-domain matches from a time-sorted
// corpus and returns a sealed manifest. Deterministic: ids sorted ascending.
func SealHoldout(matches []cache.Match, frac float64, stratify string) (*Holdout, error) {
	if frac <= 0 || frac >= 0.5 {
		return nil, fmt.Errorf("holdout frac must be in (0, 0.5), got %v", frac)
	}
	if stratify == "" {
		stratify = "cm"
	}
	sorted := append([]cache.Match(nil), matches...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartTime < sorted[j].StartTime })

	var targets []cache.Match
	for _, m := range sorted {
		if isTargetDomain(m, stratify) {
			targets = append(targets, m)
		}
	}
	n := int(float64(len(targets)) * frac)
	if n < 1 {
		return nil, fmt.Errorf("not enough %s matches to seal holdout (have %d, frac %.2f)", stratify, len(targets), frac)
	}
	// Newest slice of the target list.
	slice := targets[len(targets)-n:]
	ids := make([]int64, len(slice))
	minT, maxT := slice[0].StartTime, slice[0].StartTime
	for i, m := range slice {
		ids[i] = m.ID
		if m.StartTime < minT {
			minT = m.StartTime
		}
		if m.StartTime > maxT {
			maxT = m.StartTime
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	h := &Holdout{
		SchemaVersion: HoldoutSchemaVersion,
		SealedAt:      time.Now().UTC().Format(time.RFC3339),
		Policy:        "newest-cm-frac",
		Frac:          frac,
		Stratify:      stratify,
		MatchIDs:      ids,
		N:             len(ids),
		MinStartTime:  minT,
		MaxStartTime:  maxT,
	}
	h.ContentSHA256 = h.hash()
	return h, nil
}

func (h *Holdout) hash() string {
	if h == nil {
		return ""
	}
	sum := sha256.New()
	fmt.Fprintf(sum, "v%d|%s|%.6f|%s|", h.SchemaVersion, h.Policy, h.Frac, h.Stratify)
	for _, id := range h.MatchIDs {
		fmt.Fprintf(sum, "%d,", id)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// Verify checks schema and content hash.
func (h *Holdout) Verify() error {
	if h == nil {
		return fmt.Errorf("nil holdout")
	}
	if h.SchemaVersion != HoldoutSchemaVersion {
		return fmt.Errorf("holdout schema %d, code %d", h.SchemaVersion, HoldoutSchemaVersion)
	}
	if len(h.MatchIDs) == 0 {
		return fmt.Errorf("holdout has no match ids")
	}
	if got := h.hash(); got != h.ContentSHA256 {
		return fmt.Errorf("holdout hash mismatch: file %s, recomputed %s", h.ContentSHA256, got)
	}
	return nil
}

// IDSet returns match ids for O(1) exclusion.
func (h *Holdout) IDSet() map[int64]bool {
	out := make(map[int64]bool, len(h.MatchIDs))
	for _, id := range h.MatchIDs {
		out[id] = true
	}
	return out
}

// Exclude removes sealed matches from a corpus (for tuning evals).
func Exclude(matches []cache.Match, h *Holdout) []cache.Match {
	if h == nil || len(h.MatchIDs) == 0 {
		return matches
	}
	set := h.IDSet()
	out := make([]cache.Match, 0, len(matches))
	for _, m := range matches {
		if !set[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// FinalSplit trains on everything outside the holdout and tests only on sealed ids
// present in matches. Missing ids are skipped (corpus may have been pruned).
func FinalSplit(matches []cache.Match, h *Holdout) (train, test []cache.Match, err error) {
	if err := h.Verify(); err != nil {
		return nil, nil, err
	}
	set := h.IDSet()
	for _, m := range matches {
		if set[m.ID] {
			test = append(test, m)
		} else {
			train = append(train, m)
		}
	}
	if len(test) == 0 {
		return nil, nil, fmt.Errorf("holdout sealed %d ids but none are in the loaded corpus", h.N)
	}
	if len(train) == 0 {
		return nil, nil, fmt.Errorf("no training matches left after removing holdout")
	}
	sort.SliceStable(train, func(i, j int) bool { return train[i].StartTime < train[j].StartTime })
	sort.SliceStable(test, func(i, j int) bool { return test[i].StartTime < test[j].StartTime })
	return train, test, nil
}

// SaveHoldout writes the manifest atomically. Refuses to overwrite unless force.
func SaveHoldout(path string, h *Holdout, force bool) error {
	if h == nil {
		return fmt.Errorf("nil holdout")
	}
	h.ContentSHA256 = h.hash()
	if err := h.Verify(); err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("holdout already sealed at %s (pass --force to replace — this burns the old final set)", path)
		}
	}
	raw, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return atomicfile.Write(path, raw, 0o644)
}

// LoadHoldout reads and verifies a sealed manifest.
func LoadHoldout(path string) (*Holdout, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var h Holdout
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	if err := h.Verify(); err != nil {
		return nil, err
	}
	return &h, nil
}
