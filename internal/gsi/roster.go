package gsi

import (
	"cmnet/internal/draft"
	"cmnet/internal/gsi/track"
)

// SnapshotRoster adapts the live GSI server draft snapshot into a RosterProvider.
// It does not parse drafts itself; ParseDraft / Server.merge remain the source.
type SnapshotRoster struct {
	Last  func() *draft.Snapshot
	Names track.NameLookup
}

func (s SnapshotRoster) Roster() (track.Roster, bool) {
	if s.Last == nil {
		return track.Roster{}, false
	}
	return track.DraftRoster{Snap: s.Last(), Names: s.Names}.Roster()
}
