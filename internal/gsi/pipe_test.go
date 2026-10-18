package gsi

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"cmnet/internal/draft"
	"cmnet/internal/gsi/track"
)

func TestPipePreservesSubmitOrder(t *testing.T) {
	tr := track.New(track.Options{Log: func(string) {}})
	p := NewPipe(tr, 32)
	go p.Run()
	t.Cleanup(p.Close)

	const n = 20
	for i := 1; i <= n; i++ {
		body := []byte(`{"map":{"matchid":"order","game_time":` + strconv.Itoa(i) + `}}`)
		p.Submit(time.Unix(int64(i), 0), body)
	}
	waitPipe(t, p, n)

	st := tr.Snapshot()
	if st.Clock.Seq != uint64(n) {
		t.Fatalf("seq = %d, want %d", st.Clock.Seq, n)
	}
	if st.Clock.GameTime == nil || *st.Clock.GameTime != n {
		t.Fatalf("game_time = %v, want %d (processing out of order?)", st.Clock.GameTime, n)
	}
}

func TestPipeDropsWhenFull(t *testing.T) {
	tr := track.New(track.Options{Log: func(string) {}})
	p := NewPipe(tr, 2)
	// Do not start Run: the consumer is stalled.
	for i := 0; i < 10; i++ {
		p.Submit(time.Now(), []byte(`{"map":{"clock_time":1}}`))
	}
	st := p.Stats()
	if st.Received != 10 {
		t.Fatalf("received = %d", st.Received)
	}
	if st.Dropped == 0 {
		t.Fatal("expected drops on a full bounded queue")
	}
	done := make(chan struct{})
	go func() {
		p.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked")
	}
}

func TestPipeSubmitDoesNotBlock(t *testing.T) {
	tr := track.New(track.Options{Log: func(string) {}})
	p := NewPipe(tr, 1)
	p.Submit(time.Now(), []byte(`{"map":{}}`))
	done := make(chan struct{})
	go func() {
		p.Submit(time.Now(), []byte(`{"map":{}}`))
		p.Submit(time.Now(), []byte(`{"map":{}}`))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Submit blocked on a full queue")
	}
	p.Close()
}

func TestPipeRosterNotReadyDoesNotPanic(t *testing.T) {
	tr := track.New(track.Options{
		Roster: SnapshotRoster{
			Last:  func() *draft.Snapshot { return nil },
			Names: track.MapNames{},
		},
		Log: func(string) {},
	})
	p := NewPipe(tr, 8)
	go p.Run()
	t.Cleanup(p.Close)
	p.Submit(time.Now(), []byte(`{"map":{"matchid":"x","game_time":3},"minimap":{"o1":{"unitname":"npc_dota_hero_luna","image":"minimap_enemyicon","xpos":1,"ypos":2,"team":2}}}`))
	waitPipe(t, p, 1)
	_ = tr.Snapshot()
}

func TestPipeStatsConcurrent(t *testing.T) {
	tr := track.New(track.Options{Log: func(string) {}})
	p := NewPipe(tr, 64)
	go p.Run()
	t.Cleanup(p.Close)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				p.Submit(time.Now(), []byte(`{"map":{"game_time":1}}`))
				_ = p.Stats()
			}
		}()
	}
	wg.Wait()
	waitPipe(t, p, 1)
}

func waitPipe(t *testing.T, p *Pipe, processed uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.Stats().Processed >= processed {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("pipe processed = %d, want >= %d", p.Stats().Processed, processed)
}
