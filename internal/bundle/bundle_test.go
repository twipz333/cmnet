package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmnet/internal/model"
)

func sampleBundle() *Bundle {
	return &Bundle{
		PatchTag:        "7.36",
		TrainWindowDays: 31,
		MMRTier:         "battlecup",
		HeroIDs:         []int{1, 2, 8, 14},
		PickPredictor:   &model.SoftmaxReg{Weights: [][]float64{{0.1, 0.2}, {0.3, 0.4}}, Bias: []float64{0, 0}},
		BanPredictor:    &model.SoftmaxReg{Weights: [][]float64{{0.2, 0.1}, {0.4, 0.3}}, Bias: []float64{0, 0}},
		WinProb:         &model.BinaryLogReg{Weights: []float64{0.5, -0.5}, Bias: 0.1},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	b := sampleBundle()
	if err := b.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if b.BundleVersion == "" {
		t.Fatal("version not set after save")
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.BundleVersion != b.BundleVersion {
		t.Errorf("version changed: %q vs %q", loaded.BundleVersion, b.BundleVersion)
	}
	if loaded.HeroMap().Len() != 4 {
		t.Errorf("hero map len = %d, want 4", loaded.HeroMap().Len())
	}
}

func TestLoadRejectsTamperedHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := sampleBundle().Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	// Flip the patch tag without recomputing the hash -> verification must fail.
	tampered := strings.Replace(string(data), `"7.36"`, `"7.99"`, 1)
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("expected hash mismatch error on tampered bundle")
	}
}

func TestLoadRejectsIncompatibleSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	b := sampleBundle()
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	// Re-marshal with a bumped model schema and a matching hash to isolate the
	// schema check from the hash check.
	b.ModelSchemaVersion = 999
	v, _ := computeVersion(b)
	b.BundleVersion = v
	data, _ := json.MarshalIndent(b, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("expected incompatible schema error")
	}
}

// Versioning must be deterministic across runs.
func TestComputeVersionStable(t *testing.T) {
	a, _ := computeVersion(sampleBundle())
	b, _ := computeVersion(sampleBundle())
	if a != b {
		t.Errorf("version not stable: %q vs %q", a, b)
	}
}
