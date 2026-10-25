package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"cmnet/internal/features"
)

// computeVersion returns the sha256 (hex) of the bundle's canonical JSON with the
// BundleVersion field blanked, so the hash covers content only and is reproducible.
func computeVersion(b *Bundle) (string, error) {
	clone := *b
	clone.BundleVersion = ""
	data, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// verify checks the stored content hash and asserts the bundle was produced by a
// compatible schema/code version.
func (b *Bundle) verify() error {
	want, err := computeVersion(b)
	if err != nil {
		return err
	}
	if b.BundleVersion != want {
		return fmt.Errorf("bundle hash mismatch: file says %q, content hashes to %q (corrupt or tampered)",
			b.BundleVersion, want)
	}
	if b.ModelSchemaVersion != ModelSchemaVersion {
		return fmt.Errorf("incompatible model schema: bundle v%d, code v%d", b.ModelSchemaVersion, ModelSchemaVersion)
	}
	if b.FeatureSchemaVersion != features.FeatureSchemaVersion {
		return fmt.Errorf("incompatible feature schema: bundle v%d, code v%d", b.FeatureSchemaVersion, features.FeatureSchemaVersion)
	}
	if b.CodeVersion != CodeVersion {
		return fmt.Errorf("incompatible code version: bundle %q, code %q", b.CodeVersion, CodeVersion)
	}
	return nil
}
