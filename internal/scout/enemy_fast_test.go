package scout

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// enemyFastSource detects accidental smurf/history requests. Normal scouting
// must use hero-affinity only, even when asked to scout an entire enemy team.
type enemyFastSource struct {
	active atomic.Int32
	peak atomic.Int32
	heroCalls atomic.Int32
	recentCalls atomic.Int32
	draftCalls atomic.Int32
}

func (s *enemyFastSource) PlayerHeroes(ctx context.Context, id int64, days int) ([]PlayerHero, error) {
	if days == 0 {
		return nil, errors.New("career lookup forbidden on fast path")
	}
	s.heroCalls.Add(1)
	n := s.active.Add(1)
	for {
		v := s.peak.Load()
		if n <= v || s.peak.CompareAndSwap(v, n) { break }
	}
	defer s.active.Add(-1)
	select {
	case <-ctx.Done(): return nil, ctx.Err()
	case <-time.After(10*time.Millisecond):
		return []PlayerHero{{HeroID:int(id), Games:10, Wins:6}}, nil
	}
}
func (s *enemyFastSource) RecentMatches(context.Context, int64) ([]MatchRef, error) {
	s.recentCalls.Add(1)
	return nil, nil
}
func (s *enemyFastSource) MatchDraft(context.Context, int64) (*MatchDraft, error) {
	s.draftCalls.Add(1)
	return nil, nil
}

func TestEnemyFastAffinityConcurrentAndNoSmurfRequests(t *testing.T) {
	src := &enemyFastSource{}
	players := []ResolvedPlayer{
		{AccountID:5, Resolved:true}, {AccountID:3, Resolved:true},
		{AccountID:1, Resolved:true}, {AccountID:4, Resolved:true},
		{AccountID:2, Resolved:true},
	}
	got := BuildLayer2Concurrent(context.Background(), src, players, 30, nil)
	if len(got.Players) != 5 { t.Fatalf("players=%d", len(got.Players)) }
	for i, p := range got.Players {
		if p.AccountID != players[i].AccountID || p.Status != "ok" ||
			len(p.Heroes) != 1 || p.Smurf != nil {
			t.Fatalf("row %d not deterministic or unexpected smurf: %+v", i, p)
		}
	}
	if src.heroCalls.Load()!=5 || src.recentCalls.Load()!=0 || src.draftCalls.Load()!=0 {
		t.Fatalf("unexpected calls: heroes=%d recent=%d match=%d",
			src.heroCalls.Load(),src.recentCalls.Load(),src.draftCalls.Load())
	}
	if src.peak.Load() <= 1 || src.peak.Load() > 4 {
		t.Fatalf("expected bounded parallelism, got peak=%d", src.peak.Load())
	}
}

func TestNormalLayer2NoSmurfCareerOrHistory(t *testing.T) {
	src := &enemyFastSource{}
	got := BuildLayer2(context.Background(), src, []ResolvedPlayer{{AccountID:7,Resolved:true}}, 30, nil)
	if len(got.Players)!=1 || got.Players[0].Smurf!=nil || got.Players[0].Status!="ok" {
		t.Fatalf("normal scout unexpectedly detects smurfs: %+v", got)
	}
	if src.heroCalls.Load()!=1 || src.recentCalls.Load()!=0 {
		t.Fatalf("unexpected extra requests: heroes=%d recent=%d",src.heroCalls.Load(),src.recentCalls.Load())
	}
}

type draftFastSource struct {
	enemyFastSource
	matchIDs []int64
}

func (s *draftFastSource) RecentMatches(_ context.Context, _ int64) ([]MatchRef,error) {
	s.recentCalls.Add(1)
	out:=make([]MatchRef,len(s.matchIDs))
	for i,id:=range s.matchIDs {
		out[i]=MatchRef{MatchID:id,StartTime:int64(len(s.matchIDs)-i)}
	}
	return out,nil
}
func (s *draftFastSource) MatchDraft(_ context.Context, id int64) (*MatchDraft,error) {
	s.draftCalls.Add(1)
	return &MatchDraft{MatchID:id,Players:[]Participant{{AccountID:42,Radiant:true}}},nil
}

func TestFastEventScanStopsAtThreeWithoutCM(t *testing.T) {
	src:=&draftFastSource{matchIDs:[]int64{1,2,3,4,5,6,7,8}}
	got:=BuildLayer3Fast(context.Background(),src,42,"",0,nil)
	if got!=nil {t.Fatalf("expected no drafts, got %+v",got)}
	if src.recentCalls.Load()!=1 || src.draftCalls.Load()!=3 {
		t.Fatalf("fast scan exceeded its budget: recent=%d matches=%d",src.recentCalls.Load(),src.draftCalls.Load())
	}
}

type stalledDraftSource struct{ draftFastSource }
func (s *stalledDraftSource) MatchDraft(ctx context.Context, _ int64) (*MatchDraft,error) {
	<-ctx.Done()
	return nil,ctx.Err()
}
func TestFastEventScanRespectsCancellation(t *testing.T) {
	src:=&stalledDraftSource{draftFastSource:draftFastSource{matchIDs:[]int64{1,2,3}}}
	ctx,cancel:=context.WithTimeout(context.Background(),25*time.Millisecond)
	defer cancel()
	start:=time.Now()
	_ = BuildLayer3Fast(ctx,src,42,"",0,nil)
	if time.Since(start)>time.Second {
		t.Fatal("fast event scan ignored context cancellation")
	}
}
