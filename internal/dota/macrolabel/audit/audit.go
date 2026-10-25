package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

type HumanAnnotation struct {
	ActionID    *int   `json:"action_id,omitempty"`
	Status      string `json:"status,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Target      string `json:"target,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Annotator   string `json:"annotator,omitempty"`
	AnnotatedAt string `json:"annotated_at,omitempty"`
}

type AuditItem struct {
	AuditID   string                        `json:"audit_id"`
	MatchID   string                        `json:"match_id"`
	Side      string                        `json:"side"`
	StartTime int                           `json:"start_game_time"`
	EndTime   int                           `json:"end_game_time"`
	Status    macrolabel.LabelStatus        `json:"status"`
	Segment   macrolabel.MacroActionSegment `json:"segment"`
	Human     *HumanAnnotation              `json:"human,omitempty"`
}

func AuditID(seg macrolabel.MacroActionSegment) string {
	raw := fmt.Sprintf("%s|%s|%d|%d|%s", seg.MatchID, seg.Side, seg.StartGameTime, seg.EndGameTime, seg.Status)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}

func BuildAuditSample(segs []macrolabel.MacroActionSegment, n int, seed int64, space action.ActionSpace) []AuditItem {
	if n <= 0 || len(segs) == 0 {
		return nil
	}
	buckets := map[macrolabel.LabelStatus][]macrolabel.MacroActionSegment{}
	for _, s := range segs {
		buckets[s.Status] = append(buckets[s.Status], s)
	}
	order := []macrolabel.LabelStatus{
		macrolabel.LabelStatusLabeled,
		macrolabel.LabelStatusAmbiguous,
		macrolabel.LabelStatusUnlabeled,
		macrolabel.LabelStatusExcluded,
	}
	per := n / len(order)
	if per < 1 {
		per = 1
	}
	var picked []macrolabel.MacroActionSegment
	for _, st := range order {
		rows := buckets[st]
		if len(rows) == 0 {
			continue
		}
		idxs := deterministicPerm(len(rows), seed+int64(statusSeed(st)))
		take := per
		if len(rows) < take {
			take = len(rows)
		}
		for i := 0; i < take; i++ {
			picked = append(picked, rows[idxs[i]])
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		if picked[i].StartGameTime != picked[j].StartGameTime {
			return picked[i].StartGameTime < picked[j].StartGameTime
		}
		return picked[i].EndGameTime < picked[j].EndGameTime
	})
	if len(picked) > n {
		picked = picked[:n]
	}
	out := make([]AuditItem, 0, len(picked))
	for _, s := range picked {
		out = append(out, AuditItem{
			AuditID:   AuditID(s),
			MatchID:   s.MatchID,
			Side:      s.Side,
			StartTime: s.StartGameTime,
			EndTime:   s.EndGameTime,
			Status:    s.Status,
			Segment:   s,
		})
	}
	_ = space
	return out
}

func ExportAuditNDJSON(path string, items []AuditItem) error {
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			return err
		}
	}
	return nil
}

func LoadAuditNDJSON(r io.Reader) ([]AuditItem, error) {
	dec := json.NewDecoder(r)
	var out []AuditItem
	for {
		var it AuditItem
		if err := dec.Decode(&it); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

func statusSeed(st macrolabel.LabelStatus) int64 {
	return int64(len(st)) + int64(st[0])
}

func deterministicPerm(n int, seed int64) []int {
	idxs := make([]int, n)
	for i := range idxs {
		idxs[i] = i
	}
	x := uint64(seed)
	for i := n - 1; i > 0; i-- {
		x = x*6364136223846793005 + 1
		j := int(x % uint64(i+1))
		idxs[i], idxs[j] = idxs[j], idxs[i]
	}
	return idxs
}
