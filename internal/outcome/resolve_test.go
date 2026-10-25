package outcome

import (
	"context"
	"errors"
	"testing"

	"cmnet/internal/draft"
	"cmnet/internal/sources/opendota"
)

type fakeSrc struct {
	match   map[int64]*opendota.Match
	matchErr map[int64]error
	recent  map[int64][]opendota.PlayerMatch
}

func (f *fakeSrc) Match(_ context.Context, id int64) (*opendota.Match, error) {
	if err := f.matchErr[id]; err != nil {
		return nil, err
	}
	return f.match[id], nil
}

func (f *fakeSrc) RecentMatches(_ context.Context, accountID int64) ([]opendota.PlayerMatch, error) {
	return f.recent[accountID], nil
}

func TestResolveByMatchID(t *testing.T) {
	src := &fakeSrc{match: map[int64]*opendota.Match{
		10: {
			MatchID: 10, Duration: 2400, RadiantWin: true,
			PickBans: []opendota.PickBan{
				{IsPick: true, Team: 0, HeroID: 1},
				{IsPick: true, Team: 0, HeroID: 2},
				{IsPick: true, Team: 1, HeroID: 3},
			},
		},
	}}
	got, err := Resolve(context.Background(), src, Query{
		MatchID: 10, OurTeam: draft.Radiant,
		RadiantPicks: []int{2, 1}, DirePicks: []int{3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Win || got.MatchID != 10 {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveByMatchIDPending(t *testing.T) {
	src := &fakeSrc{match: map[int64]*opendota.Match{
		10: {MatchID: 10, Duration: 0},
	}}
	_, err := Resolve(context.Background(), src, Query{MatchID: 10, OurTeam: draft.Radiant})
	if !errors.Is(err, ErrPending) {
		t.Fatalf("err = %v, want pending", err)
	}
}

func TestResolveByAlliesAndBoard(t *testing.T) {
	src := &fakeSrc{
		recent: map[int64][]opendota.PlayerMatch{
			7: {{MatchID: 99, StartTime: 1000, HeroID: 5, Duration: 2000, RadiantWin: false}},
		},
		match: map[int64]*opendota.Match{
			99: {
				MatchID: 99, Duration: 2000, RadiantWin: false,
				PickBans: []opendota.PickBan{
					{IsPick: true, Team: 0, HeroID: 1},
					{IsPick: true, Team: 1, HeroID: 5},
					{IsPick: true, Team: 1, HeroID: 6},
				},
			},
		},
	}
	got, err := Resolve(context.Background(), src, Query{
		OurTeam: draft.Dire, AllyIDs: []int64{7},
		RadiantPicks: []int{1}, DirePicks: []int{6, 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Win { // dire won because radiant_win=false
		t.Fatalf("want win for dire, got %+v", got)
	}
}

func TestParseMatchID(t *testing.T) {
	id, err := ParseMatchID("8123456789")
	if err != nil || id != 8123456789 {
		t.Fatalf("got %d %v", id, err)
	}
	id, err = ParseMatchID("")
	if err != nil || id != 0 {
		t.Fatalf("empty: %d %v", id, err)
	}
}

func TestSameHeroesOrderIndependent(t *testing.T) {
	if !sameHeroes([]int{3, 1, 2}, []int{1, 2, 3}) {
		t.Fatal("expected equal")
	}
	if sameHeroes([]int{1, 2}, []int{1, 3}) {
		t.Fatal("expected unequal")
	}
}
