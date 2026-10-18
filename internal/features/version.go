package features

// FeatureSchemaVersion identifies the feature-encoding contract. Bump it whenever
// the layout of any encoded vector changes; the bundle stores this so serve can
// refuse incompatible bundles (§6).
//
// v1: antisymmetric hero main effects only (length N).
// v2: main effects + signed (hero, position) block (length N + 5N). Positions are
// latent mid-draft — train writes hard labels from match_players; serve writes
// expected occupancy under the assignment posterior (§6.3 path A).
const FeatureSchemaVersion = 2
