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
	// Recent is the per-second history the chart draws. Filled by SnapshotWithSeries.
	Recent []Sample `json:"recent,omitempty"`
}

// Sample is one second of traffic to a source.
//
// Per second rather than per request: a sync makes thousands of calls, and the
// question the chart answers ("is it moving, and how fast") is about rate, not
// about individual requests. A fixed-size ring of seconds also means the memory
// cost is known in advance and does not grow with a long collection run.
type Sample struct {
	// Sec is the unix second this bucket covers.
	Sec int64 `json:"sec"`
	// Requests is how many requests STARTED in this second.
	Requests int `json:"requests"`
	// Failures and RateLimited are subsets of the responses that landed in it, so
	// a bar can show trouble as a share of the traffic rather than as a
	// separate series nobody can line up by eye.
	Failures    int `json:"failures"`
	RateLimited int `json:"rateLimited"`
	// SlowestMs is the longest response observed in this second, which is what
	// turns "many requests" into "many requests, all of them crawling".
	SlowestMs int `json:"slowestMs"`
}

// historySeconds is how much of the recent past the chart keeps: five minutes.
//
// Long enough to see a rate-limit pause begin and end and to judge whether a
// collection pass is holding its rate; short enough that this stays a liveness
// readout rather than a metrics store. The ring is fixed-size, so the cost is a
// few kilobytes per source and does not grow with a run that lasts days.
const historySeconds = 300

// bucket is one second of the ring.
type bucket struct {
	sec         int64
	requests    int
	failures    int
	rateLimited int
	slowestMs   int
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
	ring         [historySeconds]bucket
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

// bucketFor returns the ring slot for a unix second, resetting it when it still
// holds an older second.
//
// A ring indexed by (second mod size) needs no shifting and no allocation, and a
// gap in traffic costs nothing: the stale slot is simply reset the moment it is
// reused, so an idle minute reads as zeros rather than as ancient data.
//
// Callers must hold registry.mu.
func (st *sourceState) bucketFor(sec int64) *bucket {
	b := &st.ring[sec%historySeconds]
	if b.sec != sec {
		*b = bucket{sec: sec}
	}
	return b
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
	now := time.Now()
	st.inFlight[id] = now
	st.requests++
	st.bucketFor(now.Unix()).requests++
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

	now := time.Now()
	// Attributed to the second the response LANDED, not the one the request left
	// in: a slow call belongs where its cost showed up, and the started-in count
	// already records the other end.
	b := st.bucketFor(now.Unix())
	if started, ok := st.inFlight[id]; ok {
		if ms := int(now.Sub(started).Milliseconds()); ms > b.slowestMs {
			b.slowestMs = ms
		}
	}
	delete(st.inFlight, id)
	if status > 0 {
		st.lastStatus = status
	}
	if status == 429 {
		b.rateLimited++
	}
	if err != nil {
		st.failures++
		st.lastErr = err.Error()
		b.failures++
	} else if status >= 400 {
		b.failures++
	} else if status > 0 {
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

// Series returns the last n seconds for a source, oldest first, including the
// seconds in which nothing happened.
//
// Gaps are returned as explicit zero samples rather than omitted: a chart that
// skips empty seconds compresses a pause into nothing and makes a stalled source
// look busy.
func Series(source string, n int) []Sample {
	if n <= 0 || n > historySeconds {
		n = historySeconds
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()

	st := registry.src[source]
	if st == nil {
		return nil
	}
	// The current second is excluded: it is still filling, and a half-counted bar
	// at the leading edge reads as a drop in traffic.
	end := time.Now().Unix() - 1
	out := make([]Sample, 0, n)
	for sec := end - int64(n) + 1; sec <= end; sec++ {
		s := Sample{Sec: sec}
		if b := &st.ring[((sec%historySeconds)+historySeconds)%historySeconds]; b.sec == sec {
			s.Requests, s.Failures = b.requests, b.failures
			s.RateLimited, s.SlowestMs = b.rateLimited, b.slowestMs
		}
		out = append(out, s)
	}
	return out
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
	bestScore := int64(-1) // -1 = nothing selected yet
	for _, a := range Snapshot() {
		// A deliberate backoff counts as waiting too, and outranks a young request:
		// "rate-limited for 40s" is more informative than "a request is 2s old".
		score := a.OldestMs
		if a.WaitingUntilMs > score {
			score = a.WaitingUntilMs
		}
		// InFlight with age truncated to 0ms is still outstanding. Skipping it
		// raced TestActivityReportsInFlight (and the ops banner) whenever the
		// open request was younger than one millisecond.
		if score == 0 && a.InFlight == 0 {
			continue
		}
		if bestScore < 0 || score > bestScore {
			best = a
			bestScore = score
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

// SnapshotWithSeries is Snapshot plus the per-second history each source.
//
// Separate from Snapshot because the series is only wanted by the one screen that
// draws it; the banner asks several times a second and has no use for it.
func SnapshotWithSeries(seconds int) []Activity {
	out := Snapshot()
	for i := range out {
		out[i].Recent = Series(out[i].Source, seconds)
	}
	return out
}
