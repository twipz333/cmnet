package scout

import (
	"context"

	"cmnet/internal/sources/opendota"
)

// Source abstracts the per-player data backend so scout assembly is testable with
// a fake. *opendota.Client is adapted to it via OpenDotaSource.
type Source interface {
	// PlayerHeroes returns a player's per-hero games/wins within the window.
	PlayerHeroes(ctx context.Context, accountID int64, windowDays int) ([]PlayerHero, error)
	// RecentMatches returns a player's recent match references.
	RecentMatches(ctx context.Context, accountID int64) ([]MatchRef, error)
	// MatchDraft returns the full draft of a match.
	MatchDraft(ctx context.Context, matchID int64) (*MatchDraft, error)
}

// PlayerHero is per-hero performance for affinity (layer 2).
type PlayerHero struct {
	HeroID int
	Games  int
	Wins   int
}

// MatchRef is a recent match row used for event windows and smurf heuristics.
type MatchRef struct {
	MatchID   int64
	StartTime int64
	HeroID    int
	Won       bool // player's team won
	PartySize int
	LobbyType int
}

// DraftEntry is one pick/ban from a match draft.
type DraftEntry struct {
	HeroID int
	IsPick bool
	Team   int // 0 radiant / 1 dire
}

// Participant identifies a player and their side in a match.
type Participant struct {
	AccountID int64
	Radiant   bool
}

// MatchDraft is the full draft of one match (layer 3).
type MatchDraft struct {
	MatchID    int64
	RadiantWin bool
	Entries    []DraftEntry
	Players    []Participant
}

// OpenDotaSource adapts *opendota.Client to Source.
type OpenDotaSource struct{ C *opendota.Client }

// NewOpenDotaSource builds a Source backed by OpenDota.
func NewOpenDotaSource() *OpenDotaSource { return &OpenDotaSource{C: opendota.New()} }

func (s *OpenDotaSource) PlayerHeroes(ctx context.Context, accountID int64, windowDays int) ([]PlayerHero, error) {
	rows, err := s.C.PlayerHeroes(ctx, accountID, windowDays)
	if err != nil {
		return nil, err
	}
	out := make([]PlayerHero, len(rows))
	for i, r := range rows {
		out[i] = PlayerHero{HeroID: r.HeroID, Games: r.Games, Wins: r.Win}
	}
	return out, nil
}

func (s *OpenDotaSource) RecentMatches(ctx context.Context, accountID int64) ([]MatchRef, error) {
	rows, err := s.C.RecentMatches(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]MatchRef, len(rows))
	for i, r := range rows {
		radiant := r.PlayerSlot < 128
		out[i] = MatchRef{
			MatchID:   r.MatchID,
			StartTime: r.StartTime,
			HeroID:    r.HeroID,
			Won:       radiant == r.RadiantWin,
			PartySize: r.PartySize,
			LobbyType: r.LobbyType,
		}
	}
	return out, nil
}

func (s *OpenDotaSource) MatchDraft(ctx context.Context, matchID int64) (*MatchDraft, error) {
	m, err := s.C.Match(ctx, matchID)
	if err != nil {
		return nil, err
	}
	md := &MatchDraft{MatchID: m.MatchID, RadiantWin: m.RadiantWin}
	for _, pb := range m.PickBans {
		md.Entries = append(md.Entries, DraftEntry{HeroID: pb.HeroID, IsPick: pb.IsPick, Team: pb.Team})
	}
	for _, p := range m.Players {
		md.Players = append(md.Players, Participant{AccountID: p.AccountID, Radiant: p.Radiant})
	}
	return md, nil
}
