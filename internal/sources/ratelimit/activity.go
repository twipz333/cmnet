package ratelimit

import (
	"sort"
	"sync"
	"time"
)

// Activity is what the UI needs to answer "who is not responding".
//
// The old answer was nothing at all: a sync that had been silent for two minutes
// looked exactly like a sync that had stalled, and the user could not tell whether
// STRATZ was slow, OpenDota was rate-limiting us, or the pass was simply between
// bands. All three are normal; being unable to distinguish them is not.
type Activity struct {
	// Source is the API this is about ("STRATZ", "OpenDota").
	Source string `json:"source"`
	// InFlight is how many requests are open right now.
	InFlight int `json:"inFlight"`
	// OldestMs is how long the longest open request has been waiting, or 0 when
	// nothing is open. This is the number that answers "is it hung".
	OldestMs int64 `json:"oldestMs"`
	// WaitingUntilMs is how long we are deliberately sleeping before the next
	// attempt, after a 429 or a Retry-After. Distinct from a slow request: here
	// the server answered and told us to wait.
	WaitingUntilMs int64 `json:"waitingUntilMs"`
	// Counters since the process started.
	Requests    int64 `json:"requests"`
	Retries     int64 `json:"retries"`
	RateLimited int64 `json:"rateLimited"`
	Failures    int64 `json:"failures"`
	// LastStatus is the HTTP status of the most recent response, 0 if none yet.
	LastStatus int `json:"lastStatus"`
	// LastErr is the most recent transport error, if any.
	LastErr string `json:"lastErr,omitempty"`
}

// registry tracks per-source activity for the whole process.
//
// A package-level registry rather than something threaded through every call
// site: the clients are constructed deep inside the source packages and the UI is
// three layers away, and passing an observer down for a diagnostic would touch
// every signature between them for no other benefit.
var registry = struct {
	mu  sync.Mutex
	src map[string]*sourceState
}{src: map[string]*sourceState{}}

type sourceState struct {
	inFlight     map[int64]time.Time // request id -> started
	nextID       int64
	waitingUntil time.Time
	requests     int64
	retries      int64
	rateLimited  int64
	failures     int64
	lastStatus   int
	lastErr      string
}

func stateFor(source string) *sourceState {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	st := registry.src[source]
	if st == nil {
		st = &sourceState{inFlight: map[int64]time.Time{}}
		registry.src[source] = st
	}
	return st
}

// beginRequest records a request going out and returns its id.
func beginRequest(source string) int64 {
	if source == "" {
		return 0
	}
	st := stateFor(source)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	st.nextID++
	id := st.nextID
	st.inFlight[id] = time.Now()
	st.requests++
	return id
}

// endRequest clears a request and records how it went.
func endRequest(source string, id int64, status int, err error) {
	if source == "" || id == 0 {
		return
	}
	st := stateFor(source)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	delete(st.inFlight, id)
	if status > 0 {
		st.lastStatus = status
	}
	if err != nil {
		st.failures++
		st.lastErr = err.Error()
	} else if status > 0 && status < 400 {
		st.lastErr = ""
	}
}

// noteRetry records that we are about to wait before trying again.
func noteRetry(source string, d time.Duration, rateLimited bool) {
	if source == "" {
		return
	}
	st := stateFor(source)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	st.retries++
	if rateLimited {
		st.rateLimited++
	}
	if until := time.Now().Add(d); until.After(st.waitingUntil) {
		st.waitingUntil = until
	}
}

// Snapshot returns the current activity per source, ordered by name so the UI
// does not reshuffle between frames.
func Snapshot() []Activity {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	now := time.Now()
	out := make([]Activity, 0, len(registry.src))
	for name, st := range registry.src {
		a := Activity{
			Source:      name,
			InFlight:    len(st.inFlight),
			Requests:    st.requests,
			Retries:     st.retries,
			RateLimited: st.rateLimited,
			Failures:    st.failures,
			LastStatus:  st.lastStatus,
			LastErr:     st.lastErr,
		}
		for _, started := range st.inFlight {
			if ms := now.Sub(started).Milliseconds(); ms > a.OldestMs {
				a.OldestMs = ms
			}
		}
		if st.waitingUntil.After(now) {
			a.WaitingUntilMs = time.Until(st.waitingUntil).Milliseconds()
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// Busiest names the source we are most plausibly waiting on, with how long, or
// ("", 0) when nothing is outstanding. Used for the one-line "waiting on X"
// the operations banner shows.
func Busiest() (source string, waited time.Duration) {
	var best Activity
	for _, a := range Snapshot() {
		// A deliberate backoff counts as waiting too, and outranks a young request:
		// "rate-limited for 40s" is more informative than "a request is 2s old".
		score := a.OldestMs
		if a.WaitingUntilMs > score {
			score = a.WaitingUntilMs
		}
		if score == 0 {
			continue
		}
		bestScore := best.OldestMs
		if best.WaitingUntilMs > bestScore {
			bestScore = best.WaitingUntilMs
		}
		if score > bestScore {
			best = a
		}
	}
	if best.Source == "" {
		return "", 0
	}
	ms := best.OldestMs
	if best.WaitingUntilMs > ms {
		ms = best.WaitingUntilMs
	}
	return best.Source, time.Duration(ms) * time.Millisecond
}

// ResetForTest clears the registry. Tests only.
func ResetForTest() {
	registry.mu.Lock()
	registry.src = map[string]*sourceState{}
	registry.mu.Unlock()
}
