package discordbot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpusPacketsFromNTTSVorbisCache(t *testing.T) {
	matches, _ := filepath.Glob("../../tts-cache/*.ogg")
	if len(matches) == 0 {
		t.Skip("no tts-cache/*.ogg")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	pkts, err := OpusPacketsFromOgg(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkts) < 5 {
		t.Fatalf("got %d packets, want many 20ms frames", len(pkts))
	}
	t.Logf("%s -> %d opus packets, first=%d bytes", filepath.Base(matches[0]), len(pkts), len(pkts[0]))
}

func TestSniffVorbis(t *testing.T) {
	matches, _ := filepath.Glob("../../tts-cache/*.ogg")
	if len(matches) == 0 {
		t.Skip("no cache")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	kind, err := sniffOggCodec(data)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "vorbis" {
		t.Fatalf("kind=%q", kind)
	}
}
