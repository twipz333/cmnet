package web

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"cmnet/internal/cache"
	"cmnet/internal/jobs"
)

// Seating players one at a time must end as one scout of all of them: a scout
// still waiting is absorbed into the new request, and duplicates are dropped.
func TestFoldScoutsAbsorbsWaitingScout(t *testing.T) {
	st, err := cache.Open(filepath.Join(t.TempDir(), "cmnet.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	q := jobs.New(st, jobs.Options{})
	payload, _ := json.Marshal(map[string][]string{"players": {"11", "22"}})
	id, err := q.Enqueue(jobs.KindScoutAllies, jobs.PriorityManual, string(payload), false)
	if err != nil {
		t.Fatal(err)
	}
	got := foldScouts(q, jobs.KindScoutAllies, []string{"22", "33"})
	if want := []string{"11", "22", "33"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("folded = %v, want %v", got, want)
	}
	j, err := q.Job(id)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != cache.JobCancelled {
		t.Fatalf("absorbed job state = %s, want cancelled", j.State)
	}
	// Another kind is left alone.
	if got := foldScouts(q, jobs.KindScout, []string{"44"}); !reflect.DeepEqual(got, []string{"44"}) {
		t.Fatalf("enemy scout folded into ally scouts: %v", got)
	}
}
