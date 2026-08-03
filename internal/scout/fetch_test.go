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
