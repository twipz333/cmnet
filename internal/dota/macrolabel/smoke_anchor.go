package macrolabel

import (
	"cmnet/internal/dota/action"

	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

type SmokeAnchor struct {
	ID         string
	MatchID    string
	Side       string
	GameTime   int
	FrameIndex int
	Seq        uint64
}

func smokeAnchorID(matchID, side string, gameTime int) string {
	raw := fmt.Sprintf("%s|%s|%d", matchID, side, gameTime)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

func DedupeSmokeActivationTimes(frames []BehaviorFrame) []int {
	var times []int
	for _, f := range frames {
		for _, ev := range f.Events {
			if ev.Kind != "SMOKE_ACTIVATED" {
				continue
			}
			gt := f.GameTime
			if ev.GameTime != nil {
				gt = *ev.GameTime
			}
			merged := false
			for _, t := range times {
				if absInt(t-gt) <= 2 {
					merged = true
					break
				}
			}
			if !merged {
				times = append(times, gt)
			}
		}
	}
	sort.Ints(times)
	return times
}

func smokeActiveAt(anchorTime int, activationTimes []int, followSeconds float64) bool {
	for _, t := range activationTimes {
		if anchorTime < t {
			continue
		}
		if float64(anchorTime-t) <= followSeconds {
			return true
		}
	}
	return false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func detectSmokeAnchors(frames []BehaviorFrame) []SmokeAnchor {
	times := DedupeSmokeActivationTimes(frames)
	anchors := make([]SmokeAnchor, 0, len(times))
	for _, gt := range times {
		idx := 0
		seq := uint64(0)
		matchID := ""
		side := ""
		for i, f := range frames {
			if f.GameTime < gt {
				continue
			}
			for _, ev := range f.Events {
				if ev.Kind != "SMOKE_ACTIVATED" {
					continue
				}
				egt := f.GameTime
				if ev.GameTime != nil {
					egt = *ev.GameTime
				}
				if absInt(egt-gt) <= 2 {
					idx = i
					seq = f.Seq
					matchID = f.MatchID
					side = f.Side
					break
				}
			}
			if matchID != "" {
				break
			}
		}
		anchors = append(anchors, SmokeAnchor{
			ID:         smokeAnchorID(matchID, side, gt),
			MatchID:    matchID,
			Side:       side,
			GameTime:   gt,
			FrameIndex: idx,
			Seq:        seq,
		})
	}
	return anchors
}

func ListSmokeAnchors(frames []BehaviorFrame) []SmokeAnchor {
	return detectSmokeAnchors(frames)
}

func assignSmokeAnchorIDs(segs []MacroActionSegment, anchors []SmokeAnchor, space ActionSpaceProvider) []MacroActionSegment {
	if len(anchors) == 0 {
		return segs
	}
	for i := range segs {
		if segs[i].Status != LabelStatusLabeled || segs[i].Action == nil {
			continue
		}
		if segs[i].Action.Kind != action.KindSmoke {
			continue
		}
		best := ""
		bestDist := int(^uint(0) >> 1)
		for _, a := range anchors {
			d := segs[i].StartGameTime - a.GameTime
			if d < 0 {
				d = -d
			}
			if d < bestDist {
				bestDist = d
				best = a.ID
			}
		}
		segs[i].SmokeAnchorID = best
	}
	return dedupeSmokeSegmentsPerAnchor(segs)
}

func dedupeSmokeSegmentsPerAnchor(segs []MacroActionSegment) []MacroActionSegment {
	byAnchor := map[string][]int{}
	for i, s := range segs {
		if s.SmokeAnchorID == "" {
			continue
		}
		if s.Status != LabelStatusLabeled || s.Action == nil {
			continue
		}
		byAnchor[s.SmokeAnchorID] = append(byAnchor[s.SmokeAnchorID], i)
	}
	drop := map[int]struct{}{}
	for _, idxs := range byAnchor {
		if len(idxs) <= 1 {
			continue
		}
		best := idxs[0]
		bestConf := -1.0
		for _, ix := range idxs {
			c := 0.0
			if segs[ix].Confidence != nil {
				c = *segs[ix].Confidence
			}
			if c > bestConf || (c == bestConf && segs[ix].StartGameTime < segs[best].StartGameTime) {
				best = ix
				bestConf = c
			}
		}
		for _, ix := range idxs {
			if ix != best {
				drop[ix] = struct{}{}
			}
		}
	}
	if len(drop) == 0 {
		return segs
	}
	out := make([]MacroActionSegment, 0, len(segs)-len(drop))
	for i, s := range segs {
		if _, ok := drop[i]; ok {
			continue
		}
		out = append(out, s)
	}
	return out
}
