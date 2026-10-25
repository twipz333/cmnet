package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// FormatVersion is the session.json schema. Bump when the on-disk layout
	// of a capture session changes in a way readers must distinguish.
	FormatVersion = 1

	payloadDirName  = "payloads"
	indexFileName   = "index.ndjson"
	sessionFileName = "session.json"
	cfgCopyName     = "gsi-config.cfg"
)

// IndexRecord is one line of index.ndjson. It describes a captured HTTP body
// without interpreting any Dota field.
type IndexRecord struct {
	Seq            uint64 `json:"seq"`
	ReceivedAt     string `json:"received_at"`
	ReceivedUnixMs int64  `json:"received_unix_ms"`
	File           string `json:"file"`
	Bytes          int    `json:"bytes"`
	ValidJSON      bool   `json:"valid_json"`
	SHA256         string `json:"sha256"`
	ContentType    string `json:"content_type,omitempty"`
	RemoteAddr     string `json:"remote_addr,omitempty"`
}

// SessionMeta is the session.json document written at start and rewritten on
// graceful shutdown.
type SessionMeta struct {
	FormatVersion int          `json:"format_version"`
	StartedAt     string       `json:"started_at"`
	EndedAt       *string      `json:"ended_at"`
	Advisor       AdvisorInfo  `json:"advisor"`
	Probe         ProbeInfo    `json:"probe"`
	Capture       CaptureStats `json:"capture"`
}

// AdvisorInfo is the build identity of the binary that produced the capture.
type AdvisorInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// ProbeInfo records how this process was listening.
type ProbeInfo struct {
	Listen string `json:"listen"`
}

// CaptureStats is the running (and final) size of the capture.
type CaptureStats struct {
	PayloadCount int   `json:"payload_count"`
	TotalBytes   int64 `json:"total_bytes"`
	Errors       int   `json:"errors,omitempty"`
}

// capture serializes payload numbering, index.ndjson, and counters. Dota can
// POST faster than a match needs, so every mutation goes through this mutex
// rather than a pipeline.
type capture struct {
	mu      sync.Mutex
	dir     string
	index   *os.File
	seq     uint64
	stats   CaptureStats
	started time.Time
	listen  string
	advisor AdvisorInfo
}

func (c *capture) snapshot() (CaptureStats, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats, c.seq
}

func (c *capture) noteError() {
	c.mu.Lock()
	c.stats.Errors++
	c.mu.Unlock()
}

func (c *capture) write(receivedAt time.Time, body []byte, contentType, remoteAddr string) (IndexRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.seq++
	seq := c.seq
	rel := fmt.Sprintf("%s/%08d.json", payloadDirName, seq)
	path := filepath.Join(c.dir, filepath.FromSlash(rel))

	if err := os.WriteFile(path, body, 0o644); err != nil {
		c.stats.Errors++
		return IndexRecord{}, fmt.Errorf("write %s: %w", rel, err)
	}

	rec := IndexRecord{
		Seq:            seq,
		ReceivedAt:     receivedAt.UTC().Format(time.RFC3339Nano),
		ReceivedUnixMs: receivedAt.UTC().UnixMilli(),
		File:           rel,
		Bytes:          len(body),
		ValidJSON:      json.Valid(body),
		SHA256:         sha256Hex(body),
		ContentType:    contentType,
		RemoteAddr:     remoteAddr,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		c.stats.Errors++
		return IndexRecord{}, fmt.Errorf("marshal index: %w", err)
	}
	if _, err := c.index.Write(append(line, '\n')); err != nil {
		c.stats.Errors++
		return IndexRecord{}, fmt.Errorf("write index: %w", err)
	}
	// Flush to disk so a crash (or an observer reading mid-session) sees the
	// line. Capture is a dataset, not a hot path.
	if err := c.index.Sync(); err != nil {
		c.stats.Errors++
		return IndexRecord{}, fmt.Errorf("sync index: %w", err)
	}
	c.stats.PayloadCount++
	c.stats.TotalBytes += int64(len(body))
	return rec, nil
}

func (c *capture) closeIndex() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.index == nil {
		return nil
	}
	err := c.index.Sync()
	if closeErr := c.index.Close(); err == nil {
		err = closeErr
	}
	c.index = nil
	return err
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
