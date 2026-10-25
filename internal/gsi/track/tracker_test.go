package track

import (
	"testing"

	"cmnet/internal/gsi/observe"
)

func testRoster() Roster {
	return Roster{
		AllySide: observe.TeamDire,
		Allies: []HeroRef{
			{UnitName: "npc_dota_hero_mirana"},
			{UnitName: "npc_dota_hero_sven"},
		},
		Enemies: []HeroRef{
			{UnitName: "npc_dota_hero_luna"},
			{UnitName: "npc_dota_hero_tidehunter"},
		},
	}
}

func newTestTracker(t *testing.T) *Tracker {
	t.Helper()
	return New(Options{Roster: StaticRoster{R: testRoster()}, Log: func(string) {}})
}

func batchAt(gameTime int, heroes []observe.HeroPosition) *observe.Batch {
	gt := gameTime
	return &observe.Batch{
		Meta: observe.Meta{Seq: uint64(gameTime), GameTime: &gt, Source: observe.SourceGSIMinimap, Confidence: 1},
		Match: observe.MatchObservation{
			Present:  true,
			GameTime: &gt,
			MatchID:  strPtr("m1"),
		},
		Heroes: heroes,
	}
}

func strPtr(s string) *string { return &s }

func TestEnemyVisibilitySequence(t *testing.T) {
	tr := newTestTracker(t)
	hero := "npc_dota_hero_luna"
	A := observe.Vec2{X: 1, Y: 1}
	B := observe.Vec2{X: 2, Y: 2}

	tick := func(gt int, pos *observe.Vec2) EnemyHeroTrack {
		var hs []observe.HeroPosition
		if pos != nil {
			hs = []observe.HeroPosition{{
				Hero: hero, Team: observe.TeamRadiant, Position: *pos,
				Image: observe.ImageEnemyIcon, ImageKind: observe.HeroImageEnemy,
			}}
		}
		tr.Apply(batchAt(gt, hs))
		st := tr.Snapshot()
		for _, e := range st.Enemies {
			if e.Hero == hero {
				return e
			}
		}
		t.Fatalf("luna missing")
		return EnemyHeroTrack{}
	}

	e := tick(10, nil)
	if e.VisibleNow {
		t.Fatal("tick1: want hidden")
	}

	e = tick(11, &A)
	if !e.VisibleNow || len(e.VisiblePositions) != 1 || !e.VisiblePositions[0].Equal(A) {
		t.Fatalf("tick2: %+v", e)
	}
	if e.LastSeenAt == nil || *e.LastSeenAt != 11 {
		t.Fatalf("tick2 last_seen_at %+v", e.LastSeenAt)
	}

	e = tick(12, &B)
	if !e.VisibleNow || !e.LastSeenPositions[0].Equal(B) {
		t.Fatalf("tick3: %+v", e)
	}

	e = tick(13, nil)
	if e.VisibleNow {
		t.Fatal("tick4: want hidden")
	}
	if len(e.VisiblePositions) != 0 {
		t.Fatal("tick4: current position must be unknown")
	}
	if len(e.LastSeenPositions) != 1 || !e.LastSeenPositions[0].Equal(B) {
		t.Fatalf("tick4 last_seen lost: %+v", e.LastSeenPositions)
	}
	if e.MissingSince == nil || *e.MissingSince != 13 {
		t.Fatalf("tick4 missing_since %+v", e.MissingSince)
	}

	e = tick(20, nil)
	if e.VisibleNow {
		t.Fatal("tick5 visible")
	}
	if !e.LastSeenPositions[0].Equal(B) {
		t.Fatal("tick5 last_seen lost")
	}
	if e.LastSeenAgeSeconds == nil || *e.LastSeenAgeSeconds != 8 {
		t.Fatalf("tick5 age = %v want 8", e.LastSeenAgeSeconds)
	}
}

func TestPausedClockDoesNotAdvanceAge(t *testing.T) {
	tr := newTestTracker(t)
	pos := observe.Vec2{X: 5, Y: 5}
	h := observe.HeroPosition{
		Hero: "npc_dota_hero_luna", Team: observe.TeamRadiant, Position: pos,
		Image: observe.ImageEnemyIcon, ImageKind: observe.HeroImageEnemy,
	}
	tr.Apply(batchAt(10, []observe.HeroPosition{h}))
	paused := true
	gt := 10
	tr.Apply(&observe.Batch{
		Meta:  observe.Meta{Seq: 2, GameTime: &gt},
		Match: observe.MatchObservation{Present: true, GameTime: &gt, Paused: &paused, MatchID: strPtr("m1")},
	})
	st := tr.Snapshot()
	var e EnemyHeroTrack
	for _, x := range st.Enemies {
		if x.Hero == "npc_dota_hero_luna" {
			e = x
		}
	}
	if e.LastSeenAgeSeconds == nil || *e.LastSeenAgeSeconds != 0 {
		t.Fatalf("paused age = %v", e.LastSeenAgeSeconds)
	}
}

func TestIllusionKeepsAllPositions(t *testing.T) {
	tr := newTestTracker(t)
	ps := []observe.Vec2{{X: 1, Y: 1}, {X: 2, Y: 2}, {X: 3, Y: 3}}
	var hs []observe.HeroPosition
	for _, p := range ps {
		hs = append(hs, observe.HeroPosition{
			Hero: "npc_dota_hero_luna", Team: observe.TeamRadiant, Position: p,
			Image: observe.ImageEnemyIcon, ImageKind: observe.HeroImageEnemy,
		})
	}
	tr.Apply(batchAt(5, hs))
	st := tr.Snapshot()
	var e EnemyHeroTrack
	for _, x := range st.Enemies {
		if x.Hero == "npc_dota_hero_luna" {
			e = x
		}
	}
	if !e.VisibleNow || len(e.VisiblePositions) != 3 {
		t.Fatalf("positions = %+v", e.VisiblePositions)
	}
}

func TestEventDedup(t *testing.T) {
	tr := newTestTracker(t)
	gt := 124
	pid := 9
	item := 43
	ev := observe.Event{
		Kind: observe.EventItemPurchase, RawType: "generic_event",
		InnerType: "CHAT_MESSAGE_ITEM_PURCHASE", GameTime: &gt, PlayerID: &pid, ItemID: &item, Value: &item,
	}
	b := batchAt(124, nil)
	b.Events = []observe.Event{ev}
	tr.Apply(b)
	tr.Apply(b)
	tr.Apply(b)
	st := tr.Snapshot()
	if n := len(st.RecentEvents); n != 1 {
		t.Fatalf("recent events = %d, want 1", n)
	}
	if n := len(st.RecentPurchases); n != 1 {
		t.Fatalf("purchases = %d", n)
	}
}

func TestStructureNotDestroyedByAbsence(t *testing.T) {
	tr := newTestTracker(t)
	unit := "npc_dota_goodguys_tower1_mid"
	seen := func(gt int, present bool, ev *observe.Event) EnemyStructureTrack {
		b := batchAt(gt, nil)
		if present {
			pos := observe.Vec2{X: 10, Y: 20}
			b.Structures = []observe.StructureObservation{{
				UnitName: unit, Team: observe.TeamRadiant,
				Class: observe.StructureTowerT1, Lane: observe.LaneMid, Position: &pos,
			}}
		}
		if ev != nil {
			b.Events = []observe.Event{*ev}
		}
		tr.Apply(b)
		st := tr.Snapshot()
		for _, s := range st.EnemyStructures {
			if s.UnitName == unit {
				return s
			}
		}
		t.Fatalf("structure missing")
		return EnemyStructureTrack{}
	}
	if s := seen(1, true, nil); s.Life != StructureSeenAlive {
		t.Fatalf("seen = %s", s.Life)
	}
	if s := seen(2, false, nil); s.Life != StructureSeenAlive {
		t.Fatalf("absent must stay SEEN_ALIVE, got %s", s.Life)
	}
	building := unit
	ev := observe.Event{Kind: observe.EventTowerKill, InnerType: "CHAT_MESSAGE_TOWER_KILL", Building: &building, Team: observe.TeamRadiant}
	if s := seen(3, false, &ev); s.Life != StructureDestroyed {
		t.Fatalf("after kill = %s", s.Life)
	}
}

func TestRoshanDisappearanceIsNotDeath(t *testing.T) {
	tr := newTestTracker(t)
	apply := func(gt int, pos *observe.Vec2) RoshanTrack {
		b := batchAt(gt, nil)
		if pos != nil {
			b.Roshan = []observe.RoshanObservation{{Position: *pos, Team: observe.TeamNeutral}}
		}
		tr.Apply(b)
		return tr.Snapshot().Roshan
	}
	A := observe.Vec2{X: 1, Y: 1}
	B := observe.Vec2{X: 2, Y: 2}
	if r := apply(1, nil); r.VisibleNow {
		t.Fatal("absent")
	}
	if r := apply(2, &A); !r.VisibleNow || !r.LastSeenAlive {
		t.Fatalf("%+v", r)
	}
	if r := apply(3, &B); !r.LastSeenPositions[0].Equal(B) {
		t.Fatalf("%+v", r)
	}
	r := apply(4, nil)
	if r.VisibleNow {
		t.Fatal("want hidden")
	}
	if !r.LastSeenPositions[0].Equal(B) || !r.LastSeenAlive || r.KnownDeadAt != nil {
		t.Fatalf("disappearance must not be death: %+v", r)
	}
}

func TestSnapshotDoesNotAliasInternalSlices(t *testing.T) {
	tr := newTestTracker(t)
	pos := observe.Vec2{X: 9, Y: 9}
	tr.Apply(batchAt(1, []observe.HeroPosition{{
		Hero: "npc_dota_hero_luna", Team: observe.TeamRadiant, Position: pos,
		Image: observe.ImageEnemyIcon, ImageKind: observe.HeroImageEnemy,
	}}))
	s1 := tr.Snapshot()
	s1.Enemies[0].VisiblePositions[0].X = 0
	s2 := tr.Snapshot()
	if s2.Enemies[0].VisiblePositions[0].X != 9 {
		t.Fatal("snapshot aliased internal state")
	}
}

func TestMatchIDChangeResets(t *testing.T) {
	tr := newTestTracker(t)
	pos := observe.Vec2{X: 1, Y: 1}
	tr.Apply(batchAt(1, []observe.HeroPosition{{
		Hero: "npc_dota_hero_luna", Team: observe.TeamRadiant, Position: pos,
		Image: observe.ImageEnemyIcon, ImageKind: observe.HeroImageEnemy,
	}}))
	b := batchAt(2, nil)
	b.Match.MatchID = strPtr("m2")
	tr.Apply(b)
	st := tr.Snapshot()
	if st.Clock.MatchID != "m2" {
		t.Fatalf("match %s", st.Clock.MatchID)
	}
	// roster reloaded from provider; last seen must be cleared
	for _, e := range st.Enemies {
		if e.Hero == "npc_dota_hero_luna" && e.LastSeenAt != nil {
			t.Fatalf("last seen survived reset: %+v", e)
		}
	}
}

func TestTowerKillWithoutBuildingIsNotConfirmed(t *testing.T) {
	tr := newTestTracker(t)
	unit := "npc_dota_goodguys_tower1_mid"
	pos := observe.Vec2{X: 10, Y: 20}
	b := batchAt(1, nil)
	b.Structures = []observe.StructureObservation{{
		UnitName: unit, Team: observe.TeamRadiant,
		Class: observe.StructureTowerT1, Lane: observe.LaneMid, Position: &pos,
	}}
	tr.Apply(b)
	ev := observe.Event{Kind: observe.EventTowerKill, InnerType: "CHAT_MESSAGE_TOWER_KILL", Team: observe.TeamRadiant}
	b2 := batchAt(2, nil)
	b2.Events = []observe.Event{ev}
	tr.Apply(b2)
	st := tr.Snapshot()
	var got EnemyStructureTrack
	for _, x := range st.EnemyStructures {
		if x.UnitName == unit {
			got = x
		}
	}
	if got.Life == StructureConfirmedDestroyed || got.Life == StructureDestroyed {
		t.Fatalf("ambiguous tower kill must not be CONFIRMED_DESTROYED, got %s", got.Life)
	}
	if got.Life != StructureLikelyDestroyed {
		t.Fatalf("want LIKELY_DESTROYED, got %s", got.Life)
	}
}

func TestStructureReappearsAfterConfirmed(t *testing.T) {
	tr := newTestTracker(t)
	unit := "npc_dota_goodguys_tower1_mid"
	pos := observe.Vec2{X: 10, Y: 20}
	b := batchAt(1, nil)
	b.Structures = []observe.StructureObservation{{
		UnitName: unit, Team: observe.TeamRadiant,
		Class: observe.StructureTowerT1, Position: &pos,
	}}
	tr.Apply(b)
	building := unit
	ev := observe.Event{Kind: observe.EventTowerKill, Building: &building, Team: observe.TeamRadiant}
	b2 := batchAt(2, nil)
	b2.Events = []observe.Event{ev}
	tr.Apply(b2)
	b3 := batchAt(3, nil)
	b3.Structures = []observe.StructureObservation{{
		UnitName: unit, Team: observe.TeamRadiant,
		Class: observe.StructureTowerT1, Position: &pos,
	}}
	tr.Apply(b3)
	st := tr.Snapshot()
	for _, x := range st.EnemyStructures {
		if x.UnitName == unit && x.Life != StructureSeenAlive {
			t.Fatalf("minimap observation must restore SEEN_ALIVE, got %s", x.Life)
		}
	}
}

func TestApplyWithoutRosterDoesNotPanic(t *testing.T) {
	tr := New(Options{Log: func(string) {}})
	pos := observe.Vec2{X: 1, Y: 1}
	tr.Apply(batchAt(1, []observe.HeroPosition{{
		Hero: "npc_dota_hero_luna", Team: observe.TeamRadiant, Position: pos,
		Image: observe.ImageEnemyIcon, ImageKind: observe.HeroImageEnemy,
	}}))
	st := tr.Snapshot()
	if len(st.Enemies) != 0 {
		t.Fatalf("no roster: enemies = %d", len(st.Enemies))
	}
}

func TestMatchResetDropsOldRosterWhenProviderEmpty(t *testing.T) {
	p := &switchRoster{r: testRoster(), ok: true}
	tr := New(Options{Roster: p, Log: func(string) {}})
	pos := observe.Vec2{X: 1, Y: 1}
	tr.Apply(batchAt(1, []observe.HeroPosition{{
		Hero: "npc_dota_hero_luna", Team: observe.TeamRadiant, Position: pos,
		Image: observe.ImageEnemyIcon, ImageKind: observe.HeroImageEnemy,
	}}))
	p.ok = false
	b := batchAt(2, nil)
	b.Match.MatchID = strPtr("m2")
	tr.Apply(b)
	st := tr.Snapshot()
	if st.Clock.MatchID != "m2" {
		t.Fatalf("match %s", st.Clock.MatchID)
	}
	if len(st.Enemies) != 0 {
		t.Fatalf("old roster reused: %+v", st.Enemies)
	}
}

type switchRoster struct {
	r  Roster
	ok bool
}

func (s *switchRoster) Roster() (Roster, bool) {
	if !s.ok {
		return Roster{}, false
	}
	return s.r, s.r.Valid()
}

func TestSnapshotConcurrentWithApply(t *testing.T) {
	tr := newTestTracker(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		pos := observe.Vec2{X: 1, Y: 1}
		for i := 0; i < 200; i++ {
			hs := []observe.HeroPosition{{
				Hero: "npc_dota_hero_luna", Team: observe.TeamRadiant, Position: pos,
				Image: observe.ImageEnemyIcon, ImageKind: observe.HeroImageEnemy,
			}}
			if i%3 == 0 {
				hs = nil
			}
			tr.Apply(batchAt(i, hs))
		}
	}()
	for i := 0; i < 200; i++ {
		st := tr.Snapshot()
		_ = st.FormatLive()
	}
	<-done
}

func TestFormatLiveCompact(t *testing.T) {
	tr := newTestTracker(t)
	pos := observe.Vec2{X: -2561, Y: -1131}
	tr.Apply(batchAt(10, []observe.HeroPosition{{
		Hero: "npc_dota_hero_luna", Team: observe.TeamRadiant, Position: pos,
		Image: observe.ImageEnemyIcon, ImageKind: observe.HeroImageEnemy,
	}}))
	tr.Apply(batchAt(14, nil))
	text := tr.Snapshot().FormatLive()
	if !containsAll(text, "Luna", "missing", "last=") {
		t.Fatalf("format:\n%s", text)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !stringContains(s, p) {
			return false
		}
	}
	return true
}

func stringContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
