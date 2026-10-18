package track

import (
	"testing"

	"cmnet/internal/gsi/observe"
)

func TestKeepLastSelfWhenHeroSectionEmpty(t *testing.T) {
	tr := newTestTracker(t)
	name := "npc_dota_hero_mirana"
	pos := observe.Vec2{X: 5285, Y: -3553}
	b := batchAt(10, nil)
	b.Self.Present = true
	b.Self.HeroName = &name
	b.Self.Position = &pos
	tr.Apply(b)
	empty := batchAt(11, nil)
	empty.Self.Present = true
	tr.Apply(empty)
	st := tr.Snapshot()
	if st.Self.HeroName == nil || *st.Self.HeroName != name {
		t.Fatalf("self wiped: %+v", st.Self)
	}
	if st.Self.Position == nil || !st.Self.Position.Equal(pos) {
		t.Fatalf("self pos wiped: %+v", st.Self.Position)
	}
}
