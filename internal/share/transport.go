package share

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Transport is one way of meeting the other side. Offer blocks until one guest
// has taken blob (or ctx ends); paired is called once a guest is on the line, so
// the host knows the attempt can no longer be swapped for a fresher payload.
// Receive blocks until a host hands over a blob.
type Transport interface {
	Name() string
	Offer(ctx context.Context, code string, blob []byte, paired func()) error
	Receive(ctx context.Context, code string) ([]byte, error)
}

// DefaultTransports are the public relays, tried together.
func DefaultTransports() []Transport {
	return []Transport{
		&Relay{Addr: "transit.magic-wormhole.io:4001"},
		&Piping{Base: "https://ppng.io"},
	}
}

// errPeerIsGuest: the relay paired two guests waiting on the same code. Both
// hang up and try again; the host will be next in line.
var errPeerIsGuest = errors.New("paired with another guest")

// --- magic-wormhole transit relay -------------------------------------------

// Relay speaks the transit relay handshake: each side sends
// "please relay <token> for side <side>\n", the relay pairs the first two
// connections with the same token and different sides, answers "ok\n" to both
// and from then on is a dumb byte pipe. Everything after that is ours: a role
// byte, then the sealed payload with its length, then an ack.
type Relay struct {
	Addr string
	Dial func(ctx context.Context, network, addr string) (net.Conn, error) // tests
}

func (r *Relay) Name() string { return "relay" }

func (r *Relay) connect(ctx context.Context, code string) (net.Conn, error) {
	dial := r.Dial
	if dial == nil {
		d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		dial = d.DialContext
	}
	conn, err := dial(ctx, "tcp", r.Addr)
	if err != nil {
		return nil, err
	}
	// Closing the connection is the only way to abandon a blocked read.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	token := rendezvous(code, "relay")
	var side [8]byte
	_, _ = rand.Read(side[:])
	hello := fmt.Sprintf("please relay %s for side %s\n", hex.EncodeToString(token[:]), hex.EncodeToString(side[:]))
	if _, err := conn.Write([]byte(hello)); err != nil {
		stop()
		conn.Close()
		return nil, err
	}
	ok := make([]byte, 3)
	if _, err := io.ReadFull(conn, ok); err != nil {
		stop()
		conn.Close()
		return nil, ctxErr(ctx, err)
	}
	if string(ok) != "ok\n" {
		stop()
		conn.Close()
		return nil, fmt.Errorf("relay answered %q", ok)
	}
	return &stopConn{Conn: conn, stop: stop}, nil
}

type stopConn struct {
	net.Conn
	stop func() bool
}

func (c *stopConn) Close() error { c.stop(); return c.Conn.Close() }

func exchangeRole(conn net.Conn, mine byte) error {
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer conn.SetDeadline(time.Time{})
	if _, err := conn.Write([]byte{mine}); err != nil {
		return err
	}
	var peer [1]byte
	if _, err := io.ReadFull(conn, peer[:]); err != nil {
		return err
	}
	if peer[0] == mine {
		return errPeerIsGuest
	}
	return nil
}

func (r *Relay) Offer(ctx context.Context, code string, blob []byte, paired func()) error {
	for {
		conn, err := r.connect(ctx, code)
		if err != nil {
			return err
		}
		err = exchangeRole(conn, 'H')
		if errors.Is(err, errPeerIsGuest) { // another host instance; should not happen
			conn.Close()
			continue
		}
		if err != nil {
			conn.Close()
			return err
		}
		paired()
		err = sendFramed(conn, blob)
		conn.Close()
		return err
	}
}

func sendFramed(conn net.Conn, blob []byte) error {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(blob)))
	// A slow uplink still moves a few megabytes a minute; the deadline only
	// catches a peer that vanished.
	_ = conn.SetDeadline(time.Now().Add(10*time.Minute + time.Duration(len(blob)/20_000)*time.Second))
	if _, err := conn.Write(n[:]); err != nil {
		return err
	}
	if _, err := conn.Write(blob); err != nil {
		return err
	}
	var ack [1]byte
	if _, err := io.ReadFull(conn, ack[:]); err != nil {
		return fmt.Errorf("guest did not confirm: %w", err)
	}
	if ack[0] != 'K' {
		return errors.New("guest rejected the payload")
	}
	return nil
}

func (r *Relay) Receive(ctx context.Context, code string) ([]byte, error) {
	for {
		conn, err := r.connect(ctx, code)
		if err != nil {
			return nil, err
		}
		err = exchangeRole(conn, 'G')
		if errors.Is(err, errPeerIsGuest) {
			conn.Close()
			select { // let the host's connection get to the relay first
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}
		if err != nil {
			conn.Close()
			return nil, err
		}
		blob, err := readFramed(conn)
		if err == nil {
			_, _ = conn.Write([]byte{'K'})
		}
		conn.Close()
		return blob, err
	}
}

func readFramed(conn net.Conn) ([]byte, error) {
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var n [8]byte
	if _, err := io.ReadFull(conn, n[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint64(n[:])
	if size == 0 || size > MaxPayload {
		return nil, fmt.Errorf("payload of %d bytes refused", size)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10*time.Minute + time.Duration(size/20_000)*time.Second))
	blob := make([]byte, size)
	if _, err := io.ReadFull(conn, blob); err != nil {
		return nil, err
	}
	return blob, nil
}

// --- piping server over HTTPS -----------------------------------------------

// Piping uses a piping server (github.com/nwtgck/piping-server): the sender
// POSTs to a path, the receiver GETs the same path, and the server streams one
// into the other. It works wherever HTTPS does.
type Piping struct {
	Base   string
	Client *http.Client
}

func (p *Piping) Name() string { return "https" }

func (p *Piping) url(code string) string {
	rv := rendezvous(code, "piping")
	return strings.TrimRight(p.Base, "/") + "/cmnet-" + hex.EncodeToString(rv[:20])
}

func (p *Piping) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

func (p *Piping) Offer(ctx context.Context, code string, blob []byte, paired func()) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url(code), bytes.NewReader(blob))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := p.client().Do(req)
	if err != nil {
		return ctxErr(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("piping server: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	// The server narrates: "[INFO] Waiting for 1 receiver(s)…", "[INFO] A
	// receiver was connected.", "[INFO] Sent successfully!". The narration is
	// how the host learns a guest is on the line, and that it worked.
	sc := bufio.NewScanner(resp.Body)
	sent := false
	for sc.Scan() {
		line := strings.ToLower(sc.Text())
		if strings.Contains(line, "connected") {
			paired()
		}
		if strings.Contains(line, "success") {
			sent = true
		}
		if strings.Contains(line, "[error]") {
			return fmt.Errorf("piping server: %s", sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		return ctxErr(ctx, err)
	}
	if !sent {
		return errors.New("piping server closed before the guest finished")
	}
	return nil
}

func (p *Piping) Receive(ctx context.Context, code string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url(code), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, ctxErr(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("piping server: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	blob, err := io.ReadAll(io.LimitReader(resp.Body, MaxPayload+1))
	if err != nil {
		return nil, ctxErr(ctx, err)
	}
	if len(blob) > MaxPayload {
		return nil, errors.New("payload too large")
	}
	return blob, nil
}

func ctxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
