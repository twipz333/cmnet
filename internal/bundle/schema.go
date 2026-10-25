// Package bundle is the self-contained, versioned model artifact that is trained
// once and shared with friends. serve loads a bundle, verifies its version, and
// refuses to run on a mismatch so there is never a silent divergence (§6).
package bundle

// Schema version constants. The running code asserts a loaded bundle matches
// these; bump on any incompatible change to weights layout or feature encoding.
const (
	// ModelSchemaVersion is the on-disk weights/structure contract.
	ModelSchemaVersion = 1
	// CodeVersion identifies the inference code that produced/consumes the bundle.
	CodeVersion = "0.1-mvp"
)
