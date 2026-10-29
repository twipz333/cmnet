package scout

import (
	"context"
	"errors"
	"testing"
)

type unavailableSource struct{ fakeSource }

func (unavailableSource) PlayerHeroes(context.Context, int64, int) ([]PlayerHero, error) {
	return nil, errors.New("429")
}

func TestScoutStatusDistinguishesEmptyStatsAndRequestFailure(t *testing.T) {
	players := []ResolvedPlayer{{AccountID: 42, Resolved: true}}
	empty := BuildLayer2(context.Background(), &fakeSource{}, players, 30, nil)
	if empty.Players[0].Status != "no_stats" || !empty.Players[0].Resolved {
		t.Fatalf("no-match profile misidentified: %+v", empty.Players[0])
	}
	failed := BuildLayer2(context.Background(), &unavailableSource{}, players, 30, nil)
	if failed.Players[0].Status != "error" || !failed.Players[0].Resolved {
		t.Fatalf("temporary fetch failure misidentified: %+v", failed.Players[0])
	}
}
