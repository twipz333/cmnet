package share

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadPackRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "team.cmnetpack")
	p := Payload{
		Meta: Meta{
			BundleVersion: "bundle-abc",
			PatchTag:      "7.39",
			HostVersion:   "test",
			SharedAt:      time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
			HasSnapshot:   true,
			Scouted:       3,
		},
		Files: []File{
			{Name: "bundle.json", Data: []byte(`{"bundle_version":"bundle-abc"}`)},
			{Name: "snapshot.json", Data: []byte(`{"players":[]}`)},
		},
	}
	if err := WritePackFile(path, p); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPackFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.BundleVersion != "bundle-abc" || !got.Meta.HasSnapshot || got.Meta.Scouted != 3 {
		t.Fatalf("meta lost: %+v", got.Meta)
	}
	if string(got.File("bundle.json")) != `{"bundle_version":"bundle-abc"}` {
		t.Fatalf("bundle lost: %q", got.File("bundle.json"))
	}
	if string(got.File("snapshot.json")) != `{"players":[]}` {
		t.Fatalf("snapshot lost: %q", got.File("snapshot.json"))
	}
	raw, _ := os.ReadFile(path)
	if len(raw) < len(packMagic) || string(raw[:len(packMagic)]) != string(packMagic) {
		t.Fatalf("missing pack magic")
	}
}

func TestReadPackRejectsSealedOrGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.cmnetpack")
	if err := os.WriteFile(path, []byte("not-a-pack"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPackFile(path); err == nil {
		t.Fatal("garbage accepted")
	}
}
