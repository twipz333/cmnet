package opendota

import (
	"testing"
	"time"
)

func TestPatchIndexByIDAndTime(t *testing.T) {
	idx := NewPatchIndex([]Patch{
		{ID: 58, Name: "7.39", Date: "2025-01-01T00:00:00Z"},
		{ID: 59, Name: "7.40", Date: "2025-06-01T00:00:00Z"},
		{ID: 60, Name: "7.41", Date: "2026-03-24T00:00:00Z"},
	})
	if got := idx.NameByID(60); got != "7.41" {
		t.Fatalf("NameByID(60)=%q", got)
	}
	t739 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	t740 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC).Unix()
	t741 := time.Date(2026, 3, 24, 0, 50, 59, 0, time.UTC).Unix()
	if got := idx.NameByTime(t739); got != "7.39" {
		t.Fatalf("at 7.39 start: %q", got)
	}
	if got := idx.NameByTime(t740 - 1); got != "7.39" {
		t.Fatalf("just before 7.40: %q", got)
	}
	if got := idx.NameByTime(t740); got != "7.40" {
		t.Fatalf("at 7.40 start: %q", got)
	}
	if got := idx.NameByTime(t741); got != "7.41" {
		t.Fatalf("at 7.41: %q", got)
	}
	if idx.Latest() != "7.41" {
		t.Fatalf("Latest=%q", idx.Latest())
	}
	ranges := idx.Ranges()
	if len(ranges) != 3 || ranges[0].End != ranges[1].Start || ranges[2].End != 0 {
		t.Fatalf("ranges=%+v", ranges)
	}
}
