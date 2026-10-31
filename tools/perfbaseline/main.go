// Command perfbaseline times the local-only scout assembly path using an
// in-memory fixture Source (no network, no OpenDota/STRATZ, no paid APIs).
//
// Cold = first Fetch in-process. Warm = second Fetch against the same fixture.
// This measures CPU/assembly cost of layers 2–3, not live HTTP latency.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"cmnet/internal/scout"
)

type fixtureSource struct {
	heroes  map[int64][]scout.PlayerHero
	recents map[int64][]scout.MatchRef
	drafts  map[int64]*scout.MatchDraft
}

func (f *fixtureSource) PlayerHeroes(_ context.Context, id int64, _ int) ([]scout.PlayerHero, error) {
	return f.heroes[id], nil
}
func (f *fixtureSource) RecentMatches(_ context.Context, id int64) ([]scout.MatchRef, error) {
	return f.recents[id], nil
}
func (f *fixtureSource) MatchDraft(_ context.Context, id int64) (*scout.MatchDraft, error) {
	if d, ok := f.drafts[id]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("match %d not in fixture", id)
}

func fivePlayerFixture() (*fixtureSource, []scout.ResolvedPlayer) {
	players := []scout.ResolvedPlayer{
		{AccountID: 101, Nickname: "p1", Resolved: true},
		{AccountID: 102, Nickname: "p2", Resolved: true},
		{AccountID: 103, Nickname: "p3", Resolved: true},
		{AccountID: 104, Nickname: "p4", Resolved: true},
		{AccountID: 105, Nickname: "p5", Resolved: true},
	}
	src := &fixtureSource{
		heroes: map[int64][]scout.PlayerHero{},
		recents: map[int64][]scout.MatchRef{
			101: {{MatchID: 9001, StartTime: 1_700_000_000}, {MatchID: 9002, StartTime: 1_700_100_000}},
		},
		drafts: map[int64]*scout.MatchDraft{
			9001: {
				MatchID: 9001,
				Players: []scout.Participant{
					{AccountID: 101, Radiant: false}, {AccountID: 102, Radiant: false},
					{AccountID: 103, Radiant: false}, {AccountID: 104, Radiant: false},
					{AccountID: 105, Radiant: false},
				},
				Entries: []scout.DraftEntry{{HeroID: 1, IsPick: true, Team: 1}},
			},
			9002: {
				MatchID: 9002,
				Players: []scout.Participant{
					{AccountID: 101, Radiant: false}, {AccountID: 102, Radiant: false},
					{AccountID: 103, Radiant: false},
				},
				Entries: []scout.DraftEntry{{HeroID: 2, IsPick: true, Team: 1}},
			},
		},
	}
	for _, p := range players {
		src.heroes[p.AccountID] = []scout.PlayerHero{
			{HeroID: 1, Games: 40, Wins: 22},
			{HeroID: 2, Games: 25, Wins: 14},
			{HeroID: 3, Games: 18, Wins: 9},
		}
	}
	return src, players
}

func timeFetchBatch(src scout.Source, players []scout.ResolvedPlayer, opts scout.FetchOptions, iters int) (perCall time.Duration, err error) {
	start := time.Now()
	for i := 0; i < iters; i++ {
		if _, err = scout.Fetch(context.Background(), src, players, opts, nil); err != nil {
			return 0, err
		}
	}
	return time.Since(start) / time.Duration(iters), nil
}

func main() {
	src, players := fivePlayerFixture()
	opts := scout.FetchOptions{
		WindowDays: 30, EventWindow: "perf-baseline-fixture", MaxDiscovered: -1,
	}

	// Cold: first Fetch after process start (includes any first-touch allocs).
	coldStart := time.Now()
	if _, err := scout.Fetch(context.Background(), src, players, opts, nil); err != nil {
		fmt.Fprintf(os.Stderr, "cold fetch: %v\n", err)
		os.Exit(1)
	}
	coldDur := time.Since(coldStart)

	// Warm: batched repeats against the same in-memory fixture (no disk/HTTP
	// cache layer on Source — steady-state assembly cost only). Batch timing
	// avoids 0µs samples from single-call timer granularity.
	const warmIters = 500
	warmPerCall, err := timeFetchBatch(src, players, opts, warmIters)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warm fetch: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"methodology": "in-memory fixture Source; no HTTP/disk. cold=first Fetch in process; warm=mean per-call over batched repeats. Product live scout always calls OpenDotaSource (no warm SQLite short-circuit in Fetch).",
		"players":     5,
		"warm_iters":  warmIters,
		"cold_us":     us(coldDur),
		"cold_ms":     ms(coldDur),
		"warm_us":     us(warmPerCall),
		"warm_ms":     ms(warmPerCall),
		"not_measured": []string{
			"live OpenDota /players/{id}/heroes latency",
			"STRATZ (token / paid-adjacent)",
			"SQLite-backed warm scout (opScout Fetch path does not read affinity cache)",
		},
	})
}

func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func us(d time.Duration) float64 {
	return float64(d) / float64(time.Microsecond)
}
