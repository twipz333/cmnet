package behavioraudit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cmnet/internal/dota/macrolabel"
)

func TestSaveLoadMergeResume(t *testing.T) {
	dir := t.TempDir()
	// SaveReviewedNDJSON writes via CreateTemp+Rename; on Windows a just-written
	// file can stay briefly locked by the scanner, so testing.TempDir RemoveAll
	// false-fails with "directory is not empty". Empty the dir first (Cleanup is
	// LIFO, so this runs before TempDir's own RemoveAll). Assertions unchanged.
	t.Cleanup(func() { releaseTempDirFiles(dir) })
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

// releaseTempDirFiles deletes everything in dir so testing.TempDir RemoveAll only
// has to remove an empty directory. Retries cover transient Windows file locks.
func releaseTempDirFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		for i := 0; i < 40; i++ {
			err := os.RemoveAll(p)
			if err == nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
}