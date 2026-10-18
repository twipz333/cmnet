package discordbot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderTemplate(t *testing.T) {
	got := RenderTemplate("{{label}} через {{nextIn}}", AlertPhrase{Label: "Power", NextIn: 12})
	if got != "Power через 12" {
		t.Fatalf("got %q", got)
	}
}

func TestTemplateFor(t *testing.T) {
	m := map[string]string{"power": "P {{nextIn}}", "default": "D"}
	if TemplateFor(m, "power") != "P {{nextIn}}" {
		t.Fatal("kind")
	}
	if TemplateFor(m, "lotus") != "D" {
		t.Fatal("default")
	}
}

func TestAudioCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := NewAudioCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Put("spk", "hello", "ogg", "", []byte("fakexxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := c.Get("spk", "hello", "ogg", "")
	if !ok || got != p {
		t.Fatalf("get %v %q", ok, got)
	}
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("expected removed, err=%v", err)
	}
	_ = filepath.Base(p)
}

func TestCacheKeyChangesWithSpeaker(t *testing.T) {
	dir := t.TempDir()
	c, err := NewAudioCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	p1 := c.Path("a", "hi", "ogg", "")
	p2 := c.Path("b", "hi", "ogg", "")
	if p1 == p2 {
		t.Fatal("speaker must change cache key")
	}
}