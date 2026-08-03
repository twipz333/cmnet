package features

// FeatureSchemaVersion identifies the feature-encoding contract. Bump it whenever
// the layout of any encoded vector changes; the bundle stores this so serve can
// refuse incompatible bundles (§6).
const FeatureSchemaVersion = 1
