package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func newTestProbe(t *testing.T) *Probe {
	t.Helper()
	p, err := New(Options{
		Listen:  "127.0.0.1:0",
		Output:  t.TempDir(),
		Version: "test",
		Commit:  "deadbeef",
		Log:     func(string) {},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func postRaw(t *testing.T, h http.Handler, body []byte, contentType string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func readIndex(t *testing.T, dir string) []IndexRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, indexFileName))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var out []IndexRecord
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec IndexRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("index line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func readSession(t *testing.T, dir string) SessionMeta {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, sessionFileName))
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	var meta SessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("session.json: %v", err)
	}
	return meta
}

func TestValidPayloadPreservedByteForByte(t *testing.T) {
	p := newTestProbe(t)
	body := []byte(`{
  "provider": {
    "name": "Dota 2"
  },
  "unknown_future_section": {
    "foo": 123
  }
}`)
	resp := postRaw(t, p.Handler(), body, "application/json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	path := filepath.Join(p.SessionDir(), "payloads", "00000001.json")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("payload file: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("payload rewritten:\n got: %q\nwant: %q", got, body)
	}
	if !bytes.Contains(got, []byte(`"unknown_future_section"`)) {
		t.Fatal("unknown section was dropped")
	}

	idx := readIndex(t, p.SessionDir())
	if len(idx) != 1 {
		t.Fatalf("index records = %d, want 1", len(idx))
	}
	if idx[0].Seq != 1 || !idx[0].ValidJSON || idx[0].Bytes != len(body) {
		t.Errorf("index record = %+v", idx[0])
	}
	if idx[0].File != "payloads/00000001.json" {
		t.Errorf("file = %q", idx[0].File)
	}
	if idx[0].SHA256 == "" || idx[0].ReceivedAt == "" {
		t.Errorf("missing sha256/timestamp: %+v", idx[0])
	}
}

func TestUnknownNestedFieldsKept(t *testing.T) {
	p := newTestProbe(t)
	body := []byte(`{"map":{"clock_time":12,"brand_new_field":{"nested":[1,2,{"x":true}]}},"also_unknown":null}`)
	resp := postRaw(t, p.Handler(), body, "application/json")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got, err := os.ReadFile(filepath.Join(p.SessionDir(), "payloads", "00000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("nested unknown fields not preserved:\n got: %s\nwant: %s", got, body)
	}
}

func TestMultipleRequestsStrictSequence(t *testing.T) {
	p := newTestProbe(t)
	const n = 100
	for i := 0; i < n; i++ {
		body := []byte(fmt.Sprintf(`{"i":%d}`, i))
		resp := postRaw(t, p.Handler(), body, "application/json")
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("req %d status %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}

	idx := readIndex(t, p.SessionDir())
	if len(idx) != n {
		t.Fatalf("index records = %d, want %d", len(idx), n)
	}
	seen := map[uint64]bool{}
	for i, rec := range idx {
		want := uint64(i + 1)
		if rec.Seq != want {
			t.Fatalf("seq[%d] = %d, want %d", i, rec.Seq, want)
		}
		if seen[rec.Seq] {
			t.Fatalf("duplicate seq %d", rec.Seq)
		}
		seen[rec.Seq] = true
		wantFile := fmt.Sprintf("payloads/%08d.json", rec.Seq)
		if rec.File != wantFile {
			t.Errorf("file[%d] = %q, want %q", i, rec.File, wantFile)
		}
		raw, err := os.ReadFile(filepath.Join(p.SessionDir(), filepath.FromSlash(rec.File)))
		if err != nil {
			t.Fatalf("missing %s: %v", rec.File, err)
		}
		if string(raw) != fmt.Sprintf(`{"i":%d}`, i) {
			t.Errorf("payload %d = %s", i, raw)
		}
	}
}

func TestIdenticalPayloadsAreNotDeduped(t *testing.T) {
	p := newTestProbe(t)
	body := []byte(`{"provider":{"name":"Dota 2"}}`)
	for i := 0; i < 3; i++ {
		resp := postRaw(t, p.Handler(), body, "application/json")
		resp.Body.Close()
	}
	idx := readIndex(t, p.SessionDir())
	if len(idx) != 3 {
		t.Fatalf("got %d records, identical payloads must all be kept", len(idx))
	}
}

func TestMalformedJSONIsCaptured(t *testing.T) {
	p := newTestProbe(t)
	body := []byte(`{"provider":`)
	resp := postRaw(t, p.Handler(), body, "application/json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (capture must succeed)", resp.StatusCode)
	}
	got, err := os.ReadFile(filepath.Join(p.SessionDir(), "payloads", "00000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("malformed body rewritten: %q", got)
	}
	idx := readIndex(t, p.SessionDir())
	if len(idx) != 1 || idx[0].ValidJSON {
		t.Fatalf("want valid_json=false, got %+v", idx)
	}
}

func TestCloseWritesSessionMeta(t *testing.T) {
	p := newTestProbe(t)
	bodies := [][]byte{
		[]byte(`{"a":1}`),
		[]byte(`{"b":2}`),
	}
	var wantBytes int64
	for _, b := range bodies {
		wantBytes += int64(len(b))
		resp := postRaw(t, p.Handler(), b, "application/json")
		resp.Body.Close()
	}

	before := time.Now().UTC()
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	after := time.Now().UTC()

	meta := readSession(t, p.SessionDir())
	if meta.FormatVersion != FormatVersion {
		t.Errorf("format_version = %d", meta.FormatVersion)
	}
	if meta.EndedAt == nil {
		t.Fatal("ended_at is null after Close")
	}
	ended, err := time.Parse(time.RFC3339Nano, *meta.EndedAt)
	if err != nil {
		t.Fatalf("ended_at parse: %v", err)
	}
	if ended.Before(before.Add(-time.Second)) || ended.After(after.Add(time.Second)) {
		t.Errorf("ended_at %s outside Close window", *meta.EndedAt)
	}
	if meta.Capture.PayloadCount != len(bodies) {
		t.Errorf("payload_count = %d, want %d", meta.Capture.PayloadCount, len(bodies))
	}
	if meta.Capture.TotalBytes != wantBytes {
		t.Errorf("total_bytes = %d, want %d", meta.Capture.TotalBytes, wantBytes)
	}
	if meta.Advisor.Version != "test" || meta.Advisor.Commit != "deadbeef" {
		t.Errorf("advisor = %+v", meta.Advisor)
	}
	if meta.Probe.Listen != "127.0.0.1:0" {
		t.Errorf("listen = %q", meta.Probe.Listen)
	}
}

func TestConcurrentRequestsUniqueSequence(t *testing.T) {
	p := newTestProbe(t)
	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	errc := make(chan int, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			body := []byte(fmt.Sprintf(`{"n":%d}`, i))
			resp := postRaw(t, p.Handler(), body, "application/json")
			code := resp.StatusCode
			resp.Body.Close()
			if code != http.StatusNoContent {
				errc <- code
			}
		}()
	}
	wg.Wait()
	close(errc)
	for code := range errc {
		t.Errorf("status %d", code)
	}

	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	idx := readIndex(t, p.SessionDir())
	if len(idx) != n {
		t.Fatalf("index records = %d, want %d", len(idx), n)
	}
	seqs := make([]int, 0, n)
	files := map[string]bool{}
	for _, rec := range idx {
		seqs = append(seqs, int(rec.Seq))
		if files[rec.File] {
			t.Errorf("file reused: %s", rec.File)
		}
		files[rec.File] = true
		raw, err := os.ReadFile(filepath.Join(p.SessionDir(), filepath.FromSlash(rec.File)))
		if err != nil {
			t.Errorf("missing %s: %v", rec.File, err)
		}
		if !json.Valid(raw) {
			t.Errorf("%s is not the JSON we posted: %s", rec.File, raw)
		}
	}
	sort.Ints(seqs)
	for i, seq := range seqs {
		if seq != i+1 {
			t.Fatalf("sequence gap or duplicate: sorted[%d]=%d", i, seq)
		}
	}
	meta := readSession(t, p.SessionDir())
	if meta.Capture.PayloadCount != n {
		t.Errorf("payload_count = %d, want %d", meta.Capture.PayloadCount, n)
	}
}

func TestStartGracefulShutdown(t *testing.T) {
	p, err := New(Options{
		Listen:  "127.0.0.1:0",
		Output:  t.TempDir(),
		Version: "test",
		Log:     func(string) {},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- p.Start(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for p.Addr() == "" {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("server never bound")
		}
		time.Sleep(5 * time.Millisecond)
	}

	body := []byte(`{"provider":{"name":"Dota 2"},"test":{"value":123}}`)
	resp, err := http.Post("http://"+p.Addr()+"/", "application/json", bytes.NewReader(body))
	if err != nil {
		cancel()
		t.Fatalf("post: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		cancel()
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	meta := readSession(t, p.SessionDir())
	if meta.EndedAt == nil {
		t.Fatal("ended_at is null")
	}
	if meta.Capture.PayloadCount != 1 {
		t.Errorf("payload_count = %d, want 1", meta.Capture.PayloadCount)
	}
	if meta.Capture.TotalBytes != int64(len(body)) {
		t.Errorf("total_bytes = %d, want %d", meta.Capture.TotalBytes, len(body))
	}
}

func TestNewFailsWhenOutputUnusable(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{Listen: "127.0.0.1:0", Output: blocked, Log: func(string) {}})
	if err == nil {
		t.Fatal("New succeeded with a file as output dir")
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	p := newTestProbe(t)
	body := bytes.Repeat([]byte("x"), MaxBodyBytes+1)
	resp := postRaw(t, p.Handler(), body, "application/octet-stream")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
	idx := readIndex(t, p.SessionDir())
	if len(idx) != 0 {
		t.Fatalf("oversized body was stored: %+v", idx)
	}
}
