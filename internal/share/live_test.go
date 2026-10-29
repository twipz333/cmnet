package share

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// streamingPiping is a piping server that streams: the sender's body flows to
// the receiver as it arrives, as the real one does.
func streamingPiping() (*httptest.Server, func()) {
	quit := make(chan struct{})
	type meet struct {
		body chan io.Reader // the sender's body, handed to the receiver
		done chan struct{}  // the receiver finished reading it
	}
	var mu sync.Mutex
	meets := map[string]*meet{}
	get := func(path string) *meet {
		mu.Lock()
		defer mu.Unlock()
		m, ok := meets[path]
		if !ok {
			m = &meet{body: make(chan io.Reader), done: make(chan struct{})}
			meets[path] = m
		}
		return m
	}
	drop := func(path string, m *meet) {
		mu.Lock()
		if meets[path] == m {
			delete(meets, path)
		}
		mu.Unlock()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		m := get(r.URL.Path)
		fl := rw.(http.Flusher)
		switch r.Method {
		case http.MethodPost:
			// Go's server stops reading a body once the response has started
			// unless asked; the real piping server is full duplex.
			_ = http.NewResponseController(rw).EnableFullDuplex()
			rw.Write([]byte("[INFO] Waiting for 1 receiver(s)...\n"))
			fl.Flush()
			select {
			case m.body <- r.Body:
			case <-r.Context().Done():
				drop(r.URL.Path, m)
				return
			case <-quit:
				return
			}
			drop(r.URL.Path, m)
			rw.Write([]byte("[INFO] A receiver was connected.\n"))
			fl.Flush()
			<-m.done // only the receiver reads the body, and it always finishes
			rw.Write([]byte("[INFO] Sent successfully!\n"))
		case http.MethodGet:
			rw.WriteHeader(http.StatusOK)
			fl.Flush()
			var body io.Reader
			select {
			case body = <-m.body:
			case <-r.Context().Done():
				return
			case <-quit:
				return
			}
			defer close(m.done)
			buf := make([]byte, 32<<10)
			for {
				n, err := body.Read(buf)
				if n > 0 {
					if _, werr := rw.Write(buf[:n]); werr != nil {
						return
					}
					fl.Flush()
				}
				if err != nil {
					return
				}
				select {
				case <-quit:
					return
				default:
				}
			}
		}
	}))
	// A waiting request whose client went away is not noticed by Go's server
	// while the body is open; the test ends them itself.
	return srv, func() { close(quit); srv.CloseClientConnections(); srv.Close() }
}

func follow(t *testing.T, ctx context.Context, ss []Streamer, code string) (<-chan string, <-chan string) {
	t.Helper()
	frames := make(chan string, 16)
	via := make(chan string, 4)
	go func() {
		for ctx.Err() == nil {
			_ = Follow(ctx, ss, code, func(v string) { via <- v }, func(b []byte) { frames <- string(b) })
			sleep(ctx, 50*time.Millisecond)
		}
	}()
	return frames, via
}

func want(t *testing.T, ch <-chan string, v string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case got := <-ch:
			if got == v {
				return
			}
		case <-deadline:
			t.Fatalf("did not get %q", v)
		}
	}
}

func TestLiveFansOutToGuests(t *testing.T) {
	ss := []Streamer{&Relay{Addr: fakeRelay(t)}}
	code := NewCode()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &LiveHost{Streamers: ss}
	h.Publish([]byte("board-1"))
	go h.Run(ctx, code)

	f1, _ := follow(t, ctx, ss, code)
	want(t, f1, "board-1") // a guest joining late gets the current board first
	f2, _ := follow(t, ctx, ss, code)
	want(t, f2, "board-1")
	h.Publish([]byte("board-2"))
	want(t, f1, "board-2")
	want(t, f2, "board-2")
	deadline := time.Now().Add(2 * time.Second)
	for h.Guests() != 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.Guests() != 2 {
		t.Fatalf("guests = %d, want 2", h.Guests())
	}
}

func TestLiveOverHTTPSWhenRelayIsBlocked(t *testing.T) {
	srv, stop := streamingPiping()
	defer stop()
	ss := []Streamer{&Relay{Addr: "127.0.0.1:1"}, &Piping{Base: srv.URL}}
	code := NewCode()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &LiveHost{Streamers: ss}
	h.Publish([]byte("b1"))
	go h.Run(ctx, code)
	frames, via := follow(t, ctx, ss, code)
	want(t, frames, "b1")
	want(t, via, "https")
	h.Publish([]byte("b2"))
	want(t, frames, "b2")
}

func TestLiveGuestReconnectsToRestartedHost(t *testing.T) {
	ss := []Streamer{&Relay{Addr: fakeRelay(t)}}
	code := NewCode()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hctx, stopHost := context.WithCancel(ctx)
	h := &LiveHost{Streamers: ss}
	h.Publish([]byte("before"))
	go h.Run(hctx, code)
	frames, _ := follow(t, ctx, ss, code)
	want(t, frames, "before")

	stopHost() // the host's app closed
	time.Sleep(100 * time.Millisecond)
	h2 := &LiveHost{Streamers: ss}
	h2.Publish([]byte("after"))
	go h2.Run(ctx, code)
	want(t, frames, "after")
}

func TestLiveWrongCodeSeesNothing(t *testing.T) {
	ss := []Streamer{&Relay{Addr: fakeRelay(t)}}
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	h := &LiveHost{Streamers: ss}
	h.Publish([]byte("secret"))
	go h.Run(ctx, NewCode())
	err := Follow(ctx, ss, NewCode(), nil, func([]byte) { t.Error("frame from another code") })
	if err == nil {
		t.Fatal("follow on an unknown code returned no error")
	}
}

var _ io.Closer = (*pipingWriter)(nil)
