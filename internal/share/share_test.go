package share

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCodeNormalizeAndFormat(t *testing.T) {
	c := NewCode()
	n, err := Normalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if Format(n) != c {
		t.Fatalf("format(%q) = %q, want %q", n, Format(n), c)
	}
	if got, _ := Normalize(" 7k3m qx9d 24hb "); got != "7K3MQX9D24HB" {
		t.Fatalf("normalize spaces/case = %q", got)
	}
	if got, _ := Normalize("OOOO-IIII-LLLL"); got != "000011111111" {
		t.Fatalf("look-alikes = %q", got)
	}
	if _, err := Normalize("ABC"); err == nil {
		t.Fatal("short code accepted")
	}
}

func TestSealOpen(t *testing.T) {
	code := NewCode()
	p := Payload{Meta: Meta{BundleVersion: "abc", PatchTag: "7.41"},
		Files: []File{{Name: "bundle.json", Data: []byte(`{"x":1}`)}, {Name: "assets/heroes/1.png", Data: []byte{1, 2, 3}}}}
	blob, err := Seal(code, p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(strings.ToLower(code), blob)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.BundleVersion != "abc" || string(got.File("bundle.json")) != `{"x":1}` || len(got.File("assets/heroes/1.png")) != 3 {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if _, err := Open(NewCode(), blob); !errors.Is(err, ErrWrongCode) {
		t.Fatalf("wrong code: err = %v", err)
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"bundle.json": "bundle.json", "assets/a.png": "assets/a.png", "../x": "x", "a/../../b": "b"} {
		if got, ok := SafeName(in); !ok || got != want {
			t.Errorf("SafeName(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "C:/Windows/x", "."} {
		if _, ok := SafeName(bad); ok {
			t.Errorf("SafeName(%q) accepted", bad)
		}
	}
}

// fakeRelay implements the transit relay pairing: same token, different side.
func fakeRelay(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	type pending struct {
		c  net.Conn
		br *bufio.Reader
	}
	var mu sync.Mutex
	waiting := map[string]pending{}
	// alive: a waiting side sends nothing, so a read that ends before the short
	// deadline means it hung up (as the real relay notices on connectionLost).
	alive := func(p pending) bool {
		p.c.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
		_, err := p.br.Peek(1)
		p.c.SetReadDeadline(time.Time{})
		var ne net.Error
		return errors.As(err, &ne) && ne.Timeout()
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				br := bufio.NewReader(c)
				line, err := br.ReadString('\n')
				if err != nil || !strings.HasPrefix(line, "please relay ") {
					c.Close()
					return
				}
				token := strings.Fields(line)[2]
				me := pending{c, br}
				mu.Lock()
				peer, ok := waiting[token]
				if ok && !alive(peer) {
					ok = false
				}
				if !ok {
					waiting[token] = me
					mu.Unlock()
					return
				}
				delete(waiting, token)
				mu.Unlock()
				c.Write([]byte("ok\n"))
				peer.c.Write([]byte("ok\n"))
				go func() { io.Copy(c, peer.br); c.Close() }()
				io.Copy(peer.c, br)
				peer.c.Close()
			}(c)
		}
	}()
	return ln.Addr().String()
}

// fakePiping: POST waits for a GET on the same path and streams into it.
func fakePiping() *httptest.Server {
	var mu sync.Mutex
	type waiter struct {
		body chan []byte
		done chan struct{}
	}
	pipes := map[string]*waiter{}
	get := func(path string) *waiter {
		mu.Lock()
		defer mu.Unlock()
		w, ok := pipes[path]
		if !ok {
			w = &waiter{body: make(chan []byte, 1), done: make(chan struct{})}
			pipes[path] = w
		}
		return w
	}
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w := get(r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			data, _ := io.ReadAll(r.Body)
			rw.Write([]byte("[INFO] Waiting for 1 receiver(s)...\n"))
			rw.(http.Flusher).Flush()
			w.body <- data
			select {
			case <-w.done:
				rw.Write([]byte("[INFO] A receiver was connected.\n[INFO] Sent successfully!\n"))
			case <-r.Context().Done():
			}
		case http.MethodGet:
			select {
			case data := <-w.body:
				rw.Write(data)
				mu.Lock()
				delete(pipes, r.URL.Path)
				mu.Unlock()
				close(w.done)
			case <-r.Context().Done():
			}
		}
	}))
}

func testHost(ts []Transport, payload func() Payload) *Host {
	return &Host{Transports: ts, Build: func() (Payload, error) { return payload(), nil }}
}

func TestHostServesGuestsOverRelay(t *testing.T) {
	ts := []Transport{&Relay{Addr: fakeRelay(t)}}
	n := 0
	h := testHost(ts, func() Payload {
		n++
		return Payload{Files: []File{{Name: "bundle.json", Data: bytes.Repeat([]byte("b"), 1<<20)}}}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := h.Start(ctx)
	for i := 0; i < 2; i++ { // the same code serves the next guest too
		p, via, err := Receive(ctx, ts, strings.ToLower(st.Code), 10*time.Second)
		if err != nil {
			t.Fatalf("guest %d: %v", i, err)
		}
		if via != "relay" || len(p.File("bundle.json")) != 1<<20 {
			t.Fatalf("guest %d got via=%s %d bytes", i, via, len(p.File("bundle.json")))
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for h.Status().Served < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.Status().Served != 2 {
		t.Fatalf("served = %d, want 2", h.Status().Served)
	}
	h.Stop()
	if _, _, err := Receive(context.Background(), ts, st.Code, 500*time.Millisecond); !errors.Is(err, ErrNoHost) {
		t.Fatalf("after stop: err = %v, want ErrNoHost", err)
	}
}

func TestReceiveFallsBackToPiping(t *testing.T) {
	srv := fakePiping()
	defer srv.Close()
	// The relay port is closed: everything has to go over HTTPS.
	dead := &Relay{Addr: "127.0.0.1:1"}
	ts := []Transport{dead, &Piping{Base: srv.URL}}
	h := testHost(ts, func() Payload { return Payload{Meta: Meta{PatchTag: "7.41"}} })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := h.Start(ctx)
	p, via, err := Receive(ctx, ts, st.Code, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if via != "https" || p.Meta.PatchTag != "7.41" {
		t.Fatalf("via=%s meta=%+v", via, p.Meta)
	}
}

func TestRefreshRebuildsWaitingOffer(t *testing.T) {
	ts := []Transport{&Relay{Addr: fakeRelay(t)}}
	var mu sync.Mutex
	tag := "old"
	h := testHost(ts, func() Payload {
		mu.Lock()
		defer mu.Unlock()
		return Payload{Meta: Meta{PatchTag: tag}}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := h.Start(ctx)
	time.Sleep(200 * time.Millisecond) // the offer is waiting with "old"
	mu.Lock()
	tag = "new"
	mu.Unlock()
	h.Refresh()
	p, _, err := Receive(ctx, ts, st.Code, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta.PatchTag != "new" {
		t.Fatalf("guest got %q, want the refreshed payload", p.Meta.PatchTag)
	}
}
