package objectives

import "testing"

func TestNextSpawnPeriodic(t *testing.T) {
	s := Spawn{Kind: Lotus, FirstSpawn: 180, IntervalSec: 180}
	cases := []struct {
		clock, want int
	}{
		{-60, 180},
		{0, 180},
		{180, 180},
		{181, 360},
		{360, 360},
		{400, 540},
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
	alerts := Upcoming(specs, 355, Leads{Default: 30}, Windows{})

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

func TestUpcomingPerKindLead(t *testing.T) {
	// At 5:55 both power and lotus are 5s away. Power lead 30 → Due; lotus lead 3 → not.
	specs := DefaultObjectives()
	alerts := Upcoming(specs, 355, Leads{
		Default: 15,
		ByKind:  map[Kind]int{Power: 30, Lotus: 3},
	}, Windows{})
	var power, lotus *Alert
	for i := range alerts {
		switch alerts[i].Kind {
		case Power:
			power = &alerts[i]
		case Lotus:
			lotus = &alerts[i]
		}
	}
	if power == nil || !power.Due {
		t.Fatalf("power Due want true, got %+v", power)
	}
	if lotus == nil || lotus.Due {
		t.Fatalf("lotus Due want false, got %+v", lotus)
	}
}

func TestLeadsForFallback(t *testing.T) {
	l := Leads{Default: 20, ByKind: map[Kind]int{Power: 40, Lotus: 0}}
	if l.For(Power) != 40 {
		t.Errorf("power override")
	}
	if l.For(Lotus) != 20 {
		t.Errorf("zero override should fall back to default")
	}
	if l.For(Bounty) != 20 {
		t.Errorf("missing override uses default")
	}
	if (Leads{}).For(Bounty) != DefaultLead {
		t.Errorf("empty leads → DefaultLead")
	}
}

func TestUpcomingOmitsPassedOneShot(t *testing.T) {
	specs := DefaultObjectives()
	alerts := Upcoming(specs, 1500, Leads{Default: 30}, Windows{})
	for _, a := range alerts {
		if a.Kind == Tormentor {
			t.Errorf("tormentor one-shot should be omitted after its spawn")
		}
	}
}

func TestUpcomingDeterministicTieBreak(t *testing.T) {
	specs := []Spawn{
		{Kind: Wisdom, Label: "W", FirstSpawn: 100, IntervalSec: 100},
		{Kind: Bounty, Label: "B", FirstSpawn: 100, IntervalSec: 100},
	}
	a := Upcoming(specs, 0, Leads{Default: 999}, Windows{})
	b := Upcoming(specs, 0, Leads{Default: 999}, Windows{})
	if len(a) != 2 || a[0].Kind != Bounty || a[1].Kind != Wisdom {
		t.Fatalf("tie-break by Kind wrong: %+v", a)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("non-deterministic output at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}


func TestWindowsContains(t *testing.T) {
	w := Windows{ByKind: map[Kind]Window{
		Wisdom: {From: 0, Until: 2100}, // 00:00 .. 35:00
	}}
	if !w.Contains(Wisdom, 0) || !w.Contains(Wisdom, 2100) {
		t.Fatalf("wisdom should be active at 0 and 35:00")
	}
	if w.Contains(Wisdom, 2101) {
		t.Fatalf("wisdom should be silent after 35:00")
	}
	if !w.Contains(Bounty, -60) {
		t.Fatalf("unset kind is unlimited, including pre-horn")
	}
}

func TestUpcomingRespectsUntilWindow(t *testing.T) {
	specs := DefaultObjectives()
	// 36:00 — next wisdom would be 42:00, but Until=35:00 silences the kind.
	alerts := Upcoming(specs, 2160, Leads{Default: 30}, Windows{
		ByKind: map[Kind]Window{Wisdom: {From: 0, Until: 2100}},
	})
	for _, a := range alerts {
		if a.Kind == Wisdom {
			t.Fatalf("wisdom alert present after window: %+v", a)
		}
	}
	// 34:50 — still inside window, wisdom due for 35:00 spawn.
	alerts = Upcoming(specs, 2090, Leads{Default: 30}, Windows{
		ByKind: map[Kind]Window{Wisdom: {From: 0, Until: 2100}},
	})
	var wisdom *Alert
	for i := range alerts {
		if alerts[i].Kind == Wisdom {
			wisdom = &alerts[i]
		}
	}
	if wisdom == nil || wisdom.NextAt != 2100 || !wisdom.Due {
		t.Fatalf("want wisdom due at 35:00, got %+v", wisdom)
	}
}


func TestLeadDisabledOmitsKind(t *testing.T) {
	specs := DefaultObjectives()
	alerts := Upcoming(specs, 355, Leads{
		Default: 30,
		ByKind:  map[Kind]int{Power: LeadDisabled},
	}, Windows{})
	for _, a := range alerts {
		if a.Kind == Power {
			t.Fatalf("power should be disabled, got %+v", a)
		}
	}
}

func TestLeadDisabledDefault(t *testing.T) {
	l := Leads{Default: LeadDisabled, ByKind: map[Kind]int{Lotus: 10}}
	if l.Enabled(Power) {
		t.Fatal("power should follow disabled default")
	}
	if !l.Enabled(Lotus) || l.For(Lotus) != 10 {
		t.Fatalf("lotus override should stay on, got %d", l.For(Lotus))
	}
	if l.For(Bounty) != LeadDisabled {
		t.Fatalf("bounty For=%d want LeadDisabled", l.For(Bounty))
	}
}

func TestLeadsForExplicitDisable(t *testing.T) {
	l := Leads{Default: 15, ByKind: map[Kind]int{DayNight: LeadDisabled}}
	if l.Enabled(DayNight) {
		t.Fatal("daynight should be disabled")
	}
	if l.For(Bounty) != 15 {
		t.Fatalf("bounty = %d", l.For(Bounty))
	}
}
