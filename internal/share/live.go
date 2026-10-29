package share

// Live draft: the host streams its draft board to guests, who follow it the way
// they would follow their own Dota through GSI. One-way, host to guests, over
// the same relays and with the same code as the payload, but at a different
// rendezvous so a guest asking for the model never lands on the live stream.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	livePurpose     = "-live"
	liveHeartbeat   = 15 * time.Second
	liveSilence     = 45 * time.Second // no frame and no heartbeat for this long: the host is gone
	maxLiveFrame    = 1 << 20
	liveSaltContext = "cmnet live v1"
)

// Streamer opens one-way host→guest streams.
type Streamer interface {
	Name() string
	// HostStream blocks until a guest is on the line and returns where to write.
	HostStream(ctx context.Context, code string) (io.WriteCloser, error)
	// GuestStream blocks until a host is on the line and returns what to read.
	GuestStream(ctx context.Context, code string) (io.ReadCloser, error)
}

// DefaultStreamers are the public relays, for live drafts.
func DefaultStreamers() []Streamer {
	return []Streamer{
		&Relay{Addr: "transit.magic-wormhole.io:4001"},
		&Piping{Base: "https://ppng.io"},
	}
}

func liveAEAD(code string) (cipher.AEAD, error) {
	k, err := pbkdf2.Key(sha256.New, code, []byte(liveSaltContext), kdfIterations, 32)
	if err != nil {
		return nil, err
	}
	return newAEAD(k)
}

func writeFrame(w io.Writer, aead cipher.AEAD, plain []byte) error {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := aead.Seal(nonce, nonce, plain, []byte(liveSaltContext))
	buf := make([]byte, 4, 4+len(sealed))
	binary.BigEndian.PutUint32(buf, uint32(len(sealed)))
	_, err := w.Write(append(buf, sealed...))
	return err
}

func readFrame(r io.Reader, aead cipher.AEAD) ([]byte, error) {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(n[:])
	if size < uint32(aead.NonceSize()+aead.Overhead()) || size > maxLiveFrame {
		return nil, fmt.Errorf("live frame of %d bytes refused", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	ns := aead.NonceSize()
	plain, err := aead.Open(nil, buf[:ns], buf[ns:], []byte(liveSaltContext))
	if err != nil {
		return nil, ErrWrongCode
	}
	return plain, nil
}

// --- host -------------------------------------------------------------------

// LiveHost fans the latest draft frame out to every following guest.
type LiveHost struct {
	Streamers []Streamer
	Changed   func() // guest count changed

	mu     sync.Mutex
	latest []byte
	subs   map[int]chan []byte
	nextID int
}

// Guests is how many guests are following right now.
func (l *LiveHost) Guests() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.subs)
}

// Publish sends frame to every guest (and to every guest who comes later).
// Identical frames are dropped.
func (l *LiveHost) Publish(frame []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if bytes.Equal(frame, l.latest) {
		return
	}
	l.latest = append([]byte(nil), frame...)
	for _, ch := range l.subs {
		select { // keep only the newest frame queued
		case <-ch:
		default:
		}
		ch <- l.latest
	}
}

// Run accepts guests on code until ctx ends.
func (l *LiveHost) Run(ctx context.Context, code string) error {
	code, err := Normalize(code)
	if err != nil {
		return err
	}
	aead, err := liveAEAD(code)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, s := range l.Streamers {
		wg.Add(1)
		go func(s Streamer) {
			defer wg.Done()
			backoff := 2 * time.Second
			for ctx.Err() == nil {
				w, err := s.HostStream(ctx, code)
				if err != nil {
					sleep(ctx, backoff)
					if backoff < time.Minute {
						backoff *= 2
					}
					continue
				}
				backoff = 2 * time.Second
				go l.serve(ctx, w, aead)
			}
		}(s)
	}
	wg.Wait()
	return ctx.Err()
}

func (l *LiveHost) serve(ctx context.Context, w io.WriteCloser, aead cipher.AEAD) {
	defer w.Close()
	ch := make(chan []byte, 1)
	l.mu.Lock()
	if l.subs == nil {
		l.subs = map[int]chan []byte{}
	}
	id := l.nextID
	l.nextID++
	l.subs[id] = ch
	if l.latest != nil {
		ch <- l.latest
	}
	l.mu.Unlock()
	if l.Changed != nil {
		l.Changed()
	}
	defer func() {
		l.mu.Lock()
		delete(l.subs, id)
		l.mu.Unlock()
		if l.Changed != nil {
			l.Changed()
		}
	}()
	tick := time.NewTicker(liveHeartbeat)
	defer tick.Stop()
	for {
		var frame []byte
		select {
		case <-ctx.Done():
			return
		case frame = <-ch:
		case <-tick.C:
			frame = []byte{} // heartbeat: tells the guest we are still here
		}
		if err := writeFrame(w, aead, frame); err != nil {
			return
		}
	}
}

// --- guest ------------------------------------------------------------------

// Follow connects to the host's live draft and calls onFrame for each board it
// sends, until the stream breaks or ctx ends. onConnected runs once the host is
// on the line. Callers loop on it to reconnect.
func Follow(ctx context.Context, streamers []Streamer, code string, onConnected func(via string), onFrame func([]byte)) error {
	code, err := Normalize(code)
	if err != nil {
		return err
	}
	aead, err := liveAEAD(code)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Each relay dials under its own context: once one connects, the others are
	// cancelled at once. A guest left waiting on the second relay would be
	// paired with the host there too and hold a stream nobody reads.
	type got struct {
		i   int
		r   io.ReadCloser
		via string
		err error
	}
	out := make(chan got, len(streamers))
	cancels := make([]context.CancelFunc, len(streamers))
	for i, s := range streamers {
		dctx, dcancel := context.WithCancel(ctx)
		cancels[i] = dcancel
		go func(i int, s Streamer) {
			r, err := s.GuestStream(dctx, code)
			out <- got{i, r, s.Name(), err}
		}(i, s)
	}
	var win got
	var lastErr error
	pending := len(streamers)
	for pending > 0 && win.r == nil {
		g := <-out
		pending--
		if g.err == nil && g.r != nil {
			win = g
			break
		}
		cancels[g.i]()
		lastErr = g.err
	}
	if win.r == nil {
		if lastErr == nil {
			lastErr = ctx.Err()
		}
		return lastErr
	}
	for i, c := range cancels {
		if i != win.i {
			c()
		}
	}
	go func(n int) { // close any loser that connected anyway
		for ; n > 0; n-- {
			if g := <-out; g.r != nil {
				g.r.Close()
			}
		}
	}(pending)
	defer win.r.Close()
	if onConnected != nil {
		onConnected(win.via)
	}

	// Watchdog: a host that vanished without closing sends nothing at all, and
	// a piping-server body has no read deadline.
	alive := make(chan struct{}, 1)
	go func() {
		t := time.NewTimer(liveSilence)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				win.r.Close()
				return
			case <-alive:
				if !t.Stop() {
					<-t.C
				}
				t.Reset(liveSilence)
			case <-t.C:
				win.r.Close()
				return
			}
		}
	}()
	br := bufio.NewReader(win.r)
	for {
		plain, err := readFrame(br, aead)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		select {
		case alive <- struct{}{}:
		default:
		}
		if len(plain) > 0 {
			onFrame(plain)
		}
	}
}

// --- relay streams ----------------------------------------------------------

func (r *Relay) HostStream(ctx context.Context, code string) (io.WriteCloser, error) {
	for {
		conn, err := r.connect(ctx, code, livePurpose)
		if err != nil {
			return nil, err
		}
		if err := exchangeRole(conn, 'H'); err != nil {
			conn.Close()
			if errors.Is(err, errPeerIsGuest) {
				continue
			}
			return nil, err
		}
		return conn, nil
	}
}

func (r *Relay) GuestStream(ctx context.Context, code string) (io.ReadCloser, error) {
	for {
		conn, err := r.connect(ctx, code, livePurpose)
		if err != nil {
			return nil, err
		}
		err = exchangeRole(conn, 'G')
		if errors.Is(err, errPeerIsGuest) {
			conn.Close()
			sleep(ctx, time.Second)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if err != nil {
			conn.Close()
			return nil, err
		}
		return conn, nil
	}
}

var _ net.Conn = (*stopConn)(nil)

// --- piping streams ---------------------------------------------------------

type pipingWriter struct {
	pw   *io.PipeWriter
	done chan struct{}
}

func (w *pipingWriter) Write(b []byte) (int, error) { return w.pw.Write(b) }
func (w *pipingWriter) Close() error {
	err := w.pw.Close()
	<-w.done
	return err
}

func (p *Piping) HostStream(ctx context.Context, code string) (io.WriteCloser, error) {
	pr, pw := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.urlFor(code, livePurpose), pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	paired := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := p.client().Do(req)
		if err != nil {
			paired <- ctxErr(ctx, err)
			pr.CloseWithError(err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			paired <- fmt.Errorf("piping server: %s %s", resp.Status, strings.TrimSpace(string(msg)))
			pr.Close()
			return
		}
		sc := bufio.NewScanner(resp.Body)
		told := false
		for sc.Scan() {
			if !told && strings.Contains(strings.ToLower(sc.Text()), "connected") {
				told = true
				paired <- nil
			}
		}
		if !told {
			paired <- errors.New("piping server closed before a guest came")
		}
		pr.Close() // the guest left: writes fail and the host drops this stream
	}()
	select {
	case err := <-paired:
		if err != nil {
			pw.Close()
			<-done
			return nil, err
		}
		return &pipingWriter{pw: pw, done: done}, nil
	case <-ctx.Done():
		pw.Close()
		<-done
		return nil, ctx.Err()
	}
}

func (p *Piping) GuestStream(ctx context.Context, code string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.urlFor(code, livePurpose), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, ctxErr(ctx, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("piping server: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return resp.Body, nil
}
