package session

import (
	"encoding/json"
	"testing"

	"cmnet/internal/draft"
)

// The host's board, sent as JSON and applied on the guest, must be the same
// board: same picks and bans, same step, the guest on the host's side, and the
// guest's own entry locked while it follows.
func TestGuestFollowsHostBoard(t *testing.T) {
	rec := testRecommender(t)
	host := New(Config{FirstPick: draft.Radiant, OurTeam: draft.Dire, Rec: rec})
	for _, id := range []int{1, 2, 3} {
		if err := host.Apply(id); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(host.BoardSnapshot())
	if err != nil {
		t.Fatal(err)
	}

	guest := New(Config{FirstPick: draft.Radiant, OurTeam: draft.Radiant, Rec: rec})
	guest.SetFollowing(true)
	if !guest.GSIMode() || !guest.Following() {
		t.Fatal("following guest is not read-only")
	}
	var snap draft.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	guest.ApplyGSI(&snap)

	hv, gv := host.View(), guest.View()
	for _, team := range []draft.Team{draft.Radiant, draft.Dire} {
		if a, b := hv.Bans(team), gv.Bans(team); len(a) != len(b) {
			t.Fatalf("%v bans: host %v guest %v", team, a, b)
		}
	}
	hs, _ := hv.Current()
	gs, _ := gv.Current()
	if hs != gs {
		t.Fatalf("step: host %+v guest %+v", hs, gs)
	}
	if guest.OurTeam() != draft.Dire {
		t.Fatalf("guest side = %v, want the host's side", guest.OurTeam())
	}
	if err := guest.Apply(4); err == nil {
		t.Fatal("a following guest could enter a hero by hand")
	}

	guest.SetFollowing(false)
	if guest.GSIMode() {
		t.Fatal("stopping following left the board read-only")
	}
	if err := guest.Apply(4); err != nil {
		t.Fatalf("manual entry after following: %v", err)
	}
}
