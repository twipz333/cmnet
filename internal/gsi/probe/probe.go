// Package probe captures raw Dota 2 Game State Integration HTTP bodies
// without decoding them into structs. It exists so a later DotaState design
// can be based on what the playing client actually sends, not on library types.
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"cmnet/internal/atomicfile"
)

// MaxBodyBytes is the largest HTTP body the probe will store. GSI payloads are
// typically tens to a few hundred KiB even with minimap; several MiB leaves
// headroom without letting a stray client fill the disk.
const MaxBodyBytes = 8 << 20

const (
	defaultOutput  = "data/gsi-probe"
	statLogEvery   = 100
	shutdownWait   = 5 * time.Second
	sessionDirTime = "2006-01-02T15-04-05Z"
)

// Options configures a probe process.
type Options struct {
	Listen  string // host:port; default DefaultListen
	Output  string // parent directory for session folders; default data/gsi-probe
	Version string // advisor version string (main.version)
	Commit  string // optional; falls back to VCS revision stamped into the binary
	Log     func(string)
}

// Probe is a diagnostic GSI capture: it stores every POST body byte-for-byte
// and never interprets Dota fields.
type Probe struct {
	opts      Options
	dir       string
	started   time.Time
	cap       *capture
	http      *http.Server
	closeOnce sync.Once
	closeErr  error

	mu   sync.Mutex
	addr string // actual bound address after Start
}

// New creates the session directory, writes session.json and a copy of the GSI
// config, and returns a probe ready to Start. It does not listen.
func New(opts Options) (*Probe, error) {
	opts = applyDefaults(opts)
	if _, _, err := net.SplitHostPort(opts.Listen); err != nil {
		return nil, fmt.Errorf("listen %q: %w", opts.Listen, err)
	}

	started := time.Now().UTC()
	dir, err := createSessionDir(opts.Output, started)
	if err != nil {
		return nil, err
	}

	p := &Probe{
		opts:    opts,
		dir:     dir,
		started: started,
		cap: &capture{
			dir:     dir,
			started: started,
			listen:  opts.Listen,
			advisor: AdvisorInfo{Version: opts.Version, Commit: opts.Commit},
		},
	}

	if err := p.initSession(); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handle)
	p.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return p, nil
}

func applyDefaults(opts Options) Options {
	if opts.Listen == "" {
		opts.Listen = DefaultListen
	}
	if opts.Output == "" {
		opts.Output = defaultOutput
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.Commit == "" {
		opts.Commit = vcsRevision()
	}
	return opts
}

func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

func createSessionDir(root string, started time.Time) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create output dir %s: %w", root, err)
	}
	base := filepath.Join(root, started.Format(sessionDirTime))
	dir := base
	for n := 2; ; n++ {
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("create session dir %s: %w", dir, err)
		}
		dir = fmt.Sprintf("%s-%d", base, n)
	}
}

func (p *Probe) initSession() error {
	if err := os.Mkdir(filepath.Join(p.dir, payloadDirName), 0o755); err != nil {
		return fmt.Errorf("create payloads dir: %w", err)
	}
	idx, err := os.OpenFile(filepath.Join(p.dir, indexFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	p.cap.index = idx
	if err := os.WriteFile(filepath.Join(p.dir, cfgCopyName), []byte(ConfigContent(URI(p.opts.Listen))), 0o644); err != nil {
		idx.Close()
		return fmt.Errorf("write gsi-config.cfg: %w", err)
	}
	if err := p.writeSession(nil); err != nil {
		idx.Close()
		return err
	}
	return nil
}

func (p *Probe) writeSession(endedAt *time.Time) error {
	meta := SessionMeta{
		FormatVersion: FormatVersion,
		StartedAt:     p.started.Format(time.RFC3339Nano),
		Advisor:       p.cap.advisor,
		Probe:         ProbeInfo{Listen: p.opts.Listen},
	}
	stats, _ := p.cap.snapshot()
	meta.Capture = stats
	if endedAt != nil {
		s := endedAt.UTC().Format(time.RFC3339Nano)
		meta.EndedAt = &s
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session.json: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(filepath.Join(p.dir, sessionFileName), data, 0o644); err != nil {
		return fmt.Errorf("write session.json: %w", err)
	}
	return nil
}

// SessionDir is the folder this run is writing into.
func (p *Probe) SessionDir() string { return p.dir }

// Addr is the address the HTTP server is bound to after Start. Empty before that.
func (p *Probe) Addr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.addr
}

// Stats returns a snapshot of capture counters.
func (p *Probe) Stats() CaptureStats {
	s, _ := p.cap.snapshot()
	return s
}

// Handler returns the HTTP handler. Tests drive it through httptest without
// binding a real port.
func (p *Probe) Handler() http.Handler { return p.http.Handler }

// Start listens on Options.Listen until ctx is cancelled, then drains in-flight
// requests. Call Close after Start returns to flush index.ndjson and stamp
// session.json with ended_at.
func (p *Probe) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", p.opts.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", p.opts.Listen, err)
	}
	p.mu.Lock()
	p.addr = ln.Addr().String()
	p.mu.Unlock()

	errc := make(chan error, 1)
	go func() {
		err := p.http.Serve(ln)
		if err != nil && err != http.ErrServerClosed {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownWait)
		defer cancel()
		if err := p.http.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
		return <-errc
	case err := <-errc:
		return err
	}
}

// Close flushes the index, writes the final session.json, and is safe to call
// more than once. It does not stop the HTTP server; call it after Start returns.
func (p *Probe) Close() error {
	p.closeOnce.Do(func() {
		end := time.Now().UTC()
		if err := p.cap.closeIndex(); err != nil {
			p.closeErr = fmt.Errorf("close index: %w", err)
		}
		if err := p.writeSession(&end); err != nil && p.closeErr == nil {
			p.closeErr = err
		}
	})
	return p.closeErr
}

func (p *Probe) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "GSI probe is running\n")
		return
	}

	receivedAt := time.Now().UTC()

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		p.cap.noteError()
		p.logf("read body: %v", err)
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > MaxBodyBytes {
		p.cap.noteError()
		p.logf("rejected payload: body exceeds %d bytes", MaxBodyBytes)
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	rec, err := p.cap.write(receivedAt, body, r.Header.Get("Content-Type"), r.RemoteAddr)
	if err != nil {
		p.logf("capture error: %v", err)
		http.Error(w, "capture failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	if rec.Seq == 1 || rec.Seq%statLogEvery == 0 {
		stats, _ := p.cap.snapshot()
		p.logf("Captured: %d payloads / %s", stats.PayloadCount, formatBytes(stats.TotalBytes))
	}
}

func (p *Probe) logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if p.opts.Log != nil {
		p.opts.Log(msg)
		return
	}
	fmt.Fprintln(os.Stderr, msg)
}

// Summary is the human-readable stop banner.
type Summary struct {
	Duration time.Duration
	Stats    CaptureStats
	Dir      string
}

// MakeSummary is the numbers printed after a graceful stop.
func (p *Probe) MakeSummary() Summary {
	return Summary{
		Duration: time.Since(p.started),
		Stats:    p.Stats(),
		Dir:      p.dir,
	}
}

// String renders the shutdown banner.
func (s Summary) String() string {
	return fmt.Sprintf("GSI Probe stopped\n\nDuration:       %s\nPayloads:       %d\nCaptured:       %s\nErrors:         %d\nSession:        %s\n",
		formatDuration(s.Duration), s.Stats.PayloadCount, formatBytes(s.Stats.TotalBytes), s.Stats.Errors, s.Dir)
}

func formatBytes(n int64) string {
	const kb = 1024
	switch {
	case n >= kb*kb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(kb*kb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	sec := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, sec)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, sec)
	}
	return fmt.Sprintf("%ds", sec)
}

// WarnIfNonLoopback reports whether listen is bound beyond loopback. The
// caller decides what to do with that; probe itself still honours the address.
func WarnIfNonLoopback(listen string) string {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return fmt.Sprintf("listen %s is not loopback; probe will be reachable off-machine", listen)
	}
	ip := net.ParseIP(host)
	if ip != nil && !ip.IsLoopback() {
		return fmt.Sprintf("listen %s is not loopback; probe will be reachable off-machine", listen)
	}
	return ""
}
