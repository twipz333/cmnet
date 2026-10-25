package behavioraudit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReviewedItem is one persisted NDJSON row (case + optional review).
type ReviewedItem struct {
	AuditID string       `json:"audit_id"`
	Case    AuditCase    `json:"case"`
	Review  *HumanReview `json:"review,omitempty"`
}

// LoadAuditItems loads reviewed NDJSON (or case-only rows).
func LoadAuditItems(r io.Reader) ([]ReviewedItem, error) {
	dec := json.NewDecoder(r)
	var out []ReviewedItem
	for {
		var it ReviewedItem
		if err := dec.Decode(&it); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if it.AuditID == "" {
			it.AuditID = it.Case.ID
		}
		if it.Case.ID == "" {
			it.Case.ID = it.AuditID
		}
		if it.Review != nil && it.Case.Review == nil {
			it.Case.Review = it.Review
		}
		out = append(out, it)
	}
	return out, nil
}

// SaveReviewedNDJSON atomically writes items (temp + rename). Never touches inputPath.
func SaveReviewedNDJSON(outputPath string, items []ReviewedItem) error {
	if outputPath == "" {
		return fmt.Errorf("empty output path")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	dir := filepath.Dir(outputPath)
	tmp, err := os.CreateTemp(dir, "behavior-audit-*.ndjson.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	for _, it := range items {
		if it.AuditID == "" {
			it.AuditID = it.Case.ID
		}
		if it.Review == nil {
			it.Review = it.Case.Review
		}
		if err := enc.Encode(it); err != nil {
			tmp.Close()
			_ = os.Remove(tmpName)
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, outputPath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// MergeReviews overlays existing reviews from resume file onto cases by audit_id.
// Input items are never mutated as a file; returns a new slice.
func MergeReviews(cases []AuditCase, existing []ReviewedItem) []AuditCase {
	byID := map[string]*HumanReview{}
	for i := range existing {
		it := &existing[i]
		id := it.AuditID
		if id == "" {
			id = it.Case.ID
		}
		rev := it.Review
		if rev == nil {
			rev = it.Case.Review
		}
		if id != "" && rev != nil {
			cp := *rev
			byID[id] = &cp
		}
	}
	out := make([]AuditCase, len(cases))
	copy(out, cases)
	for i := range out {
		if rev, ok := byID[out[i].ID]; ok {
			cp := *rev
			out[i].Review = &cp
		}
	}
	return out
}

// ResumeFromOutput loads an existing output NDJSON if present (for resume).
func ResumeFromOutput(outputPath string) ([]ReviewedItem, error) {
	f, err := os.Open(outputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	return LoadAuditItems(f)
}

// CasesToReviewed converts cases to ReviewedItem rows.
func CasesToReviewed(cases []AuditCase) []ReviewedItem {
	out := make([]ReviewedItem, 0, len(cases))
	for _, c := range cases {
		out = append(out, ReviewedItem{
			AuditID: c.ID,
			Case:    c,
			Review:  c.Review,
		})
	}
	return out
}

// UpsertReview sets review on the matching case and returns updated slice.
func UpsertReview(cases []AuditCase, auditID string, rev HumanReview) ([]AuditCase, error) {
	found := false
	out := make([]AuditCase, len(cases))
	copy(out, cases)
	for i := range out {
		if out[i].ID == auditID {
			cp := rev
			out[i].Review = &cp
			found = true
			break
		}
	}
	if !found {
		return cases, fmt.Errorf("unknown audit_id %s", auditID)
	}
	return out, nil
}