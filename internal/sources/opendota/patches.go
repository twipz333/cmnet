package opendota

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Patch is one entry from GET /constants/patch.
type Patch struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Date string `json:"date"` // RFC3339 from OpenDota
}

// PatchIndex resolves OpenDota numeric ids and match start times to human tags
// like "7.41". Built once per sync/train from /constants/patch.
type PatchIndex struct {
	byID   map[int]string
	ranges []patchRange // sorted ascending by Start
}

type patchRange struct {
	Start int64 // unix seconds, inclusive
	Name  string
}

// Patches fetches the OpenDota patch constant table.
func (c *Client) Patches(ctx context.Context) ([]Patch, error) {
	var rows []Patch
	if err := c.getJSON(ctx, "/constants/patch", &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("opendota: empty /constants/patch")
	}
	return rows, nil
}

// NewPatchIndex builds a lookup from the constant table. Deterministic: ranges
// sorted by start time, then id.
func NewPatchIndex(rows []Patch) *PatchIndex {
	type dated struct {
		id    int
		name  string
		start int64
	}
	var list []dated
	for _, p := range rows {
		if p.Name == "" {
			continue
		}
		start := parsePatchDate(p.Date)
		list = append(list, dated{id: p.ID, name: p.Name, start: start})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].start != list[j].start {
			return list[i].start < list[j].start
		}
		return list[i].id < list[j].id
	})
	idx := &PatchIndex{byID: make(map[int]string, len(list))}
	for _, d := range list {
		idx.byID[d.id] = d.name
		idx.ranges = append(idx.ranges, patchRange{Start: d.start, Name: d.name})
	}
	return idx
}

func parsePatchDate(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// NameByID returns the human patch tag for an OpenDota numeric id.
func (idx *PatchIndex) NameByID(id int) string {
	if idx == nil || id <= 0 {
		return ""
	}
	return idx.byID[id]
}

// ByID returns a copy of the id→name map for cache backfill of legacy numeric tags.
func (idx *PatchIndex) ByID() map[int]string {
	if idx == nil {
		return nil
	}
	out := make(map[int]string, len(idx.byID))
	for k, v := range idx.byID {
		out[k] = v
	}
	return out
}

// NameByTime returns the patch in effect at unix time t (last range with Start <= t).
func (idx *PatchIndex) NameByTime(t int64) string {
	if idx == nil || len(idx.ranges) == 0 || t <= 0 {
		return ""
	}
	// Linear scan is fine: ~60 patches. Keep last match with Start <= t.
	name := ""
	for _, r := range idx.ranges {
		if r.Start > t {
			break
		}
		name = r.Name
	}
	return name
}

// Latest returns the newest patch name, or "".
func (idx *PatchIndex) Latest() string {
	if idx == nil || len(idx.ranges) == 0 {
		return ""
	}
	return idx.ranges[len(idx.ranges)-1].Name
}

// Ranges returns [start,end) windows for SQL backfill. end=0 means open-ended.
func (idx *PatchIndex) Ranges() []struct {
	Name       string
	Start, End int64
} {
	if idx == nil {
		return nil
	}
	out := make([]struct {
		Name       string
		Start, End int64
	}, 0, len(idx.ranges))
	for i, r := range idx.ranges {
		end := int64(0)
		if i+1 < len(idx.ranges) {
			end = idx.ranges[i+1].Start
		}
		out = append(out, struct {
			Name       string
			Start, End int64
		}{Name: r.Name, Start: r.Start, End: end})
	}
	return out
}
