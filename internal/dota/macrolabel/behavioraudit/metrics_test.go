package behavioraudit

import (
	"testing"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
)

func TestBehaviorPrecisionExcludesUnclear(t *testing.T) {
	space := action.DefaultSpaceV0()
	push, _ := space.New(action.KindPush, action.TargetTop)
	cases := []AuditCase{
		{ID: "1", AutoStatus: macrolabel.LabelStatusLabeled, AutoAction: &push, Review: &HumanReview{Status: ReviewConfirmed, AcceptedAuto: true}},
		{ID: "2", AutoStatus: macrolabel.LabelStatusLabeled, AutoAction: &push, Review: &HumanReview{Status: ReviewConfirmed, AcceptedAuto: true}},
		{ID: "3", AutoStatus: macrolabel.LabelStatusLabeled, AutoAction: &push, Review: &HumanReview{Status: ReviewRejected, AcceptedAuto: false}},
		{ID: "4", AutoStatus: macrolabel.LabelStatusLabeled, AutoAction: &push, Review: &HumanReview{Status: ReviewUnclear}},
		{ID: "5", AutoStatus: macrolabel.LabelStatusLabeled, AutoAction: &push}, // pending
	}
	rep := BuildReport(cases)
	if rep.Confirmed != 2 || rep.Rejected != 1 || rep.Unclear != 1 {
		t.Fatalf("counts confirmed=%d rejected=%d unclear=%d", rep.Confirmed, rep.Rejected, rep.Unclear)
	}
	want := 2.0 / 3.0
	if rep.BehaviorPrecision != want {
		t.Fatalf("precision=%v want %v", rep.BehaviorPrecision, want)
	}
	if rep.PendingCount != 1 || !rep.Incomplete {
		t.Fatalf("pending=%d incomplete=%v", rep.PendingCount, rep.Incomplete)
	}
}

func TestRejectedTopRecovery(t *testing.T) {
	space := action.DefaultSpaceV0()
	push, _ := space.New(action.KindPush, action.TargetTop)
	farm, _ := space.New(action.KindFarm, action.TargetOurJungle)
	hid := farm.ID
	cases := []AuditCase{{
		ID: "u1", AutoStatus: macrolabel.LabelStatusUnlabeled,
		RejectedCandidates: []macrolabel.RejectedCandidate{
			{ActionID: push.ID, Confidence: 0.4},
			{ActionID: farm.ID, Confidence: 0.9},
		},
		Review: &HumanReview{Status: ReviewConfirmed, HumanActionID: &hid},
	}}
	rep := BuildReport(cases)
	if rep.RejectedTop1Recovery != 1 {
		t.Fatalf("top1=%v", rep.RejectedTop1Recovery)
	}
	if rep.UnlabeledRecoveryRate != 1 {
		t.Fatalf("recovery=%v", rep.UnlabeledRecoveryRate)
	}
}