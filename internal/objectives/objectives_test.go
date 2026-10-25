package objectives

import "testing"

func TestNextSpawnPeriodic(t *testing.T) {
	s := Spawn{Kind: Lotus, FirstSpawn: 180, IntervalSec: 180}
	cases := []struct {
		clock, want int
	}{
		{-60, 180},  // pre-horn → first spawn
		{0, 180},     // before first → first
		{180, 180},   // exactly on a spawn → that spawn (NextIn 0)
		{181, 360},   // just past → next multiple
		{360, 360},   // on a later multiple
		{400, 540},   // between multiples → ceil up
	}
	for _, c := range cases {
		got, ok := nextSpawn(s, c.clock)
		if !ok || got != c.want {
			t.Errorf("nextSpawn(clock=%d) = %d,%v; want %d,true", c.clock, got, ok, c.want)
		}
	}
}

func TestNextSpawnOneShot(t *testing.T) {
	s := Spawn{Kind: Tormentor, FirstSpawn: 1200, IntervalSec: 0}
	if got, ok := nextSpawn(s, 600); !ok || got != 1200 {
		t.Errorf("before spawn: got %d,%v want 1200,true", got, ok)
	}
	if got, ok := nextSpawn(s, 1200); !ok || got != 1200 {
		t.Errorf("on spawn: got %d,%v want 1200,true", got, ok)
	}
	if _, ok := nextSpawn(s, 1201); ok {
		t.Errorf("past one-shot spawn should be omitted")
	}
}

func TestUpcomingDueAndOrder(t *testing.T) {
	specs := DefaultObjectives()
	// At 5:55 (355s), power runes spawn at 6:00 (in 5s) → Due with lead 30.
	alerts := Upcoming(specs, 355, 30)

	// Sorted ascending by NextIn.
	for i := 1; i < len(alerts); i++ {
		if alerts[i-1].NextIn > alerts[i].NextIn {
			t.Fatalf("alerts not sorted by NextIn at %d", i)
		}
	}

	var power *Alert
	for i := range alerts {
		if alerts[i].Kind == Power {
			power = &alerts[i]
			break
		}
	}
	if power == nil {
		t.Fatal("power rune alert missing")
	}
	if power.NextAt != 360 || power.NextIn != 5 {
		t.Errorf("power: NextAt=%d NextIn=%d, want 360/5", power.NextAt, power.NextIn)
	}
	if !power.Due {
		t.Errorf("power should be Due (5s <= lead 30)")
	}
}

func TestUpcomingOmitsPassedOneShot(t *testing.T) {
	specs := DefaultObjectives()
	alerts := Upcoming(specs, 1500, 30) // past 20:00 tormentor one-shot
	for _, a := range alerts {
		if a.Kind == Tormentor {
			t.Errorf("tormentor one-shot should be omitted after its spawn")
		}
	}
}

func TestUpcomingDeterministicTieBreak(t *testing.T) {
	// Two specs with the same NextIn must order by Kind, stably.
	specs := []Spawn{
		{Kind: Wisdom, Label: "W", FirstSpawn: 100, IntervalSec: 100},
		{Kind: Bounty, Label: "B", FirstSpawn: 100, IntervalSec: 100},
	}
	a := Upcoming(specs, 0, 999)
	b := Upcoming(specs, 0, 999)
	if len(a) != 2 || a[0].Kind != Bounty || a[1].Kind != Wisdom {
		t.Fatalf("tie-break by Kind wrong: %+v", a)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("non-deterministic output at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}
