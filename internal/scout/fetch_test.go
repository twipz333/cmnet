package scout

import (
	"context"
	"testing"
)

// fakeSource is an in-memory Source for testing assembly without network.
type fakeSource struct {
	heroes  map[int64][]PlayerHero
	recents map[int64][]MatchRef
	drafts  map[int64]*MatchDraft
}

func (f *fakeSource) PlayerHeroes(_ context.Context, id int64, _ int) ([]PlayerHero, error) {
	return f.heroes[id], nil
}
func (f *fakeSource) RecentMatches(_ context.Context, id int64) ([]MatchRef, error) {
	return f.recents[id], nil
}
func (f *fakeSource) MatchDraft(_ context.Context, id int64) (*MatchDraft, error) {
	return f.drafts[id], nil
}

func TestBuildLayer2(t *testing.T) {
	src := &fakeSource{heroes: map[int64][]PlayerHero{
		7: {{HeroID: 1, Games: 10, Wins: 7}, {HeroID: 2, Games: 0, Wins: 0}},
	}}
	players := []ResolvedPlayer{
		{AccountID: 7, Nickname: "a", Resolved: true},
		{Nickname: "unknown", Resolved: false},
	}
	l2 := BuildLayer2(context.Background(), src, players, 30, nil)
	if len(l2.Players) != 2 {
		t.Fatalf("players = %d, want 2", len(l2.Players))
	}
	// Resolved player: hero 1 kept (10 games), hero 2 dropped (0 games).
	if len(l2.Players[0].Heroes) != 1 || l2.Players[0].Heroes[0].HeroID != 1 {
		t.Errorf("unexpected heroes: %+v", l2.Players[0].Heroes)
	}
	if l2.Players[0].Heroes[0].WinRate != 0.7 {
		t.Errorf("winrate = %v, want 0.7", l2.Players[0].Heroes[0].WinRate)
	}
	// Unresolved player kept but empty.
	if l2.Players[1].Resolved || len(l2.Players[1].Heroes) != 0 {
		t.Errorf("unresolved player should have no heroes: %+v", l2.Players[1])
	}
}

func TestBuildLayer3ExtractsEntryTeamDraft(t *testing.T) {
	src := &fakeSource{
		recents: map[int64][]MatchRef{
			7: {{MatchID: 100, StartTime: 2000}, {MatchID: 99, StartTime: 500}},
		},
		drafts: map[int64]*MatchDraft{
			100: {
				MatchID:    100,
				RadiantWin: true,
				Players:    []Participant{{AccountID: 7, Radiant: false}}, // entry is on dire
				Entries: []DraftEntry{
					{HeroID: 10, IsPick: true, Team: 0},  // radiant pick (ignored)
					{HeroID: 20, IsPick: true, Team: 1},  // dire pick (kept)
					{HeroID: 21, IsPick: false, Team: 1}, // dire ban (kept)
				},
			},
		},
	}
	// eventSince=1000 excludes match 99 (start 500).
	l3 := BuildLayer3(context.Background(), src, 7, "evt", 1000, 5, nil)
	if l3 == nil {
		t.Fatal("nil layer3")
	}
	if l3.DraftsObserved != 1 {
		t.Fatalf("drafts observed = %d, want 1 (match 99 excluded by window)", l3.DraftsObserved)
	}
	td := l3.TeamDrafts[0]
	if td.Radiant {
		t.Error("entry team should be dire")
	}
	if len(td.Picks) != 1 || td.Picks[0] != 20 {
		t.Errorf("picks = %v, want [20]", td.Picks)
	}
	if len(td.Bans) != 1 || td.Bans[0] != 21 {
		t.Errorf("bans = %v, want [21]", td.Bans)
	}
}

func TestBuildLayer3NilWhenEntryNotInMatch(t *testing.T) {
	src := &fakeSource{
		recents: map[int64][]MatchRef{7: {{MatchID: 100, StartTime: 2000}}},
		drafts: map[int64]*MatchDraft{
			100: {MatchID: 100, Players: []Participant{{AccountID: 999, Radiant: true}},
				Entries: []DraftEntry{{HeroID: 1, IsPick: true, Team: 0}}},
		},
	}
	if l3 := BuildLayer3(context.Background(), src, 7, "evt", 0, 5, nil); l3 != nil {
		t.Errorf("expected nil layer3 when entry absent from match, got %+v", l3)
	}
}

// squadSource is a two-match event: the entry player (7) plays with 8 and 9 in the
// first match and with 8 only in the second, plus an anonymous participant.
func squadSource() *fakeSource {
	return &fakeSource{
		heroes: map[int64][]PlayerHero{
			7: {{HeroID: 1, Games: 10, Wins: 6}},
			8: {{HeroID: 2, Games: 8, Wins: 5}},
			9: {{HeroID: 3, Games: 4, Wins: 1}},
		},
		recents: map[int64][]MatchRef{
			7: {{MatchID: 100, StartTime: 2000}, {MatchID: 101, StartTime: 3000}},
		},
		drafts: map[int64]*MatchDraft{
			100: {
				MatchID: 100,
				Players: []Participant{
					{AccountID: 7, Radiant: false}, {AccountID: 8, Radiant: false},
					{AccountID: 9, Radiant: false}, {AccountID: 0, Radiant: false}, // anonymous
					{AccountID: 500, Radiant: true}, // other team, must be ignored
				},
				Entries: []DraftEntry{{HeroID: 20, IsPick: true, Team: 1}},
			},
			101: {
				MatchID: 101,
				Players: []Participant{
					{AccountID: 7, Radiant: false}, {AccountID: 8, Radiant: false},
					{AccountID: 501, Radiant: true},
				},
				Entries: []DraftEntry{{HeroID: 21, IsPick: true, Team: 1}},
			},
		},
	}
}

func TestBuildLayer3RecoversTeammates(t *testing.T) {
	l3 := BuildLayer3(context.Background(), squadSource(), 7, "evt", 0, 5, nil)
	if l3 == nil {
		t.Fatal("nil layer3")
	}
	// 8 appears in both matches, 9 in one -> 8 first. The anonymous participant
	// (id 0) and the opposing side are excluded.
	want := []int64{8, 9}
	if len(l3.TeamAccountIDs) != len(want) {
		t.Fatalf("teammates = %v, want %v", l3.TeamAccountIDs, want)
	}
	for i, id := range want {
		if l3.TeamAccountIDs[i] != id {
			t.Errorf("teammate[%d] = %d, want %d (order is frequency desc, id asc)", i, l3.TeamAccountIDs[i], id)
		}
	}
}

func TestFetchScoutsDiscoveredTeammates(t *testing.T) {
	players := []ResolvedPlayer{{AccountID: 7, Nickname: "a", Resolved: true}}
	snap, err := Fetch(context.Background(), squadSource(), players, FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// One named player plus the two recovered from the team's own match records.
	if len(snap.Layer2.Players) != 3 {
		t.Fatalf("layer2 players = %d, want 3: %+v", len(snap.Layer2.Players), snap.Layer2.Players)
	}
	for _, p := range snap.Layer2.Players[1:] {
		if !p.Discovered {
			t.Errorf("account %d should be marked discovered", p.AccountID)
		}
		if len(p.Heroes) == 0 {
			t.Errorf("account %d discovered but no affinity fetched", p.AccountID)
		}
	}
	if snap.Layer2.Players[0].Discovered {
		t.Error("the player the user named must not be marked discovered")
	}
}

func TestFetchDoesNotRefetchNamedTeammates(t *testing.T) {
	players := []ResolvedPlayer{
		{AccountID: 7, Nickname: "a", Resolved: true},
		{AccountID: 8, Nickname: "b", Resolved: true},
	}
	snap, err := Fetch(context.Background(), squadSource(), players, FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 7 and 8 named, only 9 left to discover — no duplicate row for 8.
	if len(snap.Layer2.Players) != 3 {
		t.Fatalf("layer2 players = %d, want 3: %+v", len(snap.Layer2.Players), snap.Layer2.Players)
	}
	seen := map[int64]int{}
	for _, p := range snap.Layer2.Players {
		seen[p.AccountID]++
	}
	if seen[8] != 1 {
		t.Errorf("account 8 appears %d times, want 1", seen[8])
	}
}

func TestFetchDiscoveryCapAndDisable(t *testing.T) {
	players := []ResolvedPlayer{{AccountID: 7, Nickname: "a", Resolved: true}}

	capped, err := Fetch(context.Background(), squadSource(), players, FetchOptions{MaxDiscovered: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(capped.Layer2.Players) != 2 {
		t.Errorf("with cap 1: layer2 players = %d, want 2", len(capped.Layer2.Players))
	}

	off, err := Fetch(context.Background(), squadSource(), players, FetchOptions{MaxDiscovered: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(off.Layer2.Players) != 1 {
		t.Errorf("with discovery disabled: layer2 players = %d, want 1", len(off.Layer2.Players))
	}
	// Layer 3 itself must survive discovery being off.
	if off.Layer3 == nil || off.Layer3.DraftsObserved == 0 {
		t.Error("disabling discovery must not disable layer 3")
	}
}

// Discovery must not make the snapshot hash depend on lookup order: teammates are
// ranked by a total order, so two teammates freezing the same event agree (§6).
func TestDiscoveryIsDeterministic(t *testing.T) {
	players := []ResolvedPlayer{{AccountID: 7, Nickname: "a", Resolved: true}}
	var first string
	for i := 0; i < 5; i++ {
		snap, err := Fetch(context.Background(), squadSource(), players, FetchOptions{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		snap.FrozenAt = "" // wall clock is not part of what we are comparing
		v, err := computeVersion(snap)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = v
			continue
		}
		if v != first {
			t.Fatalf("run %d hashed to %s, want %s", i, v, first)
		}
	}
}

func TestFetchDegradesWithoutResolvedEntry(t *testing.T) {
	src := &fakeSource{}
	players := []ResolvedPlayer{{Nickname: "x", Resolved: false}}
	snap, err := Fetch(context.Background(), src, players, FetchOptions{}, nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	// Layer 2 present (with the unresolved player recorded), layer 3 absent.
	if snap.Layer3 != nil {
		t.Error("layer3 should be nil with no resolved entry")
	}
}

func TestFetchAssemblesBothLayers(t *testing.T) {
	src := &fakeSource{
		heroes:  map[int64][]PlayerHero{7: {{HeroID: 1, Games: 10, Wins: 6}}},
		recents: map[int64][]MatchRef{7: {{MatchID: 100, StartTime: 2000}}},
		drafts: map[int64]*MatchDraft{
			100: {MatchID: 100, Players: []Participant{{AccountID: 7, Radiant: true}},
				Entries: []DraftEntry{{HeroID: 1, IsPick: true, Team: 0}}},
		},
	}
	players := []ResolvedPlayer{{AccountID: 7, Nickname: "a", Resolved: true}}
	snap, err := Fetch(context.Background(), src, players, FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Layer2 == nil || snap.Layer3 == nil {
		t.Fatalf("both layers expected: l2=%v l3=%v", snap.Layer2, snap.Layer3)
	}
	if snap.Layer3.DraftsObserved != 1 {
		t.Errorf("layer3 drafts = %d, want 1", snap.Layer3.DraftsObserved)
	}
}


func TestBuildLayer3RecoversTeammatesWithoutDraft(t *testing.T) {
	src := &fakeSource{
		recents: map[int64][]MatchRef{7: {{MatchID: 50, StartTime: 2000}}},
		drafts: map[int64]*MatchDraft{
			50: {
				MatchID: 50,
				Players: []Participant{
					{AccountID: 7, Radiant: true},
					{AccountID: 8, Radiant: true},
					{AccountID: 9, Radiant: true},
				},
				// No Entries: AP / empty picks_bans must still reveal the squad.
			},
		},
	}
	l3 := BuildLayer3(context.Background(), src, 7, "evt", 0, 5, nil)
	if l3 == nil {
		t.Fatal("nil layer3: expected teammate shell even without drafts")
	}
	if l3.DraftsObserved != 0 {
		t.Fatalf("drafts = %d, want 0", l3.DraftsObserved)
	}
	want := []int64{8, 9}
	if len(l3.TeamAccountIDs) != len(want) {
		t.Fatalf("teammates = %v, want %v", l3.TeamAccountIDs, want)
	}
}

func TestFetchDiscoversWithoutPublishingEmptyLayer3(t *testing.T) {
	src := &fakeSource{
		heroes: map[int64][]PlayerHero{
			7: {{HeroID: 1, Games: 5, Wins: 3}},
			8: {{HeroID: 2, Games: 4, Wins: 2}},
		},
		recents: map[int64][]MatchRef{7: {{MatchID: 50, StartTime: 2000}}},
		drafts: map[int64]*MatchDraft{
			50: {
				MatchID: 50,
				Players: []Participant{
					{AccountID: 7, Radiant: true}, {AccountID: 8, Radiant: true},
				},
			},
		},
	}
	snap, err := Fetch(context.Background(), src, []ResolvedPlayer{{AccountID: 7, Resolved: true}}, FetchOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Layer3 != nil {
		t.Fatalf("Layer3 = %+v, want nil when no event drafts", snap.Layer3)
	}
	if len(snap.Layer2.Players) != 2 {
		t.Fatalf("layer2 = %d, want 2 (named+discovered)", len(snap.Layer2.Players))
	}
}

func TestExpandWithTeammates(t *testing.T) {
	src := squadSource()
	out := ExpandWithTeammates(context.Background(), src,
		[]ResolvedPlayer{{AccountID: 7, Nickname: "a", Resolved: true}}, 30, 4, nil)
	if out == nil || len(out.Players) != 3 {
		t.Fatalf("players = %v, want 3", out)
	}
}
