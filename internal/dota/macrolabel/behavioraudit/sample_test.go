package behavioraudit

import (
	"testing"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

func TestStratifiedSampleDeterministic(t *testing.T) {
	segs := syntheticSegments()
	a := StratifiedSample(segs, SampleConfig{Size: 40, Seed: 42})
	b := StratifiedSample(segs, SampleConfig{Size: 40, Seed: 42})
	if len(a) != len(b) {
		t.Fatalf("len %d vs %d", len(a), len(b))
	}
	for i := range a {
		if episodeKey(a[i]) != episodeKey(b[i]) {
			t.Fatalf("order mismatch at %d", i)
		}
	}
	c := StratifiedSample(segs, SampleConfig{Size: 40, Seed: 99})
	same := len(a) == len(c)
	if same {
		same = true
		for i := range a {
			if episodeKey(a[i]) != episodeKey(c[i]) {
				same = false
				break
			}
		}
	}
	if same && len(a) > 5 {
		t.Fatalf("different seeds produced identical sample")
	}
}

func TestStratifiedSampleIncludesSmokeAndRetreat(t *testing.T) {
	segs := syntheticSegments()
	out := StratifiedSample(segs, SampleConfig{Size: 50, Seed: 7})
	var smoke, retreat int
	for _, s := range out {
		if s.SmokeAnchorID != "" || (s.Action != nil && s.Action.Kind == action.KindSmoke) {
			smoke++
		}
		if s.Status == macrolabel.LabelStatusLabeled && s.Action != nil && s.Action.Kind == action.KindRetreat {
			retreat++
		}
	}
	// All smoke-related and retreat labeled from input must be present.
	var wantSmoke, wantRetreat int
	for _, s := range segs {
		if s.Status == macrolabel.LabelStatusExcluded {
			continue
		}
		if s.SmokeAnchorID != "" || (s.Action != nil && s.Action.Kind == action.KindSmoke) {
			wantSmoke++
		}
		if s.Status == macrolabel.LabelStatusLabeled && s.Action != nil && s.Action.Kind == action.KindRetreat {
			wantRetreat++
		}
	}
	if smoke < wantSmoke {
		t.Fatalf("smoke inclusion: got %d want >= %d", smoke, wantSmoke)
	}
	if retreat < wantRetreat {
		t.Fatalf("retreat inclusion: got %d want >= %d", retreat, wantRetreat)
	}
}

func TestStratifiedSampleExcludesExcludedByDefault(t *testing.T) {
	segs := syntheticSegments()
	out := StratifiedSample(segs, SampleConfig{Size: 100, Seed: 1})
	for _, s := range out {
		if s.Status == macrolabel.LabelStatusExcluded {
			t.Fatalf("EXCLUDED present without include-excluded")
		}
	}
}

func syntheticSegments() []macrolabel.MacroActionSegment {
	space := action.DefaultSpaceV0()
	smoke, _ := space.New(action.KindSmoke, action.TargetMid)
	retreat, _ := space.New(action.KindRetreat, action.TargetNone)
	push, _ := space.New(action.KindPush, action.TargetTop)
	farm, _ := space.New(action.KindFarm, action.TargetOurJungle)
	conf := func(v float64) *float64 { return &v }

	var segs []macrolabel.MacroActionSegment
	// smoke labeled + anchor
	segs = append(segs, macrolabel.MacroActionSegment{
		MatchID: "m1", Side: "radiant", StartGameTime: 100, EndGameTime: 110,
		Status: macrolabel.LabelStatusLabeled, Action: &smoke, Confidence: conf(0.8),
		SmokeAnchorID: "sm1", DurationSeconds: 10,
	})
	// retreat labeled
	segs = append(segs, macrolabel.MacroActionSegment{
		MatchID: "m1", Side: "radiant", StartGameTime: 200, EndGameTime: 205,
		Status: macrolabel.LabelStatusLabeled, Action: &retreat, Confidence: conf(0.7),
		DurationSeconds: 5,
	})
	// more labeled
	for i := 0; i < 30; i++ {
		a := push
		if i%2 == 0 {
			a = farm
		}
		segs = append(segs, macrolabel.MacroActionSegment{
			MatchID: "m1", Side: "radiant", StartGameTime: 300 + i*10, EndGameTime: 305 + i*10,
			Status: macrolabel.LabelStatusLabeled, Action: &a, Confidence: conf(0.6),
			DurationSeconds: 5,
		})
	}
	// ambiguous
	for i := 0; i < 5; i++ {
		segs = append(segs, macrolabel.MacroActionSegment{
			MatchID: "m1", Side: "radiant", StartGameTime: 700 + i*10, EndGameTime: 705 + i*10,
			Status: macrolabel.LabelStatusAmbiguous,
			Competing: []macrolabel.CompetingCandidate{
				{ActionID: push.ID, Confidence: 0.5},
				{ActionID: farm.ID, Confidence: 0.48},
			},
			DurationSeconds: 5,
		})
	}
	// unlabeled strong/weak
	for i := 0; i < 40; i++ {
		rc := []macrolabel.RejectedCandidate{}
		if i < 25 {
			rc = []macrolabel.RejectedCandidate{{ActionID: push.ID, Confidence: 0.5}}
		}
		segs = append(segs, macrolabel.MacroActionSegment{
			MatchID: "m1", Side: "radiant", StartGameTime: 900 + i*10, EndGameTime: 905 + i*10,
			Status: macrolabel.LabelStatusUnlabeled, RejectedCandidates: rc, DurationSeconds: 5,
		})
	}
	// excluded
	segs = append(segs, macrolabel.MacroActionSegment{
		MatchID: "m1", Side: "radiant", StartGameTime: -10, EndGameTime: 0,
		Status: macrolabel.LabelStatusExcluded, ExclusionReason: macrolabel.ExclusionPreGame,
	})
	return segs
}