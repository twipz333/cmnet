package share

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Host keeps one code open and serves every guest that comes with it.
type Host struct {
	Transports []Transport
	// Build makes the payload; it runs again for every guest, so what they get
	// is always what the host has now.
	Build func() (Payload, error)
	// Changed is called after any status change (for pushing to the page).
	Changed func()

	mu     sync.Mutex
	code   string
	cancel context.CancelFunc
	gen    int // bumped by Refresh: attempts built from an older payload restart
	served int
	last   time.Time
	lastBy string
	err    string
	since  time.Time
	stale  map[int]context.CancelFunc // waiting attempts by id, cancelled on Refresh
	nextID int
}

// HostStatus is what the page shows to the host.
type HostStatus struct {
	Active     bool      `json:"active"`
	Code       string    `json:"code,omitempty"`
	Since      time.Time `json:"since,omitempty"`
	Served     int       `json:"served"`
	LastServed time.Time `json:"lastServed,omitempty"`
	LastVia    string    `json:"lastVia,omitempty"`
	Error      string    `json:"error,omitempty"`
}

func (h *Host) Status() HostStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HostStatus{Active: h.cancel != nil, Code: Format(h.code), Since: h.since,
		Served: h.served, LastServed: h.last, LastVia: h.lastBy, Error: h.err}
}

func (h *Host) changed() {
	if h.Changed != nil {
		h.Changed()
	}
}

// Start opens a code (keeping the current one if already open).
func (h *Host) Start(parent context.Context) HostStatus {
	h.mu.Lock()
	if h.cancel != nil {
		h.mu.Unlock()
		return h.Status()
	}
	ctx, cancel := context.WithCancel(parent)
	h.code, _ = Normalize(NewCode())
	h.cancel, h.since, h.served, h.err, h.last, h.lastBy = cancel, time.Now(), 0, "", time.Time{}, ""
	h.stale = map[int]context.CancelFunc{}
	code := h.code
	h.mu.Unlock()
	for _, t := range h.Transports {
		go h.loop(ctx, t, code)
	}
	h.changed()
	return h.Status()
}

// Stop closes the code; a guest that types it afterwards gets nothing.
func (h *Host) Stop() {
	h.mu.Lock()
	if h.cancel != nil {
		h.cancel()
	}
	h.cancel, h.code = nil, ""
	h.mu.Unlock()
	h.changed()
}

// Refresh makes waiting attempts rebuild their payload (new scouting, new
// bundle). A transfer already under way finishes with what it has.
func (h *Host) Refresh() {
	h.mu.Lock()
	h.gen++
	for _, c := range h.stale {
		c()
	}
	h.stale = map[int]context.CancelFunc{}
	h.mu.Unlock()
}

func (h *Host) loop(ctx context.Context, t Transport, code string) {
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		h.mu.Lock()
		gen := h.gen
		h.mu.Unlock()
		p, err := h.Build()
		var blob []byte
		if err == nil {
			p.Meta.SharedAt = time.Now().UTC()
			blob, err = Seal(code, p)
		}
		if err != nil {
			h.fail(err)
			sleep(ctx, 10*time.Second)
			continue
		}
		actx, acancel := context.WithCancel(ctx)
		h.mu.Lock()
		id := h.nextID
		h.nextID++
		if h.stale != nil {
			h.stale[id] = acancel
		}
		outdated := h.gen != gen // refreshed while this payload was being built
		h.mu.Unlock()
		if outdated {
			acancel()
			continue
		}
		paired := func() {
			h.mu.Lock()
			delete(h.stale, id) // a guest is on the line: do not interrupt
			h.mu.Unlock()
		}
		err = t.Offer(actx, code, blob, paired)
		refreshed := actx.Err() != nil && ctx.Err() == nil
		acancel()
		h.mu.Lock()
		delete(h.stale, id)
		h.mu.Unlock()
		switch {
		case ctx.Err() != nil:
			return
		case refreshed:
			continue // rebuild with the fresh payload
		case err == nil:
			h.mu.Lock()
			h.served++
			h.last, h.lastBy, h.err = time.Now(), t.Name(), ""
			h.mu.Unlock()
			h.changed()
			backoff = 2 * time.Second
		default:
			// One relay failing is normal (a blocked port, a restart); only say
			// so when no guest has ever been served over anything.
			h.fail(err)
			sleep(ctx, backoff)
			if backoff < time.Minute {
				backoff *= 2
			}
		}
	}
}

func (h *Host) fail(err error) {
	h.mu.Lock()
	quiet := h.served > 0
	if !quiet {
		h.err = err.Error()
	}
	h.mu.Unlock()
	if !quiet {
		h.changed()
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// ErrNoHost: nobody answered on this code in time.
var ErrNoHost = errors.New("хост не ответил: проверьте код и что у хоста открыт cmnet с включённой раздачей")

// Receive fetches a payload with code over every transport at once; the first
// that delivers one that opens wins.
func Receive(ctx context.Context, transports []Transport, code string, timeout time.Duration) (Payload, string, error) {
	code, err := Normalize(code)
	if err != nil {
		return Payload{}, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type res struct {
		p   Payload
		via string
		err error
	}
	out := make(chan res, len(transports))
	for _, t := range transports {
		go func(t Transport) {
			for {
				blob, err := t.Receive(ctx, code)
				if err == nil {
					p, err := Open(code, blob)
					out <- res{p, t.Name(), err}
					return
				}
				if ctx.Err() != nil {
					out <- res{err: ctx.Err()}
					return
				}
				sleep(ctx, 2*time.Second) // relay hiccup: try again until the deadline
			}
		}(t)
	}
	var lastErr error
	for range transports {
		r := <-out
		if r.err == nil {
			return r.p, r.via, nil
		}
		if errors.Is(r.err, ErrWrongCode) {
			return Payload{}, "", r.err
		}
		lastErr = r.err
	}
	if errors.Is(lastErr, context.DeadlineExceeded) || errors.Is(lastErr, context.Canceled) {
		return Payload{}, "", ErrNoHost
	}
	return Payload{}, "", lastErr
}
