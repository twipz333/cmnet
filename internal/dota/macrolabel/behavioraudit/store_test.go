package behavioraudit

import (
	"os"
	"path/filepath"
	"testing"

	"cmnet/internal/dota/macrolabel"
)

func TestSaveLoadMergeResume(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "reviewed.ndjson")
	cases := []AuditCase{
		{ID: "a1", AutoStatus: macrolabel.LabelStatusLabeled, StartGameTime: 1, EndGameTime: 2,
			BehaviorAuditSchemaVersion: BehaviorAuditSchemaVersion, BehaviorReviewerVersion: BehaviorReviewerVersion},
		{ID: "a2", AutoStatus: macrolabel.LabelStatusUnlabeled, StartGameTime: 3, EndGameTime: 4,
			BehaviorAuditSchemaVersion: BehaviorAuditSchemaVersion, BehaviorReviewerVersion: BehaviorReviewerVersion},
	}
	if err := SaveReviewedNDJSON(out, CasesToReviewed(cases)); err != nil {
		t.Fatal(err)
	}
	// input untouched: create separate input
	in := filepath.Join(dir, "input.ndjson")
	if err := os.WriteFile(in, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inBefore, _ := os.ReadFile(in)

	rev := HumanReview{Status: ReviewConfirmed, AcceptedAuto: true, Confidence: ConfidenceHigh}
	cases, err := UpsertReview(cases, "a1", rev)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveReviewedNDJSON(out, CasesToReviewed(cases)); err != nil {
		t.Fatal(err)
	}
	inAfter, _ := os.ReadFile(in)
	if string(inBefore) != string(inAfter) {
		t.Fatal("input file was modified")
	}

	existing, err := ResumeFromOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	fresh := []AuditCase{
		{ID: "a1", AutoStatus: macrolabel.LabelStatusLabeled},
		{ID: "a2", AutoStatus: macrolabel.LabelStatusUnlabeled},
	}
	merged := MergeReviews(fresh, existing)
	if merged[0].Review == nil || merged[0].Review.Status != ReviewConfirmed {
		t.Fatalf("resume failed: %+v", merged[0].Review)
	}
}