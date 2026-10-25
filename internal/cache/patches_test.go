package cache

import "testing"

func TestDominantPatch(t *testing.T) {
	matches := []Match{
		{Patch: "7.40"}, {Patch: "7.41"}, {Patch: "7.41"}, {Patch: ""}, {Patch: "7.40"},
	}
	// 7.40 and 7.41 both appear twice — tie-break lexicographic → 7.40
	if got := DominantPatch(matches); got != "7.40" {
		t.Fatalf("got %q want 7.40", got)
	}
	if DominantPatch(nil) != "" {
		t.Fatal("empty")
	}
}

func TestBackfillPatches(t *testing.T) {
	s := openTemp(t)
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.UpsertMatch(Match{ID: 1, StartTime: 100, RadiantWin: true}))
	must(s.UpsertMatch(Match{ID: 2, StartTime: 200, RadiantWin: true, Patch: "55"}))
	must(s.UpsertMatch(Match{ID: 3, StartTime: 300, RadiantWin: true, Patch: "7.41"}))

	n, err := s.BackfillPatches([]PatchWindow{
		{Name: "7.39", Start: 0, End: 150},
		{Name: "7.40", Start: 150, End: 250},
		{Name: "7.41", Start: 250, End: 0},
	}, map[int]string{55: "7.40b"})
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("updated %d, want at least 2", n)
	}
	ms, err := s.Matches(0)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]string{}
	for _, m := range ms {
		byID[m.ID] = m.Patch
	}
	if byID[1] != "7.39" {
		t.Fatalf("id1=%q want 7.39", byID[1])
	}
	if byID[2] != "7.40b" {
		t.Fatalf("id2=%q want 7.40b (numeric rewrite)", byID[2])
	}
	if byID[3] != "7.41" {
		t.Fatalf("id3=%q want unchanged 7.41", byID[3])
	}
}
