package scout

import (
    "context"
    "testing"
)

type countingScoutSource struct {
    *fakeSource
    heroCalls int
    recentCalls int
    matchCalls int
}

func (s *countingScoutSource) PlayerHeroes(ctx context.Context, id int64, days int) ([]PlayerHero, error) {
    s.heroCalls++
    return s.fakeSource.PlayerHeroes(ctx, id, days)
}
func (s *countingScoutSource) RecentMatches(ctx context.Context, id int64) ([]MatchRef, error) {
    s.recentCalls++
    return s.fakeSource.RecentMatches(ctx, id)
}
func (s *countingScoutSource) MatchDraft(ctx context.Context, id int64) (*MatchDraft, error) {
    s.matchCalls++
    return s.fakeSource.MatchDraft(ctx, id)
}

func TestFastAllyScoutUsesAtMostTwoMatchDetails(t *testing.T) {
    src := &countingScoutSource{fakeSource: &fakeSource{
        heroes: map[int64][]PlayerHero{
            7: {{HeroID: 1, Games: 10, Wins: 7}},
            8: {{HeroID: 2, Games: 7, Wins: 4}},
            9: {{HeroID: 3, Games: 5, Wins: 3}},
        },
        recents: map[int64][]MatchRef{7: {
            {MatchID: 100, StartTime: 300},
            {MatchID: 101, StartTime: 200},
            {MatchID: 102, StartTime: 100},
        }},
        drafts: map[int64]*MatchDraft{
            100: {MatchID: 100, Players: []Participant{{AccountID: 7, Radiant: true}, {AccountID: 8, Radiant: true}}, Entries: nil},
            101: {MatchID: 101, Players: []Participant{{AccountID: 7, Radiant: true}, {AccountID: 9, Radiant: true}}, Entries: nil},
            102: {MatchID: 102, Players: []Participant{{AccountID: 7, Radiant: true}, {AccountID: 10, Radiant: true}}, Entries: nil},
        },
    }}
    got := ExpandWithTeammates(context.Background(), src, []ResolvedPlayer{{AccountID:7, Resolved:true}}, 30, 4, nil)
    if got == nil || len(got.Players) != 3 {
        t.Fatalf("got %+v, want entry and two recent teammates", got)
    }
    if src.heroCalls != 3 || src.recentCalls != 1 || src.matchCalls != 2 {
        t.Fatalf("calls: heroes=%d recent=%d match=%d (want 3/1/2)", src.heroCalls, src.recentCalls, src.matchCalls)
    }
}
func TestFastAllyScoutSkipsDiscoveryWhenFull(t *testing.T) {
    src := &countingScoutSource{fakeSource: squadSource()}
    got := ExpandWithTeammates(context.Background(), src, []ResolvedPlayer{{AccountID:7, Resolved:true}}, 30, -1, nil)
    if got == nil || len(got.Players) != 1 {
        t.Fatalf("unexpected players: %+v", got)
    }
    if src.heroCalls != 1 || src.recentCalls != 0 || src.matchCalls != 0 {
        t.Fatalf("extra network calls: %+v", src)
    }
}
func TestEnemyScoutCapsNoDraftMatchWalk(t *testing.T) {
    refs := make([]MatchRef, 100)
    drafts := make(map[int64]*MatchDraft)
    for i := range refs {
        id := int64(i+1000)
        refs[i] = MatchRef{MatchID:id, StartTime: int64(100-i)}
        drafts[id] = &MatchDraft{MatchID:id, Players: []Participant{{AccountID:7, Radiant:true}}}
    }
    src := &countingScoutSource{fakeSource: &fakeSource{recents: map[int64][]MatchRef{7:refs}, drafts:drafts}}
    got := BuildLayer3(context.Background(), src, 7, "", 0, 5, nil)
    if got != nil {
        t.Fatalf("no drafts and no teammates should return nil: %+v", got)
    }
    if src.matchCalls != 15 {
        t.Fatalf("queried %d full matches, want hard cap 15", src.matchCalls)
    }
}
