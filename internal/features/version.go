package features

// FeatureSchemaVersion identifies the feature-encoding contract. Bump it whenever
// the layout of any encoded vector changes; the bundle stores this so serve can
// refuse incompatible bundles (§6).
//
// v1: antisymmetric hero main effects only (length N).
// v2: main effects + signed (hero, position) block (length N + 5N). Positions are
// latent mid-draft — train writes hard labels from match_players; serve writes
// expected occupancy under the assignment posterior (§6.3 path A).
// v3: predictor phase features follow the 7.34+ Captains Mode schedule (phases
// 7/2/3/6/4/2, within-phase one-hot widened to 7). Same layout idea as v2, but a
// v2 predictor's phase weights were learned against the wrong phase boundaries
// and its input is one slot shorter, so v2 bundles must be retrained.
const FeatureSchemaVersion = 3
