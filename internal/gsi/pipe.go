package gsi

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/lanestate"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/track"
)

// DefaultPipeBuffer holds ~25s of GSI at ~10 Hz. Full queue drops the newest
// payload rather than blocking Dota or growing without bound.
const DefaultPipeBuffer = 256

type rawBody struct {
	at   time.Time
	body []byte
}

// PipeStats is a snapshot of pipe counters.
type PipeStats struct {
	Received      uint64
	Processed     uint64
	ExtractErrors uint64
	ApplyErrors   uint64
	Dropped       uint64
}

// Pipe serializes GSI payloads onto one goroutine: extract then track.
type Pipe struct {
	Extractor observe.Extractor
	Tracker   *track.Tracker
	Lanes     *lanestate.Tracker
	LaneGeo   *worldmap.LaneGeometry
	Log       func(string)

	ch     chan rawBody
	mu     sync.Mutex
	closed bool

	received      uint64
	processed     uint64
	extractErrors uint64
	applyErrors   uint64
	dropped       uint64
	dropLogN      uint64
}

// NewPipe returns a buffered serial pipe. Start the worker with Run.
func NewPipe(tr *track.Tracker, buf int) *Pipe {
	if buf < 1 {
		buf = DefaultPipeBuffer
	}
	return &Pipe{Tracker: tr, ch: make(chan rawBody, buf)}
}

// Submit queues a payload after the HTTP ACK. It does not block: a full
// buffer increments Dropped and returns.
func (p *Pipe) Submit(at time.Time, body []byte) {
	if p == nil {
		return
	}
	atomic.AddUint64(&p.received, 1)
	p.mu.Lock()
	ch := p.ch
	closed := p.closed
	p.mu.Unlock()
	if closed || ch == nil {
		atomic.AddUint64(&p.dropped, 1)
		return
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	ev := rawBody{at: at, body: cp}
	select {
	case ch <- ev:
		return
	default:
		atomic.AddUint64(&p.dropped, 1)
		n := atomic.AddUint64(&p.dropLogN, 1)
		if n == 1 || n%100 == 0 {
			p.logf("gsi pipe: dropped payload (queue full, total %d)", atomic.LoadUint64(&p.dropped))
		}
	}
}

// Run consumes the queue until Close. Call from a single goroutine.
func (p *Pipe) Run() {
	var seq uint64
	for ev := range p.ch {
		p.handle(ev, &seq)
	}
}

func (p *Pipe) handle(ev rawBody, seq *uint64) {
	defer func() {
		if rec := recover(); rec != nil {
			atomic.AddUint64(&p.applyErrors, 1)
			p.logf("gsi pipe: tracker panic: %v", rec)
		}
	}()
	*seq++
	batch, err := p.Extractor.Extract(ev.body, observe.ExtractOptions{
		ReceivedAt: ev.at,
		Seq:        *seq,
	})
	if err != nil {
		atomic.AddUint64(&p.extractErrors, 1)
		if p.Tracker != nil {
			p.Tracker.NoteParseError()
		}
		return
	}
	if p.Tracker != nil {
		p.Tracker.Apply(batch)
		if p.Lanes != nil && p.LaneGeo != nil {
			st := p.Tracker.Snapshot()
			p.Lanes.Apply(lanestate.Build(st, p.LaneGeo))
		}
	}
	atomic.AddUint64(&p.processed, 1)
}

// Close stops Run. The HTTP server must already have stopped submitting.
func (p *Pipe) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.ch == nil {
		return
	}
	p.closed = true
	close(p.ch)
}

// Stats returns counters. Safe from any goroutine.
func (p *Pipe) Stats() PipeStats {
	if p == nil {
		return PipeStats{}
	}
	return PipeStats{
		Received:      atomic.LoadUint64(&p.received),
		Processed:     atomic.LoadUint64(&p.processed),
		ExtractErrors: atomic.LoadUint64(&p.extractErrors),
		ApplyErrors:   atomic.LoadUint64(&p.applyErrors),
		Dropped:       atomic.LoadUint64(&p.dropped),
	}
}

func (p *Pipe) logf(format string, args ...any) {
	if p == nil || p.Log == nil {
		return
	}
	p.Log(fmt.Sprintf(format, args...))
}
