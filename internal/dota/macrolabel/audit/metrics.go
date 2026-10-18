package audit

import (
	"fmt"
	"sort"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

type CompareMetrics struct {
	TotalItems          int            `json:"total_items"`
	WithHuman           int            `json:"with_human"`
	Precision           float64        `json:"precision"`
	KindAccuracy        float64        `json:"kind_accuracy"`
	Confusion           map[string]int `json:"confusion"`
	UnlabeledHumanKinds map[string]int `json:"unlabeled_human_kinds,omitempty"`
}

func CompareAutoHuman(items []AuditItem, space action.ActionSpace) CompareMetrics {
	m := CompareMetrics{Confusion: map[string]int{}, UnlabeledHumanKinds: map[string]int{}}
	var tp, labeledHuman, kindMatch int
	for _, it := range items {
		if it.Human == nil {
			continue
		}
		m.WithHuman++
		auto := it.Segment
		h := it.Human
		if h.Status == string(macrolabel.LabelStatusUnlabeled) || h.ActionID == nil {
			if auto.Status == macrolabel.LabelStatusUnlabeled {
				tp++
			}
			if h.Kind != "" {
				m.UnlabeledHumanKinds[h.Kind]++
			}
			key := fmt.Sprintf("%s->%s", auto.Status, macrolabel.LabelStatusUnlabeled)
			m.Confusion[key]++
			continue
		}
		labeledHuman++
		autoID := 0
		if auto.Status == macrolabel.LabelStatusLabeled && auto.Action != nil {
			autoID = auto.Action.ID
		}
		humanID := *h.ActionID
		if autoID == humanID {
			tp++
		}
		key := fmt.Sprintf("%d->%d", autoID, humanID)
		m.Confusion[key]++
		aAuto, _ := space.ByID(autoID)
		aHuman, _ := space.ByID(humanID)
		if aAuto.Kind == aHuman.Kind {
			kindMatch++
		}
	}
	m.TotalItems = len(items)
	if labeledHuman > 0 {
		m.Precision = float64(tp) / float64(labeledHuman)
		m.KindAccuracy = float64(kindMatch) / float64(labeledHuman)
	}
	return m
}

func SortConfusionKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
