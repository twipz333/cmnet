package lanestate

import (
	"testing"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
)

func TestDisappearanceKeepsLastSeen(t *testing.T) {
	g := mustLanes(t)
	tr := New(g)
	p := atProgress(t, g, worldmap.LaneBot, 0.44)
	st := tickState(100, "m", observe.TeamDire, true, melee(observe.TeamRadiant, p))
	tr.Apply(Build(st, g))
	snap := tr.Snapshot()
	if !snap.Bot.Enemy.HasVisibleFront {
		t.Fatal("expected visible enemy bot wave")
	}
	front := snap.Bot.Enemy.VisibleFrontTeam

	st2 := tickState(108, "m", observe.TeamDire, true)
	tr.Apply(Build(st2, g))
	snap = tr.Snapshot()
	if len(snap.Bot.Enemy.Visible) != 0 {
		t.Fatal("visible must be empty after present+absent creeps")
	}
	if len(snap.Bot.Enemy.LastSeen) != 1 {
		t.Fatalf("last seen %d", len(snap.Bot.Enemy.LastSeen))
	}
	if snap.Bot.Enemy.LastSeenAgeSeconds == nil || *snap.Bot.Enemy.LastSeenAgeSeconds != 8 {
		t.Fatalf("age %v", snap.Bot.Enemy.LastSeenAgeSeconds)
	}
	if snap.Bot.Enemy.LastSeenFrontTeam != front {
		t.Fatalf("last seen moved: %v -> %v", front, snap.Bot.Enemy.LastSeenFrontTeam)
	}
}

func TestAbsentMinimapDoesNotCountAsFog(t *testing.T) {
	g := mustLanes(t)
	tr := New(g)
	p := atProgress(t, g, worldmap.LaneBot, 0.5)
	tr.Apply(Build(tickState(10, "m", observe.TeamDire, true, melee(observe.TeamRadiant, p)), g))
	tr.Apply(Build(tickState(11, "m", observe.TeamDire, false), g))
	snap := tr.Snapshot()
	if !snap.Bot.Enemy.HasVisibleFront {
		t.Fatal("missing section must not clear visible waves")
	}
	if tr.StatsSnapshot().EnemyVisibilityLost != 0 {
		t.Fatal("absent section is not a visibility loss")
	}
}

func TestPauseDoesNotAdvanceAge(t *testing.T) {
	g := mustLanes(t)
	tr := New(g)
	p := atProgress(t, g, worldmap.LaneMid, 0.5)
	tr.Apply(Build(tickState(50, "m", observe.TeamDire, true, melee(observe.TeamRadiant, p)), g))
	gone := tickState(55, "m", observe.TeamDire, true)
	tr.Apply(Build(gone, g))
	age1 := *tr.Snapshot().Mid.Enemy.LastSeenAgeSeconds
	// Same game time (pause): age must stay.
	paused := tickState(55, "m", observe.TeamDire, true)
	paused.Clock.Paused = true
	tr.Apply(Build(paused, g))
	age2 := *tr.Snapshot().Mid.Enemy.LastSeenAgeSeconds
	if age1 != age2 {
		t.Fatalf("pause advanced age %v -> %v", age1, age2)
	}
}

func TestMultipleEnemyGroupsLastSeen(t *testing.T) {
	g := mustLanes(t)
	tr := New(g)
	a := melee(observe.TeamRadiant, atProgress(t, g, worldmap.LaneTop, 0.25))
	b := melee(observe.TeamRadiant, atProgress(t, g, worldmap.LaneTop, 0.70))
	tr.Apply(Build(tickState(3, "m", observe.TeamDire, true, a, b), g))
	if n := len(tr.Snapshot().Top.Enemy.Visible); n != 2 {
		t.Fatalf("visible groups %d", n)
	}
	tr.Apply(Build(tickState(4, "m", observe.TeamDire, true), g))
	if n := len(tr.Snapshot().Top.Enemy.LastSeen); n != 2 {
		t.Fatalf("last seen groups %d", n)
	}
	if len(tr.Snapshot().Top.Enemy.Visible) != 0 {
		t.Fatal("current must not copy last seen")
	}
}

func TestMatchResetClearsHistory(t *testing.T) {
	g := mustLanes(t)
	tr := New(g)
	p := atProgress(t, g, worldmap.LaneMid, 0.4)
	tr.Apply(Build(tickState(9, "match-a", observe.TeamDire, true, melee(observe.TeamRadiant, p)), g))
	tr.Apply(Build(tickState(1, "match-b", observe.TeamDire, true), g))
	snap := tr.Snapshot()
	if len(snap.Mid.Enemy.LastSeen) != 0 || snap.Mid.Enemy.HasVisibleFront {
		t.Fatalf("history leaked across matches: %+v", snap.Mid.Enemy)
	}
}

func TestSnapshotCopiesSlices(t *testing.T) {
	g := mustLanes(t)
	tr := New(g)
	p := atProgress(t, g, worldmap.LaneMid, 0.5)
	tr.Apply(Build(tickState(1, "m", observe.TeamDire, true, melee(observe.TeamRadiant, p)), g))
	a := tr.Snapshot()
	a.Mid.Enemy.Visible[0].CreepCount = 99
	b := tr.Snapshot()
	if b.Mid.Enemy.Visible[0].CreepCount == 99 {
		t.Fatal("snapshot leaked internal memory")
	}
}

func TestOurEmptyNotFilledFromHistory(t *testing.T) {
	g := mustLanes(t)
	tr := New(g)
	p := atProgress(t, g, worldmap.LaneTop, 0.3)
	tr.Apply(Build(tickState(1, "m", observe.TeamDire, true, melee(observe.TeamDire, p)), g))
	if tr.Snapshot().Top.Our.CreepCount != 1 {
		t.Fatal("our wave")
	}
	tr.Apply(Build(tickState(2, "m", observe.TeamDire, true), g))
	if tr.Snapshot().Top.Our.CreepCount != 0 || len(tr.Snapshot().Top.Our.Waves) != 0 {
		t.Fatal("our waves must not persist without a current observation")
	}
}

func TestObservedContact(t *testing.T) {
	g := mustLanes(t)
	tr := New(g)
	a := melee(observe.TeamDire, atProgress(t, g, worldmap.LaneMid, 0.50))
	b := melee(observe.TeamRadiant, atProgress(t, g, worldmap.LaneMid, 0.52))
	tr.Apply(Build(tickState(20, "m", observe.TeamDire, true, a, b), g))
	v := tr.Snapshot().Mid
	if !v.ObservedContact {
		t.Fatalf("expected contact, gap=%v our=%d enemy=%d", v.VisibleFrontPathGap, len(v.Our.Waves), len(v.Enemy.Visible))
	}
	if v.VisibleFrontPathGap == nil || *v.VisibleFrontPathGap > g.WaveContactGap() {
		t.Fatalf("gap %v", v.VisibleFrontPathGap)
	}
}
