// Package observe turns one raw Dota 2 GSI payload into a normalized
// ObservationBatch. It is stateless: history, last-seen, and roster tracking
// belong to package track.
package observe
